package project_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/service/project"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite"
)

// fakePauseSessions records Kill calls and satisfies the project service's
// session collaborator (teardown + kill).
type fakePauseSessions struct {
	mu        sync.Mutex
	killed    []domain.SessionID
	attempted []domain.SessionID
	killErr   error
	failIDs   map[domain.SessionID]bool
	torndown  []domain.ProjectID
}

func (f *fakePauseSessions) TeardownProject(_ context.Context, p domain.ProjectID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.torndown = append(f.torndown, p)
	return nil
}

func (f *fakePauseSessions) Kill(_ context.Context, id domain.SessionID) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.attempted = append(f.attempted, id)
	if f.killErr != nil {
		return false, f.killErr
	}
	if f.failIDs[id] {
		return false, errKillFailed
	}
	f.killed = append(f.killed, id)
	return true, nil
}

var errKillFailed = errors.New("kill failed")

func newPauseStore(t *testing.T) *sqlite.Store {
	t.Helper()
	s, err := sqlite.Open(t.TempDir())
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func seedPauseProject(t *testing.T, s *sqlite.Store, id string) {
	t.Helper()
	if err := s.UpsertProject(context.Background(), domain.ProjectRecord{
		ID: id, Path: "/tmp/" + id, RegisteredAt: time.Now().UTC().Truncate(time.Second),
	}); err != nil {
		t.Fatalf("seed project %s: %v", id, err)
	}
}

func seedSession(t *testing.T, s *sqlite.Store, project string, kind domain.SessionKind) domain.SessionID {
	t.Helper()
	now := time.Now().UTC().Truncate(time.Second)
	rec, err := s.CreateSession(context.Background(), domain.SessionRecord{
		ProjectID: domain.ProjectID(project),
		Kind:      kind,
		Harness:   domain.HarnessClaudeCode,
		Activity:  domain.Activity{State: domain.ActivityActive, LastActivityAt: now},
		Metadata:  domain.SessionMetadata{Branch: "feat/x", WorkspacePath: "/ws"},
		CreatedAt: now,
		UpdatedAt: now,
	})
	if err != nil {
		t.Fatalf("seed session: %v", err)
	}
	return rec.ID
}

// A soft project pause writes the bit, reports the draining/paused state from
// live workers, and terminates nothing.
func TestSetProjectPausedSoft(t *testing.T) {
	ctx := context.Background()
	store := newPauseStore(t)
	seedPauseProject(t, store, "proj")
	seedSession(t, store, "proj", domain.KindWorker) // one live worker
	sessions := &fakePauseSessions{}
	m := project.NewWithDeps(project.Deps{Store: store, Sessions: sessions})

	got, err := m.SetProjectPaused(ctx, "proj", true, false)
	if err != nil {
		t.Fatalf("SetProjectPaused: %v", err)
	}
	if !got.Paused || got.PauseState != project.PauseStateDraining || got.DrainingWorkers != 1 {
		t.Fatalf("got Paused=%v state=%q draining=%d, want true/draining/1", got.Paused, got.PauseState, got.DrainingWorkers)
	}
	if len(sessions.killed) != 0 {
		t.Fatalf("soft pause killed %v, want none", sessions.killed)
	}
}

// A hard project pause terminates the project's live workers immediately but
// leaves orchestrators alone.
func TestSetProjectPausedHardKillsWorkersNotOrchestrators(t *testing.T) {
	ctx := context.Background()
	store := newPauseStore(t)
	seedPauseProject(t, store, "proj")
	w := seedSession(t, store, "proj", domain.KindWorker)
	seedSession(t, store, "proj", domain.KindOrchestrator)
	sessions := &fakePauseSessions{}
	m := project.NewWithDeps(project.Deps{Store: store, Sessions: sessions})

	if _, err := m.SetProjectPaused(ctx, "proj", true, true); err != nil {
		t.Fatalf("SetProjectPaused hard: %v", err)
	}
	if len(sessions.killed) != 1 || sessions.killed[0] != w {
		t.Fatalf("hard pause killed %v, want only worker %s", sessions.killed, w)
	}
}

// A hard drain is best-effort: one worker's Kill failure must not stop the
// others from being terminated. Every eligible worker is attempted and the
// error is surfaced.
func TestSetProjectPausedHardIsBestEffortOnKillError(t *testing.T) {
	ctx := context.Background()
	store := newPauseStore(t)
	seedPauseProject(t, store, "proj")
	w1 := seedSession(t, store, "proj", domain.KindWorker)
	w2 := seedSession(t, store, "proj", domain.KindWorker)
	sessions := &fakePauseSessions{failIDs: map[domain.SessionID]bool{w1: true}}
	m := project.NewWithDeps(project.Deps{Store: store, Sessions: sessions})

	_, err := m.SetProjectPaused(ctx, "proj", true, true)
	if err == nil {
		t.Fatalf("expected an error surfaced from the failing Kill")
	}
	if len(sessions.attempted) != 2 {
		t.Fatalf("attempted %d kills, want 2 (both workers attempted despite w1 failing)", len(sessions.attempted))
	}
	if len(sessions.killed) != 1 || sessions.killed[0] != w2 {
		t.Fatalf("killed = %v, want only w2 (%s) succeeded", sessions.killed, w2)
	}
}

// Fleet pause is a daemon-global flag that round-trips and is reflected in the
// per-project derived state.
func TestFleetPauseReflectedInProjectState(t *testing.T) {
	ctx := context.Background()
	store := newPauseStore(t)
	seedPauseProject(t, store, "proj")
	m := project.NewWithDeps(project.Deps{Store: store})

	if paused, err := m.FleetPaused(ctx); err != nil || paused {
		t.Fatalf("initial FleetPaused = %v/%v, want false/nil", paused, err)
	}
	if err := m.SetFleetPaused(ctx, true, false); err != nil {
		t.Fatalf("SetFleetPaused: %v", err)
	}
	if paused, err := m.FleetPaused(ctx); err != nil || !paused {
		t.Fatalf("FleetPaused after set = %v/%v, want true/nil", paused, err)
	}

	// The project's own bit is unset, but the fleet flag gates it: with no live
	// workers it reads as paused.
	list, err := m.List(ctx)
	if err != nil || len(list) != 1 {
		t.Fatalf("List = %v/%v", list, err)
	}
	if list[0].PauseState != project.PauseStatePaused {
		t.Fatalf("fleet-paused project state = %q, want paused", list[0].PauseState)
	}
}

// A hard fleet pause fans out worker termination across all projects but leaves
// orchestrators alive (they stay up so supervision/alerting keeps running).
func TestSetFleetPausedHardDrainsWorkersNotOrchestrators(t *testing.T) {
	ctx := context.Background()
	store := newPauseStore(t)
	seedPauseProject(t, store, "p1")
	seedPauseProject(t, store, "p2")
	w := seedSession(t, store, "p1", domain.KindWorker)
	seedSession(t, store, "p2", domain.KindOrchestrator)
	sessions := &fakePauseSessions{}
	m := project.NewWithDeps(project.Deps{Store: store, Sessions: sessions})

	if err := m.SetFleetPaused(ctx, true, true); err != nil {
		t.Fatalf("SetFleetPaused hard: %v", err)
	}
	if len(sessions.killed) != 1 || sessions.killed[0] != w {
		t.Fatalf("fleet hard pause killed %v, want only the worker %s (orchestrators stay alive)", sessions.killed, w)
	}
}
