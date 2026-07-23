package controllers

import (
	"context"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/httpd/apispec"
	"github.com/aoagents/agent-orchestrator/backend/internal/httpd/envelope"
	"github.com/aoagents/agent-orchestrator/backend/internal/roleprompt"
	primesvc "github.com/aoagents/agent-orchestrator/backend/internal/service/prime"
)

// PrimeService owns daemon-global Prime settings and prompt inspection.
type PrimeService interface {
	GetSettings(ctx context.Context) (primesvc.SettingsView, error)
	SetSettings(ctx context.Context, settings domain.PrimeSettings) (primesvc.SettingsView, error)
	Prompt(ctx context.Context) (string, error)
}

// PrimeSettingsRequest is the body of PUT /prime/settings.
type PrimeSettingsRequest struct {
	Settings domain.PrimeSettings `json:"settings"`
}

// PrimeController owns the daemon-global /prime routes.
type PrimeController struct {
	Svc PrimeService
}

// Register mounts Prime settings and prompt visibility routes.
func (c *PrimeController) Register(r chi.Router) {
	r.Get("/prime/settings", c.getSettings)
	r.Put("/prime/settings", c.setSettings)
	r.Get("/prime/prompt", c.prompt)
}

func (c *PrimeController) getSettings(w http.ResponseWriter, r *http.Request) {
	if c.Svc == nil {
		apispec.NotImplemented(w, r, "GET", "/api/v1/prime/settings")
		return
	}
	view, err := c.Svc.GetSettings(r.Context())
	if err != nil {
		envelope.WriteError(w, r, err)
		return
	}
	envelope.WriteJSON(w, http.StatusOK, view)
}

func (c *PrimeController) setSettings(w http.ResponseWriter, r *http.Request) {
	if c.Svc == nil {
		apispec.NotImplemented(w, r, "PUT", "/api/v1/prime/settings")
		return
	}
	var in PrimeSettingsRequest
	if err := decodeJSONStrict(r, &in); err != nil {
		envelope.WriteAPIError(w, r, http.StatusBadRequest, "bad_request", "INVALID_JSON", "Invalid JSON body", nil)
		return
	}
	view, err := c.Svc.SetSettings(r.Context(), in.Settings)
	if err != nil {
		envelope.WriteError(w, r, err)
		return
	}
	envelope.WriteJSON(w, http.StatusOK, view)
}

func (c *PrimeController) prompt(w http.ResponseWriter, r *http.Request) {
	if c.Svc == nil {
		apispec.NotImplemented(w, r, "GET", "/api/v1/prime/prompt")
		return
	}
	prompt, err := c.Svc.Prompt(r.Context())
	switch {
	case roleprompt.IsRulesMisconfig(err):
		envelope.WriteAPIError(w, r, http.StatusUnprocessableEntity, "unprocessable", "ROLE_PROMPT_UNAVAILABLE", err.Error(), nil)
		return
	case err != nil:
		envelope.WriteError(w, r, err)
		return
	}
	envelope.WriteJSON(w, http.StatusOK, RolePromptResponse{Role: roleprompt.RolePrime, Prompt: prompt})
}
