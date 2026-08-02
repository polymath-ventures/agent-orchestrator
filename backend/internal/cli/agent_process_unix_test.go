//go:build !windows

package cli

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/creack/pty"
)

func TestAgentProcessSuperviseReportsFailureAndPreservesOutput(t *testing.T) {
	cfg := setConfigEnv(t)
	srv, capture := activityServer(t, http.StatusOK, `{"ok":true}`)
	writeRunFileFor(t, cfg, srv)
	t.Setenv("TMUX_PANE", "%7")

	out, errOut, err := executeCLI(t, Deps{
		In:           strings.NewReader(""),
		ProcessAlive: func(int) bool { return true },
		CommandOutput: func(_ context.Context, name string, args ...string) ([]byte, error) {
			want := []string{"capture-pane", "-p", "-t", "%7", "-S", "-100"}
			if name != "tmux" || !reflect.DeepEqual(args, want) {
				t.Fatalf("capture command = %s %v, want tmux %v", name, args, want)
			}
			return []byte("session collision"), nil
		},
	}, "agent-process", "supervise", "--session", "ao-7", "--launch", "launch-3", "--", "sh", "-c", "printf supervised; printf 'session collision' >&2; exit 23")
	if err != nil {
		t.Fatalf("supervise returned child exit as command failure: %v\nstderr=%s", err, errOut)
	}
	if out != "supervised" {
		t.Fatalf("stdout = %q, want supervised", out)
	}
	if errOut != "session collision" {
		t.Fatalf("stderr = %q, want child stderr preserved", errOut)
	}
	var req setActivityAPIRequest
	if err := json.Unmarshal([]byte(capture.body), &req); err != nil {
		t.Fatal(err)
	}
	want := setActivityAPIRequest{State: "exited", Event: "process-exited", LaunchID: "launch-3", Error: "session collision"}
	if !reflect.DeepEqual(req, want) {
		t.Fatalf("exit report = %+v, want %+v", req, want)
	}
}

func TestAgentProcessSuperviseSuccessfulExitOmitsError(t *testing.T) {
	cfg := setConfigEnv(t)
	srv, capture := activityServer(t, http.StatusOK, `{"ok":true}`)
	writeRunFileFor(t, cfg, srv)

	_, errOut, err := executeCLI(t, Deps{
		In:           strings.NewReader(""),
		ProcessAlive: func(int) bool { return true },
	}, "agent-process", "supervise", "--session", "ao-7", "--launch", "launch-3", "--", "sh", "-c", "printf normal >&2")
	if err != nil {
		t.Fatalf("supervise returned child exit as command failure: %v\nstderr=%s", err, errOut)
	}
	var req setActivityAPIRequest
	if err := json.Unmarshal([]byte(capture.body), &req); err != nil {
		t.Fatal(err)
	}
	if req.Error != "" {
		t.Fatalf("success exit error = %q, want empty", req.Error)
	}
}

func TestAgentProcessSuperviseDoesNotWaitForDescendantHoldingStderr(t *testing.T) {
	cfg := setConfigEnv(t)
	srv, _ := activityServer(t, http.StatusOK, `{"ok":true}`)
	writeRunFileFor(t, cfg, srv)
	t.Setenv("TMUX_PANE", "")
	stderr, err := os.CreateTemp(t.TempDir(), "agent-stderr-*")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = stderr.Close() })
	cmd := NewRootCommand(Deps{
		In:           strings.NewReader(""),
		Out:          io.Discard,
		Err:          stderr,
		ProcessAlive: func(int) bool { return true },
	})
	cmd.SetArgs([]string{"agent-process", "supervise", "--session", "ao-7", "--launch", "launch-3", "--", "sh", "-c", "printf 'launch failed' >&2; sleep 2 >&2 & exit 23"})

	done := make(chan error, 1)
	go func() {
		done <- cmd.Execute()
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("supervise returned child exit as command failure: %v", err)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("supervise waited for a descendant that inherited stderr")
	}
}

func TestAgentProcessSupervisePreservesChildStderrTTY(t *testing.T) {
	cfg := setConfigEnv(t)
	srv, capture := activityServer(t, http.StatusOK, `{"ok":true}`)
	writeRunFileFor(t, cfg, srv)

	master, slave, err := pty.Open()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = master.Close()
		_ = slave.Close()
	})
	cmd := NewRootCommand(Deps{
		In:           strings.NewReader(""),
		Out:          io.Discard,
		Err:          slave,
		ProcessAlive: func(int) bool { return true },
	})
	cmd.SetArgs([]string{"agent-process", "supervise", "--session", "ao-7", "--launch", "launch-3", "--", "sh", "-c", "test -t 2"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}

	var req setActivityAPIRequest
	if err := json.Unmarshal([]byte(capture.body), &req); err != nil {
		t.Fatal(err)
	}
	if req.Error != "" {
		t.Fatalf("child stderr was not a TTY: exit report error = %q", req.Error)
	}
}

func TestAgentProcessSuperviseDoesNotPersistLateProcessFailureOutput(t *testing.T) {
	cfg := setConfigEnv(t)
	srv, capture := activityServer(t, http.StatusOK, `{"ok":true}`)
	writeRunFileFor(t, cfg, srv)
	t.Setenv("TMUX_PANE", "")

	startedAt := time.Unix(100, 0)
	nowCalls := 0
	deps := DefaultDeps()
	deps.In = strings.NewReader("")
	deps.Out = io.Discard
	deps.Err = io.Discard
	deps.ProcessAlive = func(int) bool { return true }
	deps.Now = func() time.Time {
		nowCalls++
		if nowCalls == 1 {
			return startedAt
		}
		return startedAt.Add(supervisedLaunchErrorWindow + time.Second)
	}
	(&commandContext{deps: deps}).runSupervisedProcess(context.Background(), "ao-7", "launch-3", []string{"sh", "-c", "printf old-warning >&2; exit 23"})

	var req setActivityAPIRequest
	if err := json.Unmarshal([]byte(capture.body), &req); err != nil {
		t.Fatal(err)
	}
	if req.Error != "" {
		t.Fatalf("late process exit error = %q, want no launch diagnostic", req.Error)
	}
}

func TestTailBufferKeepsBoundedStderrTail(t *testing.T) {
	b := &tailBuffer{limit: 4}
	if _, err := b.Write([]byte("abc")); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Write([]byte("def")); err != nil {
		t.Fatal(err)
	}
	if got := b.String(); got != "cdef" {
		t.Fatalf("tail = %q, want cdef", got)
	}
}

func TestAgentProcessSuperviseRejectsInvalidGeneration(t *testing.T) {
	_, _, err := executeCLI(t, Deps{}, "agent-process", "supervise", "--session", "ao-7", "--launch", "../stale", "--", "true")
	if err == nil {
		t.Fatal("invalid launch id should be rejected before starting the child")
	}
}
