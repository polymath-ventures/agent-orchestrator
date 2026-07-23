// Package codexrollout owns the pure parsing of a Codex rollout's
// token_count.rate_limits event into quota snapshots, plus a cwd-independent
// "newest rollout carrying rate_limits" read for daemon-side quota probing.
//
// It is a leaf package: it depends only on the domain types and the standard
// library, so both the CLI stop-hook path (internal/cli) and the daemon codex
// adapter (internal/adapters/agent/codex) can share one copy of the parser
// without either importing the other. Keeping the parsing here means the two
// consumers can never drift apart on how a Codex rate-limit window is read.
package codexrollout

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
)

const (
	// rolloutScanLimit bounds how many rollout files (newest-first by mtime) the
	// cwd-independent locator inspects before giving up.
	rolloutScanLimit = 512
	// recentDayDirs is how many NON-EMPTY date-partitioned day directories
	// (codex writes rollouts under sessions/YYYY/MM/DD/) the locator gathers
	// candidates from. A rollout is filed under its session's START date but is
	// appended to for the whole session, so a long-running or overnight session's
	// actively-written rollout can sit under an older date than today — this
	// window must be generous enough to still cover it. Two weeks dwarfs any
	// realistic worker session while keeping the scan bounded.
	recentDayDirs = 14
	// maxDayDirsExamined hard-caps how many day directories are looked at even
	// when many are empty, so a long-lived CODEX_HOME with years of history can
	// never turn a probe into an unbounded full-tree walk.
	maxDayDirsExamined = 90
	// lineCap bounds a single JSONL line buffered while scanning a rollout. Lines
	// above it are skipped rather than aborting the scan.
	lineCap = 32 << 20
)

// RateWindow is one Codex rate-limit window (primary or secondary) as reported
// in a rollout token_count event. Nil pointers mean the field was absent.
type RateWindow struct {
	UsedPercent   *float64 `json:"used_percent"`
	WindowMinutes *float64 `json:"window_minutes"`
	ResetsAt      *int64   `json:"resets_at"`
}

// RateLimits mirrors the token_count.rate_limits payload Codex writes to its
// rollout JSONL. It carries the primary and secondary usage windows plus the
// plan metadata used to annotate a snapshot's basis.
type RateLimits struct {
	LimitID   string      `json:"limit_id"`
	PlanType  string      `json:"plan_type"`
	Primary   *RateWindow `json:"primary"`
	Secondary *RateWindow `json:"secondary"`
}

// Snapshots converts the primary and secondary windows into exact quota
// snapshots observed at observedAt. It is best-effort: a window with a missing,
// NaN/Inf, out-of-range used_percent, a non-positive window_minutes, or a
// non-positive resets_at is silently dropped rather than reported as a fact.
func (r RateLimits) Snapshots(observedAt time.Time) []domain.QuotaSnapshot {
	var out []domain.QuotaSnapshot
	for _, entry := range []struct {
		name string
		win  *RateWindow
	}{
		{name: "primary", win: r.Primary},
		{name: "secondary", win: r.Secondary},
	} {
		if snap, ok := r.snapshot(entry.name, entry.win, observedAt); ok {
			out = append(out, snap)
		}
	}
	return out
}

func (r RateLimits) snapshot(name string, win *RateWindow, observedAt time.Time) (domain.QuotaSnapshot, bool) {
	if win == nil || win.UsedPercent == nil || win.WindowMinutes == nil || win.ResetsAt == nil {
		return domain.QuotaSnapshot{}, false
	}
	used := *win.UsedPercent
	minutes := *win.WindowMinutes
	if math.IsNaN(used) || math.IsInf(used, 0) || used < 0 || used > 100 {
		return domain.QuotaSnapshot{}, false
	}
	if math.IsNaN(minutes) || math.IsInf(minutes, 0) || minutes <= 0 {
		return domain.QuotaSnapshot{}, false
	}
	if *win.ResetsAt <= 0 {
		return domain.QuotaSnapshot{}, false
	}
	reset := time.Unix(*win.ResetsAt, 0).UTC()
	if reset.IsZero() {
		return domain.QuotaSnapshot{}, false
	}
	limit := 100.0
	remaining := limit - used
	source := "codex rollout token_count.rate_limits"
	basis := "Parsed " + name + " Codex rate-limit window from matching rollout JSONL"
	if r.LimitID != "" {
		basis += "; limit_id=" + sanitizeForLog(r.LimitID)
	}
	if r.PlanType != "" {
		basis += "; plan_type=" + sanitizeForLog(r.PlanType)
	}
	return domain.QuotaSnapshot{
		Harness:       domain.HarnessCodex,
		AccountID:     "unknown",
		WindowName:    name,
		WindowStart:   reset.Add(-time.Duration(minutes * float64(time.Minute))),
		WindowEnd:     reset,
		Used:          floatPtr(used),
		Remaining:     floatPtr(remaining),
		Limit:         floatPtr(limit),
		SignalQuality: domain.QuotaSignalExact,
		Source:        source,
		Basis:         basis,
		ObservedAt:    observedAt,
	}, true
}

// NewestRateLimits reads the newest Codex rollout under codexHome that carries a
// token_count.rate_limits event and returns its parsed quota snapshots. Unlike
// the CLI stop-hook locator it is cwd-independent: it does not match a session's
// working directory, it simply finds the machine's most recent usage report.
//
// found reports whether any rollout carrying a rate_limits event was located at
// all. found can be true while snaps is empty: the newest rollout carried a
// rate_limits event whose windows were all implausible (a real source that just
// has nothing trustworthy to report right now). snaps is the parsed windows of
// the newest event that produced any; found is false only when no rollout under
// codexHome carries a rate_limits event (a fresh install, or none written yet).
//
// The scan is bounded the same way the stop-hook locator is — recent non-empty
// day directories, capped, newest-first by mtime — so a long-lived CODEX_HOME
// never becomes a full-tree walk.
func NewestRateLimits(codexHome string, observedAt time.Time) (snaps []domain.QuotaSnapshot, found bool) {
	for _, path := range newestRolloutPaths(codexHome) {
		windows, sawRateLimits := rolloutRateLimits(path, observedAt)
		if sawRateLimits {
			return windows, true
		}
	}
	return nil, false
}

// rolloutRateLimits scans one rollout and returns the snapshots of the newest
// token_count event that produced any, plus whether the rollout carried a
// rate_limits payload at all (even one whose windows were all rejected).
func rolloutRateLimits(path string, observedAt time.Time) (snaps []domain.QuotaSnapshot, sawRateLimits bool) {
	f, ok := openRegularFile(path)
	if !ok {
		return nil, false
	}
	defer func() { _ = f.Close() }()

	scanJSONLines(f, func(line []byte) {
		var rec struct {
			Type    string `json:"type"`
			Payload struct {
				Type       string      `json:"type"`
				RateLimits *RateLimits `json:"rate_limits"`
			} `json:"payload"`
		}
		if err := json.Unmarshal(line, &rec); err != nil {
			return
		}
		if rec.Type != "event_msg" || rec.Payload.Type != "token_count" || rec.Payload.RateLimits == nil {
			return
		}
		sawRateLimits = true
		if windows := rec.Payload.RateLimits.Snapshots(observedAt.UTC()); len(windows) > 0 {
			snaps = windows
		}
	})
	return snaps, sawRateLimits
}

// newestRolloutPaths returns rollout file paths under codexHome/sessions sorted
// newest-first by mtime, bounded to the recent non-empty day directories and to
// rolloutScanLimit files. A run of newer EMPTY day dirs does not count toward
// the window, so it cannot hide an older, actively-written session's rollout.
func newestRolloutPaths(codexHome string) []string {
	type candidate struct {
		path  string
		mtime time.Time
	}
	var candidates []candidate
	nonEmptyDays := 0
	for _, dir := range recentDayDirsUnder(codexHome) {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		dayHadRollout := false
		for _, d := range entries {
			if d.IsDir() {
				continue
			}
			name := d.Name()
			if !strings.HasPrefix(name, "rollout-") || !strings.HasSuffix(name, ".jsonl") {
				continue
			}
			info, err := d.Info()
			if err != nil {
				continue
			}
			candidates = append(candidates, candidate{path: filepath.Join(dir, name), mtime: info.ModTime()})
			dayHadRollout = true
		}
		if dayHadRollout {
			nonEmptyDays++
			if nonEmptyDays >= recentDayDirs {
				break
			}
		}
	}
	sort.Slice(candidates, func(i, j int) bool { return candidates[i].mtime.After(candidates[j].mtime) })

	limit := len(candidates)
	if limit > rolloutScanLimit {
		limit = rolloutScanLimit
	}
	paths := make([]string, 0, limit)
	for i := 0; i < limit; i++ {
		paths = append(paths, candidates[i].path)
	}
	return paths
}

// recentDayDirsUnder returns the newest sessions/YYYY/MM/DD directories under
// codexHome, newest-first, without walking the whole tree. It descends year →
// month → day, each level sorted descending, stopping once it has collected
// maxDayDirsExamined directories. The caller decides how many NON-EMPTY days to
// actually use; this hard cap only bounds work when many recent day dirs are
// empty.
func recentDayDirsUnder(codexHome string) []string {
	root := filepath.Join(codexHome, "sessions")
	var days []string
	for _, year := range descendingDirNames(root) {
		yearDir := filepath.Join(root, year)
		for _, month := range descendingDirNames(yearDir) {
			monthDir := filepath.Join(yearDir, month)
			for _, day := range descendingDirNames(monthDir) {
				days = append(days, filepath.Join(monthDir, day))
				if len(days) >= maxDayDirsExamined {
					return days
				}
			}
		}
	}
	return days
}

// descendingDirNames lists the sub-directory names of dir sorted descending, so
// the most recent date partition comes first. Non-directory entries and unread
// dirs yield an empty slice.
func descendingDirNames(dir string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	names := make([]string, 0, len(entries))
	for _, d := range entries {
		if d.IsDir() {
			names = append(names, d.Name())
		}
	}
	sort.Sort(sort.Reverse(sort.StringSlice(names)))
	return names
}

// openRegularFile opens path only when it resolves to a regular file. It rejects
// FIFOs, devices, and other special files whose open/read could block the
// caller. os.Stat follows symlinks, so a symlink to a regular file is allowed
// while a symlink to a FIFO is rejected.
func openRegularFile(path string) (*os.File, bool) {
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() {
		return nil, false
	}
	f, err := os.Open(path) //nolint:gosec // path is a rollout discovered under CODEX_HOME
	if err != nil {
		return nil, false
	}
	return f, true
}

// scanJSONLines calls fn for each newline-delimited record in r, tolerating
// arbitrarily long lines up to lineCap (longer lines are skipped). It never
// returns an error: a truncated tail simply yields fewer records.
func scanJSONLines(r io.Reader, fn func(line []byte)) {
	br := bufio.NewReaderSize(r, 1<<20)
	for {
		line, err := readLine(br)
		if len(line) > 0 {
			fn(line)
		}
		if err != nil {
			return
		}
	}
}

// readLine returns the next line (without the trailing newline), or the partial
// final line with io.EOF. A single line longer than lineCap is consumed and
// discarded (returns empty) so the scan can continue past it.
func readLine(br *bufio.Reader) ([]byte, error) {
	var buf []byte
	tooLong := false
	for {
		chunk, err := br.ReadSlice('\n')
		if !tooLong {
			buf = append(buf, chunk...)
			if len(buf) > lineCap {
				buf = nil
				tooLong = true
			}
		}
		if errors.Is(err, bufio.ErrBufferFull) {
			continue
		}
		if err != nil {
			return trimNewline(buf), err
		}
		return trimNewline(buf), nil
	}
}

func trimNewline(b []byte) []byte {
	b = bytes.TrimSuffix(b, []byte{'\n'})
	b = bytes.TrimSuffix(b, []byte{'\r'})
	return b
}

// sanitizeForLog strips control characters (notably newlines) from a value
// before it goes into a snapshot basis string, so an odd limit_id/plan_type
// cannot inject a forged line downstream.
func sanitizeForLog(s string) string {
	return strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, s)
}

func floatPtr(f float64) *float64 { return &f }
