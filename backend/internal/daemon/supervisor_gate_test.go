package daemon

import (
	"net"
	"os"
	"path/filepath"
	"testing"

	"github.com/aoagents/agent-orchestrator/backend/internal/config"
	"github.com/aoagents/agent-orchestrator/backend/internal/daemon/supervisor"
)

// The supervisor watchdog exists so the desktop app does not orphan the daemon
// it spawned. A daemon nobody spawned — headless `ao daemon` under systemd, a
// keep-alive daemon — must never self-stop, and the only way to guarantee that
// is to not install the mechanism at all. Installing it and relying on the
// client to decline the link leaves the socket open for anyone to arm.
func TestSupervisorInstalledOnlyForAppOwnedDaemon(t *testing.T) {
	for _, tc := range []struct {
		name  string
		owner string
		want  bool
	}{
		{"app-owned desktop daemon", config.OwnerApp, true},
		{"keep-alive daemon", "persistent", false},
		{"headless daemon", "", false},
		{"unrecognised owner", "something-else", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := supervisorEnabled(tc.owner); got != tc.want {
				t.Fatalf("supervisorEnabled(%q) = %v, want %v", tc.owner, got, tc.want)
			}
		})
	}
}

// The guarantee has to be structural: with the watchdog not installed there is
// no socket, so nothing that connects can arm it — no matter what connects, how
// often, or whether it speaks the handshake.
func TestNonAppOwnedDaemonExposesNoSupervisorSocket(t *testing.T) {
	dir := t.TempDir()
	runFile := filepath.Join(dir, "running.json")
	sockPath := filepath.Join(dir, "supervise.sock")

	for _, owner := range []string{"", "persistent", "something-else"} {
		t.Run("owner="+owner, func(t *testing.T) {
			if supervisorEnabled(owner) {
				t.Fatalf("owner %q would install the watchdog", owner)
			}
			// Nothing installed the listener, so the socket must not exist and a
			// client cannot connect to arm anything.
			if _, err := os.Stat(sockPath); !os.IsNotExist(err) {
				t.Fatalf("supervise.sock exists for a non-app-owned daemon: %v", err)
			}
			if conn, err := net.Dial("unix", sockPath); err == nil {
				_ = conn.Close()
				t.Fatal("connected to a supervisor socket that must not exist")
			}
		})
	}

	// Control: the app-owned path is unchanged and still creates the socket.
	if !supervisorEnabled(config.OwnerApp) {
		t.Fatal("app-owned daemon would not install the watchdog")
	}
	ln, addr, err := supervisor.Listen(runFile)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ln.Close() }()
	if addr != sockPath {
		t.Fatalf("socket path = %q, want %q", addr, sockPath)
	}
	conn, err := net.Dial("unix", sockPath)
	if err != nil {
		t.Fatalf("app-owned daemon's supervisor socket is not connectable: %v", err)
	}
	_ = conn.Close()
}
