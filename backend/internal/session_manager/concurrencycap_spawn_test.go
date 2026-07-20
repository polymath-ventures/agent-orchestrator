package sessionmanager

import (
	"errors"
	"testing"

	"github.com/aoagents/agent-orchestrator/backend/internal/candidatehealth"
	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

// capManager builds a manager over project "mer" with cfg. It returns the store
// so a test can seed live workers and assert no new row was created on a
// refusal. A Worker role harness is set by callers that need non-mix resolution
// to succeed once the cap admits the spawn.
func capManager(cfg domain.ProjectConfig) (*Manager, *fakeStore) {
	st := newFakeStore()
	st.projects["mer"] = domain.ProjectRecord{ID: "mer", Config: cfg}
	m := New(Deps{
		Runtime: &fakeRuntime{}, Agents: fakeAgents{}, Workspace: &fakeWorkspace{}, Store: st,
		Messenger: &fakeMessenger{}, Lifecycle: &fakeLCM{store: st},
		LookPath: func(string) (string, error) { return "/bin/true", nil },
	})
	return m, st
}

// seedLiveWorker inserts a non-terminated worker row directly, bypassing Spawn,
// so a test can put the project at an arbitrary live-worker count. The id is a
// custom prefix so it never collides with CreateSession's "mer-<n>" ids.
func seedLiveWorker(st *fakeStore, id string) {
	st.sessions[domain.SessionID(id)] = domain.SessionRecord{
		ID: domain.SessionID(id), ProjectID: "mer", Kind: domain.KindWorker,
		Harness: domain.HarnessClaudeCode,
	}
}

// A worker spawn is refused with ErrWorkerConcurrencyCap once the project is at
// its cap, and the refusal creates no durable state: the cap check runs before
// CreateSession, and workspace creation runs strictly after it, so an unchanged
// session count proves neither a row nor a workspace was created.
func TestSpawn_AtCapRefusedCreatesNothing(t *testing.T) {
	m, st := capManager(domain.ProjectConfig{
		MaxLiveWorkers: 2,
		Worker:         domain.RoleOverride{Harness: domain.HarnessClaudeCode},
	})
	seedLiveWorker(st, "mer-live-a")
	seedLiveWorker(st, "mer-live-b")
	before := len(st.sessions)

	_, err := m.Spawn(ctx, ports.SpawnConfig{ProjectID: "mer", Kind: domain.KindWorker})
	if !errors.Is(err, ErrWorkerConcurrencyCap) {
		t.Fatalf("spawn err = %v, want ErrWorkerConcurrencyCap", err)
	}
	if got := len(st.sessions); got != before {
		t.Fatalf("session count = %d, want %d — a cap refusal must create no row", got, before)
	}
}

// An orchestrator session does not count toward the worker cap and is never
// limited by it: with the project already at its worker cap, an orchestrator
// spawn is still admitted.
func TestSpawn_OrchestratorDoesNotCountTowardCap(t *testing.T) {
	m, st := capManager(domain.ProjectConfig{
		MaxLiveWorkers: 1,
		Worker:         domain.RoleOverride{Harness: domain.HarnessClaudeCode},
		Orchestrator:   domain.RoleOverride{Harness: domain.HarnessClaudeCode},
	})
	seedLiveWorker(st, "mer-live-a") // project is now at the worker cap

	// An orchestrator spawn is admitted even at the worker cap.
	if _, err := m.Spawn(ctx, ports.SpawnConfig{ProjectID: "mer", Kind: domain.KindOrchestrator}); err != nil {
		t.Fatalf("orchestrator spawn at worker cap = %v, want admitted", err)
	}
	// And that live orchestrator does not consume worker-cap capacity: a worker
	// spawn is still refused because only the one live worker counts.
	if _, err := m.Spawn(ctx, ports.SpawnConfig{ProjectID: "mer", Kind: domain.KindWorker}); !errors.Is(err, ErrWorkerConcurrencyCap) {
		t.Fatalf("worker spawn err = %v, want ErrWorkerConcurrencyCap — the orchestrator must not free worker capacity", err)
	}
}

// Capacity frees when a live worker terminates: a spawn refused at the cap is
// admitted once one of the counted workers is no longer live.
func TestSpawn_CapacityFreesOnTermination(t *testing.T) {
	m, st := capManager(domain.ProjectConfig{
		MaxLiveWorkers: 1,
		Worker:         domain.RoleOverride{Harness: domain.HarnessClaudeCode},
	})
	seedLiveWorker(st, "mer-live-a")

	if _, err := m.Spawn(ctx, ports.SpawnConfig{ProjectID: "mer", Kind: domain.KindWorker}); !errors.Is(err, ErrWorkerConcurrencyCap) {
		t.Fatalf("spawn at cap err = %v, want ErrWorkerConcurrencyCap", err)
	}

	// Terminate the one live worker; the project is now below the cap.
	rec := st.sessions["mer-live-a"]
	rec.IsTerminated = true
	st.sessions["mer-live-a"] = rec

	if _, err := m.Spawn(ctx, ports.SpawnConfig{ProjectID: "mer", Kind: domain.KindWorker}); err != nil {
		t.Fatalf("spawn after a worker terminated = %v, want admitted", err)
	}
}

// A cap refusal is not a launch failure: it marks no candidate down and emits
// no candidate-down event, because being at capacity is not evidence that a
// harness or model is broken. The check also runs before the mix is consulted,
// so a mix-selected spawn refused at the cap never touches candidate health.
func TestSpawn_CapRefusalMarksNoCandidateDown(t *testing.T) {
	sink := &healthRecordingSink{}
	tr := candidatehealth.New(candidatehealth.Config{Source: "session_manager", Telemetry: sink})
	st := newFakeStore()
	st.projects["mer"] = domain.ProjectRecord{ID: "mer", Config: domain.ProjectConfig{
		MaxLiveWorkers: 1,
		WorkerMix:      singleBucketMix(),
	}}
	m := New(Deps{
		Runtime: &fakeRuntime{}, Agents: fakeAgents{}, Workspace: &fakeWorkspace{}, Store: st,
		Messenger: &fakeMessenger{}, Lifecycle: &fakeLCM{store: st},
		LookPath: func(string) (string, error) { return "/bin/true", nil }, Health: tr,
	})
	seedLiveWorker(st, "mer-live-a") // at the cap

	_, err := m.Spawn(ctx, ports.SpawnConfig{ProjectID: "mer", Kind: domain.KindWorker})
	if !errors.Is(err, ErrWorkerConcurrencyCap) {
		t.Fatalf("spawn err = %v, want ErrWorkerConcurrencyCap", err)
	}
	if tr.IsDown(workerMixCandidate(domain.HarnessClaudeCode, "")) {
		t.Fatal("a cap refusal must not mark any candidate down")
	}
	if got := sink.count(candidatehealth.EventCandidateDown); got != 0 {
		t.Fatalf("candidate-down events = %d, want 0 on a cap refusal", got)
	}
}

// A zero (absent) cap is unbounded: with no MaxLiveWorkers configured, worker
// spawns are never refused by this feature no matter how many workers are live.
func TestSpawn_ZeroCapIsUnbounded(t *testing.T) {
	m, st := capManager(domain.ProjectConfig{
		Worker: domain.RoleOverride{Harness: domain.HarnessClaudeCode},
	})
	for i := 0; i < 5; i++ {
		seedLiveWorker(st, "mer-live-"+string(rune('a'+i)))
	}

	if _, err := m.Spawn(ctx, ports.SpawnConfig{ProjectID: "mer", Kind: domain.KindWorker}); err != nil {
		t.Fatalf("spawn with no cap and 5 live workers = %v, want admitted", err)
	}
}
