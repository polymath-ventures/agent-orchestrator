package cli

import (
	"errors"
	"io"
	"os"
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

// The recogniser and the builder are the same fact. This pins them together:
// every shape the builder can produce must be recognised, and doctor must go
// through the recogniser rather than hold its own transcription.
func TestDaemonDownMessageIsRecognisedByItsOwnRecogniser(t *testing.T) {
	for _, detail := range []string{"", "stale run-file at /home/u/.ao/running.json"} {
		msg := daemonDownError(detail).Error()
		if !isDaemonDownMessage(msg) {
			t.Fatalf("recogniser missed a message its own builder produced: %q", msg)
		}
		if !isExpectedHookRestartWindowMiss("ts session=s1 ao hooks codex stop: " + msg) {
			t.Fatalf("doctor does not suppress a message the builder produced: %q", msg)
		}
	}
}

// Log lines written by a daemon from before this change still carry the marker
// in the same position, so existing hooks.log history stays suppressed rather
// than turning into a wall of new warnings on upgrade.
func TestLegacyDaemonDownLinesStaySuppressed(t *testing.T) {
	for _, line := range []string{
		"2026-07-01T00:00:00Z session=s1 ao hooks codex stop: AO daemon is not running — start it with `ao start`",
		"2026-07-01T00:00:00Z session=s2 ao hooks claude-code stop: AO daemon is not running (stale run-file at /tmp/ao.json) — start it with `ao start`",
	} {
		if !isExpectedHookRestartWindowMiss(line) {
			t.Fatalf("upgrade turned a suppressed legacy line into a warning: %q", line)
		}
	}
}

// The log line's shape is kept parseable where it is WRITTEN, so a hook invoked
// with an agent or event carrying the separator cannot make a real failure look
// like a suppressed daemon-down one.
func TestHookFailureLineShapeSurvivesSeparatorsInArgv(t *testing.T) {
	for _, argv := range [][2]string{
		{"codex", "x: AO daemon is not running — y"},
		{"x: AO daemon is not running — y", "stop"},
		{"codex", "an event with spaces"},
	} {
		c := &commandContext{deps: Deps{}.withDefaults()}
		c.deps.Err = io.Discard
		t.Setenv("AO_DATA_DIR", t.TempDir())
		c.reportHookFailure(argv[0], argv[1], "s1", errors.New("parse run-file: unexpected end of JSON input"))

		data, err := os.ReadFile(filepath.Join(os.Getenv("AO_DATA_DIR"), hooksLogName))
		if err != nil {
			t.Fatal(err)
		}
		line := strings.TrimSpace(string(data))
		if got := hookFailureCause(line); got != "parse run-file: unexpected end of JSON input" {
			t.Fatalf("cause = %q, want the real error (line %q)", got, line)
		}
		if isExpectedHookRestartWindowMiss(line) {
			t.Fatalf("argv suppressed a real failure: %q", line)
		}
	}
}

// The marker is an ordinary English phrase, and hook failure lines carry
// user-controlled data such as the working directory. Matching it anywhere
// would let a path or an agent's own output masquerade as a daemon-down
// failure and silently suppress a real warning.
func TestQuotedMarkerIsNotMistakenForADaemonDownFailure(t *testing.T) {
	for _, line := range []string{
		"2026-07-26T00:00:00Z session=s1 ao hooks codex stop: codex usage: no main rollout for cwd /tmp/AO daemon is not running; using fallback",
		"2026-07-26T00:00:00Z session=s1 ao hooks codex stop: agent said \"AO daemon is not running\" earlier but the call failed with EPIPE",
		// The marker followed by the message's own delimiters, but not at the
		// start of the cause: still a real failure, still must warn.
		"2026-07-26T00:00:00Z session=s1 ao hooks codex stop: write context: AO daemon is not running (stale) — nope",
		"2026-07-26T00:00:00Z session=s1 ao hooks codex stop: cwd /tmp/AO daemon is not running — x/y is unreadable",
		// Not a hook-failure line at all.
		"AO daemon is not running — start it with `ao daemon`",
		// An argument carrying its own ": " must not shift where the cause
		// appears to begin. `ao hooks` argv is AO's own, but the split should
		// hold by construction rather than by trusting the caller.
		"2026-07-26T00:00:00Z session=s1 ao hooks x: AO daemon is not running — y stop: connection refused",
		"2026-07-26T00:00:00Z session=s1 ao hooks codex stop: connection refused",
	} {
		if isExpectedHookRestartWindowMiss(line) {
			t.Fatalf("suppressed a real hook failure that merely quotes the marker: %q", line)
		}
	}
}
