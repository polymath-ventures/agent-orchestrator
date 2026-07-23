package cli

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/harness/codexrollout"
)

// Real per-turn token usage is NOT present in any harness stop-hook payload —
// the inline usageMeta above never fires in production because no configured
// harness puts tokens on the wire. The real numbers live in the harness's
// transcript/rollout file on disk, which this extractor reads.
//
// Both supported shapes report only CUMULATIVE session usage, so to turn a
// stop-hook (which fires ~once per turn) into a real per-turn delta we keep a
// tiny per-session cursor of the last-seen cumulative under
// $AO_DATA_DIR/usage-cursors/<session>.json and emit current-stored.
//
// The cursor advances only AFTER the activity POST that carries the delta
// succeeds (the caller invokes the returned commit): a failed delivery leaves
// the cursor put, so the next turn recomputes and re-emits the still-undelivered
// usage — usage is deferred, never lost and never double-counted.
//
// Everything here is best-effort: any parse/read/IO error yields no usage so the
// hook forwards nothing rather than breaking the agent (the runHook contract).

const (
	// usageCursorDir is the sub-directory of AO_DATA_DIR that holds per-session
	// cumulative-usage cursors.
	usageCursorDir = "usage-cursors"
	// codexRolloutScanLimit bounds how many rollout files (newest-first) the
	// codex locator inspects before giving up.
	codexRolloutScanLimit = 512
	// codexRecentDayDirs is how many NON-EMPTY date-partitioned day directories
	// (codex writes rollouts under sessions/YYYY/MM/DD/) the locator gathers
	// candidates from. A rollout is filed under its session's START date but
	// appended to for the whole session, so a long-running or overnight session's
	// actively-written rollout can sit under an older date than today — this
	// window must be generous enough to still cover it. Two weeks dwarfs any
	// realistic AO worker session while keeping the scan bounded.
	codexRecentDayDirs = 14
	// codexMaxDayDirsExamined hard-caps how many day directories are looked at
	// even when many are empty, so a long-lived CODEX_HOME with years of history
	// can never turn a stop hook into an unbounded full-tree walk.
	codexMaxDayDirsExamined = 90
	// usageLineCap bounds a single JSONL line we will buffer while scanning a
	// transcript/rollout. Lines above it are skipped rather than aborting the
	// scan.
	usageLineCap = 32 << 20
)

// usageCumulative is the cumulative token usage read from a harness file and
// the shape persisted in the per-session cursor.
type usageCumulative struct {
	Input  float64 `json:"input_tokens"`
	Output float64 `json:"output_tokens"`
	Total  float64 `json:"total_tokens"`
}

// usageExtractor reads real cumulative token usage from a harness's on-disk
// transcript/rollout and converts it to a per-turn delta against a persisted
// cursor. Its inputs are injected so it is unit-testable with a temp AO_DATA_DIR
// and CODEX_HOME.
type usageExtractor struct {
	dataDir   string           // AO_DATA_DIR; when empty the extractor emits nothing
	codexHome string           // CODEX_HOME; empty resolves to ~/.codex
	cwd       string           // hook process working directory (codex rollout match key)
	sessionID string           // AO_SESSION_ID; names the cursor file
	logf      func(msg string) // best-effort ambiguity sink; may be nil
}

// stopUsageDelta returns the per-turn usage delta for a stop event together with
// a commit closure the caller must invoke ONLY after the delta is successfully
// delivered — commit advances the persisted cursor. It returns (nil, nil) when
// there is nothing to report (no readable usage, zero delta, unset data dir, a
// cumulative reset, or any error). cost_usd is never populated: neither harness
// reports a dollar cost, and fabricating one is forbidden by #375.
func (e *usageExtractor) stopUsageDelta(agent string, payload []byte) (*usageAPIRequest, func()) {
	cumulative, ok := e.currentCumulative(agent, payload)
	if !ok {
		return nil, nil
	}
	// Without a data dir there is nowhere to persist the cursor, so a delta would
	// double-count on the next turn. Emit nothing rather than mis-report.
	if strings.TrimSpace(e.dataDir) == "" || !sessionIDPattern.MatchString(e.sessionID) {
		return nil, nil
	}
	stored := e.loadCursor()
	// A cumulative that dropped below the stored value in ANY bucket means the
	// underlying file identity changed (a reused worktree now hosts a fresh
	// session/rollout, or a transcript was replaced). Emitting a per-field delta
	// here would be a meaningless mix of the old and new sessions, so reset the
	// cursor to the new baseline immediately (no delivery to gate) and skip this
	// turn.
	if cumulative.Total < stored.Total || cumulative.Input < stored.Input || cumulative.Output < stored.Output {
		_ = e.storeCursor(cumulative)
		return nil, nil
	}
	deltaIn := nonNegDelta(cumulative.Input, stored.Input)
	deltaOut := nonNegDelta(cumulative.Output, stored.Output)
	deltaTotal := nonNegDelta(cumulative.Total, stored.Total)
	if deltaIn == 0 && deltaOut == 0 && deltaTotal == 0 {
		return nil, nil
	}
	// commit advances the cursor after the delta is delivered. A persist failure
	// here is rare (the data dir was writable a moment ago) and self-limited to at
	// most one duplicated delta next turn; it is logged for observability. True
	// end-to-end idempotency would require a server-side event key, which lives in
	// the daemon (a sensitive path) and is out of scope for this fix.
	commit := func() {
		if err := e.storeCursor(cumulative); err != nil {
			e.log("usage: cursor persist failed after delivery; a duplicate delta may follow: " + sanitizeForLog(err.Error()))
		}
	}
	return &usageAPIRequest{
		InputTokens:  floatPtr(deltaIn),
		OutputTokens: floatPtr(deltaOut),
		TotalTokens:  floatPtr(deltaTotal),
	}, commit
}

func (e *usageExtractor) currentCumulative(agent string, payload []byte) (usageCumulative, bool) {
	switch agent {
	case "claude-code":
		return e.claudeCumulative(payload)
	case "codex":
		return e.codexCumulative()
	default:
		return usageCumulative{}, false
	}
}

// claudeCumulative sums usage across every assistant line of the transcript
// named by the stop payload's transcript_path. Billing input is the sum of the
// three input buckets; output is output_tokens.
func (e *usageExtractor) claudeCumulative(payload []byte) (usageCumulative, bool) {
	var p struct {
		TranscriptPath string `json:"transcript_path"`
	}
	if err := json.Unmarshal(payload, &p); err != nil {
		return usageCumulative{}, false
	}
	path := strings.TrimSpace(p.TranscriptPath)
	if path == "" {
		return usageCumulative{}, false
	}
	f, ok := openRegularFile(path)
	if !ok {
		return usageCumulative{}, false
	}
	defer func() { _ = f.Close() }()

	var cum usageCumulative
	found := false
	scanJSONLines(f, func(line []byte) {
		var rec struct {
			Type    string `json:"type"`
			Message struct {
				Usage *struct {
					InputTokens              float64 `json:"input_tokens"`
					CacheCreationInputTokens float64 `json:"cache_creation_input_tokens"`
					CacheReadInputTokens     float64 `json:"cache_read_input_tokens"`
					OutputTokens             float64 `json:"output_tokens"`
				} `json:"usage"`
			} `json:"message"`
		}
		if err := json.Unmarshal(line, &rec); err != nil {
			return
		}
		if rec.Type != "assistant" || rec.Message.Usage == nil {
			return
		}
		u := rec.Message.Usage
		cum.Input += u.InputTokens + u.CacheCreationInputTokens + u.CacheReadInputTokens
		cum.Output += u.OutputTokens
		found = true
	})
	// A readable transcript that carries no assistant usage record yet (e.g. a
	// fresh transcript after a resume/compaction, before the first response) is
	// "no signal", NOT a zero cumulative — returning zero-as-cumulative here would
	// look like a decrease against a non-zero cursor and spuriously reset it,
	// which could then re-emit already-delivered usage. Report no signal instead.
	if !found {
		return usageCumulative{}, false
	}
	cum.Total = cum.Input + cum.Output
	return cum, true
}

// codexCumulative reads the last token_count event's cumulative
// total_token_usage from the codex rollout that belongs to this session.
func (e *usageExtractor) codexCumulative() (usageCumulative, bool) {
	path, ok := e.locateCodexRollout()
	if !ok {
		return usageCumulative{}, false
	}
	f, ok := openRegularFile(path)
	if !ok {
		return usageCumulative{}, false
	}
	defer func() { _ = f.Close() }()

	var cum usageCumulative
	found := false
	scanJSONLines(f, func(line []byte) {
		var rec struct {
			Type    string `json:"type"`
			Payload struct {
				Type string `json:"type"`
				Info struct {
					TotalTokenUsage struct {
						InputTokens  float64 `json:"input_tokens"`
						OutputTokens float64 `json:"output_tokens"`
						TotalTokens  float64 `json:"total_tokens"`
					} `json:"total_token_usage"`
				} `json:"info"`
			} `json:"payload"`
		}
		if err := json.Unmarshal(line, &rec); err != nil {
			return
		}
		if rec.Type != "event_msg" || rec.Payload.Type != "token_count" {
			return
		}
		u := rec.Payload.Info.TotalTokenUsage
		cum = usageCumulative{Input: u.InputTokens, Output: u.OutputTokens, Total: u.TotalTokens}
		found = true
	})
	if !found {
		return usageCumulative{}, false
	}
	return cum, true
}

// codexQuotaSnapshots reads the newest Codex token_count rate_limits payload
// from the rollout that belongs to this session and converts provider
// used-percent windows into exact quota snapshots. It is best-effort like usage
// extraction: malformed or implausible quota facts are ignored.
func (e *usageExtractor) codexQuotaSnapshots(observedAt time.Time) []domain.QuotaSnapshot {
	path, ok := e.locateCodexRollout()
	if !ok {
		return nil
	}
	f, ok := openRegularFile(path)
	if !ok {
		return nil
	}
	defer func() { _ = f.Close() }()

	var latest []domain.QuotaSnapshot
	scanJSONLines(f, func(line []byte) {
		var rec struct {
			Type    string `json:"type"`
			Payload struct {
				Type       string                  `json:"type"`
				RateLimits codexrollout.RateLimits `json:"rate_limits"`
			} `json:"payload"`
		}
		if err := json.Unmarshal(line, &rec); err != nil {
			return
		}
		if rec.Type != "event_msg" || rec.Payload.Type != "token_count" {
			return
		}
		if snaps := rec.Payload.RateLimits.Snapshots(observedAt.UTC()); len(snaps) > 0 {
			latest = snaps
		}
	})
	return latest
}

// codexSessionMeta is the first-line header of a codex rollout.
type codexSessionMeta struct {
	CWD          string
	ThreadSource string
}

// locateCodexRollout finds the rollout whose session_meta cwd equals the hook
// process cwd. The codex Stop hook rides the codex process as a `-c` session
// flag, so the hook child inherits the worktree cwd that codex recorded as the
// rollout's session_meta.cwd — cwd-matching is therefore reliable and precise.
//
// A worktree may host several rollouts at once: the main session plus any
// subagent threads (thread_source == "subagent"), and reused worktrees leave
// stale rollouts behind. The main session's rollout is the one that accrues the
// turn the Stop hook closes, so we prefer a non-subagent match and, among ties,
// the newest by mtime (the actively-written session). We only fall back to a
// subagent rollout when no main rollout matches the cwd, and log that ambiguity.
//
// The scan is bounded to the most recent date-partitioned day directories so a
// long-lived CODEX_HOME does not turn every stop hook into a full-tree walk.
func (e *usageExtractor) locateCodexRollout() (string, bool) {
	targetCWD := strings.TrimSpace(e.cwd)
	if targetCWD == "" {
		return "", false
	}

	type candidate struct {
		path  string
		mtime time.Time
	}
	var candidates []candidate
	nonEmptyDays := 0
	for _, dir := range e.recentCodexDayDirs() {
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
		// Only a day that actually held rollouts counts toward the window, so a
		// run of newer empty day dirs cannot hide an older active session.
		if dayHadRollout {
			nonEmptyDays++
			if nonEmptyDays >= codexRecentDayDirs {
				break
			}
		}
	}
	sort.Slice(candidates, func(i, j int) bool { return candidates[i].mtime.After(candidates[j].mtime) })

	limit := len(candidates)
	if limit > codexRolloutScanLimit {
		limit = codexRolloutScanLimit
	}
	subagentFallback := ""
	for i := 0; i < limit; i++ {
		meta, ok := readCodexSessionMeta(candidates[i].path)
		if !ok || meta.CWD != targetCWD {
			continue
		}
		if meta.ThreadSource != "subagent" {
			return candidates[i].path, true // main session: the precise match
		}
		if subagentFallback == "" {
			subagentFallback = candidates[i].path
		}
	}
	if subagentFallback != "" {
		e.log("codex usage: no main rollout for cwd " + sanitizeForLog(targetCWD) + "; using subagent rollout " + sanitizeForLog(subagentFallback))
		return subagentFallback, true
	}
	return "", false
}

// recentCodexDayDirs returns the newest sessions/YYYY/MM/DD directories under
// CODEX_HOME, newest-first, without walking the whole tree. It descends year →
// month → day, each level sorted descending, and stops once it has collected
// codexMaxDayDirsExamined directories (crossing month/year boundaries
// naturally). The caller decides how many NON-EMPTY days to actually use; this
// hard cap only bounds work when many recent day dirs are empty.
func (e *usageExtractor) recentCodexDayDirs() []string {
	root := filepath.Join(e.resolveCodexHome(), "sessions")
	var days []string
	for _, year := range descendingDirNames(root) {
		yearDir := filepath.Join(root, year)
		for _, month := range descendingDirNames(yearDir) {
			monthDir := filepath.Join(yearDir, month)
			for _, day := range descendingDirNames(monthDir) {
				days = append(days, filepath.Join(monthDir, day))
				if len(days) >= codexMaxDayDirsExamined {
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

func (e *usageExtractor) resolveCodexHome() string {
	if home := strings.TrimSpace(e.codexHome); home != "" {
		return home
	}
	if userHome, err := os.UserHomeDir(); err == nil {
		return filepath.Join(userHome, ".codex")
	}
	return ".codex"
}

// readCodexSessionMeta reads just the first line of a rollout and returns its
// session_meta cwd and thread source.
func readCodexSessionMeta(path string) (codexSessionMeta, bool) {
	f, ok := openRegularFile(path)
	if !ok {
		return codexSessionMeta{}, false
	}
	defer func() { _ = f.Close() }()

	r := bufio.NewReaderSize(f, 1<<20)
	line, err := r.ReadBytes('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return codexSessionMeta{}, false
	}
	if len(line) == 0 {
		return codexSessionMeta{}, false
	}
	var rec struct {
		Type    string `json:"type"`
		Payload struct {
			CWD          string `json:"cwd"`
			ThreadSource string `json:"thread_source"`
		} `json:"payload"`
	}
	if err := json.Unmarshal(line, &rec); err != nil {
		return codexSessionMeta{}, false
	}
	if rec.Type != "session_meta" {
		return codexSessionMeta{}, false
	}
	return codexSessionMeta{CWD: rec.Payload.CWD, ThreadSource: rec.Payload.ThreadSource}, true
}

func (e *usageExtractor) cursorPath() string {
	return filepath.Join(e.dataDir, usageCursorDir, e.sessionID+".json")
}

// loadCursor reads the persisted cumulative for this session, or a zero value
// when the cursor is absent or unreadable (first turn of the session).
func (e *usageExtractor) loadCursor() usageCumulative {
	data, err := os.ReadFile(e.cursorPath())
	if err != nil {
		return usageCumulative{}
	}
	var cur usageCumulative
	if err := json.Unmarshal(data, &cur); err != nil {
		return usageCumulative{}
	}
	return cur
}

// storeCursor persists the latest cumulative for this session via a temp-file +
// atomic rename, so a concurrent loadCursor never observes a torn write. It
// returns an error only to let callers reason about persistence; the hook path
// treats a failed persist as "cursor not advanced", which defers (never
// double-counts) the delta to the next turn.
func (e *usageExtractor) storeCursor(cum usageCumulative) error {
	dir := filepath.Join(e.dataDir, usageCursorDir)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return err
	}
	data, err := json.Marshal(cum)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, e.sessionID+".*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	if err := os.Chmod(tmpName, 0o600); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	if err := os.Rename(tmpName, e.cursorPath()); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	return nil
}

func (e *usageExtractor) log(msg string) {
	if e.logf != nil {
		e.logf(msg)
	}
}

// openRegularFile opens path only when it resolves to a regular file. It rejects
// FIFOs, devices, and other special files whose open/read could block the stop
// hook and break the agent (the runHook best-effort contract). os.Stat follows
// symlinks, so a symlink to a regular file is allowed while a symlink to a FIFO
// is rejected.
func openRegularFile(path string) (*os.File, bool) {
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() {
		return nil, false
	}
	f, err := os.Open(path) //nolint:gosec // path is a harness transcript or a rollout discovered under CODEX_HOME
	if err != nil {
		return nil, false
	}
	return f, true
}

// sanitizeForLog strips control characters (notably newlines) from a value
// before it goes into the single-line hooks.log, so an odd worktree/path name
// cannot inject a forged log line.
func sanitizeForLog(s string) string {
	return strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, s)
}

// scanJSONLines calls fn for each newline-delimited record in r, tolerating
// arbitrarily long lines up to usageLineCap (longer lines are skipped). It never
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
// final line with io.EOF. A single line longer than usageLineCap is consumed and
// discarded (returns empty) so the scan can continue past it.
func readLine(br *bufio.Reader) ([]byte, error) {
	var buf []byte
	tooLong := false
	for {
		chunk, err := br.ReadSlice('\n')
		if !tooLong {
			buf = append(buf, chunk...)
			if len(buf) > usageLineCap {
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

func nonNegDelta(current, stored float64) float64 {
	d := current - stored
	if d < 0 {
		return 0
	}
	return d
}

func floatPtr(f float64) *float64 { return &f }
