package tmux

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

// newSocketTestRuntime builds a Runtime whose socket is derived from dataDir,
// with the exec seam faked. legacySocket is left empty so socketFor short-
// circuits: the transitional fallback has its own tests below.
func newSocketTestRuntime(t *testing.T, dataDir string) (*Runtime, *fakeRunner) {
	t.Helper()
	fr := &fakeRunner{}
	r := New(Options{Binary: "tmux-test", Timeout: time.Second, Shell: "/bin/sh", DataDir: dataDir})
	r.runner = fr
	r.enterDelay = 0
	r.legacySocket = ""
	r.reapSessions = (&recordingReaper{}).reap
	return r, fr
}

// assertEveryTmuxCallCarriesSocket checks the -S invariant on every recorded
// tmux invocation. Non-tmux helpers (pgrep) are ignored.
func assertEveryTmuxCallCarriesSocket(t *testing.T, r *Runtime, fr *fakeRunner, want string) {
	t.Helper()
	seen := 0
	for i, call := range fr.calls {
		if call.name != r.binary {
			continue
		}
		seen++
		if len(call.args) < 2 || call.args[0] != "-S" || call.args[1] != want {
			t.Errorf("tmux call[%d] argv = %v, want it to begin with -S %s", i, call.args, want)
		}
	}
	if seen == 0 {
		t.Fatal("no tmux invocations recorded; the assertion would vacuously pass")
	}
}

// TestSocketPathIsUnderDataDir pins the socket location to the AO data dir, so
// a /tmp sweep cannot delete a live session's socket (#160).
func TestSocketPathIsUnderDataDir(t *testing.T) {
	if got, want := SocketPath("/var/lib/ao"), filepath.Join("/var/lib/ao", "run", "tmux", "default"); got != want {
		t.Fatalf("SocketPath = %q, want %q", got, want)
	}
	if got := SocketPath(""); got != "" {
		t.Fatalf("SocketPath(\"\") = %q, want empty (tmux default socket)", got)
	}
}

// TestEveryTmuxInvocationCarriesSocketPath is the regression guard the ticket
// asks for: every operation must reach tmux through the AO-owned socket, so a
// future call site cannot silently fall back to /tmp/tmux-$UID/default.
func TestEveryTmuxInvocationCarriesSocketPath(t *testing.T) {
	dataDir := t.TempDir()
	want := SocketPath(dataDir)

	cases := []struct {
		name    string
		outputs [][]byte
		run     func(t *testing.T, r *Runtime)
	}{
		{
			name:    "Create",
			outputs: [][]byte{nil, []byte("/tmp/ws\n"), nil, nil, nil, nil},
			run: func(t *testing.T, r *Runtime) {
				if _, err := r.Create(context.Background(), ports.RuntimeConfig{
					SessionID:     "sess-1",
					WorkspacePath: "/tmp/ws",
					Argv:          []string{"echo", "hi"},
				}); err != nil {
					t.Fatalf("Create: %v", err)
				}
			},
		},
		{
			name: "Destroy",
			run: func(t *testing.T, r *Runtime) {
				if err := r.Destroy(context.Background(), ports.RuntimeHandle{ID: "sess-1"}); err != nil {
					t.Fatalf("Destroy: %v", err)
				}
			},
		},
		{
			name: "IsAlive",
			run: func(t *testing.T, r *Runtime) {
				if _, err := r.IsAlive(context.Background(), ports.RuntimeHandle{ID: "sess-1"}); err != nil {
					t.Fatalf("IsAlive: %v", err)
				}
			},
		},
		{
			name:    "IsRunningCommand",
			outputs: [][]byte{[]byte("1234\n")},
			run: func(t *testing.T, r *Runtime) {
				if _, err := r.IsRunningCommand(context.Background(), ports.RuntimeHandle{ID: "sess-1"}, "claude"); err != nil {
					t.Fatalf("IsRunningCommand: %v", err)
				}
			},
		},
		{
			name: "SendMessage",
			run: func(t *testing.T, r *Runtime) {
				if err := r.SendMessage(context.Background(), ports.RuntimeHandle{ID: "sess-1"}, "hello"); err != nil {
					t.Fatalf("SendMessage: %v", err)
				}
			},
		},
		{
			name: "Interrupt",
			run: func(t *testing.T, r *Runtime) {
				if err := r.Interrupt(context.Background(), ports.RuntimeHandle{ID: "sess-1"}); err != nil {
					t.Fatalf("Interrupt: %v", err)
				}
			},
		},
		{
			name:    "GetOutput",
			outputs: [][]byte{[]byte("pane text\n")},
			run: func(t *testing.T, r *Runtime) {
				if _, err := r.GetOutput(context.Background(), ports.RuntimeHandle{ID: "sess-1"}, 10); err != nil {
					t.Fatalf("GetOutput: %v", err)
				}
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r, fr := newSocketTestRuntime(t, dataDir)
			fr.outputs = tc.outputs
			tc.run(t, r)
			assertEveryTmuxCallCarriesSocket(t, r, fr, want)
		})
	}
}

// TestAttachCommandCarriesSocketPath covers the one tmux invocation that does
// not go through run(): Attach spawns its argv on a PTY via ptyexec.
func TestAttachCommandCarriesSocketPath(t *testing.T) {
	dataDir := t.TempDir()
	r, _ := newSocketTestRuntime(t, dataDir)

	argv, err := r.attachCommand(context.Background(), ports.RuntimeHandle{ID: "sess-1"})
	if err != nil {
		t.Fatalf("attachCommand: %v", err)
	}
	want := []string{r.binary, "-S", SocketPath(dataDir), "-u", "attach-session", "-t", "sess-1"}
	if strings.Join(argv, " ") != strings.Join(want, " ") {
		t.Fatalf("attach argv = %v, want %v", argv, want)
	}
}

// TestCreateCreatesSocketDirectory0700 — tmux requires the socket's parent
// directory to exist, and it must be no looser than tmux's own /tmp/tmux-$UID
// (0700).
func TestCreateCreatesSocketDirectory0700(t *testing.T) {
	dataDir := t.TempDir()
	r, fr := newSocketTestRuntime(t, dataDir)
	fr.outputs = [][]byte{nil, []byte("/tmp/ws\n"), nil, nil, nil, nil}

	if _, err := r.Create(context.Background(), ports.RuntimeConfig{
		SessionID:     "sess-1",
		WorkspacePath: "/tmp/ws",
		Argv:          []string{"echo", "hi"},
	}); err != nil {
		t.Fatalf("Create: %v", err)
	}

	dir := filepath.Dir(SocketPath(dataDir))
	info, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("socket dir not created: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o700 {
		t.Fatalf("socket dir mode = %04o, want 0700", perm)
	}
}

// -- transitional legacy-socket fallback (#160) --
//
// A tmux server already running on tmux's default /tmp socket is not migrated
// by changing the path. Without a fallback the daemon would stop seeing every
// live session, report them dead, and hand them to the reaper.

func TestSocketForPrefersPrimaryButFallsBackToLegacySession(t *testing.T) {
	dataDir := t.TempDir()
	legacy := filepath.Join(t.TempDir(), "default")
	if err := os.WriteFile(legacy, nil, 0o600); err != nil {
		t.Fatalf("seed legacy socket: %v", err)
	}

	t.Run("session on primary", func(t *testing.T) {
		r, fr := newSocketTestRuntime(t, dataDir)
		r.legacySocket = legacy
		// has-session on the primary socket exits 0 => primary hosts it.
		if got, want := r.socketFor(context.Background(), "sess-1"), SocketPath(dataDir); got != want {
			t.Fatalf("socketFor = %q, want %q", got, want)
		}
		if len(fr.calls) != 1 {
			t.Fatalf("probes = %d, want 1 (primary only)", len(fr.calls))
		}
	})

	t.Run("session on legacy", func(t *testing.T) {
		r, fr := newSocketTestRuntime(t, dataDir)
		r.legacySocket = legacy
		// Primary probe: definitive miss. Legacy probe: exit 0 => legacy hosts it.
		fr.outputs = [][]byte{[]byte("can't find session: sess-1"), nil}
		fr.errs = []error{commandExitError(t, "1"), nil}

		if got := r.socketFor(context.Background(), "sess-1"); got != legacy {
			t.Fatalf("socketFor = %q, want legacy %q", got, legacy)
		}
	})

	t.Run("legacy socket absent: no probe at all", func(t *testing.T) {
		r, fr := newSocketTestRuntime(t, dataDir)
		r.legacySocket = filepath.Join(t.TempDir(), "gone")
		if got, want := r.socketFor(context.Background(), "sess-1"), SocketPath(dataDir); got != want {
			t.Fatalf("socketFor = %q, want %q", got, want)
		}
		if len(fr.calls) != 0 {
			t.Fatalf("probes = %d, want 0 once the legacy socket is drained", len(fr.calls))
		}
	})
}

// TestOperationsTargetTheLegacySocketForLegacySessions proves the fallback is
// wired through the operations the reaper and supervisor actually call, not
// just through socketFor in isolation.
func TestOperationsTargetTheLegacySocketForLegacySessions(t *testing.T) {
	dataDir := t.TempDir()
	legacy := filepath.Join(t.TempDir(), "default")
	if err := os.WriteFile(legacy, nil, 0o600); err != nil {
		t.Fatalf("seed legacy socket: %v", err)
	}

	r, fr := newSocketTestRuntime(t, dataDir)
	r.legacySocket = legacy
	// socketFor: primary miss, legacy hit. Then the has-session for IsAlive.
	fr.outputs = [][]byte{[]byte("can't find session: sess-1"), nil, nil}
	fr.errs = []error{commandExitError(t, "1"), nil, nil}

	alive, err := r.IsAlive(context.Background(), ports.RuntimeHandle{ID: "sess-1"})
	if err != nil {
		t.Fatalf("IsAlive: %v", err)
	}
	if !alive {
		t.Fatal("IsAlive = false for a session living on the legacy socket; the reaper would kill it")
	}
	last := fr.calls[len(fr.calls)-1]
	if len(last.args) < 2 || last.args[0] != "-S" || last.args[1] != legacy {
		t.Fatalf("final probe argv = %v, want it targeted at the legacy socket %s", last.args, legacy)
	}
}

// TestSocketForDoesNotFallBackOnTransientPrimaryFailure — a timed-out or
// otherwise non-definitive probe on AO's socket says nothing about where the
// session lives. Falling back on it could route a kill-session at a same-named
// session on the legacy socket and report success.
func TestSocketForDoesNotFallBackOnTransientPrimaryFailure(t *testing.T) {
	dataDir := t.TempDir()
	legacy := filepath.Join(t.TempDir(), "default")
	if err := os.WriteFile(legacy, nil, 0o600); err != nil {
		t.Fatalf("seed legacy socket: %v", err)
	}

	r, fr := newSocketTestRuntime(t, dataDir)
	r.legacySocket = legacy
	// A non-ExitError failure is transient by the adapter's own contract.
	fr.outputs = [][]byte{[]byte("lost server")}
	fr.errs = []error{context.DeadlineExceeded}

	if got, want := r.socketFor(context.Background(), "sess-1"), SocketPath(dataDir); got != want {
		t.Fatalf("socketFor = %q, want %q (must not guess the legacy socket)", got, want)
	}
	if len(fr.calls) != 1 {
		t.Fatalf("probes = %d, want 1 — the legacy socket must not be probed after a transient failure", len(fr.calls))
	}
}

// TestIsAliveProbesOnce — IsAlive is on the reaper's hot path (once per running
// session per tick). It and socketFor are two views of one probe, so resolving
// the socket must not cost a probe that IsAlive then repeats.
func TestIsAliveProbesOnce(t *testing.T) {
	dataDir := t.TempDir()
	legacy := filepath.Join(t.TempDir(), "default")
	if err := os.WriteFile(legacy, nil, 0o600); err != nil {
		t.Fatalf("seed legacy socket: %v", err)
	}

	t.Run("alive on primary", func(t *testing.T) {
		r, fr := newSocketTestRuntime(t, dataDir)
		r.legacySocket = legacy

		alive, err := r.IsAlive(context.Background(), ports.RuntimeHandle{ID: "sess-1"})
		if err != nil || !alive {
			t.Fatalf("IsAlive = %v, %v; want true, nil", alive, err)
		}
		if len(fr.calls) != 1 {
			t.Fatalf("probes = %d, want 1", len(fr.calls))
		}
	})

	t.Run("gone from both is a definitive dead", func(t *testing.T) {
		r, fr := newSocketTestRuntime(t, dataDir)
		r.legacySocket = legacy
		fr.outputs = [][]byte{[]byte("can't find session: sess-1"), []byte("can't find session: sess-1")}
		fr.errs = []error{commandExitError(t, "1"), commandExitError(t, "1")}

		// (false, nil) — not an error — or the reaper would treat a dead
		// session as a failed probe and never terminate the row.
		alive, err := r.IsAlive(context.Background(), ports.RuntimeHandle{ID: "sess-1"})
		if alive || err != nil {
			t.Fatalf("IsAlive = %v, %v; want false, nil", alive, err)
		}
		if len(fr.calls) != 2 {
			t.Fatalf("probes = %d, want 2 (primary then legacy)", len(fr.calls))
		}
	})
}

// TestSendMessageResolvesTheSocketOnce — every chunk and the trailing Enter
// must reach the same pane, and re-resolving per chunk doubles the tmux
// invocations on the message hot path.
func TestSendMessageResolvesTheSocketOnce(t *testing.T) {
	dataDir := t.TempDir()
	legacy := filepath.Join(t.TempDir(), "default")
	if err := os.WriteFile(legacy, nil, 0o600); err != nil {
		t.Fatalf("seed legacy socket: %v", err)
	}

	r, fr := newSocketTestRuntime(t, dataDir)
	r.legacySocket = legacy
	r.chunkSize = 4 // force 3 chunks out of a 12-byte message

	if err := r.SendMessage(context.Background(), ports.RuntimeHandle{ID: "sess-1"}, "abcdefghijkl"); err != nil {
		t.Fatalf("SendMessage: %v", err)
	}
	// 1 socket probe + 3 send-keys + 1 Enter.
	if len(fr.calls) != 5 {
		var got []string
		for _, c := range fr.calls {
			got = append(got, strings.Join(c.args, " "))
		}
		t.Fatalf("calls = %d, want 5 (one probe, three chunks, one Enter):\n  %s", len(fr.calls), strings.Join(got, "\n  "))
	}
}

// TestLegacySocketPathMatchesTmuxDefault pins the transitional path to tmux's
// own resolution ($TMUX_TMPDIR, else $TMPDIR, else /tmp) so the fallback finds
// the server AO used to talk to.
func TestLegacySocketPathMatchesTmuxDefault(t *testing.T) {
	uid := os.Geteuid()

	t.Setenv("TMUX_TMPDIR", "")
	t.Setenv("TMPDIR", "")
	if got, want := legacySocketPath(), filepath.Join("/tmp", tmuxSocketDirName(uid), "default"); got != want {
		t.Fatalf("legacySocketPath = %q, want %q", got, want)
	}

	t.Setenv("TMPDIR", "/var/folders/x")
	if got, want := legacySocketPath(), filepath.Join("/var/folders/x", tmuxSocketDirName(uid), "default"); got != want {
		t.Fatalf("legacySocketPath with TMPDIR = %q, want %q", got, want)
	}

	t.Setenv("TMUX_TMPDIR", "/run/user/1002")
	if got, want := legacySocketPath(), filepath.Join("/run/user/1002", tmuxSocketDirName(uid), "default"); got != want {
		t.Fatalf("legacySocketPath with TMUX_TMPDIR = %q, want %q", got, want)
	}
}
