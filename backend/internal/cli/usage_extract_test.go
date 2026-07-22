package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// claudeStopPayload builds a realistic claude-code stop-hook payload naming the
// transcript at tp, with the path correctly JSON-escaped (jsonQuote lives in
// session_test.go). Embedding a raw path breaks on Windows, where the path
// carries backslashes that would form invalid JSON escapes.
func claudeStopPayload(tp string) []byte {
	return []byte(`{"transcript_path":` + jsonQuote(tp) + `}`)
}

// emitAndCommit runs stopUsageDelta and, on a non-nil delta, invokes the commit
// closure to simulate a successful activity POST advancing the cursor. Tests
// that assert cross-stop delta behaviour need the cursor to advance, which now
// only happens on delivery.
func emitAndCommit(e *usageExtractor, agent string, payload []byte) *usageAPIRequest {
	d, commit := e.stopUsageDelta(agent, payload)
	if commit != nil {
		commit()
	}
	return d
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func claudeAssistantLine(in, cacheCreate, cacheRead, out int) string {
	return `{"type":"assistant","message":{"usage":{` +
		`"input_tokens":` + strconv.Itoa(in) +
		`,"cache_creation_input_tokens":` + strconv.Itoa(cacheCreate) +
		`,"cache_read_input_tokens":` + strconv.Itoa(cacheRead) +
		`,"output_tokens":` + strconv.Itoa(out) + `}}}`
}

func want3(t *testing.T, got *usageAPIRequest, in, out, total float64) {
	t.Helper()
	if got == nil {
		t.Fatalf("usage = nil, want in=%v out=%v total=%v", in, out, total)
	}
	if got.InputTokens == nil || *got.InputTokens != in {
		t.Errorf("input = %v, want %v", deref(got.InputTokens), in)
	}
	if got.OutputTokens == nil || *got.OutputTokens != out {
		t.Errorf("output = %v, want %v", deref(got.OutputTokens), out)
	}
	if got.TotalTokens == nil || *got.TotalTokens != total {
		t.Errorf("total = %v, want %v", deref(got.TotalTokens), total)
	}
	if got.CostUSD != nil {
		t.Errorf("cost_usd = %v, want nil (never fabricated)", *got.CostUSD)
	}
}

func deref(p *float64) float64 {
	if p == nil {
		return -1
	}
	return *p
}

// TestUsageFixtures_WindowsPathsAreValidJSON guards the cross-platform break
// that failed only on the Windows CI runner: a raw filesystem path embedded in
// a JSON fixture carries backslashes (C:\Users\...) that form invalid JSON
// escapes. jsonQuote must keep both the claude payload and the codex rollout
// header valid JSON regardless of separator, so this asserts it on every OS.
func TestUsageFixtures_WindowsPathsAreValidJSON(t *testing.T) {
	winPath := `C:\Users\ao\AppData\Local\Temp\t.jsonl`
	if p := claudeStopPayload(winPath); !json.Valid(p) {
		t.Fatalf("claudeStopPayload produced invalid JSON for a Windows path: %s", p)
	}
	meta := `{"type":"session_meta","payload":{"cwd":` + jsonQuote(winPath) + `}}`
	if !json.Valid([]byte(meta)) {
		t.Fatalf("codex session_meta produced invalid JSON for a Windows path: %s", meta)
	}
	var decoded struct {
		TranscriptPath string `json:"transcript_path"`
	}
	if err := json.Unmarshal(claudeStopPayload(winPath), &decoded); err != nil || decoded.TranscriptPath != winPath {
		t.Fatalf("round-trip failed: err=%v path=%q", err, decoded.TranscriptPath)
	}
}

// --- claude-code -----------------------------------------------------------

func TestUsageExtract_ClaudeCumulativeSumsAssistantLines(t *testing.T) {
	dir := t.TempDir()
	tp := filepath.Join(dir, "transcript.jsonl")
	writeFile(t, tp, strings.Join([]string{
		claudeAssistantLine(2, 62292, 0, 667),
		`{"type":"user","message":{"content":"hi"}}`,
		claudeAssistantLine(5, 100, 1000, 50),
	}, "\n")+"\n")

	e := &usageExtractor{dataDir: t.TempDir(), sessionID: "ao-1"}
	got := emitAndCommit(e, "claude-code", claudeStopPayload(tp))
	// input=(2+62292+0)+(5+100+1000)=63399; output=667+50=717; total=64116.
	want3(t, got, 63399, 717, 64116)
}

func TestUsageExtract_ClaudeDeltaOnSecondStop(t *testing.T) {
	dir := t.TempDir()
	tp := filepath.Join(dir, "transcript.jsonl")
	writeFile(t, tp, claudeAssistantLine(10, 90, 0, 20)+"\n")

	dataDir := t.TempDir()
	e := &usageExtractor{dataDir: dataDir, sessionID: "ao-1"}
	payload := claudeStopPayload(tp)

	first := emitAndCommit(e, "claude-code", payload)
	want3(t, first, 100, 20, 120) // input=10+90, output=20

	// Append another assistant turn; the second stop must report only the delta.
	f, err := os.OpenFile(tp, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(claudeAssistantLine(3, 7, 0, 5) + "\n"); err != nil {
		t.Fatal(err)
	}
	_ = f.Close()

	second := emitAndCommit(e, "claude-code", payload)
	want3(t, second, 10, 5, 15) // input=3+7, output=5
}

func TestUsageExtract_ClaudeNoNewUsageEmitsNil(t *testing.T) {
	dir := t.TempDir()
	tp := filepath.Join(dir, "transcript.jsonl")
	writeFile(t, tp, claudeAssistantLine(10, 0, 0, 20)+"\n")

	e := &usageExtractor{dataDir: t.TempDir(), sessionID: "ao-1"}
	payload := claudeStopPayload(tp)
	if emitAndCommit(e, "claude-code", payload) == nil {
		t.Fatal("first stop should emit the cumulative")
	}
	if got := emitAndCommit(e, "claude-code", payload); got != nil {
		t.Fatalf("second stop with no growth = %#v, want nil", got)
	}
}

func TestUsageExtract_MissingDataDirEmitsNil(t *testing.T) {
	dir := t.TempDir()
	tp := filepath.Join(dir, "transcript.jsonl")
	writeFile(t, tp, claudeAssistantLine(10, 0, 0, 20)+"\n")

	e := &usageExtractor{dataDir: "", sessionID: "ao-1"}
	if got := emitAndCommit(e, "claude-code", claudeStopPayload(tp)); got != nil {
		t.Fatalf("usage = %#v, want nil without AO_DATA_DIR", got)
	}
}

func TestUsageExtract_MalformedTranscriptEmitsNil(t *testing.T) {
	e := &usageExtractor{dataDir: t.TempDir(), sessionID: "ao-1"}

	// Missing transcript_path.
	if got := emitAndCommit(e, "claude-code", []byte(`{}`)); got != nil {
		t.Errorf("no transcript_path = %#v, want nil", got)
	}
	// transcript_path points at a nonexistent file.
	if got := emitAndCommit(e, "claude-code", []byte(`{"transcript_path":"/no/such/file.jsonl"}`)); got != nil {
		t.Errorf("missing file = %#v, want nil", got)
	}
	// Garbage lines: no assistant usage found → zero cumulative → zero delta → nil.
	dir := t.TempDir()
	tp := filepath.Join(dir, "t.jsonl")
	writeFile(t, tp, "not json\n{\"type\":\"user\"}\n")
	if got := emitAndCommit(e, "claude-code", claudeStopPayload(tp)); got != nil {
		t.Errorf("garbage transcript = %#v, want nil", got)
	}
}

// A readable transcript with no assistant usage record must be treated as "no
// signal", not a zero cumulative: it must not reset a non-zero cursor and let a
// later valid read re-emit already-delivered usage.
func TestUsageExtract_ClaudeEmptyTranscriptDoesNotResetCursor(t *testing.T) {
	dir := t.TempDir()
	full := filepath.Join(dir, "full.jsonl")
	writeFile(t, full, claudeAssistantLine(100, 0, 0, 20)+"\n") // input 100, output 20

	e := &usageExtractor{dataDir: t.TempDir(), sessionID: "ao-1"}
	want3(t, emitAndCommit(e, "claude-code", claudeStopPayload(full)), 100, 20, 120)

	// A different, usage-less transcript path: no signal → nil, cursor untouched.
	empty := filepath.Join(dir, "empty.jsonl")
	writeFile(t, empty, `{"type":"user","message":{"content":"hi"}}`+"\n")
	if got := emitAndCommit(e, "claude-code", claudeStopPayload(empty)); got != nil {
		t.Fatalf("usage-less transcript = %#v, want nil", got)
	}

	// Re-reading the original transcript must NOT re-emit the already-counted
	// usage (the cursor was not reset by the empty read).
	if got := emitAndCommit(e, "claude-code", claudeStopPayload(full)); got != nil {
		t.Fatalf("cursor was spuriously reset; re-emitted %#v", got)
	}
}

// --- codex -----------------------------------------------------------------

func writeCodexRollout(t *testing.T, codexHome, name, cwd, threadSource string, tokenEvents [][3]int) string {
	t.Helper()
	return writeCodexRolloutDated(t, codexHome, []string{"2026", "07", "18"}, name, cwd, threadSource, tokenEvents)
}

// writeCodexRolloutDated writes a rollout under sessions/<date...>/ so tests can
// exercise the date-partitioned bounded scan.
func writeCodexRolloutDated(t *testing.T, codexHome string, date []string, name, cwd, threadSource string, tokenEvents [][3]int) string {
	t.Helper()
	meta := `{"type":"session_meta","payload":{"session_id":"sess","cwd":` + jsonQuote(cwd)
	if threadSource != "" {
		meta += `,"thread_source":"` + threadSource + `"`
	}
	meta += `}}`
	lines := []string{meta}
	for _, ev := range tokenEvents {
		lines = append(lines, `{"type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{`+
			`"input_tokens":`+strconv.Itoa(ev[0])+`,"cached_input_tokens":0,"output_tokens":`+strconv.Itoa(ev[1])+
			`,"reasoning_output_tokens":0,"total_tokens":`+strconv.Itoa(ev[2])+`}}}}`)
	}
	parts := append([]string{codexHome, "sessions"}, date...)
	parts = append(parts, name)
	path := filepath.Join(parts...)
	writeFile(t, path, strings.Join(lines, "\n")+"\n")
	return path
}

func TestUsageExtract_CodexCumulativeAndDelta(t *testing.T) {
	codexHome := t.TempDir()
	cwd := t.TempDir()
	path := writeCodexRollout(t, codexHome, "rollout-a.jsonl", cwd, "", [][3]int{
		{100, 20, 120},
		{300, 50, 350}, // cumulative/monotonic; last one wins
	})

	e := &usageExtractor{dataDir: t.TempDir(), codexHome: codexHome, cwd: cwd, sessionID: "ao-1"}
	first := emitAndCommit(e, "codex", nil)
	want3(t, first, 300, 50, 350)

	// Append a newer cumulative; second stop emits only the delta.
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(`{"type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":320,"output_tokens":70,"total_tokens":390}}}}` + "\n"); err != nil {
		t.Fatal(err)
	}
	_ = f.Close()

	second := emitAndCommit(e, "codex", nil)
	want3(t, second, 20, 20, 40) // 320-300, 70-50, 390-350
}

func TestUsageExtract_CodexPrefersMainOverSubagentRollout(t *testing.T) {
	codexHome := t.TempDir()
	cwd := t.TempDir()
	// Main rollout with an OLDER mtime; subagent rollout at the same cwd NEWER.
	mainPath := writeCodexRollout(t, codexHome, "rollout-main.jsonl", cwd, "", [][3]int{{100, 10, 110}})
	subPath := writeCodexRollout(t, codexHome, "rollout-sub.jsonl", cwd, "subagent", [][3]int{{9999, 9999, 19998}})

	old := time.Now().Add(-time.Hour)
	newer := time.Now()
	if err := os.Chtimes(mainPath, old, old); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(subPath, newer, newer); err != nil {
		t.Fatal(err)
	}

	e := &usageExtractor{dataDir: t.TempDir(), codexHome: codexHome, cwd: cwd, sessionID: "ao-1"}
	got := emitAndCommit(e, "codex", nil)
	// Main is chosen despite the subagent's newer mtime and larger counts.
	want3(t, got, 100, 10, 110)
}

func TestUsageExtract_CodexNoCwdMatchEmitsNil(t *testing.T) {
	codexHome := t.TempDir()
	writeCodexRollout(t, codexHome, "rollout-a.jsonl", "/some/other/cwd", "", [][3]int{{100, 20, 120}})

	e := &usageExtractor{dataDir: t.TempDir(), codexHome: codexHome, cwd: t.TempDir(), sessionID: "ao-1"}
	if got := emitAndCommit(e, "codex", nil); got != nil {
		t.Fatalf("usage = %#v, want nil when no rollout cwd matches", got)
	}
}

func TestUsageExtract_CodexSubagentFallbackLogs(t *testing.T) {
	codexHome := t.TempDir()
	cwd := t.TempDir()
	writeCodexRollout(t, codexHome, "rollout-sub.jsonl", cwd, "subagent", [][3]int{{50, 5, 55}})

	var logged []string
	e := &usageExtractor{
		dataDir:   t.TempDir(),
		codexHome: codexHome,
		cwd:       cwd,
		sessionID: "ao-1",
		logf:      func(msg string) { logged = append(logged, msg) },
	}
	got := emitAndCommit(e, "codex", nil)
	want3(t, got, 50, 5, 55) // uses the subagent rollout as a fallback
	if len(logged) == 0 || !strings.Contains(logged[0], "subagent") {
		t.Fatalf("expected an ambiguity log mentioning the subagent fallback, got %#v", logged)
	}
}

func TestUsageExtract_CodexMalformedRolloutEmitsNil(t *testing.T) {
	codexHome := t.TempDir()
	cwd := t.TempDir()
	// session_meta matches cwd but there are no token_count events.
	writeCodexRollout(t, codexHome, "rollout-a.jsonl", cwd, "", nil)

	e := &usageExtractor{dataDir: t.TempDir(), codexHome: codexHome, cwd: cwd, sessionID: "ao-1"}
	if got := emitAndCommit(e, "codex", nil); got != nil {
		t.Fatalf("usage = %#v, want nil when rollout has no token_count", got)
	}
}

func TestUsageExtract_CodexQuotaSnapshotsFromRateLimits(t *testing.T) {
	codexHome := t.TempDir()
	cwd := t.TempDir()
	path := writeCodexRollout(t, codexHome, "rollout-a.jsonl", cwd, "", [][3]int{{100, 20, 120}})
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(`{"type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":150,"output_tokens":50,"total_tokens":200}},"rate_limits":{"limit_id":"codex","primary":{"used_percent":92.5,"window_minutes":10080,"resets_at":1785277078},"secondary":{"used_percent":51,"window_minutes":300,"resets_at":1784680000},"plan_type":"pro"}}}` + "\n"); err != nil {
		t.Fatal(err)
	}
	_ = f.Close()

	e := &usageExtractor{codexHome: codexHome, cwd: cwd}
	got := e.codexQuotaSnapshots(time.Unix(200, 0).UTC())
	if len(got) != 2 {
		t.Fatalf("quota snapshots = %+v, want primary and secondary", got)
	}
	if got[0].WindowName != "primary" || got[0].SignalQuality != "exact" {
		t.Fatalf("primary snapshot = %+v", got[0])
	}
	if got[0].Used == nil || *got[0].Used != 92.5 {
		t.Fatalf("primary used = %v, want 92.5", deref(got[0].Used))
	}
	if got[0].Remaining == nil || *got[0].Remaining != 7.5 {
		t.Fatalf("primary remaining = %v, want 7.5", deref(got[0].Remaining))
	}
	if got[0].Limit == nil || *got[0].Limit != 100 {
		t.Fatalf("primary limit = %v, want 100", deref(got[0].Limit))
	}
	if got[0].WindowEnd.Unix() != 1785277078 {
		t.Fatalf("primary reset = %s, want unix 1785277078", got[0].WindowEnd)
	}
	if got[1].WindowName != "secondary" {
		t.Fatalf("secondary snapshot = %+v", got[1])
	}
}

func TestUsageExtract_CodexQuotaSnapshotsIgnoreInvalidRateLimits(t *testing.T) {
	codexHome := t.TempDir()
	cwd := t.TempDir()
	path := writeCodexRollout(t, codexHome, "rollout-a.jsonl", cwd, "", [][3]int{{100, 20, 120}})
	writeFile(t, path, `{"type":"session_meta","payload":{"session_id":"sess","cwd":`+jsonQuote(cwd)+`}}`+"\n"+
		`{"type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":150,"output_tokens":50,"total_tokens":200}},"rate_limits":{"limit_id":"codex","primary":{"used_percent":120,"window_minutes":10080,"resets_at":1785277078},"secondary":{"used_percent":50,"window_minutes":0,"resets_at":1784680000}}}}`+"\n"+
		`{"type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":151,"output_tokens":51,"total_tokens":202}},"rate_limits":{"limit_id":"codex","primary":{"used_percent":50,"window_minutes":10080,"resets_at":0}}}}`+"\n")

	e := &usageExtractor{codexHome: codexHome, cwd: cwd}
	if got := e.codexQuotaSnapshots(time.Unix(200, 0).UTC()); len(got) != 0 {
		t.Fatalf("invalid quota windows should be ignored, got %+v", got)
	}
}

// --- review-cycle-1 regression coverage ------------------------------------

// A failed delivery (commit not called) must not advance the cursor: the next
// turn re-emits the still-undelivered usage rather than losing it.
func TestUsageExtract_CursorNotAdvancedUntilCommit(t *testing.T) {
	dir := t.TempDir()
	tp := filepath.Join(dir, "transcript.jsonl")
	writeFile(t, tp, claudeAssistantLine(10, 90, 0, 20)+"\n")

	e := &usageExtractor{dataDir: t.TempDir(), sessionID: "ao-1"}
	payload := claudeStopPayload(tp)

	// First stop: get the delta but DO NOT commit (simulates a failed POST).
	first, commit := e.stopUsageDelta("claude-code", payload)
	want3(t, first, 100, 20, 120)
	if commit == nil {
		t.Fatal("expected a commit closure for a non-nil delta")
	}

	// Second stop with no new transcript growth still re-emits the full amount,
	// because the cursor was never advanced.
	second, commit2 := e.stopUsageDelta("claude-code", payload)
	want3(t, second, 100, 20, 120)

	// Now commit; a third stop with no growth emits nil (usage delivered once).
	commit2()
	if got := emitAndCommit(e, "claude-code", payload); got != nil {
		t.Fatalf("after commit, no-growth stop = %#v, want nil", got)
	}
}

// A cumulative that drops below the stored cursor (a reused worktree now hosts a
// fresh session/rollout) must reset the baseline and emit nil for that turn, not
// a bogus mixed delta.
func TestUsageExtract_CumulativeDecreaseResetsCursor(t *testing.T) {
	codexHome := t.TempDir()
	cwd := t.TempDir()
	path := writeCodexRollout(t, codexHome, "rollout-a.jsonl", cwd, "", [][3]int{{500, 100, 600}})

	e := &usageExtractor{dataDir: t.TempDir(), codexHome: codexHome, cwd: cwd, sessionID: "ao-1"}
	want3(t, emitAndCommit(e, "codex", nil), 500, 100, 600)

	// Replace the rollout with a fresh, lower cumulative (new session identity).
	writeFile(t, path, `{"type":"session_meta","payload":{"session_id":"sess","cwd":`+jsonQuote(cwd)+`}}`+"\n"+
		`{"type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":30,"output_tokens":5,"total_tokens":35}}}}`+"\n")
	if got := emitAndCommit(e, "codex", nil); got != nil {
		t.Fatalf("decrease should reset and emit nil, got %#v", got)
	}
	// Cursor is now rebased at 35; growth from there emits the new delta only.
	writeFile(t, path, `{"type":"session_meta","payload":{"session_id":"sess","cwd":`+jsonQuote(cwd)+`}}`+"\n"+
		`{"type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":40,"output_tokens":9,"total_tokens":49}}}}`+"\n")
	want3(t, emitAndCommit(e, "codex", nil), 10, 4, 14) // 40-30, 9-5, 49-35
}

// A non-regular transcript path (here a directory, a portable stand-in for a
// FIFO/device) must be rejected so the stop hook never blocks reading it.
func TestUsageExtract_SpecialFileTranscriptEmitsNil(t *testing.T) {
	specialDir := t.TempDir() // a directory is not a regular file
	e := &usageExtractor{dataDir: t.TempDir(), sessionID: "ao-1"}
	if got := emitAndCommit(e, "claude-code", claudeStopPayload(specialDir)); got != nil {
		t.Fatalf("special-file transcript = %#v, want nil", got)
	}
}

// A run of newer EMPTY day dirs must not hide an older, actively-written
// session's rollout — empty days don't count toward the non-empty-day window.
func TestUsageExtract_CodexEmptyNewerDaysDoNotHideMatch(t *testing.T) {
	codexHome := t.TempDir()
	cwd := t.TempDir()
	// Empty newer day dirs (created, but hold no rollout).
	if err := os.MkdirAll(filepath.Join(codexHome, "sessions", "2026", "07", "20"), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(codexHome, "sessions", "2026", "07", "19"), 0o750); err != nil {
		t.Fatal(err)
	}
	// The actual session rollout sits under an older, non-empty day.
	writeCodexRolloutDated(t, codexHome, []string{"2026", "07", "18"}, "rollout-a.jsonl", cwd, "", [][3]int{{7, 3, 10}})

	e := &usageExtractor{dataDir: t.TempDir(), codexHome: codexHome, cwd: cwd, sessionID: "ao-1"}
	want3(t, emitAndCommit(e, "codex", nil), 7, 3, 10)
}

// The scan is bounded: a cwd-matching rollout older than codexRecentDayDirs
// NON-EMPTY days back (behind that many newer non-matching day dirs) is not
// found — proving the locator is bounded rather than a full-tree walk.
func TestUsageExtract_CodexWalkBoundedToRecentDays(t *testing.T) {
	codexHome := t.TempDir()
	cwd := t.TempDir()
	other := t.TempDir()
	// codexRecentDayDirs+1 newer, non-matching, NON-EMPTY day dirs (June 01..15).
	for day := 1; day <= codexRecentDayDirs+1; day++ {
		writeCodexRolloutDated(t, codexHome, []string{"2026", "06", twoDigit(day)}, "rollout-n"+twoDigit(day)+".jsonl", other, "", [][3]int{{1, 1, 2}})
	}
	// The match sits under an even older date, past the non-empty-day window.
	writeCodexRolloutDated(t, codexHome, []string{"2026", "05", "01"}, "rollout-old.jsonl", cwd, "", [][3]int{{100, 20, 120}})

	e := &usageExtractor{dataDir: t.TempDir(), codexHome: codexHome, cwd: cwd, sessionID: "ao-1"}
	if got := emitAndCommit(e, "codex", nil); got != nil {
		t.Fatalf("match beyond the bounded window should not be found, got %#v", got)
	}
}

func twoDigit(n int) string {
	if n < 10 {
		return "0" + strconv.Itoa(n)
	}
	return strconv.Itoa(n)
}

// A per-field cumulative decrease (input drops while total rises — a new session
// identity) must reset the cursor and emit nil, not a mixed delta.
func TestUsageExtract_PerFieldDecreaseResetsCursor(t *testing.T) {
	codexHome := t.TempDir()
	cwd := t.TempDir()
	path := writeCodexRollout(t, codexHome, "rollout-a.jsonl", cwd, "", [][3]int{{100, 10, 110}})

	e := &usageExtractor{dataDir: t.TempDir(), codexHome: codexHome, cwd: cwd, sessionID: "ao-1"}
	want3(t, emitAndCommit(e, "codex", nil), 100, 10, 110)

	// input 100→50 (decrease) while total 110→130 (increase): identity changed.
	writeFile(t, path, `{"type":"session_meta","payload":{"session_id":"sess","cwd":`+jsonQuote(cwd)+`}}`+"\n"+
		`{"type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":50,"output_tokens":80,"total_tokens":130}}}}`+"\n")
	if got := emitAndCommit(e, "codex", nil); got != nil {
		t.Fatalf("per-field decrease should reset and emit nil, got %#v", got)
	}
}

// storeCursor round-trips atomically and leaves no temp file behind.
func TestUsageExtract_StoreCursorAtomicRoundTrip(t *testing.T) {
	dataDir := t.TempDir()
	e := &usageExtractor{dataDir: dataDir, sessionID: "ao-1"}
	if err := e.storeCursor(usageCumulative{Input: 12, Output: 3, Total: 15}); err != nil {
		t.Fatalf("storeCursor: %v", err)
	}
	got := e.loadCursor()
	if got.Input != 12 || got.Output != 3 || got.Total != 15 {
		t.Fatalf("loadCursor = %#v, want {12 3 15}", got)
	}
	entries, err := os.ReadDir(filepath.Join(dataDir, usageCursorDir))
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range entries {
		if strings.Contains(d.Name(), ".tmp") {
			t.Fatalf("leftover temp file after atomic write: %s", d.Name())
		}
	}
}
