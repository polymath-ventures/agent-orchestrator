package claudecode

import (
	"bytes"
	"context"
	"fmt"
	"math"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

const (
	// claudeUsageProbeTimeout bounds a single `claude -p /usage` probe. It reuses
	// the same budget as the model-validation probe: a usage turn is comparably
	// cheap, and the daemon runs this only on its hourly cadence or an explicit
	// force-probe, never on the spawn hot path.
	claudeUsageProbeTimeout = 45 * time.Second

	// quotaProbeSource labels every snapshot with the invocation that produced it.
	quotaProbeSource = "claude -p /usage"

	// quotaReasonMaxLen caps a sanitized raw excerpt carried on a failed probe.
	quotaReasonMaxLen = 200
)

// usageLineRe matches one Claude `/usage` line of the shape
//
//	<label>: <N>% used <sep> resets <resettime>
//
// It is case-insensitive on "used"/"resets", anchors on the literal "resets"
// keyword after "% used", and treats whatever sits between them (a `·` U+00B7,
// a hyphen, or other punctuation) as an opaque separator so the parse never
// hard-depends on the middle dot. The number is strictly numeric, so a NaN/Inf
// token cannot match and its line is skipped.
var usageLineRe = regexp.MustCompile(`(?i)^\s*(.+?):\s*([0-9]+(?:\.[0-9]+)?)\s*%\s*used\b.*?\bresets\b\s*(.+?)\s*$`)

// usageTZSuffixRe strips a trailing parenthesized timezone marker such as
// " (UTC)" from a reset substring before it is parsed.
var usageTZSuffixRe = regexp.MustCompile(`\s*\([^)]*\)\s*$`)

// resetLayouts are the Go reference layouts for a yearless Claude reset stamp.
// Claude renders the time with minutes ("Jul 23, 3:50pm") on the session window
// but drops them on a whole-hour weekly reset ("Jul 27, 6pm"), so both forms
// must parse. The year is supplied from observedAt after parsing. Layouts are
// tried in order; the first that parses wins.
var resetLayouts = []string{"Jan 2, 3:04pm", "Jan 2, 3pm"}

// parseClaudeUsage turns the human-readable output of `claude -p /usage` into
// quota snapshots. It performs no I/O and never panics on malformed input:
// lines that do not match the expected shape (or carry an out-of-range/NaN/Inf
// percentage) are skipped, and a line whose reset time fails to parse still
// yields a snapshot with the used percentage set and a zero WindowEnd (a
// partial parse is preferred over dropping a real usage signal). Snapshots are
// returned in input order.
func parseClaudeUsage(raw string, observedAt time.Time) []domain.QuotaSnapshot {
	var snaps []domain.QuotaSnapshot
	for _, line := range strings.Split(raw, "\n") {
		m := usageLineRe.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		label := strings.TrimSpace(m[1])
		if label == "" {
			continue
		}
		used, err := strconv.ParseFloat(m[2], 64)
		if err != nil || math.IsNaN(used) || math.IsInf(used, 0) || used < 0 || used > 100 {
			continue
		}

		windowEnd := parseResetTime(m[3], observedAt)

		limit := 100.0
		remaining := 100.0 - used
		usedVal := used
		snaps = append(snaps, domain.QuotaSnapshot{
			Harness:       domain.HarnessClaudeCode,
			AccountID:     "unknown",
			WindowName:    strings.TrimSpace(trimCurrentPrefix(label)),
			WindowEnd:     windowEnd,
			Used:          &usedVal,
			Remaining:     &remaining,
			Limit:         &limit,
			SignalQuality: domain.QuotaSignalExact,
			Source:        quotaProbeSource,
			Basis:         fmt.Sprintf("Parsed %s from %s", label, quotaProbeSource),
			ObservedAt:    observedAt,
		})
	}
	return snaps
}

// trimCurrentPrefix removes a leading "Current " (case-insensitive) from a
// usage label so "Current week (Fable)" becomes "week (Fable)".
func trimCurrentPrefix(label string) string {
	const prefix = "current "
	if len(label) >= len(prefix) && strings.EqualFold(label[:len(prefix)], prefix) {
		return label[len(prefix):]
	}
	return label
}

// parseResetTime parses a yearless Claude reset stamp into a UTC time, filling
// the year from observedAt. Because reset windows are near-future, a result
// more than 24h before observedAt is rolled forward one year (year-wrap at a
// December → January boundary). An unparseable stamp yields the zero time.
func parseResetTime(reset string, observedAt time.Time) time.Time {
	reset = strings.TrimSpace(usageTZSuffixRe.ReplaceAllString(reset, ""))
	var parsed time.Time
	var err error
	for _, layout := range resetLayouts {
		if parsed, err = time.ParseInLocation(layout, reset, time.UTC); err == nil {
			break
		}
	}
	if err != nil {
		return time.Time{}
	}
	end := time.Date(observedAt.Year(), parsed.Month(), parsed.Day(), parsed.Hour(), parsed.Minute(), 0, 0, time.UTC)
	if end.Before(observedAt.Add(-24 * time.Hour)) {
		end = end.AddDate(1, 0, 0)
	}
	return end
}

// usageProbeArgs is the single editable place defining the probe invocation.
// It is the minimal operator-proven form `claude -p /usage` (`-p` == `--print`);
// no model/MCP/tool flags are added because the usage command needs none and
// extra flags only risk rejecting the invocation on some supported releases.
func usageProbeArgs() []string {
	return []string{"--print", "/usage"}
}

// scrubProbeEnv returns os.Environ() with AO's per-session credentials removed,
// mirroring the `env -u AO_SESSION_ID -u AO_RUNTIME_TOKEN -u AO_RUN_FILE` scrub
// the identity contract requires for nested agent launches. The scrub applies
// only to this child process.
func scrubProbeEnv() []string {
	banned := map[string]struct{}{
		"AO_SESSION_ID":    {},
		"AO_RUNTIME_TOKEN": {},
		"AO_RUN_FILE":      {},
	}
	src := os.Environ()
	out := make([]string, 0, len(src))
	for _, kv := range src {
		key := kv
		if eq := strings.IndexByte(kv, '='); eq >= 0 {
			key = kv[:eq]
		}
		if _, drop := banned[key]; drop {
			continue
		}
		out = append(out, kv)
	}
	return out
}

// ProbeQuota implements ports.AgentQuotaProber for the Claude Code adapter. It
// runs the bounded, env-scrubbed `claude -p /usage` probe (mirroring the
// ValidateModel subprocess discipline: timeout, WaitDelay, process-group
// cancellation) and parses the human-readable output. A missing binary, a
// cancelled/timed-out context, or output that yields no snapshots all surface as
// a failed result carrying a short, sanitized reason — never a panic and never a
// silent ok.
// QuotaHarness reports the canonical harness this probe's usage belongs to —
// claude-code has no fork variant sharing its pool, so it is simply itself.
func (p *Plugin) QuotaHarness() domain.AgentHarness { return domain.HarnessClaudeCode }

func (p *Plugin) ProbeQuota(ctx context.Context, observedAt time.Time) (ports.QuotaProbeResult, error) {
	binary, err := p.claudeBinary(ctx)
	if err != nil {
		return ports.QuotaProbeResult{State: domain.QuotaProbeFailed, Reason: sanitizeReason(err.Error())}, nil
	}

	probeCtx, cancel := context.WithTimeout(ctx, claudeUsageProbeTimeout)
	defer cancel()

	cmd := exec.CommandContext(probeCtx, binary, usageProbeArgs()...)
	cmd.Stdin = nil // no prompt: /usage is a slash command, not a task
	cmd.Env = scrubProbeEnv()
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	cmd.WaitDelay = claudeProbeWaitDelay
	configureProbeProcessGroup(cmd)

	runErr := cmd.Run()
	if probeCtx.Err() != nil {
		return ports.QuotaProbeResult{State: domain.QuotaProbeFailed, Reason: sanitizeReason(probeCtx.Err().Error())}, nil
	}

	snaps := parseClaudeUsage(stdout.String(), observedAt)
	if len(snaps) > 0 {
		return ports.QuotaProbeResult{State: domain.QuotaProbeOK, Snapshots: snaps}, nil
	}

	excerpt := stdout.String()
	if strings.TrimSpace(excerpt) == "" {
		excerpt = stderr.String()
	}
	reason := sanitizeReason(excerpt)
	if reason == "" && runErr != nil {
		reason = sanitizeReason(runErr.Error())
	}
	return ports.QuotaProbeResult{State: domain.QuotaProbeFailed, Reason: reason}, nil
}

// sanitizeReason makes a raw excerpt safe to store and render: it strips control
// characters and truncates to quotaReasonMaxLen runes so a failed probe carries
// a short, log-safe hint rather than a wall of terminal escapes.
func sanitizeReason(raw string) string {
	var b strings.Builder
	for _, r := range raw {
		if unicode.IsControl(r) {
			if r == '\n' || r == '\t' {
				b.WriteByte(' ')
			}
			continue
		}
		b.WriteRune(r)
	}
	out := strings.TrimSpace(b.String())
	if len([]rune(out)) > quotaReasonMaxLen {
		out = string([]rune(out)[:quotaReasonMaxLen])
		out = strings.TrimSpace(out)
	}
	return out
}
