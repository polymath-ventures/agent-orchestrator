package controllers

import (
	"context"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/aoagents/agent-orchestrator/backend/internal/httpd/apispec"
	"github.com/aoagents/agent-orchestrator/backend/internal/httpd/envelope"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
	agentsvc "github.com/aoagents/agent-orchestrator/backend/internal/service/agent"
	"github.com/aoagents/agent-orchestrator/backend/internal/service/agenthealth"
)

// AgentCatalog is the controller-facing contract for local agent inventory.
type AgentCatalog interface {
	List(ctx context.Context) (agentsvc.Inventory, error)
	Refresh(ctx context.Context) (agentsvc.Inventory, error)
	Probe(ctx context.Context, agentID string) (agentsvc.ProbeResult, error)
	Models(ctx context.Context, agentID, projectID string, refresh bool) (ports.AgentModelCatalog, error)
	RevalidateModels(ctx context.Context, agentID, projectID string) (ports.AgentModelCatalog, error)
}

// AgentModels is the controller-facing model discovery and availability
// contract. The service owns catalog, cache, and probe behavior.
type AgentModels interface {
	ModelAvailability(ctx context.Context, req agentsvc.ModelAvailabilityRequest) (agentsvc.ModelAvailabilityResponse, error)
}

// AgentModelPinProvider projects configured model pins through the launch
// precedence rules. The controller deliberately does not inspect projects or
// re-derive that domain policy.
type AgentModelPinProvider interface {
	ListModelPins(ctx context.Context) ([]agentsvc.ModelPin, error)
}

// AgentHealthSnapshotProvider exposes the monitor's immutable cached snapshot.
type AgentHealthSnapshotProvider interface {
	Snapshot() agenthealth.Snapshot
}

// AgentsController owns the /agents routes.
type AgentsController struct {
	Catalog   AgentCatalog
	Models    AgentModels
	ModelPins AgentModelPinProvider
	Health    AgentHealthSnapshotProvider
}

// Register mounts the agent inventory routes on the supplied router.
func (c *AgentsController) Register(r chi.Router) {
	r.Get("/agents", c.list)
	r.Get("/agents/models", c.availableModels)
	r.Get("/agents/health", c.health)
	r.Post("/agents/refresh", c.refresh)
	r.Post("/agents/{agent}/probe", c.probe)
	r.Get("/agents/{agent}/models", c.models)
	r.Post("/agents/{agent}/models/refresh", c.refreshModels)
}

func (c *AgentsController) models(w http.ResponseWriter, r *http.Request) {
	c.writeModels(w, r, false, false)
}

func (c *AgentsController) refreshModels(w http.ResponseWriter, r *http.Request) {
	c.writeModels(w, r, true, r.URL.Query().Get("revalidate") == "true")
}

func (c *AgentsController) writeModels(w http.ResponseWriter, r *http.Request, refresh, revalidate bool) {
	if c.Catalog == nil {
		route := "/api/v1/agents/{agent}/models"
		if refresh {
			route += "/refresh"
		}
		apispec.NotImplemented(w, r, r.Method, route)
		return
	}
	agentID := strings.TrimSpace(chi.URLParam(r, "agent"))
	if agentID == "" {
		envelope.WriteAPIError(w, r, http.StatusBadRequest, "bad_request", "AGENT_REQUIRED", "agent is required", nil)
		return
	}
	projectID := strings.TrimSpace(r.URL.Query().Get("projectId"))
	var catalog ports.AgentModelCatalog
	var err error
	if revalidate {
		catalog, err = c.Catalog.RevalidateModels(r.Context(), agentID, projectID)
	} else {
		catalog, err = c.Catalog.Models(r.Context(), agentID, projectID, refresh)
	}
	if err != nil {
		envelope.WriteError(w, r, err)
		return
	}
	envelope.WriteJSON(w, http.StatusOK, catalog)
}

func (c *AgentsController) availableModels(w http.ResponseWriter, r *http.Request) {
	if c.Models == nil || c.ModelPins == nil {
		apispec.NotImplemented(w, r, "GET", "/api/v1/agents/models")
		return
	}
	force := false
	if raw := strings.TrimSpace(r.URL.Query().Get("force")); raw != "" {
		parsed, err := strconv.ParseBool(raw)
		if err != nil {
			envelope.WriteAPIError(w, r, http.StatusBadRequest, "bad_request", "INVALID_FORCE", "force must be a boolean", nil)
			return
		}
		force = parsed
	}
	pins, err := c.ModelPins.ListModelPins(r.Context())
	if err != nil {
		envelope.WriteError(w, r, err)
		return
	}
	response, err := c.Models.ModelAvailability(r.Context(), agentsvc.ModelAvailabilityRequest{Pins: pins, Force: force})
	if err != nil {
		envelope.WriteError(w, r, err)
		return
	}
	envelope.WriteJSON(w, http.StatusOK, response)
}

func (c *AgentsController) health(w http.ResponseWriter, r *http.Request) {
	if c.Health == nil {
		apispec.NotImplemented(w, r, "GET", "/api/v1/agents/health")
		return
	}
	envelope.WriteJSON(w, http.StatusOK, c.Health.Snapshot())
}

func (c *AgentsController) list(w http.ResponseWriter, r *http.Request) {
	if c.Catalog == nil {
		apispec.NotImplemented(w, r, "GET", "/api/v1/agents")
		return
	}
	inventory, err := c.Catalog.List(r.Context())
	if err != nil {
		envelope.WriteError(w, r, err)
		return
	}
	envelope.WriteJSON(w, http.StatusOK, inventory)
}

func (c *AgentsController) refresh(w http.ResponseWriter, r *http.Request) {
	if c.Catalog == nil {
		apispec.NotImplemented(w, r, "POST", "/api/v1/agents/refresh")
		return
	}
	inventory, err := c.Catalog.Refresh(r.Context())
	if err != nil {
		envelope.WriteError(w, r, err)
		return
	}
	envelope.WriteJSON(w, http.StatusOK, inventory)
}

func (c *AgentsController) probe(w http.ResponseWriter, r *http.Request) {
	if c.Catalog == nil {
		apispec.NotImplemented(w, r, "POST", "/api/v1/agents/{agent}/probe")
		return
	}
	agentID := strings.TrimSpace(chi.URLParam(r, "agent"))
	if agentID == "" {
		envelope.WriteAPIError(w, r, http.StatusBadRequest, "bad_request", "AGENT_REQUIRED", "agent is required", nil)
		return
	}
	result, err := c.Catalog.Probe(r.Context(), agentID)
	if err != nil {
		envelope.WriteError(w, r, err)
		return
	}
	envelope.WriteJSON(w, http.StatusOK, result)
}
