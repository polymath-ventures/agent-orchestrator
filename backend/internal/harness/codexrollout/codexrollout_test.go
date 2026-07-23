package codexrollout

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
)

// writeRollout writes a codex rollout JSONL at
// codexHome/sessions/<date...>/<name> whose first line is a session_meta and
// whose remaining lines are token_count events. rateLimits, when non-empty, is
// spliced verbatim into the final token_count event's payload as a rate_limits
// object.
func writeRollout(t *testing.T, codexHome string, date []string, name, rateLimits string, tokenEvents [][3]int) string {
	t.Helper()
	lines := []string{`{"type":"session_meta","payload":{"session_id":"sess","cwd":"/w"}}`}
	for i, ev := range tokenEvents {
		payload := `{"type":"token_count","info":{"total_token_usage":{` +
			`"input_tokens":` + strconv.Itoa(ev[0]) +
			`,"output_tokens":` + strconv.Itoa(ev[1]) +
			`,"total_tokens":` + strconv.Itoa(ev[2]) + `}}`
		if rateLimits != "" && i == len(tokenEvents)-1 {
			payload += `,"rate_limits":` + rateLimits
		}
		payload += `}`
		lines = append(lines, `{"type":"event_msg","payload":`+payload+`}`)
	}
	parts := append([]string{codexHome, "sessions"}, date...)
	parts = append(parts, name)
	path := filepath.Join(parts...)
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestSnapshots_PrimaryAndSecondary(t *testing.T) {
	primary := 92.5
	secondary := 51.0
	pMin := 10080.5
	sMin := 300.0
	pReset := int64(1785277078)
	sReset := int64(1784680000)
	rl := RateLimits{
		LimitID:  "codex",
		PlanType: "pro",
		Primary:  &RateWindow{UsedPercent: &primary, WindowMinutes: &pMin, ResetsAt: &pReset},
		Secondary: &RateWindow{
			UsedPercent: &secondary, WindowMinutes: &sMin, ResetsAt: &sReset,
		},
	}
	observed := time.Unix(200, 0).UTC()
	got := rl.Snapshots(observed)
	if len(got) != 2 {
		t.Fatalf("snapshots = %+v, want primary and secondary", got)
	}
	if got[0].WindowName != "primary" || got[0].SignalQuality != domain.QuotaSignalExact {
		t.Fatalf("primary snapshot = %+v", got[0])
	}
	if got[0].Harness != domain.HarnessCodex || got[0].AccountID != "unknown" {
		t.Fatalf("primary identity = %+v", got[0])
	}
	if got[0].Used == nil || *got[0].Used != 92.5 {
		t.Fatalf("primary used = %v, want 92.5", got[0].Used)
	}
	if got[0].Remaining == nil || *got[0].Remaining != 7.5 {
		t.Fatalf("primary remaining = %v, want 7.5", got[0].Remaining)
	}
	if got[0].Limit == nil || *got[0].Limit != 100 {
		t.Fatalf("primary limit = %v, want 100", got[0].Limit)
	}
	if got[0].Source != "codex rollout token_count.rate_limits" {
		t.Fatalf("primary source = %q", got[0].Source)
	}
	if got[0].WindowEnd.Unix() != pReset {
		t.Fatalf("primary reset = %s, want unix %d", got[0].WindowEnd, pReset)
	}
	if want := got[0].WindowEnd.Add(-time.Duration(pMin * float64(time.Minute))); !got[0].WindowStart.Equal(want) {
		t.Fatalf("primary window start = %s, want %s", got[0].WindowStart, want)
	}
	if !got[0].ObservedAt.Equal(observed) {
		t.Fatalf("primary observedAt = %s, want %s", got[0].ObservedAt, observed)
	}
	if got[1].WindowName != "secondary" {
		t.Fatalf("secondary snapshot = %+v", got[1])
	}
}

func TestSnapshots_RejectsImplausibleUsedPercent(t *testing.T) {
	over := 120.0
	minutes := 10080.0
	reset := int64(1785277078)
	rl := RateLimits{Primary: &RateWindow{UsedPercent: &over, WindowMinutes: &minutes, ResetsAt: &reset}}
	if got := rl.Snapshots(time.Unix(200, 0).UTC()); len(got) != 0 {
		t.Fatalf("implausible used_percent must be rejected, got %+v", got)
	}
}

func TestSnapshots_RejectsBadWindowAndReset(t *testing.T) {
	used := 50.0
	badMinutes := 0.0
	reset := int64(1785277078)
	if got := (RateLimits{Primary: &RateWindow{UsedPercent: &used, WindowMinutes: &badMinutes, ResetsAt: &reset}}).Snapshots(time.Unix(200, 0).UTC()); len(got) != 0 {
		t.Fatalf("window_minutes<=0 must be rejected, got %+v", got)
	}
	goodMinutes := 300.0
	badReset := int64(0)
	if got := (RateLimits{Primary: &RateWindow{UsedPercent: &used, WindowMinutes: &goodMinutes, ResetsAt: &badReset}}).Snapshots(time.Unix(200, 0).UTC()); len(got) != 0 {
		t.Fatalf("resets_at<=0 must be rejected, got %+v", got)
	}
}

func TestNewestRateLimits_ValidRollout(t *testing.T) {
	home := t.TempDir()
	rl := `{"limit_id":"codex","primary":{"used_percent":80,"window_minutes":10080,"resets_at":1785277078},"secondary":{"used_percent":40,"window_minutes":300,"resets_at":1784680000},"plan_type":"pro"}`
	writeRollout(t, home, []string{"2026", "07", "18"}, "rollout-a.jsonl", rl, [][3]int{{100, 20, 120}})

	snaps, found := NewestRateLimits(context.Background(), home, time.Unix(200, 0).UTC())
	if !found {
		t.Fatal("found = false, want true for a rollout carrying rate_limits")
	}
	if len(snaps) != 2 {
		t.Fatalf("snaps = %+v, want primary and secondary", snaps)
	}
	if snaps[0].Used == nil || *snaps[0].Used != 80 {
		t.Fatalf("primary used = %v, want 80", snaps[0].Used)
	}
}

func TestNewestRateLimits_ImplausibleReturnsFoundNoSnaps(t *testing.T) {
	home := t.TempDir()
	rl := `{"limit_id":"codex","primary":{"used_percent":120,"window_minutes":10080,"resets_at":1785277078}}`
	writeRollout(t, home, []string{"2026", "07", "18"}, "rollout-a.jsonl", rl, [][3]int{{100, 20, 120}})

	snaps, found := NewestRateLimits(context.Background(), home, time.Unix(200, 0).UTC())
	if !found {
		t.Fatal("found = false, want true: a rate_limits event was present even though its windows are implausible")
	}
	if len(snaps) != 0 {
		t.Fatalf("snaps = %+v, want none (all windows implausible)", snaps)
	}
}

func TestNewestRateLimits_NewestDayWithDataWins(t *testing.T) {
	home := t.TempDir()
	// A newer day dir that holds no rollout at all must not hide the older
	// day dir that carries the real rate_limits.
	if err := os.MkdirAll(filepath.Join(home, "sessions", "2026", "07", "20"), 0o750); err != nil {
		t.Fatal(err)
	}
	rl := `{"limit_id":"codex","primary":{"used_percent":73,"window_minutes":10080,"resets_at":1785277078}}`
	writeRollout(t, home, []string{"2026", "07", "18"}, "rollout-a.jsonl", rl, [][3]int{{7, 3, 10}})

	snaps, found := NewestRateLimits(context.Background(), home, time.Unix(200, 0).UTC())
	if !found || len(snaps) != 1 {
		t.Fatalf("found=%v snaps=%+v, want a single window from the older non-empty day", found, snaps)
	}
	if snaps[0].Used == nil || *snaps[0].Used != 73 {
		t.Fatalf("used = %v, want 73", snaps[0].Used)
	}
}

func TestNewestRateLimits_NewerRolloutWithoutRateLimitsFallsBack(t *testing.T) {
	home := t.TempDir()
	// Older rollout carries rate_limits; a newer rollout (later mtime) has
	// token_count events but no rate_limits. Newest-with-rate_limits wins.
	rl := `{"limit_id":"codex","primary":{"used_percent":61,"window_minutes":10080,"resets_at":1785277078}}`
	oldPath := writeRollout(t, home, []string{"2026", "07", "18"}, "rollout-old.jsonl", rl, [][3]int{{100, 20, 120}})
	newPath := writeRollout(t, home, []string{"2026", "07", "18"}, "rollout-new.jsonl", "", [][3]int{{5, 5, 10}})

	old := time.Now().Add(-time.Hour)
	newer := time.Now()
	if err := os.Chtimes(oldPath, old, old); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(newPath, newer, newer); err != nil {
		t.Fatal(err)
	}

	snaps, found := NewestRateLimits(context.Background(), home, time.Unix(200, 0).UTC())
	if !found || len(snaps) != 1 {
		t.Fatalf("found=%v snaps=%+v, want the older rollout's rate_limits", found, snaps)
	}
	if snaps[0].Used == nil || *snaps[0].Used != 61 {
		t.Fatalf("used = %v, want 61", snaps[0].Used)
	}
}

func TestNewestRateLimits_NoRolloutReturnsNotFound(t *testing.T) {
	home := t.TempDir()
	snaps, found := NewestRateLimits(context.Background(), home, time.Unix(200, 0).UTC())
	if found || snaps != nil {
		t.Fatalf("found=%v snaps=%+v, want not found for an empty home", found, snaps)
	}
}

func TestNewestRateLimits_MissingHomeReturnsNotFound(t *testing.T) {
	snaps, found := NewestRateLimits(context.Background(), filepath.Join(t.TempDir(), "does-not-exist"), time.Unix(200, 0).UTC())
	if found || snaps != nil {
		t.Fatalf("found=%v snaps=%+v, want not found for an absent home", found, snaps)
	}
}

// TestSnapshots_RejectsExpiredWindow proves an already-reset window (resets_at
// at or before observedAt) is dropped: its used_percent is stale and must not
// be presented as current usage.
func TestSnapshots_RejectsExpiredWindow(t *testing.T) {
	used := 95.0
	minutes := 10080.0
	reset := int64(1_000_000_000) // 2001, long before observedAt below
	observed := time.Unix(2_000_000_000, 0).UTC()
	rl := RateLimits{Primary: &RateWindow{UsedPercent: &used, WindowMinutes: &minutes, ResetsAt: &reset}}
	if got := rl.Snapshots(observed); len(got) != 0 {
		t.Fatalf("expired window must be rejected, got %+v", got)
	}
	// A reset exactly equal to observedAt is not strictly in the future → rejected.
	eq := observed.Unix()
	rl.Primary.ResetsAt = &eq
	if got := rl.Snapshots(observed); len(got) != 0 {
		t.Fatalf("reset == observedAt must be rejected (not strictly future), got %+v", got)
	}
}

// TestNewestRateLimits_ExpiredWindowFoundNoSnaps proves the file-level read
// still reports found=true (a rate_limits event existed) but yields zero
// snapshots when the only window has already reset.
func TestNewestRateLimits_ExpiredWindowFoundNoSnaps(t *testing.T) {
	home := t.TempDir()
	rl := `{"limit_id":"codex","primary":{"used_percent":95,"window_minutes":10080,"resets_at":1000000000}}`
	writeRollout(t, home, []string{"2026", "07", "18"}, "rollout-a.jsonl", rl, [][3]int{{100, 20, 120}})

	snaps, found := NewestRateLimits(context.Background(), home, time.Unix(2_000_000_000, 0).UTC())
	if !found {
		t.Fatal("found = false, want true: a rate_limits event was present even though its window has expired")
	}
	if len(snaps) != 0 {
		t.Fatalf("snaps = %+v, want none (window already reset)", snaps)
	}
}

// TestSnapshots_CapsBasisFields proves an oversized limit_id/plan_type is
// truncated to maxBasisFieldLen runes in the snapshot basis.
func TestSnapshots_CapsBasisFields(t *testing.T) {
	used := 50.0
	minutes := 300.0
	reset := int64(1785277078)
	longID := strings.Repeat("A", 200)
	longPlan := strings.Repeat("B", 200)
	rl := RateLimits{
		LimitID:  longID,
		PlanType: longPlan,
		Primary:  &RateWindow{UsedPercent: &used, WindowMinutes: &minutes, ResetsAt: &reset},
	}
	got := rl.Snapshots(time.Unix(200, 0).UTC())
	if len(got) != 1 {
		t.Fatalf("snapshots = %+v, want one", got)
	}
	if strings.Contains(got[0].Basis, strings.Repeat("A", maxBasisFieldLen+1)) {
		t.Fatalf("limit_id not capped to %d runes: %q", maxBasisFieldLen, got[0].Basis)
	}
	if strings.Contains(got[0].Basis, strings.Repeat("B", maxBasisFieldLen+1)) {
		t.Fatalf("plan_type not capped to %d runes: %q", maxBasisFieldLen, got[0].Basis)
	}
	if !strings.Contains(got[0].Basis, "limit_id="+strings.Repeat("A", maxBasisFieldLen)) {
		t.Fatalf("expected capped limit_id in basis: %q", got[0].Basis)
	}
}

// TestNewestRateLimits_CancelledCtxReturnsPromptly proves a cancelled context
// short-circuits the scan: NewestRateLimits returns (nil,false) without reading
// the large rollout on disk.
func TestNewestRateLimits_CancelledCtxReturnsPromptly(t *testing.T) {
	home := t.TempDir()
	// A large synthetic rollout whose only rate_limits sits at the very end. A
	// full scan would have to read the whole thing; a cancelled ctx must not.
	rl := `{"limit_id":"codex","primary":{"used_percent":50,"window_minutes":10080,"resets_at":1785277078}}`
	filler := make([][3]int, 20000)
	for i := range filler {
		filler[i] = [3]int{i, i, 2 * i}
	}
	writeRollout(t, home, []string{"2026", "07", "18"}, "rollout-big.jsonl", rl, filler)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // already cancelled before the call

	snaps, found := NewestRateLimits(ctx, home, time.Unix(200, 0).UTC())
	if found || snaps != nil {
		t.Fatalf("found=%v snaps=%+v, want (nil,false) for a cancelled ctx", found, snaps)
	}
}

// TestNewestRateLimits_TailScanReturnsNewestBeyondCap proves the bounded read is
// a TAIL scan, not a first-N-bytes scan: when an OLD rate_limits event sits early
// in a file larger than the byte cap and a NEWER one sits at the end, the newest
// (tail) window is returned — never the stale early one. This is the GH #97
// cycle-2 fix (a forward byte cap would have returned the old event).
func TestNewestRateLimits_TailScanReturnsNewestBeyondCap(t *testing.T) {
	orig := maxRolloutScanBytes
	maxRolloutScanBytes = 512 // shrink so a small file exceeds the cap
	defer func() { maxRolloutScanBytes = orig }()

	home := t.TempDir()
	dir := filepath.Join(home, "sessions", "2026", "07", "18")
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatal(err)
	}
	reset := int64(2_000_000_000) // far-future so neither window is expired
	oldEvent := `{"type":"event_msg","payload":{"type":"token_count","rate_limits":{"limit_id":"codex","primary":{"used_percent":10,"window_minutes":10080,"resets_at":` + strconv.FormatInt(reset, 10) + `}}}}`
	newEvent := `{"type":"event_msg","payload":{"type":"token_count","rate_limits":{"limit_id":"codex","primary":{"used_percent":80,"window_minutes":10080,"resets_at":` + strconv.FormatInt(reset, 10) + `}}}}`
	var b strings.Builder
	b.WriteString(`{"type":"session_meta","payload":{"session_id":"sess","cwd":"/w"}}` + "\n")
	b.WriteString(oldEvent + "\n")
	for i := 0; i < 40; i++ { // >512 bytes of filler between the two events
		b.WriteString(`{"type":"event_msg","payload":{"type":"token_count","info":{}}}` + "\n")
	}
	b.WriteString(newEvent + "\n")
	path := filepath.Join(dir, "rollout-tail.jsonl")
	if err := os.WriteFile(path, []byte(b.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	if info, _ := os.Stat(path); info.Size() <= maxRolloutScanBytes {
		t.Fatalf("test file %d bytes must exceed the %d-byte cap", info.Size(), maxRolloutScanBytes)
	}

	snaps, found := NewestRateLimits(context.Background(), home, time.Unix(1000, 0).UTC())
	if !found || len(snaps) != 1 {
		t.Fatalf("found=%v snaps=%+v, want the single newest (tail) window", found, snaps)
	}
	if got := *snaps[0].Used; got != 80 {
		t.Fatalf("Used = %v, want 80 (the tail event), not the stale 10 from before the cap", got)
	}
}

// TestNewestRateLimits_TailBoundaryKeepsWholeRecord locks in the GH #97 cycle-3
// boundary fix: when the tail window begins EXACTLY at a record boundary (the
// byte before it is the newline ending the prior record), the first whole record
// must survive. Seeking to the window start and discarding a line would wrongly
// drop it; seeking to start-1 discards only the empty boundary line.
func TestNewestRateLimits_TailBoundaryKeepsWholeRecord(t *testing.T) {
	newEvent := `{"type":"event_msg","payload":{"type":"token_count","rate_limits":{"limit_id":"codex","primary":{"used_percent":77,"window_minutes":10080,"resets_at":2000000000}}}}`

	orig := maxRolloutScanBytes
	// Cap == the tail event's byte length (incl. its trailing newline), so the tail
	// window starts precisely where newEvent begins.
	maxRolloutScanBytes = int64(len(newEvent) + 1)
	defer func() { maxRolloutScanBytes = orig }()

	home := t.TempDir()
	dir := filepath.Join(home, "sessions", "2026", "07", "18")
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatal(err)
	}
	// prefix ends with a newline; file = prefix + newEvent + "\n", so the byte at
	// (size - cap - 1) is prefix's final newline — a clean boundary.
	prefix := `{"type":"session_meta","payload":{"cwd":"/w"}}` + "\n" +
		`{"type":"event_msg","payload":{"type":"token_count","info":{}}}` + "\n"
	path := filepath.Join(dir, "rollout-boundary.jsonl")
	if err := os.WriteFile(path, []byte(prefix+newEvent+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	snaps, found := NewestRateLimits(context.Background(), home, time.Unix(1000, 0).UTC())
	if !found || len(snaps) != 1 {
		t.Fatalf("found=%v snaps=%+v, want the boundary-aligned tail record kept", found, snaps)
	}
	if got := *snaps[0].Used; got != 77 {
		t.Fatalf("Used = %v, want 77 — the whole boundary record was dropped", got)
	}
}

// TestNewestRateLimits_ExactlyCapPlusOne covers the cap+1 edge (GH #97 cycle-4):
// a file one byte over the cap must still tail correctly — the first (byte-0)
// record is discarded as outside the window and the trailing rate_limits event
// is returned rather than reading the whole file as if untailed.
func TestNewestRateLimits_ExactlyCapPlusOne(t *testing.T) {
	tailEvent := `{"type":"event_msg","payload":{"type":"token_count","rate_limits":{"limit_id":"codex","primary":{"used_percent":63,"window_minutes":10080,"resets_at":2000000000}}}}`
	head := `{"type":"session_meta","payload":{"cwd":"/w"}}` + "\n"
	content := head + tailEvent + "\n"

	orig := maxRolloutScanBytes
	maxRolloutScanBytes = int64(len(content) - 1) // size == cap+1
	defer func() { maxRolloutScanBytes = orig }()

	home := t.TempDir()
	dir := filepath.Join(home, "sessions", "2026", "07", "18")
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "rollout-capplus1.jsonl"), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	snaps, found := NewestRateLimits(context.Background(), home, time.Unix(1000, 0).UTC())
	if !found || len(snaps) != 1 || *snaps[0].Used != 63 {
		t.Fatalf("found=%v snaps=%+v, want the trailing rate_limits (63%%) at size cap+1", found, snaps)
	}
}
