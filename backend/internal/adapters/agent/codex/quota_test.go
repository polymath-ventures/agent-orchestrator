package codex

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
)

// writeQuotaRollout drops a codex rollout carrying a token_count rate_limits
// event under codexHome/sessions/2026/07/18/.
func writeQuotaRollout(t *testing.T, codexHome, name, rateLimits string) {
	t.Helper()
	line := `{"type":"session_meta","payload":{"session_id":"sess","cwd":"/w"}}` + "\n" +
		`{"type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":` +
		`{"input_tokens":100,"output_tokens":20,"total_tokens":120}},"rate_limits":` + rateLimits + `}}` + "\n"
	path := filepath.Join(codexHome, "sessions", "2026", "07", "18", name)
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(line), 0o600); err != nil {
		t.Fatal(err)
	}
}

// A CODEX_HOME with a rollout carrying valid rate_limits yields an ok probe with
// the expected primary + secondary windows.
func TestProbeQuota_ReturnsOKWithWindows(t *testing.T) {
	home := t.TempDir()
	t.Setenv("CODEX_HOME", home)
	writeQuotaRollout(t, home, "rollout-a.jsonl",
		`{"limit_id":"codex","primary":{"used_percent":80,"window_minutes":10080,"resets_at":1785277078},`+
			`"secondary":{"used_percent":40,"window_minutes":300,"resets_at":1784680000},"plan_type":"pro"}`)

	res, err := New().ProbeQuota(context.Background(), time.Unix(200, 0).UTC())
	if err != nil {
		t.Fatalf("ProbeQuota: %v", err)
	}
	if res.State != domain.QuotaProbeOK {
		t.Fatalf("state = %q, want ok", res.State)
	}
	if len(res.Snapshots) != 2 {
		t.Fatalf("snapshots = %+v, want primary and secondary", res.Snapshots)
	}
	if res.Snapshots[0].WindowName != "primary" || res.Snapshots[0].Harness != domain.HarnessCodex {
		t.Fatalf("primary snapshot = %+v", res.Snapshots[0])
	}
	if res.Snapshots[0].Used == nil || *res.Snapshots[0].Used != 80 {
		t.Fatalf("primary used = %v, want 80", res.Snapshots[0].Used)
	}
	if res.Snapshots[1].WindowName != "secondary" {
		t.Fatalf("secondary snapshot = %+v", res.Snapshots[1])
	}
}

// An empty CODEX_HOME (no usage recorded yet) is NOT a failure: the source works,
// there is just nothing to report. It must be ok with no snapshots.
func TestProbeQuota_EmptyHomeIsOKNotFailed(t *testing.T) {
	home := t.TempDir()
	t.Setenv("CODEX_HOME", home)

	res, err := New().ProbeQuota(context.Background(), time.Unix(200, 0).UTC())
	if err != nil {
		t.Fatalf("ProbeQuota: %v", err)
	}
	if res.State != domain.QuotaProbeOK {
		t.Fatalf("state = %q, want ok (empty home is not a probe failure)", res.State)
	}
	if len(res.Snapshots) != 0 {
		t.Fatalf("snapshots = %+v, want none for an empty home", res.Snapshots)
	}
}

// Fugu shares CODEX_HOME with codex, so its probe reports the same pool tagged
// with the codex harness — the daemon/widget collapse the two into one chip.
func TestProbeQuota_FuguSharesCodexHomeAndTagsCodex(t *testing.T) {
	home := t.TempDir()
	t.Setenv("CODEX_HOME", home)
	writeQuotaRollout(t, home, "rollout-a.jsonl",
		`{"limit_id":"codex","primary":{"used_percent":55,"window_minutes":10080,"resets_at":1785277078}}`)

	res, err := NewFugu().ProbeQuota(context.Background(), time.Unix(200, 0).UTC())
	if err != nil {
		t.Fatalf("ProbeQuota: %v", err)
	}
	if res.State != domain.QuotaProbeOK || len(res.Snapshots) != 1 {
		t.Fatalf("fugu probe = %+v, want ok with one window from the shared codex home", res)
	}
	if res.Snapshots[0].Harness != domain.HarnessCodex {
		t.Fatalf("fugu snapshot harness = %q, want codex (single combined chip)", res.Snapshots[0].Harness)
	}
}

// A cancelled context is honored: the probe returns the context error rather
// than reading the filesystem.
func TestProbeQuota_HonorsContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := New().ProbeQuota(ctx, time.Now()); err == nil {
		t.Fatal("ProbeQuota with a cancelled context should return an error")
	}
}
