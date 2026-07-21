package controllers_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/config"
	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/httpd"
	agentsvc "github.com/aoagents/agent-orchestrator/backend/internal/service/agent"
	"github.com/aoagents/agent-orchestrator/backend/internal/service/agenthealth"
)

type fakeAgentCatalog struct {
	inventory    agentsvc.Inventory
	refreshed    agentsvc.Inventory
	probed       agentsvc.ProbeResult
	err          error
	listCalls    int
	refreshCalls int
	probeCalls   int
	probeAgent   string
}

type fakeAgentModels struct {
	response agentsvc.ModelAvailabilityResponse
	err      error
	calls    int
	request  agentsvc.ModelAvailabilityRequest
}

func (f *fakeAgentModels) ModelAvailability(_ context.Context, req agentsvc.ModelAvailabilityRequest) (agentsvc.ModelAvailabilityResponse, error) {
	f.calls++
	f.request = req
	return f.response, f.err
}

type fakeModelPins struct {
	pins  []agentsvc.ModelPin
	err   error
	calls int
}

func (f *fakeModelPins) ListModelPins(context.Context) ([]agentsvc.ModelPin, error) {
	f.calls++
	return append([]agentsvc.ModelPin(nil), f.pins...), f.err
}

type fakeAgentHealth struct {
	snapshot agenthealth.Snapshot
	calls    int
}

func (f *fakeAgentHealth) Snapshot() agenthealth.Snapshot {
	f.calls++
	return f.snapshot
}

func (f *fakeAgentCatalog) List(context.Context) (agentsvc.Inventory, error) {
	f.listCalls++
	return f.inventory, f.err
}

func (f *fakeAgentCatalog) Refresh(context.Context) (agentsvc.Inventory, error) {
	f.refreshCalls++
	if f.refreshed.Supported != nil {
		return f.refreshed, f.err
	}
	return f.inventory, f.err
}

func (f *fakeAgentCatalog) Probe(_ context.Context, agentID string) (agentsvc.ProbeResult, error) {
	f.probeCalls++
	f.probeAgent = agentID
	return f.probed, f.err
}

func TestListAgents(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	catalog := &fakeAgentCatalog{inventory: agentsvc.Inventory{
		Supported:  []agentsvc.Info{{ID: "claude-code", Label: "Claude Code"}, {ID: "codex", Label: "Codex"}},
		Installed:  []agentsvc.Info{{ID: "codex", Label: "Codex"}},
		Authorized: []agentsvc.Info{{ID: "codex", Label: "Codex"}},
	}}
	srv := httptest.NewServer(httpd.NewRouterWithControl(config.Config{}, log, nil, httpd.APIDeps{
		Agents: catalog,
	}, httpd.ControlDeps{}))
	defer srv.Close()

	body, status, _ := doRequest(t, srv, http.MethodGet, "/api/v1/agents", "")
	if status != http.StatusOK {
		t.Fatalf("GET /agents = %d, body=%s", status, body)
	}
	for _, want := range []string{`"supported"`, `"installed"`, `"authorized"`, `"id":"codex"`} {
		if !strings.Contains(string(body), want) {
			t.Fatalf("body missing %s: %s", want, body)
		}
	}
	if strings.Contains(string(body), `"counts"`) {
		t.Fatalf("body includes removed counts field: %s", body)
	}
	if catalog.listCalls != 1 || catalog.refreshCalls != 0 {
		t.Fatalf("calls: list=%d refresh=%d, want list=1 refresh=0", catalog.listCalls, catalog.refreshCalls)
	}
}

func TestRefreshAgents(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	catalog := &fakeAgentCatalog{
		inventory: agentsvc.Inventory{Supported: []agentsvc.Info{{ID: "codex", Label: "Codex"}}},
		refreshed: agentsvc.Inventory{
			Supported:  []agentsvc.Info{{ID: "codex", Label: "Codex"}},
			Installed:  []agentsvc.Info{{ID: "codex", Label: "Codex"}},
			Authorized: []agentsvc.Info{{ID: "codex", Label: "Codex"}},
		},
	}
	srv := httptest.NewServer(httpd.NewRouterWithControl(config.Config{}, log, nil, httpd.APIDeps{
		Agents: catalog,
	}, httpd.ControlDeps{}))
	defer srv.Close()

	body, status, _ := doRequest(t, srv, http.MethodPost, "/api/v1/agents/refresh", "")
	if status != http.StatusOK {
		t.Fatalf("POST /agents/refresh = %d, body=%s", status, body)
	}
	for _, want := range []string{`"supported"`, `"installed"`, `"authorized"`, `"id":"codex"`} {
		if !strings.Contains(string(body), want) {
			t.Fatalf("body missing %s: %s", want, body)
		}
	}
	if catalog.listCalls != 0 || catalog.refreshCalls != 1 {
		t.Fatalf("calls: list=%d refresh=%d, want list=0 refresh=1", catalog.listCalls, catalog.refreshCalls)
	}
}

func TestProbeAgent(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	catalog := &fakeAgentCatalog{
		probed: agentsvc.ProbeResult{
			Agent:     agentsvc.Info{ID: "codex", Label: "Codex", AuthStatus: "authorized"},
			Supported: true,
			Installed: true,
		},
	}
	srv := httptest.NewServer(httpd.NewRouterWithControl(config.Config{}, log, nil, httpd.APIDeps{
		Agents: catalog,
	}, httpd.ControlDeps{}))
	defer srv.Close()

	body, status, _ := doRequest(t, srv, http.MethodPost, "/api/v1/agents/codex/probe", "")
	if status != http.StatusOK {
		t.Fatalf("POST /agents/codex/probe = %d, body=%s", status, body)
	}
	for _, want := range []string{`"supported":true`, `"installed":true`, `"id":"codex"`, `"authStatus":"authorized"`} {
		if !strings.Contains(string(body), want) {
			t.Fatalf("body missing %s: %s", want, body)
		}
	}
	if catalog.probeCalls != 1 || catalog.probeAgent != "codex" {
		t.Fatalf("probe calls=%d agent=%q, want one codex probe", catalog.probeCalls, catalog.probeAgent)
	}
}

func TestListAgentModelsPassesInjectedPinsAndForce(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	checkedAt := time.Date(2026, 7, 21, 22, 0, 0, 0, time.UTC)
	models := &fakeAgentModels{response: agentsvc.ModelAvailabilityResponse{
		CheckedAt: checkedAt,
		Harnesses: []agentsvc.HarnessModels{{
			ID:              "opencode",
			Label:           "OpenCode",
			CatalogSource:   agentsvc.ModelCatalogCachedAdapter,
			CatalogReason:   "refresh failed: offline",
			CatalogVerified: false,
			Models: []agentsvc.ModelAvailability{{
				Model:         "openai/gpt-5.4",
				Label:         "GPT-5.4",
				Efforts:       []domain.Effort{domain.EffortHigh, domain.Effort("turbo")},
				DefaultEffort: domain.EffortHigh,
				Dynamic:       true,
				Verified:      false,
				Status:        agentsvc.ModelStatusUnknown,
				Reason:        "probe unavailable",
				ReasonCode:    agentsvc.ModelReasonProbeUnavailable,
			}},
		}},
	}}
	pins := &fakeModelPins{pins: []agentsvc.ModelPin{{Harness: domain.HarnessOpenCode, Model: "openai/gpt-5.4"}}}
	srv := httptest.NewServer(httpd.NewRouterWithControl(config.Config{}, log, nil, httpd.APIDeps{
		AgentModels:    models,
		AgentModelPins: pins,
	}, httpd.ControlDeps{}))
	defer srv.Close()

	body, status, _ := doRequest(t, srv, http.MethodGet, "/api/v1/agents/models?force=true", "")
	if status != http.StatusOK {
		t.Fatalf("GET /agents/models = %d, body=%s", status, body)
	}
	for _, want := range []string{
		`"catalogSource":"cached-adapter"`, `"catalogReason":"refresh failed: offline"`, `"catalogVerified":false`,
		`"model":"openai/gpt-5.4"`, `"label":"GPT-5.4"`, `"efforts":["high","turbo"]`,
		`"defaultEffort":"high"`, `"dynamic":true`, `"verified":false`, `"status":"unknown"`,
		`"reasonCode":"probe-unavailable"`, `"checkedAt":"2026-07-21T22:00:00Z"`,
	} {
		if !strings.Contains(string(body), want) {
			t.Fatalf("body missing %s: %s", want, body)
		}
	}
	if models.calls != 1 || !models.request.Force {
		t.Fatalf("models calls=%d request=%#v, want force", models.calls, models.request)
	}
	if !reflect.DeepEqual(models.request.Pins, pins.pins) || pins.calls != 1 {
		t.Fatalf("model pins request=%#v provider=%#v", models.request.Pins, pins)
	}
}

func TestListAgentModelsRejectsInvalidForceBeforeDependencies(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	models := &fakeAgentModels{}
	pins := &fakeModelPins{}
	srv := httptest.NewServer(httpd.NewRouterWithControl(config.Config{}, log, nil, httpd.APIDeps{
		AgentModels:    models,
		AgentModelPins: pins,
	}, httpd.ControlDeps{}))
	defer srv.Close()

	body, status, _ := doRequest(t, srv, http.MethodGet, "/api/v1/agents/models?force=definitely", "")
	if status != http.StatusBadRequest {
		t.Fatalf("GET invalid force = %d, body=%s", status, body)
	}
	if !strings.Contains(string(body), `"code":"INVALID_FORCE"`) || !strings.Contains(string(body), `"requestId"`) {
		t.Fatalf("error envelope missing code/request id: %s", body)
	}
	if models.calls != 0 || pins.calls != 0 {
		t.Fatalf("invalid query reached dependencies: models=%d pins=%d", models.calls, pins.calls)
	}
}

func TestListAgentModelsPreservesServiceErrorEnvelope(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	models := &fakeAgentModels{err: errors.New("catalog unavailable")}
	srv := httptest.NewServer(httpd.NewRouterWithControl(config.Config{}, log, nil, httpd.APIDeps{
		AgentModels:    models,
		AgentModelPins: &fakeModelPins{},
	}, httpd.ControlDeps{}))
	defer srv.Close()

	body, status, _ := doRequest(t, srv, http.MethodGet, "/api/v1/agents/models", "")
	if status != http.StatusInternalServerError {
		t.Fatalf("GET failed models = %d, body=%s", status, body)
	}
	if !strings.Contains(string(body), `"requestId"`) || !strings.Contains(string(body), `"message"`) {
		t.Fatalf("service error is not in API envelope: %s", body)
	}
}

func TestGetAgentHealthReturnsCachedSnapshot(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	checkedAt := time.Date(2026, 7, 21, 22, 5, 0, 0, time.UTC)
	health := &fakeAgentHealth{snapshot: agenthealth.Snapshot{
		CheckedAt: checkedAt,
		Harnesses: []agenthealth.HarnessHealth{{
			ID: "codex", Label: "Codex", Health: agenthealth.HealthUnauthorized,
			AuthStatus: "unauthorized", Reason: "login required", Remedy: "Run codex login",
			ChangedAt: checkedAt.Add(-time.Hour), CheckedAt: checkedAt,
		}},
	}}
	srv := httptest.NewServer(httpd.NewRouterWithControl(config.Config{}, log, nil, httpd.APIDeps{
		AgentHealth: health,
	}, httpd.ControlDeps{}))
	defer srv.Close()

	body, status, _ := doRequest(t, srv, http.MethodGet, "/api/v1/agents/health", "")
	if status != http.StatusOK {
		t.Fatalf("GET /agents/health = %d, body=%s", status, body)
	}
	for _, want := range []string{`"id":"codex"`, `"health":"unauthorized"`, `"authStatus":"unauthorized"`, `"remedy":"Run codex login"`} {
		if !strings.Contains(string(body), want) {
			t.Fatalf("body missing %s: %s", want, body)
		}
	}
	if health.calls != 1 {
		t.Fatalf("health snapshot calls = %d, want 1", health.calls)
	}
}
