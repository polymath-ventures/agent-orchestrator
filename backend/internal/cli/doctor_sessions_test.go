package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/runfile"
)

// doctorSessionsContext points the CLI at a stub daemon serving the given
// sessions, and pins "now" so silence is exact rather than wall-clock.
func doctorSessionsContext(t *testing.T, now time.Time, sessions []sessionDTO) (*commandContext, *[]string) {
	t.Helper()
	var requests []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.Method+" "+r.URL.RequestURI())
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(sessionListResponse{Sessions: sessions})
	}))
	t.Cleanup(srv.Close)

	return sessionsContextFor(t, now, srv.URL), &requests
}

// sessionsContextFor writes a run file pointing at url and builds a context
// whose clock is pinned and whose external-command hooks fail the test: the
// check must reach the daemon and nothing else.
func sessionsContextFor(t *testing.T, now time.Time, url string) *commandContext {
	t.Helper()
	port := 0
	if _, err := fmt.Sscanf(strings.TrimPrefix(url, "http://127.0.0.1:"), "%d", &port); err != nil {
		t.Fatalf("parse stub port from %q: %v", url, err)
	}

	dir := t.TempDir()
	runFile := filepath.Join(dir, "running.json")
	t.Setenv("AO_RUN_FILE", runFile)
	t.Setenv("AO_DATA_DIR", filepath.Join(dir, "data"))
	if err := runfile.Write(runFile, runfile.Info{PID: 4242, Port: port, StartedAt: now.Add(-time.Hour)}); err != nil {
		t.Fatal(err)
	}

	return &commandContext{deps: Deps{
		ProcessAlive: func(int) bool { return true },
		Now:          func() time.Time { return now },
		CommandOutput: func(context.Context, string, ...string) ([]byte, error) {
			t.Fatal("the wedged-session check must not invoke any external command")
			return nil, nil
		},
		CommandOutputInDir: func(context.Context, string, string, ...string) ([]byte, error) {
			t.Fatal("the wedged-session check must not invoke any external command")
			return nil, nil
		},
	}.withDefaults()}
}

// doctorSessionsContextSlow points the CLI at a daemon that accepts the
// connection and then never answers.
func doctorSessionsContextSlow(t *testing.T, now time.Time) (*commandContext, *[]string) {
	t.Helper()
	var requests []string
	done := make(chan struct{})
	t.Cleanup(func() { close(done) })
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.Method+" "+r.URL.RequestURI())
		select {
		case <-done:
		case <-r.Context().Done():
		}
	}))
	t.Cleanup(srv.Close)
	return sessionsContextFor(t, now, srv.URL), &requests
}

func liveSession(id string, last time.Time, state string) sessionDTO {
	return sessionDTO{ID: id, Kind: "worker", Status: "running", Activity: sessionActivity{State: state, LastActivityAt: last}}
}

// LastActivityAt records when the CURRENT state was entered, so for an active
// session it measures how long the agent has been active without finishing a
// turn. A healthy agent transitions to idle or waiting_input between turns; one
// blocked on a leaked `curl` never leaves active.
func TestWedgedSessionsWarnsWhenActiveTooLongWithoutATransition(t *testing.T) {
	now := time.Date(2026, 7, 26, 12, 0, 0, 0, time.UTC)
	c, _ := doctorSessionsContext(t, now, []sessionDTO{
		liveSession("mer-wedged", now.Add(-8*time.Hour), "active"),
	})

	check := c.checkWedgedSessions(context.Background())
	if check.Level != doctorWarn {
		t.Fatalf("level = %s, want WARN (%+v)", check.Level, check)
	}
	if !strings.Contains(check.Message, "mer-wedged") {
		t.Fatalf("message does not name the silent session: %q", check.Message)
	}
	if !strings.Contains(check.Message, "8h") {
		t.Fatalf("message does not say how long it has been active: %q", check.Message)
	}
	if !strings.Contains(check.Message, "ao session get") {
		t.Fatalf("message gives the operator no next action: %q", check.Message)
	}
}

// idle means the agent finished a turn; waiting_input and blocked mean it is
// paused on the user, and an operator legitimately leaves those overnight.
// Warning on them would make this check noise an operator learns to ignore.
func TestWedgedSessionsIgnoresSessionsThatAreNotActive(t *testing.T) {
	now := time.Date(2026, 7, 26, 12, 0, 0, 0, time.UTC)
	c, _ := doctorSessionsContext(t, now, []sessionDTO{
		liveSession("mer-idle", now.Add(-20*time.Hour), "idle"),
		liveSession("mer-waiting", now.Add(-20*time.Hour), "waiting_input"),
		liveSession("mer-blocked", now.Add(-20*time.Hour), "blocked"),
		liveSession("mer-exited", now.Add(-20*time.Hour), "exited"),
		liveSession("mer-unknown", now.Add(-20*time.Hour), ""),
	})

	check := c.checkWedgedSessions(context.Background())
	if check.Level != doctorPass {
		t.Fatalf("level = %s, want PASS (%+v)", check.Level, check)
	}
	for _, id := range []string{"mer-idle", "mer-waiting", "mer-blocked", "mer-exited", "mer-unknown"} {
		if strings.Contains(check.Message, id) {
			t.Fatalf("warned about a session that is not active (%s): %q", id, check.Message)
		}
	}
}

// getJSON raises the client timeout to the two-minute command timeout, which
// would let one slow daemon stall the whole doctor report.
func TestWedgedSessionsDoesNotStallDoctorOnASlowDaemon(t *testing.T) {
	now := time.Date(2026, 7, 26, 12, 0, 0, 0, time.UTC)
	c, _ := doctorSessionsContextSlow(t, now)

	start := time.Now()
	check := c.checkWedgedSessions(context.Background())
	if elapsed := time.Since(start); elapsed > 30*time.Second {
		t.Fatalf("check took %s against a slow daemon; it must be bounded by the probe timeout", elapsed)
	}
	if check.Level != doctorPass {
		t.Fatalf("level = %s, want PASS (%+v)", check.Level, check)
	}
	if !strings.Contains(check.Message, "unavailable") {
		t.Fatalf("message does not report the signal as unavailable: %q", check.Message)
	}
}

func TestWedgedSessionsPassesWhenEveryoneIsRecentlyActive(t *testing.T) {
	now := time.Date(2026, 7, 26, 12, 0, 0, 0, time.UTC)
	c, _ := doctorSessionsContext(t, now, []sessionDTO{
		liveSession("mer-a", now.Add(-2*time.Minute), "active"),
		liveSession("mer-b", now.Add(-30*time.Minute), "idle"),
	})

	check := c.checkWedgedSessions(context.Background())
	if check.Level != doctorPass {
		t.Fatalf("level = %s, want PASS (%+v)", check.Level, check)
	}
	for _, id := range []string{"mer-a", "mer-b"} {
		if strings.Contains(check.Message, id) {
			t.Fatalf("passing check named a healthy session %q: %q", id, check.Message)
		}
	}
}

// A terminated session is silent by definition; warning about it would bury the
// live one that matters.
func TestWedgedSessionsIgnoresTerminatedSessions(t *testing.T) {
	now := time.Date(2026, 7, 26, 12, 0, 0, 0, time.UTC)
	// active, so the IsTerminated filter is the only thing keeping it out.
	dead := liveSession("mer-dead", now.Add(-48*time.Hour), "active")
	dead.IsTerminated = true
	c, _ := doctorSessionsContext(t, now, []sessionDTO{dead})

	check := c.checkWedgedSessions(context.Background())
	if check.Level != doctorPass {
		t.Fatalf("level = %s, want PASS (%+v)", check.Level, check)
	}
	if strings.Contains(check.Message, "mer-dead") {
		t.Fatalf("warned about a terminated session: %q", check.Message)
	}
}

func TestWedgedSessionsNamesEverySilentSession(t *testing.T) {
	now := time.Date(2026, 7, 26, 12, 0, 0, 0, time.UTC)
	c, _ := doctorSessionsContext(t, now, []sessionDTO{
		liveSession("mer-one", now.Add(-9*time.Hour), "active"),
		liveSession("mer-two", now.Add(-6*time.Hour), "active"),
		liveSession("mer-fine", now.Add(-time.Minute), "active"),
	})

	check := c.checkWedgedSessions(context.Background())
	if check.Level != doctorWarn {
		t.Fatalf("level = %s, want WARN (%+v)", check.Level, check)
	}
	for _, id := range []string{"mer-one", "mer-two"} {
		if !strings.Contains(check.Message, id) {
			t.Fatalf("message omits silent session %q: %q", id, check.Message)
		}
	}
	if strings.Contains(check.Message, "mer-fine") {
		t.Fatalf("message names an active session: %q", check.Message)
	}
}

// Silence is measured from a starting point. A session that has never recorded
// activity has none, and calling that "infinitely silent" would warn on every
// freshly created session before its first hook lands.
func TestWedgedSessionsSkipsSessionsWithNoRecordedActivity(t *testing.T) {
	now := time.Date(2026, 7, 26, 12, 0, 0, 0, time.UTC)
	c, _ := doctorSessionsContext(t, now, []sessionDTO{
		liveSession("mer-fresh", time.Time{}, "active"),
	})

	check := c.checkWedgedSessions(context.Background())
	if check.Level != doctorPass {
		t.Fatalf("level = %s, want PASS (%+v)", check.Level, check)
	}
	if strings.Contains(check.Message, "mer-fresh") {
		t.Fatalf("warned about a session with no activity timestamp: %q", check.Message)
	}
}

// A missing signal is not evidence of an unhealthy machine, and the `daemon`
// check already reports a daemon that is down.
func TestWedgedSessionsDegradesWhenTheDaemonIsUnreachable(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("AO_RUN_FILE", filepath.Join(dir, "running.json"))
	t.Setenv("AO_DATA_DIR", filepath.Join(dir, "data"))
	c := &commandContext{deps: Deps{
		ProcessAlive: func(int) bool { return false },
		Now:          func() time.Time { return time.Now() },
	}.withDefaults()}

	check := c.checkWedgedSessions(context.Background())
	if check.Level != doctorPass {
		t.Fatalf("level = %s, want PASS when the daemon is unreachable (%+v)", check.Level, check)
	}
	if !strings.Contains(check.Message, "unavailable") {
		t.Fatalf("message does not report the signal as unavailable: %q", check.Message)
	}
}

// The check must read, and only read: an HTTP request against supervise.sock is
// what perturbs daemon lifecycle in the first place (#147).
func TestWedgedSessionsOnlyReadsAndNeverTouchesTheSupervisorSocket(t *testing.T) {
	now := time.Date(2026, 7, 26, 12, 0, 0, 0, time.UTC)
	c, requests := doctorSessionsContext(t, now, []sessionDTO{
		liveSession("mer-one", now.Add(-9*time.Hour), "active"),
	})

	_ = c.checkWedgedSessions(context.Background())

	if len(*requests) == 0 {
		t.Fatal("the check made no request at all")
	}
	for _, req := range *requests {
		if !strings.HasPrefix(req, "GET ") {
			t.Fatalf("the check issued a mutating request: %q", req)
		}
		if strings.Contains(req, "supervise") || strings.Contains(req, "shutdown") {
			t.Fatalf("the check touched a lifecycle endpoint: %q", req)
		}
	}
	// It must ask only for live sessions rather than filtering a full listing
	// client-side, so a fleet with a long terminated history stays cheap.
	if !strings.Contains((*requests)[0], url.QueryEscape("active")+"=true") &&
		!strings.Contains((*requests)[0], "active=true") {
		t.Fatalf("the check did not scope the listing to live sessions: %q", (*requests)[0])
	}
}

// The check has to be part of the report, not merely available.
func TestDoctorReportsTheWedgedSessionsCheck(t *testing.T) {
	now := time.Date(2026, 7, 26, 12, 0, 0, 0, time.UTC)
	c, _ := doctorSessionsContext(t, now, []sessionDTO{
		liveSession("mer-wedged", now.Add(-8*time.Hour), "active"),
	})
	// runDoctor's other checks legitimately shell out (git, tmux, harnesses);
	// the no-external-command guarantee is asserted on the check in isolation
	// by TestWedgedSessionsOnlyReadsAndNeverTouchesTheSupervisorSocket.
	clearDoctorGitHubEnv(t)
	c.deps.LookPath = func(string) (string, error) { return "", fmt.Errorf("missing") }
	c.deps.CommandOutput = func(context.Context, string, ...string) ([]byte, error) { return nil, nil }
	c.deps.CommandOutputInDir = func(context.Context, string, string, ...string) ([]byte, error) { return nil, nil }

	check := findDoctorCheck(t, c.runDoctor(context.Background()), "sessions-idle")
	if check.Level != doctorWarn || !strings.Contains(check.Message, "mer-wedged") {
		t.Fatalf("sessions-idle = %+v, want WARN naming the silent session", check)
	}
}
