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

// PrimeRelauncher performs an explicit, user-initiated Prime relaunch: it
// clears budget-paused replacement state and reconciles Prime immediately.
//
// Deliberately separate from generic session spawn and restore, both of which
// stay forbidden for Prime (PRIME_MANUAL_SPAWN_FORBIDDEN /
// PRIME_MANUAL_RESTORE_FORBIDDEN). Those bans keep Prime a supervisor-managed
// singleton; relaunch honours that by routing through the same reconciliation
// the supervisor uses, so it cannot create a second Prime.
type PrimeRelauncher interface {
	RelaunchPrime(ctx context.Context) (domain.Session, error)
}

// PrimeController owns the daemon-global /prime routes.
type PrimeController struct {
	Svc      PrimeService
	Relaunch PrimeRelauncher
}

// Register mounts Prime settings, prompt visibility, and relaunch routes.
func (c *PrimeController) Register(r chi.Router) {
	r.Get("/prime/settings", c.getSettings)
	r.Put("/prime/settings", c.setSettings)
	r.Get("/prime/prompt", c.prompt)
	r.Post("/prime/relaunch", c.relaunch)
}

// relaunch is idempotent with respect to the Prime singleton: reconciliation
// returns the existing active Prime rather than creating a second one.
func (c *PrimeController) relaunch(w http.ResponseWriter, r *http.Request) {
	if c.Relaunch == nil {
		apispec.NotImplemented(w, r, "POST", "/api/v1/prime/relaunch")
		return
	}
	sess, err := c.Relaunch.RelaunchPrime(r.Context())
	if err != nil {
		envelope.WriteError(w, r, err)
		return
	}
	envelope.WriteJSON(w, http.StatusOK, SessionResponse{Session: sessionView(sess)})
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
