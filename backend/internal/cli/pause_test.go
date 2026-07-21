package cli

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type pauseCapture struct {
	method string
	path   string
	query  string
}

// pauseServer answers both the project and fleet pause/resume routes and
// records the last request line.
func pauseServer(t *testing.T) (*httptest.Server, *pauseCapture) {
	t.Helper()
	capture := &pauseCapture{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capture.method = r.Method
		capture.path = r.URL.Path
		capture.query = r.URL.RawQuery
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		if strings.HasPrefix(r.URL.Path, "/api/v1/fleet") {
			paused := strings.HasSuffix(r.URL.Path, "/pause")
			if paused {
				_, _ = io.WriteString(w, `{"paused":true}`)
			} else {
				_, _ = io.WriteString(w, `{"paused":false}`)
			}
			return
		}
		_, _ = io.WriteString(w, `{"project":{"id":"demo","paused":true,"pauseState":"draining","drainingWorkers":2}}`)
	}))
	t.Cleanup(srv.Close)
	return srv, capture
}

func TestPause_Project(t *testing.T) {
	cfg := setConfigEnv(t)
	srv, capture := pauseServer(t)
	writeRunFileFor(t, cfg, srv)

	out, errOut, err := executeCLI(t, Deps{ProcessAlive: func(int) bool { return true }}, "pause", "demo")
	if err != nil {
		t.Fatalf("unexpected error: %v\nstderr=%s", err, errOut)
	}
	if capture.method != http.MethodPost || capture.path != "/api/v1/projects/demo/pause" {
		t.Fatalf("request = %s %s, want POST /api/v1/projects/demo/pause", capture.method, capture.path)
	}
	if capture.query != "" {
		t.Fatalf("soft pause query = %q, want empty", capture.query)
	}
	if !strings.Contains(out, "demo") || !strings.Contains(out, "draining") {
		t.Fatalf("output = %q, want mention of demo + draining state", out)
	}
}

func TestPause_ProjectHard(t *testing.T) {
	cfg := setConfigEnv(t)
	srv, capture := pauseServer(t)
	writeRunFileFor(t, cfg, srv)

	if _, errOut, err := executeCLI(t, Deps{ProcessAlive: func(int) bool { return true }}, "pause", "demo", "--hard"); err != nil {
		t.Fatalf("unexpected error: %v\nstderr=%s", err, errOut)
	}
	if capture.path != "/api/v1/projects/demo/pause" || capture.query != "hard=true" {
		t.Fatalf("request = %s?%s, want /api/v1/projects/demo/pause?hard=true", capture.path, capture.query)
	}
}

func TestPause_FleetAll(t *testing.T) {
	cfg := setConfigEnv(t)
	srv, capture := pauseServer(t)
	writeRunFileFor(t, cfg, srv)

	if _, errOut, err := executeCLI(t, Deps{ProcessAlive: func(int) bool { return true }}, "pause", "--all"); err != nil {
		t.Fatalf("unexpected error: %v\nstderr=%s", err, errOut)
	}
	if capture.method != http.MethodPost || capture.path != "/api/v1/fleet/pause" {
		t.Fatalf("request = %s %s, want POST /api/v1/fleet/pause", capture.method, capture.path)
	}
}

func TestResume_FleetAll(t *testing.T) {
	cfg := setConfigEnv(t)
	srv, capture := pauseServer(t)
	writeRunFileFor(t, cfg, srv)

	if _, errOut, err := executeCLI(t, Deps{ProcessAlive: func(int) bool { return true }}, "resume", "--all"); err != nil {
		t.Fatalf("unexpected error: %v\nstderr=%s", err, errOut)
	}
	if capture.path != "/api/v1/fleet/resume" {
		t.Fatalf("path = %s, want /api/v1/fleet/resume", capture.path)
	}
}

// Passing both a project id and --all is a usage error (exit 2).
func TestPause_ProjectAndAllConflict(t *testing.T) {
	cfg := setConfigEnv(t)
	srv, _ := pauseServer(t)
	writeRunFileFor(t, cfg, srv)

	_, _, err := executeCLI(t, Deps{ProcessAlive: func(int) bool { return true }}, "pause", "demo", "--all")
	if err == nil {
		t.Fatalf("expected a usage error for `pause demo --all`")
	}
	if ExitCode(err) != 2 {
		t.Fatalf("exit code = %d, want 2 (usage error)", ExitCode(err))
	}
}
