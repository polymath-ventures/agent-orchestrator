package cli

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/runfile"
)

// `ao start` does not start a daemon — it fetches and opens the desktop app
// (see start.go). On a web-first deployment the operator never runs that app,
// so the product's most common error told them to launch something they do not
// use, and the action it named would not have fixed what it reported.
func TestDaemonDownErrorNamesAnActionThatStartsADaemon(t *testing.T) {
	dir := t.TempDir()
	runFile := filepath.Join(dir, "running.json")
	t.Setenv("AO_RUN_FILE", runFile)
	t.Setenv("AO_DATA_DIR", filepath.Join(dir, "data"))

	c := &commandContext{deps: Deps{ProcessAlive: func(int) bool { return false }}.withDefaults()}

	t.Run("no run file", func(t *testing.T) {
		_, err := c.daemonURL("/api/v1/fleet")
		assertDaemonDownMessage(t, err)
	})

	t.Run("stale run file", func(t *testing.T) {
		if err := runfile.Write(runFile, runfile.Info{PID: 999999, Port: 3001, StartedAt: time.Now().UTC()}); err != nil {
			t.Fatal(err)
		}
		_, err := c.daemonURL("/api/v1/fleet")
		assertDaemonDownMessage(t, err)
		if !strings.Contains(err.Error(), runFile) {
			t.Fatalf("stale message %q does not name the run file", err)
		}
	})
}

func assertDaemonDownMessage(t *testing.T, err error) {
	t.Helper()
	if err == nil {
		t.Fatal("expected a daemon-down error")
	}
	msg := err.Error()
	if !strings.Contains(msg, daemonDownMarker) {
		t.Fatalf("message %q does not carry the daemon-down marker", msg)
	}
	if strings.Contains(msg, "`ao start`") {
		t.Fatalf("message %q still tells the user to run `ao start`, which starts no daemon", msg)
	}
	if !strings.Contains(msg, "`ao daemon`") {
		t.Fatalf("message %q does not name a command that starts a daemon", msg)
	}
	if !strings.Contains(msg, "systemctl --user start ao.service") {
		t.Fatalf("message %q does not name the systemd action", msg)
	}
}

// The suppression used to re-type the message text, so changing the message
// silently broke it and flooded doctor with restart-window noise. Feed doctor
// the messages the production path actually produces: if the two ever disagree
// again, this fails.
func TestDoctorSuppressesTheDaemonDownMessagesTheCLIProduces(t *testing.T) {
	dir := t.TempDir()
	runFile := filepath.Join(dir, "running.json")
	t.Setenv("AO_RUN_FILE", runFile)
	t.Setenv("AO_DATA_DIR", filepath.Join(dir, "data"))

	c := &commandContext{deps: Deps{ProcessAlive: func(int) bool { return false }}.withDefaults()}

	_, missing := c.daemonURL("/api/v1/fleet")
	if err := runfile.Write(runFile, runfile.Info{PID: 999999, Port: 3001, StartedAt: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	_, stale := c.daemonURL("/api/v1/fleet")

	for _, err := range []error{missing, stale} {
		line := "2026-07-26T00:00:00Z session=s1 ao hooks codex stop: " + err.Error()
		if !isExpectedHookRestartWindowMiss(line) {
			t.Fatalf("doctor no longer suppresses a real daemon-down line: %q", line)
		}
	}

	for _, line := range []string{
		"2026-07-26T00:00:00Z session=s1 ao hooks codex stop: connection refused",
		"2026-07-26T00:00:00Z session=s1 ao hooks codex stop: permission denied",
	} {
		if isExpectedHookRestartWindowMiss(line) {
			t.Fatalf("doctor suppressed an unrelated failure: %q", line)
		}
	}
}

// The marker is the single copy of the sentence: doctor must match it rather
// than hold its own transcription.
func TestDaemonDownMarkerIsNotDuplicated(t *testing.T) {
	if !strings.Contains(daemonDownError("").Error(), daemonDownMarker) {
		t.Fatal("daemonDownError does not use daemonDownMarker")
	}
	if !isExpectedHookRestartWindowMiss(daemonDownError("").Error()) {
		t.Fatal("doctor's filter does not match the marker the message is built from")
	}
}
