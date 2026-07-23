package controllers

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/httpd/apispec"
	"github.com/aoagents/agent-orchestrator/backend/internal/httpd/envelope"
	"github.com/aoagents/agent-orchestrator/backend/internal/observe/metrics"
)

// MetricsProvider is the controller-facing read surface for the usage/quota
// metrics observer. It is a pure in-memory read of the observer's retained
// snapshots, so it neither blocks nor samples on the request path.
type MetricsProvider interface {
	// Snapshots returns the retained history (oldest-first) plus the latest
	// snapshot under one lock, so the response cannot mix a history and a newer
	// latest observed across two separate reads.
	Snapshots() (history []metrics.Snapshot, latest metrics.Snapshot, hasLatest bool)
}

// QuotaProber is the controller-facing surface for the daemon harness quota
// prober. Statuses is a pure in-memory read; ProbeHarness/ProbeAll trigger a
// bounded force-probe. It is optional: a nil prober means the daemon has quota
// probing disabled, so the read surface reports no statuses and the probe
// endpoint returns not-implemented.
type QuotaProber interface {
	Statuses() []domain.HarnessQuotaStatus
	ProbeHarness(ctx context.Context, harness domain.AgentHarness) (domain.HarnessQuotaStatus, bool)
	ProbeAll(ctx context.Context) []domain.HarnessQuotaStatus
}

// MetricsController owns the /metrics routes.
type MetricsController struct {
	Provider MetricsProvider
	// Prober is the optional harness quota prober. Nil when quota probing is
	// disabled: ProbeStatuses stays empty and POST /metrics/probe returns 501.
	Prober QuotaProber
}

// MetricsResponse is the wire shape for GET /api/v1/metrics: the latest
// snapshot plus a short history (oldest-first). Latest is nil-omitted until the
// observer has produced its first snapshot.
type MetricsResponse struct {
	// Latest is the most recent snapshot, or absent before the first tick.
	Latest *metrics.Snapshot `json:"latest,omitempty"`
	// History is the retained recent snapshots, oldest-first.
	History []metrics.Snapshot `json:"history"`
	// ProbeStatuses is the current per-harness quota probe status, rebuilt in
	// memory each daemon run by the prober. Empty when probing is disabled.
	ProbeStatuses []domain.HarnessQuotaStatus `json:"probeStatuses"`
}

// ProbeQuotaRequest is the optional body for POST /api/v1/metrics/probe. An
// empty/absent harness force-probes every installed harness; a specific harness
// force-probes only that one.
type ProbeQuotaRequest struct {
	Harness string `json:"harness,omitempty"`
}

// ProbeQuotaResponse is the wire shape for POST /api/v1/metrics/probe: the full
// per-harness status set after the force-probe completes, so the caller can
// refresh the whole widget from one response.
type ProbeQuotaResponse struct {
	Statuses []domain.HarnessQuotaStatus `json:"statuses"`
}

// Register mounts the metrics routes on the supplied router.
func (c *MetricsController) Register(r chi.Router) {
	r.Get("/metrics", c.get)
	r.Post("/metrics/probe", c.probe)
}

func (c *MetricsController) get(w http.ResponseWriter, r *http.Request) {
	if c.Provider == nil {
		apispec.NotImplemented(w, r, "GET", "/api/v1/metrics")
		return
	}
	history, latest, ok := c.Provider.Snapshots()
	resp := MetricsResponse{History: history}
	if ok {
		resp.Latest = &latest
	}
	if resp.History == nil {
		resp.History = []metrics.Snapshot{}
	}
	resp.ProbeStatuses = c.probeStatuses()
	envelope.WriteJSON(w, http.StatusOK, resp)
}

func (c *MetricsController) probe(w http.ResponseWriter, r *http.Request) {
	if c.Prober == nil {
		apispec.NotImplemented(w, r, "POST", "/api/v1/metrics/probe")
		return
	}
	var req ProbeQuotaRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
		envelope.WriteAPIError(w, r, http.StatusBadRequest, "bad_request", "INVALID_JSON", "Invalid JSON body", nil)
		return
	}

	if req.Harness == "" {
		c.Prober.ProbeAll(r.Context())
		envelope.WriteJSON(w, http.StatusOK, ProbeQuotaResponse{Statuses: c.probeStatuses()})
		return
	}

	if _, ok := c.Prober.ProbeHarness(r.Context(), domain.AgentHarness(req.Harness)); !ok {
		envelope.WriteAPIError(w, r, http.StatusNotFound, "not_found", "HARNESS_NOT_PROBEABLE", "No installed harness with a quota probe matches that id", nil)
		return
	}
	envelope.WriteJSON(w, http.StatusOK, ProbeQuotaResponse{Statuses: c.probeStatuses()})
}

// probeStatuses returns the prober's current status set, always non-nil so the
// field serializes as [] rather than null. Empty when probing is disabled.
func (c *MetricsController) probeStatuses() []domain.HarnessQuotaStatus {
	if c.Prober == nil {
		return []domain.HarnessQuotaStatus{}
	}
	statuses := c.Prober.Statuses()
	if statuses == nil {
		return []domain.HarnessQuotaStatus{}
	}
	return statuses
}
