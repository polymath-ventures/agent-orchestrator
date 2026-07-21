package controllers

import (
	"context"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/httpd/apispec"
	"github.com/aoagents/agent-orchestrator/backend/internal/httpd/envelope"
	"github.com/aoagents/agent-orchestrator/backend/internal/roleprompt"
)

// RolePromptService assembles the exact system prompt a role receives for a
// project. *roleprompt.Assembler satisfies it.
type RolePromptService interface {
	RolePrompt(ctx context.Context, projectID domain.ProjectID, role string) (string, error)
}

// RolePromptController owns the read-only effective-prompt visibility route.
// nil Svc keeps the route registered but returns an OpenAPI-backed 501.
type RolePromptController struct {
	Svc RolePromptService
}

// Register mounts the role-prompt route.
func (c *RolePromptController) Register(r chi.Router) {
	r.Get("/projects/{id}/roles/{role}/prompt", c.get)
}

func (c *RolePromptController) get(w http.ResponseWriter, r *http.Request) {
	if c.Svc == nil {
		apispec.NotImplemented(w, r, "GET", "/api/v1/projects/{id}/roles/{role}/prompt")
		return
	}
	role := chi.URLParam(r, "role")
	prompt, err := c.Svc.RolePrompt(r.Context(), projectID(r), role)
	switch {
	case errors.Is(err, roleprompt.ErrUnknownRole):
		envelope.WriteAPIError(w, r, http.StatusBadRequest, "bad_request", "UNKNOWN_ROLE", err.Error(), nil)
		return
	case errors.Is(err, roleprompt.ErrProjectNotFound):
		envelope.WriteAPIError(w, r, http.StatusNotFound, "not_found", "PROJECT_NOT_FOUND", err.Error(), nil)
		return
	case err != nil:
		// A configured-but-unloadable operator rules override surfaces here as
		// the same fail-closed error a spawn would raise, rather than a prompt
		// with the override silently omitted.
		envelope.WriteAPIError(w, r, http.StatusUnprocessableEntity, "unprocessable", "ROLE_PROMPT_UNAVAILABLE", err.Error(), nil)
		return
	}
	envelope.WriteJSON(w, http.StatusOK, RolePromptResponse{Role: role, Prompt: prompt})
}
