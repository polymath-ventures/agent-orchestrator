//go:build !windows

package cli

import (
	"encoding/json"
	"net/http"
	"reflect"
	"strings"
	"testing"
)

func TestAgentProcessSuperviseReportsFailureAndPreservesOutput(t *testing.T) {
	cfg := setConfigEnv(t)
	srv, capture := activityServer(t, http.StatusOK, `{"ok":true}`)
	writeRunFileFor(t, cfg, srv)

	out, errOut, err := executeCLI(t, Deps{
		In:           strings.NewReader(""),
		ProcessAlive: func(int) bool { return true },
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
