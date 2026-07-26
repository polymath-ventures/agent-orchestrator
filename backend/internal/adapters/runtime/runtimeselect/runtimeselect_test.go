package runtimeselect

import (
	"runtime"
	"testing"

	"github.com/aoagents/agent-orchestrator/backend/internal/adapters/runtime/tmux"
)

// TestNewAnchorsTmuxSocketToDataDir is the wiring half of issue #160: the
// daemon's only runtime constructor must hand the data dir to tmux, otherwise
// sessions land back on tmux's default /tmp socket and a /tmp sweep orphans the
// fleet.
func TestNewAnchorsTmuxSocketToDataDir(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("conpty has no socket")
	}

	selected := New("/var/lib/ao-test", nil)
	rt, ok := selected.(*tmux.Runtime)
	if !ok {
		t.Fatalf("New returned %T, want *tmux.Runtime on %s", selected, runtime.GOOS)
	}
	if got, want := rt.Socket(), tmux.SocketPath("/var/lib/ao-test"); got != want {
		t.Fatalf("socket = %q, want %q", got, want)
	}
}
