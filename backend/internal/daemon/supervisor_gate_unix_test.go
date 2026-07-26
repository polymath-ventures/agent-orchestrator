//go:build !windows

package daemon

import (
	"net"
	"os"
	"path/filepath"
	"testing"

	"github.com/aoagents/agent-orchestrator/backend/internal/config"
)

// The guarantee has to be structural rather than behavioral: with no socket on
// disk there is nothing to connect to, so "never self-stops regardless of what
// connects" holds without the supervisor ever running. The transport is
// platform-specific (a named pipe on Windows), so the socket assertions live
// here while the gate itself is asserted platform-neutrally.
func TestNonAppOwnedDaemonCreatesNoSupervisorSocket(t *testing.T) {
	for _, owner := range []string{"persistent", "", "something-else"} {
		t.Run("owner="+owner, func(t *testing.T) {
			cfg := gateConfig(t, owner)
			sockPath := filepath.Join(filepath.Dir(cfg.RunFilePath), "supervise.sock")

			if ln := listenSupervisor(cfg, quietLogger()); ln != nil {
				_ = ln.Close()
				t.Fatalf("owner %q installed the watchdog", owner)
			}
			if _, err := os.Stat(sockPath); !os.IsNotExist(err) {
				t.Fatalf("supervise.sock exists for a non-app-owned daemon: %v", err)
			}
			if conn, err := net.Dial("unix", sockPath); err == nil {
				_ = conn.Close()
				t.Fatal("connected to a supervisor socket that must not exist")
			}
		})
	}

	// Control: the app-owned daemon's socket is created and connectable, so the
	// absence above is the gate working and not a broken listener.
	cfg := gateConfig(t, config.OwnerApp)
	sockPath := filepath.Join(filepath.Dir(cfg.RunFilePath), "supervise.sock")
	ln := listenSupervisor(cfg, quietLogger())
	if ln == nil {
		t.Fatal("app-owned daemon did not install the watchdog")
	}
	defer func() { _ = ln.Close() }()
	if _, err := os.Stat(sockPath); err != nil {
		t.Fatalf("app-owned daemon has no supervise.sock: %v", err)
	}
	conn, err := net.Dial("unix", sockPath)
	if err != nil {
		t.Fatalf("app-owned daemon's supervisor socket is not connectable: %v", err)
	}
	_ = conn.Close()
}
