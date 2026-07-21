package controllers_test

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/aoagents/agent-orchestrator/backend/internal/config"
	"github.com/aoagents/agent-orchestrator/backend/internal/httpd"
)

// pauseProjectBody is the subset of the project read-model the pause routes must
// surface.
type pauseProjectBody struct {
	ID         string `json:"id"`
	Paused     bool   `json:"paused"`
	PauseState string `json:"pauseState"`
}

type fleetStatusBody struct {
	Paused bool `json:"paused"`
}

// The fleet pause/resume routes round-trip the daemon-global flag.
func TestFleetPauseResumeHTTP(t *testing.T) {
	srv := newTestServer(t)

	body, status, _ := doRequest(t, srv, "GET", "/api/v1/fleet", "")
	if status != http.StatusOK {
		t.Fatalf("GET /fleet = %d, want 200; body=%s", status, body)
	}
	var st fleetStatusBody
	mustJSON(t, body, &st)
	if st.Paused {
		t.Fatalf("fresh fleet paused = true, want false")
	}

	body, status, _ = doRequest(t, srv, "POST", "/api/v1/fleet/pause", "")
	if status != http.StatusOK {
		t.Fatalf("POST /fleet/pause = %d, want 200; body=%s", status, body)
	}
	mustJSON(t, body, &st)
	if !st.Paused {
		t.Fatalf("after fleet pause: paused = false, want true")
	}

	body, _, _ = doRequest(t, srv, "GET", "/api/v1/fleet", "")
	mustJSON(t, body, &st)
	if !st.Paused {
		t.Fatalf("GET /fleet after pause = false, want true")
	}

	body, status, _ = doRequest(t, srv, "POST", "/api/v1/fleet/resume", "")
	if status != http.StatusOK {
		t.Fatalf("POST /fleet/resume = %d, want 200; body=%s", status, body)
	}
	mustJSON(t, body, &st)
	if st.Paused {
		t.Fatalf("after fleet resume: paused = true, want false")
	}
}

// The project pause/resume routes flip the per-project bit and report the
// derived state; the ?hard query param is accepted.
func TestProjectPauseResumeHTTP(t *testing.T) {
	srv := newTestServer(t)
	repo := gitRepo(t, "demo")
	if _, status, _ := doRequest(t, srv, "POST", "/api/v1/projects", `{"path":`+quote(repo)+`,"projectId":"demo"}`); status != http.StatusCreated {
		t.Fatalf("add project = %d", status)
	}

	body, status, _ := doRequest(t, srv, "POST", "/api/v1/projects/demo/pause", "")
	if status != http.StatusOK {
		t.Fatalf("POST pause = %d, want 200; body=%s", status, body)
	}
	var resp struct {
		Project pauseProjectBody `json:"project"`
	}
	mustJSON(t, body, &resp)
	if !resp.Project.Paused || resp.Project.PauseState != "paused" {
		t.Fatalf("after pause: paused=%v state=%q, want true/paused", resp.Project.Paused, resp.Project.PauseState)
	}

	// Hard pause is accepted (no live workers, so it simply flips the bit).
	if _, status, _ = doRequest(t, srv, "POST", "/api/v1/projects/demo/pause?hard=true", ""); status != http.StatusOK {
		t.Fatalf("POST pause?hard=true = %d, want 200", status)
	}

	body, status, _ = doRequest(t, srv, "POST", "/api/v1/projects/demo/resume", "")
	if status != http.StatusOK {
		t.Fatalf("POST resume = %d, want 200; body=%s", status, body)
	}
	mustJSON(t, body, &resp)
	if resp.Project.Paused || resp.Project.PauseState != "running" {
		t.Fatalf("after resume: paused=%v state=%q, want false/running", resp.Project.Paused, resp.Project.PauseState)
	}
}

// Without a wired Manager the new routes return the OpenAPI-backed 501.
func TestFleetRoute_NotImplementedWithoutManager(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	srv := httptest.NewServer(httpd.NewRouterWithControl(config.Config{}, log, nil, httpd.APIDeps{}, httpd.ControlDeps{}))
	t.Cleanup(srv.Close)
	body, status, _ := doRequest(t, srv, "GET", "/api/v1/fleet", "")
	assertErrorCode(t, body, status, http.StatusNotImplemented, "NOT_IMPLEMENTED")
}
