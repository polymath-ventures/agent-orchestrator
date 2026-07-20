package cli

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/daemonmeta"
	"github.com/aoagents/agent-orchestrator/backend/internal/runfile"
)

// TestWaitForStoppedKeepsRunFileFromConcurrentStart guards against deleting a
// fresh daemon's handshake: if a concurrent `ao start` replaces running.json
// with a new live PID while we are polling the PID we stopped, waitForStopped
// must report stopped but leave the new run-file intact.
func TestWaitForStoppedKeepsRunFileFromConcurrentStart(t *testing.T) {
	dir := t.TempDir()
	runFile := filepath.Join(dir, "running.json")

	const stoppedPID, newPID = 1111, 2222
	// running.json now belongs to a different, live daemon.
	if err := runfile.Write(runFile, runfile.Info{PID: newPID, Port: 3001, StartedAt: time.Unix(100, 0).UTC()}); err != nil {
		t.Fatal(err)
	}

	c := &commandContext{deps: Deps{
		ProcessAlive: func(pid int) bool { return pid == newPID }, // stoppedPID is dead
		Now:          func() time.Time { return time.Unix(200, 0).UTC() },
		Sleep:        func(time.Duration) {},
	}.withDefaults()}

	st, err := c.waitForStopped(context.Background(), stoppedPID, runFile, dir, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if st.State != stateStopped {
		t.Fatalf("state = %q, want stopped", st.State)
	}

	info, err := runfile.Read(runFile)
	if err != nil {
		t.Fatal(err)
	}
	if info == nil {
		t.Fatal("new daemon's run-file was deleted by stop of a different PID")
		return
	}
	if info.PID != newPID {
		t.Fatalf("run-file PID = %d, want %d (new daemon)", info.PID, newPID)
	}
}

// TestWaitForStoppedRemovesOwnRunFile confirms the normal path still cleans up:
// when the dead PID owns the run-file, it is removed.
func TestWaitForStoppedRemovesOwnRunFile(t *testing.T) {
	dir := t.TempDir()
	runFile := filepath.Join(dir, "running.json")

	const stoppedPID = 1111
	if err := runfile.Write(runFile, runfile.Info{PID: stoppedPID, Port: 3001, StartedAt: time.Unix(100, 0).UTC()}); err != nil {
		t.Fatal(err)
	}

	c := &commandContext{deps: Deps{
		ProcessAlive: func(int) bool { return false },
		Now:          func() time.Time { return time.Unix(200, 0).UTC() },
		Sleep:        func(time.Duration) {},
	}.withDefaults()}

	st, err := c.waitForStopped(context.Background(), stoppedPID, runFile, dir, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if st.State != stateStopped {
		t.Fatalf("state = %q, want stopped", st.State)
	}
	info, err := runfile.Read(runFile)
	if err != nil {
		t.Fatal(err)
	}
	if info != nil {
		t.Fatalf("own run-file should have been removed, got %#v", info)
	}
}

func TestWaitForStoppedWaitsAfterRunFileRemovedUntilProcessExits(t *testing.T) {
	dir := t.TempDir()
	runFile := filepath.Join(dir, "running.json")

	const stoppedPID = 1111
	now := time.Unix(200, 0).UTC()
	aliveChecks := 0
	sleeps := 0
	c := &commandContext{deps: Deps{
		ProcessAlive: func(int) bool {
			aliveChecks++
			return aliveChecks < 3
		},
		Now: func() time.Time {
			return now
		},
		Sleep: func(d time.Duration) {
			sleeps++
			now = now.Add(d)
		},
	}.withDefaults()}

	st, err := c.waitForStopped(context.Background(), stoppedPID, runFile, dir, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if st.State != stateStopped {
		t.Fatalf("state = %q, want stopped", st.State)
	}
	if sleeps == 0 {
		t.Fatal("waitForStopped returned before waiting for process exit")
	}
	if aliveChecks < 3 {
		t.Fatalf("process checks = %d, want at least 3", aliveChecks)
	}
}

// TestWaitForStoppedReportsStoppedWhenRunFileGoneButProcessLingers covers
// issue #2214: once the daemon has removed its run-file (its liveness marker)
// the stop is committed, so if the process is still draining background workers
// past the timeout, waitForStopped must report stopped rather than erroring.
func TestWaitForStoppedReportsStoppedWhenRunFileGoneButProcessLingers(t *testing.T) {
	dir := t.TempDir()
	runFile := filepath.Join(dir, "running.json") // never written: run-file already gone

	const stoppedPID = 1111
	now := time.Unix(200, 0).UTC()
	c := &commandContext{deps: Deps{
		ProcessAlive: func(int) bool { return true }, // process never exits
		Now:          func() time.Time { return now },
		Sleep:        func(d time.Duration) { now = now.Add(d) },
	}.withDefaults()}

	st, err := c.waitForStopped(context.Background(), stoppedPID, runFile, dir, time.Second)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if st.State != stateStopped {
		t.Fatalf("state = %q, want stopped", st.State)
	}
}

func TestStopUsesSystemdStopWhenUnitOwnsDaemonPID(t *testing.T) {
	origReadProcCgroup := readProcCgroup
	readProcCgroup = func(string) ([]byte, error) {
		return nil, os.ErrNotExist
	}
	t.Cleanup(func() { readProcCgroup = origReadProcCgroup })

	cfg := setConfigEnv(t)
	shutdownCalled := make(chan struct{}, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/healthz":
			_, _ = fmt.Fprintf(w, `{"status":"ok","service":%q,"pid":%d}`, daemonmeta.ServiceName, os.Getpid())
		case "/readyz":
			_, _ = fmt.Fprintf(w, `{"status":"ready","service":%q,"pid":%d}`, daemonmeta.ServiceName, os.Getpid())
		case "/shutdown":
			shutdownCalled <- struct{}{}
			http.Error(w, "unexpected shutdown", http.StatusInternalServerError)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	if err := runfile.Write(cfg.runFile, runfile.Info{PID: os.Getpid(), Port: serverPort(t, srv.URL), StartedAt: time.Unix(100, 0).UTC(), ShutdownToken: "token"}); err != nil {
		t.Fatal(err)
	}

	stopped := false
	var systemctlCalls []string
	c := &commandContext{deps: Deps{
		ProcessAlive: func(pid int) bool {
			if pid != os.Getpid() {
				return false
			}
			return !stopped
		},
		LookPath: func(file string) (string, error) {
			if file == "systemctl" {
				return "/bin/systemctl", nil
			}
			return "", fmt.Errorf("unexpected lookup %q", file)
		},
		CommandOutput: func(_ context.Context, name string, args ...string) ([]byte, error) {
			call := name + " " + strings.Join(args, " ")
			systemctlCalls = append(systemctlCalls, call)
			switch strings.Join(args, " ") {
			case "--user is-active --quiet ao.service":
				return nil, nil
			case "--user show ao.service -P MainPID":
				return []byte(fmt.Sprintf("%d\n", os.Getpid())), nil
			case "--user --no-block stop ao.service":
				stopped = true
				_ = runfile.Remove(cfg.runFile)
				return nil, nil
			default:
				return nil, fmt.Errorf("unexpected systemctl call %q", call)
			}
		},
		Now:   func() time.Time { return time.Unix(200, 0).UTC() },
		Sleep: func(time.Duration) {},
	}.withDefaults()}

	st, err := c.stopDaemon(context.Background(), stopOptions{timeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	if st.State != stateStopped {
		t.Fatalf("state = %q, want stopped", st.State)
	}
	select {
	case <-shutdownCalled:
		t.Fatal("stop used /shutdown even though active ao.service owned the daemon PID")
	default:
	}
	want := []string{
		"/bin/systemctl --user is-active --quiet ao.service",
		"/bin/systemctl --user show ao.service -P MainPID",
		"/bin/systemctl --user --no-block stop ao.service",
	}
	if strings.Join(systemctlCalls, "\n") != strings.Join(want, "\n") {
		t.Fatalf("systemctl calls:\n%s\nwant:\n%s", strings.Join(systemctlCalls, "\n"), strings.Join(want, "\n"))
	}
}

func TestStopRefusesDirectShutdownWhenCgroupShowsSystemdOwnership(t *testing.T) {
	origReadProcCgroup := readProcCgroup
	readProcCgroup = func(path string) ([]byte, error) {
		if !strings.Contains(path, fmt.Sprintf("/proc/%d/cgroup", os.Getpid())) {
			t.Fatalf("unexpected cgroup path %q", path)
		}
		return []byte("0::/user.slice/user-1000.slice/user@1000.service/app.slice/ao.service\n"), nil
	}
	t.Cleanup(func() { readProcCgroup = origReadProcCgroup })

	cfg := setConfigEnv(t)
	shutdownCalled := make(chan struct{}, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/healthz":
			_, _ = fmt.Fprintf(w, `{"status":"ok","service":%q,"pid":%d}`, daemonmeta.ServiceName, os.Getpid())
		case "/readyz":
			_, _ = fmt.Fprintf(w, `{"status":"ready","service":%q,"pid":%d}`, daemonmeta.ServiceName, os.Getpid())
		case "/shutdown":
			shutdownCalled <- struct{}{}
			http.Error(w, "unexpected shutdown", http.StatusInternalServerError)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	if err := runfile.Write(cfg.runFile, runfile.Info{PID: os.Getpid(), Port: serverPort(t, srv.URL), StartedAt: time.Unix(100, 0).UTC(), ShutdownToken: "token"}); err != nil {
		t.Fatal(err)
	}

	c := &commandContext{deps: Deps{
		ProcessAlive: func(pid int) bool { return pid == os.Getpid() },
		LookPath: func(file string) (string, error) {
			if file == "systemctl" {
				return "", fmt.Errorf("systemctl unavailable")
			}
			return "", fmt.Errorf("unexpected lookup %q", file)
		},
	}.withDefaults()}

	_, err := c.stopDaemon(context.Background(), stopOptions{timeout: time.Second})
	if err == nil || !strings.Contains(err.Error(), "owned by ao.service") {
		t.Fatalf("stopDaemon error = %v, want ao.service ownership refusal", err)
	}
	select {
	case <-shutdownCalled:
		t.Fatal("stop used /shutdown even though cgroup showed ao.service ownership")
	default:
	}
}

func TestStopDoesNotUseCgroupHintWithoutMatchingSystemdMainPID(t *testing.T) {
	origReadProcCgroup := readProcCgroup
	readProcCgroup = func(string) ([]byte, error) {
		return []byte("0::/user.slice/user-1000.slice/user@1000.service/app.slice/ao.service\n"), nil
	}
	t.Cleanup(func() { readProcCgroup = origReadProcCgroup })

	cfg := setConfigEnv(t)
	shutdownCalled := make(chan struct{}, 1)
	var shutdownSeen atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/healthz":
			_, _ = fmt.Fprintf(w, `{"status":"ok","service":%q,"pid":%d}`, daemonmeta.ServiceName, os.Getpid())
		case "/readyz":
			_, _ = fmt.Fprintf(w, `{"status":"ready","service":%q,"pid":%d}`, daemonmeta.ServiceName, os.Getpid())
		case "/shutdown":
			if got := r.Header.Get(runfile.ShutdownTokenHeader); got != "token" {
				t.Fatalf("shutdown token header = %q, want token", got)
			}
			shutdownSeen.Store(true)
			_ = runfile.Remove(cfg.runFile)
			shutdownCalled <- struct{}{}
			_, _ = fmt.Fprintf(w, `{"status":"shutting_down","service":%q,"pid":%d}`, daemonmeta.ServiceName, os.Getpid())
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	if err := runfile.Write(cfg.runFile, runfile.Info{PID: os.Getpid(), Port: serverPort(t, srv.URL), StartedAt: time.Unix(100, 0).UTC(), ShutdownToken: "token"}); err != nil {
		t.Fatal(err)
	}

	var systemctlCalls []string
	c := &commandContext{deps: Deps{
		ProcessAlive: func(pid int) bool {
			return pid == os.Getpid() && !shutdownSeen.Load()
		},
		LookPath: func(file string) (string, error) {
			if file == "systemctl" {
				return "/bin/systemctl", nil
			}
			return "", fmt.Errorf("unexpected lookup %q", file)
		},
		CommandOutput: func(_ context.Context, name string, args ...string) ([]byte, error) {
			call := name + " " + strings.Join(args, " ")
			systemctlCalls = append(systemctlCalls, call)
			switch strings.Join(args, " ") {
			case "--user is-active --quiet ao.service":
				return nil, nil
			case "--user show ao.service -P MainPID":
				return []byte("999999\n"), nil
			default:
				return nil, fmt.Errorf("unexpected systemctl call %q", call)
			}
		},
		Now:   func() time.Time { return time.Unix(200, 0).UTC() },
		Sleep: func(time.Duration) {},
	}.withDefaults()}

	st, err := c.stopDaemon(context.Background(), stopOptions{timeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	if st.State != stateStopped {
		t.Fatalf("state = %q, want stopped", st.State)
	}
	select {
	case <-shutdownCalled:
	default:
		t.Fatal("stop did not fall back to token shutdown when cgroup was only inherited")
	}
	for _, call := range systemctlCalls {
		if strings.Contains(call, " stop ao.service") {
			t.Fatalf("stop used systemd despite mismatched MainPID; calls:\n%s", strings.Join(systemctlCalls, "\n"))
		}
	}
}

func TestStopDoesNotFallBackWhenActiveSystemdMainPIDInspectionFails(t *testing.T) {
	origReadProcCgroup := readProcCgroup
	readProcCgroup = func(string) ([]byte, error) {
		return nil, os.ErrNotExist
	}
	t.Cleanup(func() { readProcCgroup = origReadProcCgroup })

	cfg := setConfigEnv(t)
	shutdownCalled := make(chan struct{}, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/healthz":
			_, _ = fmt.Fprintf(w, `{"status":"ok","service":%q,"pid":%d}`, daemonmeta.ServiceName, os.Getpid())
		case "/readyz":
			_, _ = fmt.Fprintf(w, `{"status":"ready","service":%q,"pid":%d}`, daemonmeta.ServiceName, os.Getpid())
		case "/shutdown":
			shutdownCalled <- struct{}{}
			http.Error(w, "unexpected shutdown", http.StatusInternalServerError)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	if err := runfile.Write(cfg.runFile, runfile.Info{PID: os.Getpid(), Port: serverPort(t, srv.URL), StartedAt: time.Unix(100, 0).UTC(), ShutdownToken: "token"}); err != nil {
		t.Fatal(err)
	}

	c := &commandContext{deps: Deps{
		ProcessAlive: func(pid int) bool { return pid == os.Getpid() },
		LookPath: func(file string) (string, error) {
			if file == "systemctl" {
				return "/bin/systemctl", nil
			}
			return "", fmt.Errorf("unexpected lookup %q", file)
		},
		CommandOutput: func(_ context.Context, name string, args ...string) ([]byte, error) {
			switch strings.Join(args, " ") {
			case "--user is-active --quiet ao.service":
				return nil, nil
			case "--user show ao.service -P MainPID":
				return nil, fmt.Errorf("dbus timeout")
			default:
				return nil, fmt.Errorf("unexpected systemctl call %q %s", name, strings.Join(args, " "))
			}
		},
	}.withDefaults()}

	_, err := c.stopDaemon(context.Background(), stopOptions{timeout: time.Second})
	if err == nil || !strings.Contains(err.Error(), "inspect ao.service MainPID") {
		t.Fatalf("stopDaemon error = %v, want MainPID inspection failure", err)
	}
	select {
	case <-shutdownCalled:
		t.Fatal("stop used /shutdown after active systemd MainPID inspection failed")
	default:
	}
}

func TestSystemdProbeTimeoutUsesStopTimeoutWithProbeMinimum(t *testing.T) {
	if got := systemdProbeTimeout(10 * time.Second); got != 10*time.Second {
		t.Fatalf("systemdProbeTimeout(10s) = %s, want 10s", got)
	}
	if got := systemdProbeTimeout(time.Millisecond); got != probeTimeout {
		t.Fatalf("systemdProbeTimeout(1ms) = %s, want %s", got, probeTimeout)
	}
	if got := systemdProbeTimeout(0); got != defaultStopTimeout {
		t.Fatalf("systemdProbeTimeout(0) = %s, want %s", got, defaultStopTimeout)
	}
}

func TestRequestShutdownReportsHTTPStatusText(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "forbidden", http.StatusForbidden)
	}))
	defer srv.Close()

	c := &commandContext{deps: Deps{}.withDefaults()}
	err := c.requestShutdown(context.Background(), serverPort(t, srv.URL), "token")
	if err == nil || !strings.Contains(err.Error(), "HTTP 403 Forbidden") {
		t.Fatalf("requestShutdown error = %v, want HTTP 403 Forbidden", err)
	}
}

func TestParseSystemdMainPIDUsesLastNumericLine(t *testing.T) {
	got, err := parseSystemdMainPID([]byte("warning: noisy stderr before\n\n1234\nwarning: noisy stderr after\n"))
	if err != nil {
		t.Fatal(err)
	}
	if got != 1234 {
		t.Fatalf("MainPID = %d, want 1234", got)
	}
}

func TestCgroupContainsSystemdUnit(t *testing.T) {
	data := []byte("0::/user.slice/user-1000.slice/user@1000.service/app.slice/ao.service\n")
	if !cgroupContainsSystemdUnit(data, "ao.service") {
		t.Fatal("expected cgroup to contain ao.service")
	}
	if cgroupContainsSystemdUnit(data, "other.service") {
		t.Fatal("unexpected match for other.service")
	}
	if cgroupContainsSystemdUnit([]byte("0::/app.slice/run-ao.service-x.scope\n"), "ao.service") {
		t.Fatal("unexpected partial segment match for run-ao.service-x.scope")
	}
}
