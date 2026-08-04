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

// TestProjectIDValidationSplit pins the two-rule contract (#256 + review): a NEW
// id is held to the strict launchable charset, while an already-STORED id is only
// held to traversal safety so a project registered before the rule tightened
// stays manageable.
func TestProjectIDValidationSplit(t *testing.T) {
	// A '.' makes the derived session id tmux-unaddressable, so it must never be
	// admitted as a new id.
	newRejected := []domain.ProjectID{"goodadbadad.net", "a.b", "a."}
	for _, id := range newRejected {
		if err := validateNewProjectID(id); err == nil {
			t.Errorf("validateNewProjectID(%q) = nil, want rejection", id)
		}
	}
	newAccepted := []domain.ProjectID{"goodadbadad-net", "a_b-9", "Project123", "a"}
	for _, id := range newAccepted {
		if err := validateNewProjectID(id); err != nil {
			t.Errorf("validateNewProjectID(%q) = %v, want accepted", id, err)
		}
	}

	// The lookup validator MUST still accept a dotted id: Get/Remove/pause of an
	// already-registered dotted project is exactly how the operator cleans it up.
	// Regressing this back to the strict rule would strand the project (the very
	// recovery path the issue prescribes).
	lookupAccepted := []domain.ProjectID{"goodadbadad.net", "a.b", "goodadbadad-net", "Project123"}
	for _, id := range lookupAccepted {
		if err := validateProjectID(id); err != nil {
			t.Errorf("validateProjectID(%q) = %v, want accepted (existing dotted project must stay manageable)", id, err)
		}
	}
	// But the lookup validator still blocks path-unsafe ids.
	lookupRejected := []domain.ProjectID{"", ".", "..", "a..b", "a/b", `a\b`, "../x", "-lead"}
	for _, id := range lookupRejected {
		if err := validateProjectID(id); err == nil {
			t.Errorf("validateProjectID(%q) = nil, want rejection (path-unsafe)", id)
		}
	}
}

// TestNewProjectIDCharsetSurvivesTmux is the requirement-4 regression guard.
// Rather than test one sample id, it derives every ASCII character
// validateNewProjectID admits — probed in BOTH the leading and a remainder
// position, and including space — and asserts a tmux session named with all of
// them survives a create → list round-trip byte-for-byte. tmux silently rewrites
// "." and ":" to "_" in a session name (they are its target grammar,
// session:window.pane), leaving the name unaddressable — so any character
// re-added to the strict validator that tmux does not preserve fails here
// automatically, wherever in the id it was admitted.
func TestNewProjectIDCharsetSurvivesTmux(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux unavailable")
	}

	// Probe the admitted charset through the validator itself, so this guard
	// tracks the rule rather than duplicating it. Scan 0x20 (space) .. 0x7e in
	// both positions.
	var lead byte
	admitted := map[byte]bool{}
	for c := 0x20; c <= 0x7e; c++ {
		b := byte(c)
		if validateNewProjectID(domain.ProjectID(string(rune(b)))) == nil {
			admitted[b] = true
			if lead == 0 {
				lead = b // first char admitted in the leading position
			}
		}
		if validateNewProjectID(domain.ProjectID("a"+string(rune(b)))) == nil {
			admitted[b] = true
		}
	}
	if lead == 0 {
		t.Fatal("no character admitted in the leading position; probe is broken")
	}
	// Build a name that begins with an admitted leading char and contains every
	// admitted char exactly once, in a stable order.
	var b strings.Builder
	b.WriteByte(lead)
	for c := 0x20; c <= 0x7e; c++ {
		if byte(c) != lead && admitted[byte(c)] {
			b.WriteByte(byte(c))
		}
	}
	name := b.String()

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	socket := filepath.Join(t.TempDir(), "tmux.sock")
	tmux := func(ctx context.Context, args ...string) ([]byte, error) {
		full := append([]string{"-S", socket}, args...)
		return exec.CommandContext(ctx, "tmux", full...).CombinedOutput()
	}
	// Cleanup runs AFTER the test function returns, by which point the deferred
	// cancel() above has already cancelled ctx — reusing it would make kill-server
	// a no-op and leak the tmux server. Use a fresh context.
	t.Cleanup(func() {
		kctx, kcancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer kcancel()
		_, _ = tmux(kctx, "kill-server")
	})

	if out, err := tmux(ctx, "new-session", "-d", "-s", name); err != nil {
		t.Fatalf("tmux new-session -s %q: %v\n%s", name, err, out)
	}
	out, err := tmux(ctx, "list-sessions", "-F", "#{session_name}")
	if err != nil {
		t.Fatalf("tmux list-sessions: %v\n%s", err, out)
	}
	got := strings.TrimSpace(string(out))
	if got != name {
		t.Errorf("tmux rewrote a validated project-id charset: got %q, want %q\n"+
			"a character admitted by the strict validator is not preserved by tmux; drop it", got, name)
	}
}
