package daemon

import (
	"io"
	"log/slog"
	"path/filepath"
	"testing"

	"github.com/aoagents/agent-orchestrator/backend/internal/config"
)

func quietLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

// gateConfig builds the only two fields listenSupervisor reads.
func gateConfig(t *testing.T, owner string) config.Config {
	t.Helper()
	return config.Config{Owner: owner, RunFilePath: filepath.Join(t.TempDir(), "running.json")}
}

// The watchdog exists so the desktop app does not orphan the daemon it spawned.
// A daemon nobody spawned — headless `ao daemon` under systemd, an
// AO_KEEP_DAEMON daemon — must never self-stop, and the only way to guarantee
// that regardless of what connects is to not install the listener at all.
//
// This exercises the same function daemon.Run calls, so reverting the gate to
// an unconditional supervisor.Listen fails here.
func TestSupervisorListenerInstalledOnlyForAppOwnedDaemon(t *testing.T) {
	for _, owner := range []string{"persistent", "", "something-else", "App", "app "} {
		t.Run("owner="+owner, func(t *testing.T) {
			if ln := listenSupervisor(gateConfig(t, owner), quietLogger()); ln != nil {
				_ = ln.Close()
				t.Fatalf("owner %q installed the frontend-death watchdog", owner)
			}
		})
	}

	// Control: the app-owned path is unchanged and still gets a listener.
	ln := listenSupervisor(gateConfig(t, config.OwnerApp), quietLogger())
	if ln == nil {
		t.Fatal("app-owned daemon did not install the watchdog")
	}
	_ = ln.Close()
}
