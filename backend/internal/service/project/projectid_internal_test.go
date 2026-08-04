package project

import (
	"context"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
)

// TestValidateProjectIDRejectsDot pins the reported defect (#256): a project id
// containing a single "." passes validation, propagates into the session id
// ({project}-{num}-{generation}), and is then rejected downstream by
// cli.sessionIDPattern at `agent-process supervise` — every session in the
// project dies with "invalid session id". The id must be rejected where it
// enters the system.
func TestValidateProjectIDRejectsDot(t *testing.T) {
	rejected := []domain.ProjectID{
		"goodadbadad.net", // the reported id
		"a.b",             // a bare single dot
		"a.",              // trailing dot
	}
	for _, id := range rejected {
		if err := validateProjectID(id); err == nil {
			t.Errorf("validateProjectID(%q) = nil, want rejection: a '.' makes the session id tmux-unaddressable", id)
		}
	}

	// Ids over the intersection charset [A-Za-z0-9_-] must still be accepted —
	// the fix narrows the class, it does not forbid legitimate ids.
	accepted := []domain.ProjectID{
		"goodadbadad-net",
		"a_b-9",
		"Project123",
	}
	for _, id := range accepted {
		if err := validateProjectID(id); err != nil {
			t.Errorf("validateProjectID(%q) = %v, want accepted", id, err)
		}
	}
}

// TestProjectIDCharsetSurvivesTmux is the requirement-4 regression guard. Rather
// than test one sample id, it derives every ASCII character validateProjectID
// admits and asserts a tmux session named with all of them survives a
// create → list round-trip byte-for-byte. tmux silently rewrites "." and ":" to
// "_" in a session name (they are its target grammar, session:window.pane),
// leaving the name unaddressable — so any character re-added to projectIDPattern
// that tmux does not preserve fails here automatically.
func TestProjectIDCharsetSurvivesTmux(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux unavailable")
	}

	// Probe the admitted charset through the validator itself, so this guard
	// tracks the pattern rather than duplicating it. A leading alphanumeric
	// satisfies the first-character rule; the second position exercises the
	// remainder class.
	var admitted []byte
	for c := 0x21; c < 0x7f; c++ {
		b := byte(c)
		if validateProjectID(domain.ProjectID("a"+string(rune(b)))) == nil {
			admitted = append(admitted, b)
		}
	}
	if len(admitted) == 0 {
		t.Fatal("no characters admitted by validateProjectID; probe is broken")
	}
	name := "a" + string(admitted)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	socket := filepath.Join(t.TempDir(), "tmux.sock")
	tmux := func(args ...string) ([]byte, error) {
		full := append([]string{"-S", socket}, args...)
		return exec.CommandContext(ctx, "tmux", full...).CombinedOutput()
	}
	t.Cleanup(func() { _, _ = tmux("kill-server") })

	if out, err := tmux("new-session", "-d", "-s", name); err != nil {
		t.Fatalf("tmux new-session -s %q: %v\n%s", name, err, out)
	}
	out, err := tmux("list-sessions", "-F", "#{session_name}")
	if err != nil {
		t.Fatalf("tmux list-sessions: %v\n%s", err, out)
	}
	got := strings.TrimSpace(string(out))
	if got != name {
		t.Errorf("tmux rewrote a validated project-id charset: got %q, want %q\n"+
			"a character admitted by projectIDPattern is not preserved by tmux; drop it from the pattern", got, name)
	}
}
