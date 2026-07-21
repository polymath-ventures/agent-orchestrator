// Package roleprompt assembles the exact, fully-composed system prompt each
// agent role (worker, orchestrator, reviewer) receives for a project, for
// operator inspection. It composes the two prompt-assembly owners — the session
// manager (worker/orchestrator) and the review package (reviewer) — behind one
// role-keyed method so the visibility surface has a single entry point.
//
// It is read-only: it never spawns anything. It recomputes the prompt from
// current project config using the same assembly the daemon would use at spawn,
// so the operator sees what a role would get right now — including any
// operator-controlled rules override, and the same fail-closed error when that
// override is misconfigured.
package roleprompt

import (
	"context"
	"errors"
	"fmt"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/review"
	sessionmanager "github.com/aoagents/agent-orchestrator/backend/internal/session_manager"
)

// Role names the inspectable roles. They are the wire vocabulary for the
// {role} path segment on the visibility route.
const (
	RoleWorker       = "worker"
	RoleOrchestrator = "orchestrator"
	RoleReviewer     = "reviewer"
)

// ErrUnknownRole is returned when the requested role is not one of the
// supported roles; the transport maps it to a client error.
var ErrUnknownRole = errors.New("unknown role")

// ErrProjectNotFound is returned when the project does not exist; the transport
// maps it to a not-found error.
var ErrProjectNotFound = errors.New("project not found")

// SessionPromptAssembler assembles the worker/orchestrator system prompt for a
// project. *session_manager.Manager satisfies it via RoleSystemPrompt.
type SessionPromptAssembler interface {
	RoleSystemPrompt(ctx context.Context, kind domain.SessionKind, projectID domain.ProjectID) (string, error)
}

// ProjectGetter fetches a project's stored record (path + config), needed to
// load the reviewer's operator rules. The sqlite store satisfies it.
type ProjectGetter interface {
	GetProject(ctx context.Context, id string) (domain.ProjectRecord, bool, error)
}

// Assembler composes the per-role prompt sources.
type Assembler struct {
	sessions SessionPromptAssembler
	projects ProjectGetter
}

// New builds an Assembler from the session-prompt assembler and project getter.
func New(sessions SessionPromptAssembler, projects ProjectGetter) *Assembler {
	return &Assembler{sessions: sessions, projects: projects}
}

// RolePrompt returns the exact assembled system prompt for a (project, role).
// Unknown roles return ErrUnknownRole; a missing project returns
// ErrProjectNotFound; a misconfigured operator rules override returns the same
// fail-closed error a spawn would raise, rather than a prompt with the override
// silently omitted.
func (a *Assembler) RolePrompt(ctx context.Context, projectID domain.ProjectID, role string) (string, error) {
	if role != RoleWorker && role != RoleOrchestrator && role != RoleReviewer {
		return "", fmt.Errorf("%w: %q", ErrUnknownRole, role)
	}
	proj, ok, err := a.projects.GetProject(ctx, string(projectID))
	if err != nil {
		return "", err
	}
	if !ok {
		return "", fmt.Errorf("%w: %q", ErrProjectNotFound, projectID)
	}
	switch role {
	case RoleWorker:
		return a.sessions.RoleSystemPrompt(ctx, domain.KindWorker, projectID)
	case RoleOrchestrator:
		return a.sessions.RoleSystemPrompt(ctx, domain.KindOrchestrator, projectID)
	default: // RoleReviewer
		rules, err := sessionmanager.LoadRoleRules(sessionmanager.RoleRulesConfig{
			Role:        RoleReviewer,
			ProjectPath: proj.Path,
			InlineRules: proj.Config.ReviewerRules,
			RulesFile:   proj.Config.ReviewerRulesFile,
		})
		if err != nil {
			return "", err
		}
		return review.AssembleReviewerSystemPrompt(rules), nil
	}
}
