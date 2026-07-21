package project

import (
	"context"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
)

// Store is the durable project persistence surface required by Service.
type Store interface {
	ListProjects(ctx context.Context) ([]domain.ProjectRecord, error)
	GetProject(ctx context.Context, id string) (domain.ProjectRecord, bool, error)
	FindProjectByPath(ctx context.Context, path string) (domain.ProjectRecord, bool, error)
	UpsertProject(ctx context.Context, row domain.ProjectRecord) error
	UpsertWorkspaceProject(ctx context.Context, row domain.ProjectRecord, repos []domain.WorkspaceRepoRecord) error
	ListWorkspaceRepos(ctx context.Context, projectID string) ([]domain.WorkspaceRepoRecord, error)
	ArchiveProject(ctx context.Context, id string, at time.Time) (bool, error)

	// Pause persistence. SetProjectPaused writes only the per-project pause bit
	// (never config); the fleet flag is a daemon-global singleton. ListAllSessions
	// backs the live-worker count that derives the observable pause state.
	SetProjectPaused(ctx context.Context, id string, paused bool) (bool, error)
	GetFleetPaused(ctx context.Context) (bool, error)
	SetFleetPaused(ctx context.Context, paused bool) error
	ListAllSessions(ctx context.Context) ([]domain.SessionRecord, error)
}
