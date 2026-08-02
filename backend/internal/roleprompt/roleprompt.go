// Package roleprompt assembles the exact, fully-composed system prompt each
// agent role (worker, orchestrator, reviewer) receives for a project and, for
// workers, reports effective task-template metadata separately. It composes the
// two system-prompt assembly owners — the session manager
// (worker/orchestrator) and the review package (reviewer) — behind one role-keyed
// method so the visibility surface has a single entry point.
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
	RolePrime        = "prime"
	RoleReviewer     = "reviewer"
)

// ErrUnknownRole is returned when the requested role is not one of the
// supported roles; the transport maps it to a client error.
var ErrUnknownRole = errors.New("unknown role")

// ErrProjectNotFound is returned when the project does not exist; the transport
// maps it to a not-found error.
var ErrProjectNotFound = errors.New("project not found")

// IsRulesMisconfig reports whether err is a configured-but-unusable operator
// rules file (missing, empty, oversized, not a regular file, or escaping the
// project root). Transports map it to a client 4xx; every other error is an
// internal fault that must surface as a sanitized 5xx, not be mislabeled a
// config problem.
func IsRulesMisconfig(err error) bool {
	var rle *sessionmanager.RulesLoadError
	return errors.As(err, &rle)
}

// SessionPromptAssembler assembles the worker/orchestrator system prompt for a
// project. *session_manager.Manager satisfies it via RoleSystemPrompt.
type SessionPromptAssembler interface {
	RoleSystemPrompt(ctx context.Context, kind domain.SessionKind, projectID domain.ProjectID) (string, error)
	EffectiveWorkerTaskPrompt(ctx context.Context, projectID domain.ProjectID) (template, source string, err error)
}

// Result keeps the exact system prompt separate from optional worker task
// template metadata. Task prompts and system prompts are distinct delivery
// channels and must not be presented as one assembled instruction string.
type Result struct {
	Prompt             string
	TaskPromptTemplate string
	TaskPromptSource   string
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

// RolePrompt returns the exact assembled system prompt plus optional worker
// task-template metadata for a (project, role). Unknown roles return
// ErrUnknownRole; a missing project returns ErrProjectNotFound; a misconfigured
// operator rules override returns the same fail-closed error a spawn would
// raise, rather than a prompt with the override silently omitted.
func (a *Assembler) RolePrompt(ctx context.Context, projectID domain.ProjectID, role string) (Result, error) {
	if role != RoleWorker && role != RoleOrchestrator && role != RoleReviewer {
		return Result{}, fmt.Errorf("%w: %q", ErrUnknownRole, role)
	}
	proj, ok, err := a.projects.GetProject(ctx, string(projectID))
	if err != nil {
		return Result{}, err
	}
	if !ok {
		return Result{}, fmt.Errorf("%w: %q", ErrProjectNotFound, projectID)
	}
	switch role {
	case RoleWorker:
		prompt, err := a.sessions.RoleSystemPrompt(ctx, domain.KindWorker, projectID)
		if err != nil {
			return Result{}, err
		}
		template, source, err := a.sessions.EffectiveWorkerTaskPrompt(ctx, projectID)
		if err != nil {
			return Result{}, err
		}
		return Result{Prompt: prompt, TaskPromptTemplate: template, TaskPromptSource: source}, nil
	case RoleOrchestrator:
		prompt, err := a.sessions.RoleSystemPrompt(ctx, domain.KindOrchestrator, projectID)
		return Result{Prompt: prompt}, err
	default: // RoleReviewer
		// Same loader the reviewer spawn path uses (review.ReviewerRules), so
		// what the operator inspects matches what the reviewer is launched with.
		rules, err := review.ReviewerRules(string(projectID), proj.Path, proj.Config)
		if err != nil {
			return Result{}, err
		}
		return Result{Prompt: review.AssembleReviewerSystemPrompt(rules)}, nil
	}
}
