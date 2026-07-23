package claudecode

import (
	"context"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

// Compile-time proof the adapter satisfies the optional quota-prober capability.
var _ ports.AgentQuotaProber = (*Plugin)(nil)

func derefFloat(t *testing.T, p *float64) float64 {
	t.Helper()
	if p == nil {
		t.Fatalf("expected a non-nil float pointer")
	}
	return *p
}

func TestParseClaudeUsageThreeLineExample(t *testing.T) {
	observedAt := time.Date(2026, time.July, 23, 15, 0, 0, 0, time.UTC)
	raw := "Current session: 8% used · resets Jul 23, 3:49pm (UTC)\n" +
		"Current week (all models): 42% used · resets Jul 27, 5:59pm (UTC)\n" +
		"Current week (Fable): 41% used · resets Jul 27, 5:59pm (UTC)\n"

	snaps := parseClaudeUsage(raw, observedAt)
	if len(snaps) != 3 {
		t.Fatalf("expected 3 snapshots, got %d: %+v", len(snaps), snaps)
	}

	type want struct {
		windowName string
		used       float64
		remaining  float64
		endMonth   time.Month
		endDay     int
	}
	wants := []want{
		{"session", 8, 92, time.July, 23},
		{"week (all models)", 42, 58, time.July, 27},
		{"week (Fable)", 41, 59, time.July, 27},
	}
	for i, w := range wants {
		s := snaps[i]
		if s.WindowName != w.windowName {
			t.Errorf("snap %d: WindowName = %q, want %q", i, s.WindowName, w.windowName)
		}
		if got := derefFloat(t, s.Used); got != w.used {
			t.Errorf("snap %d: Used = %v, want %v", i, got, w.used)
		}
		if got := derefFloat(t, s.Remaining); got != w.remaining {
			t.Errorf("snap %d: Remaining = %v, want %v", i, got, w.remaining)
		}
		if got := derefFloat(t, s.Limit); got != 100 {
			t.Errorf("snap %d: Limit = %v, want 100", i, got)
		}
		if s.WindowEnd.IsZero() {
			t.Errorf("snap %d: WindowEnd is zero, want a parsed time", i)
			continue
		}
		if s.WindowEnd.Month() != w.endMonth || s.WindowEnd.Day() != w.endDay {
			t.Errorf("snap %d: WindowEnd = %v, want %v %d", i, s.WindowEnd, w.endMonth, w.endDay)
		}
		if s.WindowEnd.Year() != 2026 {
			t.Errorf("snap %d: WindowEnd.Year = %d, want 2026", i, s.WindowEnd.Year())
		}
		if s.WindowEnd.Location() != time.UTC {
			t.Errorf("snap %d: WindowEnd location = %v, want UTC", i, s.WindowEnd.Location())
		}
		if s.Harness != domain.HarnessClaudeCode {
			t.Errorf("snap %d: Harness = %q, want %q", i, s.Harness, domain.HarnessClaudeCode)
		}
		if s.AccountID != "unknown" {
			t.Errorf("snap %d: AccountID = %q, want %q", i, s.AccountID, "unknown")
		}
		if s.SignalQuality != domain.QuotaSignalExact {
			t.Errorf("snap %d: SignalQuality = %q, want %q", i, s.SignalQuality, domain.QuotaSignalExact)
		}
		if s.Source != "claude -p /usage" {
			t.Errorf("snap %d: Source = %q, want %q", i, s.Source, "claude -p /usage")
		}
		if s.Basis == "" {
			t.Errorf("snap %d: Basis is empty, want a short note", i)
		}
		if !s.ObservedAt.Equal(observedAt) {
			t.Errorf("snap %d: ObservedAt = %v, want %v", i, s.ObservedAt, observedAt)
		}
	}
}

// TestParseClaudeUsageRealFormatWholeHourReset uses the verbatim output of the
// installed `claude -p /usage` (claude 2.1.218), which surrounds the three usage
// lines with prose and renders a whole-hour weekly reset WITHOUT minutes
// ("Jul 27, 6pm"). The minutes-less stamp regressed to a zero WindowEnd on the
// headline window (GH #97 end-to-end finding); this locks the fix in and proves
// the prose lines are skipped.
func TestParseClaudeUsageRealFormatWholeHourReset(t *testing.T) {
	observedAt := time.Date(2026, time.July, 23, 14, 0, 0, 0, time.UTC)
	raw := "You are currently using your subscription to power your Claude Code usage\n" +
		"\n" +
		"Current session: 19% used · resets Jul 23, 3:50pm (UTC)\n" +
		"Current week (all models): 44% used · resets Jul 27, 6pm (UTC)\n" +
		"Current week (Fable): 42% used · resets Jul 27, 6pm (UTC)\n" +
		"\n" +
		"What's contributing to your limits usage?\n" +
		"Last 24h · 555 requests · 8 sessions\n"

	snaps := parseClaudeUsage(raw, observedAt)
	if len(snaps) != 3 {
		t.Fatalf("expected 3 usage snapshots (prose lines skipped), got %d: %+v", len(snaps), snaps)
	}

	byName := map[string]domain.QuotaSnapshot{}
	for _, s := range snaps {
		byName[s.WindowName] = s
	}
	head, ok := byName["week (all models)"]
	if !ok {
		t.Fatalf("missing 'week (all models)' headline window: %+v", snaps)
	}
	if got := derefFloat(t, head.Used); got != 44 {
		t.Errorf("headline Used = %v, want 44", got)
	}
	// The whole-hour "6pm" must parse to 18:00 UTC on Jul 27 — not a zero time.
	if head.WindowEnd.IsZero() {
		t.Fatal("headline WindowEnd is zero — the minutes-less '6pm' reset failed to parse")
	}
	if head.WindowEnd.Month() != time.July || head.WindowEnd.Day() != 27 || head.WindowEnd.Hour() != 18 || head.WindowEnd.Minute() != 0 {
		t.Errorf("headline WindowEnd = %v, want 2026-07-27 18:00 UTC", head.WindowEnd)
	}
	// The minutes-bearing session stamp must still parse (3:50pm → 15:50).
	if sess := byName["session"]; sess.WindowEnd.Hour() != 15 || sess.WindowEnd.Minute() != 50 {
		t.Errorf("session WindowEnd = %v, want 15:50", sess.WindowEnd)
	}
}

func TestParseClaudeUsageGarbageResetKeepsSnapshot(t *testing.T) {
	observedAt := time.Date(2026, time.July, 23, 15, 0, 0, 0, time.UTC)
	raw := "Current session: 8% used · resets NotADate (UTC)\n"

	snaps := parseClaudeUsage(raw, observedAt)
	if len(snaps) != 1 {
		t.Fatalf("expected 1 snapshot (partial parse), got %d", len(snaps))
	}
	if got := derefFloat(t, snaps[0].Used); got != 8 {
		t.Errorf("Used = %v, want 8", got)
	}
	if !snaps[0].WindowEnd.IsZero() {
		t.Errorf("WindowEnd = %v, want zero (unparseable reset)", snaps[0].WindowEnd)
	}
}

func TestParseClaudeUsageOutOfRangeDropped(t *testing.T) {
	observedAt := time.Date(2026, time.July, 23, 15, 0, 0, 0, time.UTC)
	raw := "Current session: 150% used · resets Jul 23, 3:49pm (UTC)\n"

	snaps := parseClaudeUsage(raw, observedAt)
	if len(snaps) != 0 {
		t.Fatalf("expected out-of-range line dropped, got %d snapshots: %+v", len(snaps), snaps)
	}
}

func TestParseClaudeUsageEmptyAndGarbage(t *testing.T) {
	observedAt := time.Date(2026, time.July, 23, 15, 0, 0, 0, time.UTC)
	for _, raw := range []string{"", "   \n\t\n", "totally unrelated text\nno percentages here"} {
		if snaps := parseClaudeUsage(raw, observedAt); len(snaps) != 0 {
			t.Errorf("parseClaudeUsage(%q) = %d snapshots, want 0", raw, len(snaps))
		}
	}
}

func TestParseClaudeUsageYearWrap(t *testing.T) {
	// Observed in December; a January reset must roll to next year.
	observedAt := time.Date(2026, time.December, 15, 12, 0, 0, 0, time.UTC)
	raw := "Current week (all models): 42% used · resets Jan 3, 5:59pm (UTC)\n"

	snaps := parseClaudeUsage(raw, observedAt)
	if len(snaps) != 1 {
		t.Fatalf("expected 1 snapshot, got %d", len(snaps))
	}
	end := snaps[0].WindowEnd
	if end.Year() != 2027 || end.Month() != time.January || end.Day() != 3 {
		t.Errorf("WindowEnd = %v, want 2027-01-03", end)
	}
}

func TestParseClaudeUsageAltSeparator(t *testing.T) {
	// The separator between "% used" and "resets" must not be hard-depended on.
	observedAt := time.Date(2026, time.July, 23, 15, 0, 0, 0, time.UTC)
	raw := "Current session: 8% USED - RESETS Jul 23, 3:49pm (UTC)\n"

	snaps := parseClaudeUsage(raw, observedAt)
	if len(snaps) != 1 {
		t.Fatalf("expected 1 snapshot (case-insensitive, alt separator), got %d", len(snaps))
	}
	if snaps[0].WindowName != "session" {
		t.Errorf("WindowName = %q, want %q", snaps[0].WindowName, "session")
	}
	if snaps[0].WindowEnd.IsZero() {
		t.Errorf("WindowEnd is zero, want a parsed time")
	}
}

func TestProbeQuotaBinaryMissingFails(t *testing.T) {
	p := New()
	// Point the resolver at a binary that cannot exist so the probe subprocess
	// fails to exec without requiring a real claude install.
	p.resolvedBinary = "/nonexistent/definitely-not-a-real-claude-binary"

	res, err := p.ProbeQuota(context.Background(), time.Now())
	if err != nil {
		t.Fatalf("ProbeQuota returned error, want nil: %v", err)
	}
	if res.State != domain.QuotaProbeFailed {
		t.Fatalf("State = %q, want %q", res.State, domain.QuotaProbeFailed)
	}
	if len(res.Snapshots) != 0 {
		t.Errorf("expected no snapshots on failure, got %d", len(res.Snapshots))
	}
}

func TestUsageProbeArgs(t *testing.T) {
	got := usageProbeArgs()
	want := []string{"--print", "/usage"}
	if len(got) != len(want) {
		t.Fatalf("usageProbeArgs() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("usageProbeArgs() = %v, want %v", got, want)
		}
	}
}

func TestScrubProbeEnvRemovesAOKeys(t *testing.T) {
	t.Setenv("AO_SESSION_ID", "sess")
	t.Setenv("AO_RUNTIME_TOKEN", "tok")
	t.Setenv("AO_RUN_FILE", "/run/file")
	t.Setenv("PATH_MARKER_KEEP", "keepme")

	env := scrubProbeEnv()
	for _, kv := range env {
		for _, banned := range []string{"AO_SESSION_ID=", "AO_RUNTIME_TOKEN=", "AO_RUN_FILE="} {
			if len(kv) >= len(banned) && kv[:len(banned)] == banned {
				t.Errorf("scrubbed env still contains %q", kv)
			}
		}
	}
	var kept bool
	for _, kv := range env {
		if kv == "PATH_MARKER_KEEP=keepme" {
			kept = true
		}
	}
	if !kept {
		t.Errorf("scrubProbeEnv dropped an unrelated env var")
	}
}
