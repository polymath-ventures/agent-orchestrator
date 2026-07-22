package project

import (
	"context"
	"errors"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/httpd/apierr"
)

// PauseState is the derived, observable pause state of a project. It is computed
// at read time from the persisted per-project and fleet pause bits plus the
// count of live workers; it is never itself persisted.
type PauseState string

const (
	// PauseStateRunning means neither the project nor the fleet flag is set.
	PauseStateRunning PauseState = "running"
	// PauseStateDraining means the project is gated but one or more workers are
	// still finishing.
	PauseStateDraining PauseState = "draining"
	// PauseStatePaused means the project is gated and no live workers remain.
	PauseStatePaused PauseState = "paused"
)

// computePauseState derives the observable state from the two persisted bits and
// the live-worker count: running when neither bit is set; draining (with the
// live count) when gated with workers still finishing; paused when gated with
// none left.
func computePauseState(projectPaused, fleetPaused bool, liveWorkers int) (PauseState, int) {
	if !projectPaused && !fleetPaused {
		return PauseStateRunning, 0
	}
	if liveWorkers > 0 {
		return PauseStateDraining, liveWorkers
	}
	return PauseStatePaused, 0
}

// liveWorkersByProject counts non-terminated worker sessions per project id.
// Orchestrators never count — pause does not gate or drain them.
func liveWorkersByProject(sessions []domain.SessionRecord) map[string]int {
	counts := make(map[string]int, len(sessions))
	for _, s := range sessions {
		if s.Kind != domain.KindWorker || s.IsTerminated {
			continue
		}
		counts[string(s.ProjectID)]++
	}
	return counts
}

// pauseState reads the fleet flag and live-worker count for one project and
// derives its observable state. Degraded reads fail open to "not paused"/zero so
// a storage blip never wedges the read model.
func (m *Service) pauseState(ctx context.Context, projectID string, projectPaused bool) (PauseState, int) {
	fleetPaused, err := m.store.GetFleetPaused(ctx)
	if err != nil {
		fleetPaused = false
	}
	live := 0
	if sessions, err := m.store.ListAllSessions(ctx); err == nil {
		live = liveWorkersByProject(sessions)[projectID]
	}
	return computePauseState(projectPaused, fleetPaused, live)
}

// FleetPaused reports the daemon-global fleet pause flag.
func (m *Service) FleetPaused(ctx context.Context) (bool, error) {
	paused, err := m.store.GetFleetPaused(ctx)
	if err != nil {
		return false, apierr.Internal("FLEET_PAUSE_LOAD_FAILED", "Failed to load fleet pause state")
	}
	return paused, nil
}

// SetFleetPaused sets or clears the daemon-global fleet pause flag. When pausing
// hard, it fans out an immediate termination across every project. A fleet-wide
// hard pause is the deliberate emergency stop, so it terminates orchestrators as
// well as workers (best-effort — reports failure if any project's drain errored).
func (m *Service) SetFleetPaused(ctx context.Context, paused, hard bool) error {
	if err := m.store.SetFleetPaused(ctx, paused); err != nil {
		return apierr.Internal("FLEET_PAUSE_FAILED", "Failed to update fleet pause state")
	}
	if !paused || !hard {
		return nil
	}
	projects, err := m.store.ListProjects(ctx)
	if err != nil {
		return apierr.Internal("FLEET_HARD_PAUSE_FAILED", "Failed to enumerate projects for hard drain")
	}
	failed := 0
	for _, row := range projects {
		// Fleet-wide emergency stop: include orchestrators.
		if err := m.hardDrain(ctx, domain.ProjectID(row.ID), true); err != nil {
			failed++
		}
	}
	if failed > 0 {
		return apierr.Internal("FLEET_HARD_PAUSE_FAILED", "Failed to terminate live sessions for some projects")
	}
	return nil
}

// SetProjectPaused sets or clears one project's pause bit and returns the
// resulting read model. When pausing hard, it terminates the project's live
// workers immediately (orchestrators are left running).
func (m *Service) SetProjectPaused(ctx context.Context, id domain.ProjectID, paused, hard bool) (Project, error) {
	if err := validateProjectID(id); err != nil {
		return Project{}, err
	}
	row, ok, err := m.store.GetProject(ctx, string(id))
	if err != nil {
		return Project{}, apierr.Internal("PROJECT_LOAD_FAILED", "Failed to load project")
	}
	if !ok || !row.ArchivedAt.IsZero() {
		return Project{}, apierr.NotFound("PROJECT_NOT_FOUND", "Unknown project")
	}
	if _, err := m.store.SetProjectPaused(ctx, string(id), paused); err != nil {
		return Project{}, apierr.Internal("PROJECT_PAUSE_FAILED", "Failed to update project pause state")
	}
	row.Paused = paused
	if paused && hard {
		// Per-project hard pause spares the orchestrator so it keeps supervising
		// the rest of the fleet.
		if err := m.hardDrain(ctx, id, false); err != nil {
			return Project{}, apierr.Internal("PROJECT_HARD_PAUSE_FAILED", "Failed to terminate live workers")
		}
	}
	p := m.projectFromRow(row)
	p.PauseState, p.DrainingWorkers = m.pauseState(ctx, row.ID, row.Paused)
	return p, nil
}

// hardDrain terminates a project's live sessions through the clean
// session-teardown Kill path. Workers are always drained. Orchestrators are
// drained only when includeOrchestrators is set — this is reserved for the
// fleet-wide hard pause (the emergency stop); a per-project hard pause passes
// false so the orchestrator stays alive and idle to keep supervision and
// alerting running. A nil session collaborator is a no-op. Best-effort: every
// eligible session is attempted and failures are aggregated.
func (m *Service) hardDrain(ctx context.Context, id domain.ProjectID, includeOrchestrators bool) error {
	if m.sessions == nil {
		return nil
	}
	sessions, err := m.store.ListAllSessions(ctx)
	if err != nil {
		return err
	}
	var errs []error
	for _, s := range sessions {
		if string(s.ProjectID) != string(id) || s.IsTerminated {
			continue
		}
		if s.Kind == domain.KindOrchestrator && !includeOrchestrators {
			continue
		}
		// Best-effort: one worker's Kill failure must not leave the rest of the
		// scope's live workers running. Attempt every eligible session and report
		// the aggregate.
		if _, err := m.sessions.Kill(ctx, s.ID); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}
