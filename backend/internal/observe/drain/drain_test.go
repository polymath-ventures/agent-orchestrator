package drain

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
	sessionsvc "github.com/aoagents/agent-orchestrator/backend/internal/service/session"
)

var errTestKill = errors.New("kill failed")

type fakeStore struct {
	projects    []domain.ProjectRecord
	fleetPaused bool
}

func (f *fakeStore) ListProjects(context.Context) ([]domain.ProjectRecord, error) {
	return append([]domain.ProjectRecord(nil), f.projects...), nil
}
func (f *fakeStore) GetFleetPaused(context.Context) (bool, error) { return f.fleetPaused, nil }

type fakeSessions struct {
	byProject map[domain.ProjectID][]domain.Session
	killed    []domain.SessionID
	// killErr marks ids whose Kill returns an error (transient failure), so they
	// stay live and retry next tick.
	killErr map[domain.SessionID]bool
	// killDirty marks ids whose Kill returns (false, nil): the session is
	// terminated but its dirty worktree is preserved (bool = reclamation, not
	// termination). These must still count as drained.
	killDirty map[domain.SessionID]bool
}

func (f *fakeSessions) List(_ context.Context, filter sessionsvc.ListFilter) ([]domain.Session, error) {
	return append([]domain.Session(nil), f.byProject[filter.ProjectID]...), nil
}

func (f *fakeSessions) Kill(_ context.Context, id domain.SessionID) (bool, error) {
	if f.killErr[id] {
		return false, errTestKill
	}
	f.killed = append(f.killed, id)
	f.markTerminated(id)
	if f.killDirty[id] {
		return false, nil // terminated, but dirty worktree preserved
	}
	return true, nil
}

func (f *fakeSessions) markTerminated(id domain.SessionID) {
	// Model the clean teardown: the session becomes terminated, so a later tick
	// no longer sees it as live.
	for pid, list := range f.byProject {
		for i := range list {
			if list[i].ID == id {
				list[i].IsTerminated = true
				f.byProject[pid] = list
			}
		}
	}
}

func worker(id domain.ProjectID, n string, status domain.SessionStatus) domain.Session {
	return domain.Session{
		SessionRecord: domain.SessionRecord{ID: domain.SessionID(string(id) + "-" + n), ProjectID: id, Kind: domain.KindWorker},
		Status:        status,
	}
}

func fixedClock() func() time.Time {
	return func() time.Time { return time.Unix(1700000000, 0).UTC() }
}

func TestDrainablePredicate(t *testing.T) {
	drainableStatuses := map[domain.SessionStatus]bool{
		domain.StatusIdle:             true,
		domain.StatusMerged:           true,
		domain.StatusWorking:          false,
		domain.StatusPROpen:           false,
		domain.StatusNeedsInput:       false,
		domain.StatusReviewPending:    false,
		domain.StatusChangesRequested: false,
		domain.StatusNoSignal:         false,
		domain.StatusTerminated:       false,
	}
	for status, want := range drainableStatuses {
		if got := drainable(status); got != want {
			t.Errorf("drainable(%q) = %v, want %v", status, got, want)
		}
	}
}

// A gated project's idle/merged workers are killed; a working worker is left to
// finish. Ungated projects are never touched.
func TestTick_DrainsIdleLeavesWorking(t *testing.T) {
	store := &fakeStore{projects: []domain.ProjectRecord{
		{ID: "paused", Paused: true},
		{ID: "running", Paused: false},
	}}
	sessions := &fakeSessions{byProject: map[domain.ProjectID][]domain.Session{
		"paused":  {worker("paused", "1", domain.StatusIdle), worker("paused", "2", domain.StatusWorking)},
		"running": {worker("running", "1", domain.StatusIdle)},
	}}
	sw := New(store, sessions, Config{Clock: fixedClock()})

	if err := sw.Tick(context.Background()); err != nil {
		t.Fatalf("Tick: %v", err)
	}
	if len(sessions.killed) != 1 || sessions.killed[0] != "paused-1" {
		t.Fatalf("killed = %v, want only paused-1 (idle worker of the paused project)", sessions.killed)
	}
}

// A fleet pause drains every project even when its own bit is clear.
func TestTick_FleetPausedDrainsAll(t *testing.T) {
	store := &fakeStore{
		fleetPaused: true,
		projects:    []domain.ProjectRecord{{ID: "a"}, {ID: "b"}},
	}
	sessions := &fakeSessions{byProject: map[domain.ProjectID][]domain.Session{
		"a": {worker("a", "1", domain.StatusIdle)},
		"b": {worker("b", "1", domain.StatusMerged)},
	}}
	sw := New(store, sessions, Config{Clock: fixedClock()})
	if err := sw.Tick(context.Background()); err != nil {
		t.Fatalf("Tick: %v", err)
	}
	if len(sessions.killed) != 2 {
		t.Fatalf("killed %d, want 2 (both projects drained under fleet pause)", len(sessions.killed))
	}
}

// drain_complete telemetry fires exactly once, on the transition to zero live
// workers — not on every subsequent tick.
func TestTick_DrainCompleteFiresOnce(t *testing.T) {
	store := &fakeStore{projects: []domain.ProjectRecord{{ID: "p", Paused: true}}}
	sessions := &fakeSessions{byProject: map[domain.ProjectID][]domain.Session{
		"p": {worker("p", "1", domain.StatusIdle)},
	}}
	sink := &captureSink{}
	sw := New(store, sessions, Config{Clock: fixedClock(), Telemetry: sink})

	for i := 0; i < 3; i++ {
		if err := sw.Tick(context.Background()); err != nil {
			t.Fatalf("Tick %d: %v", i, err)
		}
	}
	drainEvents := 0
	for _, ev := range sink.events {
		if ev.Name == "ao.fleet.drain_complete" {
			drainEvents++
		}
	}
	if drainEvents != 1 {
		t.Fatalf("drain_complete fired %d times, want exactly 1", drainEvents)
	}
}

// A Kill that errors leaves the worker live (retried next tick) and defers the
// drain-complete signal; a Kill that succeeds (even bool=false for a preserved
// dirty worktree — the session is still terminated) counts as drained.
func TestTick_KillErrorKeepsWorkerLive(t *testing.T) {
	store := &fakeStore{projects: []domain.ProjectRecord{{ID: "p", Paused: true}}}
	sessions := &fakeSessions{
		byProject: map[domain.ProjectID][]domain.Session{"p": {worker("p", "1", domain.StatusIdle)}},
		killErr:   map[domain.SessionID]bool{"p-1": true},
	}
	sink := &captureSink{}
	sw := New(store, sessions, Config{Clock: fixedClock(), Telemetry: sink})
	if err := sw.Tick(context.Background()); err != nil {
		t.Fatalf("Tick: %v", err)
	}
	for _, ev := range sink.events {
		if ev.Name == "ao.fleet.drain_complete" {
			t.Fatalf("drain_complete fired while a worker is still live (kill errored)")
		}
	}
}

// A worker whose Kill reports (false, nil) — terminated, but a dirty worktree
// was preserved — still counts as drained, so drain-complete fires.
func TestTick_DirtyPreservedKillCountsDrained(t *testing.T) {
	store := &fakeStore{projects: []domain.ProjectRecord{{ID: "p", Paused: true}}}
	sessions := &fakeSessions{
		byProject: map[domain.ProjectID][]domain.Session{"p": {worker("p", "1", domain.StatusIdle)}},
		killDirty: map[domain.SessionID]bool{"p-1": true},
	}
	sink := &captureSink{}
	sw := New(store, sessions, Config{Clock: fixedClock(), Telemetry: sink})
	if err := sw.Tick(context.Background()); err != nil {
		t.Fatalf("Tick: %v", err)
	}
	drainEvents := 0
	for _, ev := range sink.events {
		if ev.Name == "ao.fleet.drain_complete" {
			drainEvents++
		}
	}
	if drainEvents != 1 {
		t.Fatalf("drain_complete fired %d times, want 1 (dirty-preserved kill still drains)", drainEvents)
	}
}

type captureSink struct{ events []ports.TelemetryEvent }

func (s *captureSink) Emit(_ context.Context, ev ports.TelemetryEvent) {
	s.events = append(s.events, ev)
}
func (*captureSink) Close(context.Context) error { return nil }

// A fresh spawn reads idle until its first hook callback arrives, so soft
// drain must not kill it during the boot grace. A signal-less idle past the
// grace (hook-less harness) and a genuinely idle signaled worker both drain.
func TestTick_SkipsPreFirstSignalIdleWithinGrace(t *testing.T) {
	now := fixedClock()()
	fresh := worker("paused", "1", domain.StatusIdle)
	fresh.CreatedAt = now.Add(-10 * time.Second) // mid-boot, no signal yet
	hookless := worker("paused", "2", domain.StatusIdle)
	hookless.CreatedAt = now.Add(-5 * time.Minute) // past grace, never signals
	signaled := worker("paused", "3", domain.StatusIdle)
	signaled.CreatedAt = now.Add(-10 * time.Second)
	signaled.FirstSignalAt = now.Add(-5 * time.Second) // real idle reading

	store := &fakeStore{projects: []domain.ProjectRecord{{ID: "paused", Paused: true}}}
	sessions := &fakeSessions{byProject: map[domain.ProjectID][]domain.Session{
		"paused": {fresh, hookless, signaled},
	}}
	sw := New(store, sessions, Config{Clock: fixedClock()})

	if err := sw.Tick(context.Background()); err != nil {
		t.Fatalf("Tick: %v", err)
	}
	want := []domain.SessionID{"paused-2", "paused-3"}
	if len(sessions.killed) != 2 || sessions.killed[0] != want[0] || sessions.killed[1] != want[1] {
		t.Fatalf("killed = %v, want %v (mid-boot paused-1 must be spared)", sessions.killed, want)
	}
}
