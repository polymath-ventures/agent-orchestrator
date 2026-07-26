package tmux

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

// TestIntegrationSocketLivesUnderDataDir is the end-to-end reproduction of
// #160: a live session's tmux socket must be AO-owned state under the data
// dir. Before the fix the session landed on tmux's default
// /tmp/tmux-$UID/default, so wiping /tmp orphaned the whole fleet.
func TestIntegrationSocketLivesUnderDataDir(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux unavailable")
	}

	// A short base dir keeps the socket well inside the sun_path limit.
	dataDir, err := os.MkdirTemp("", "aosock")
	if err != nil {
		t.Fatalf("temp data dir: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dataDir) })

	socket := SocketPath(dataDir)
	ctx := context.Background()
	id := strings.ReplaceAll(t.Name(), "/", "_")
	r := New(Options{Timeout: 10 * time.Second, DataDir: dataDir})

	t.Cleanup(func() {
		_ = r.Destroy(context.Background(), ports.RuntimeHandle{ID: id})
	})

	h, err := r.Create(ctx, ports.RuntimeConfig{
		SessionID:     domain.SessionID(id),
		WorkspacePath: t.TempDir(),
		Argv:          []string{"sh", "-c", "echo hello-from-tmux"},
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	info, err := os.Stat(socket)
	if err != nil {
		t.Fatalf("no tmux socket at %s: %v — the session is on tmux's default /tmp socket", socket, err)
	}
	if info.Mode()&os.ModeSocket == 0 {
		t.Fatalf("%s mode = %v, want a unix socket", socket, info.Mode())
	}
	if perm := mustStatDirPerm(t, filepath.Dir(socket)); perm != 0o700 {
		t.Fatalf("socket dir mode = %04o, want 0700", perm)
	}

	// The session must be reachable on the AO socket specifically — not merely
	// reachable via whatever socket tmux picked by default.
	if out, err := exec.CommandContext(ctx, "tmux", "-S", socket, "has-session", "-t", "="+id).CombinedOutput(); err != nil {
		t.Fatalf("has-session on %s: %v (%s)", socket, err, strings.TrimSpace(string(out)))
	}

	alive, err := r.IsAlive(ctx, h)
	if err != nil || !alive {
		t.Fatalf("IsAlive = %v, %v; want true, nil", alive, err)
	}
}

func mustStatDirPerm(t *testing.T, dir string) os.FileMode {
	t.Helper()
	info, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("stat %s: %v", dir, err)
	}
	return info.Mode().Perm()
}
