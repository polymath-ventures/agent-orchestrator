package sessionmanager

import (
	"errors"
	"strings"
	"testing"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

// pauseManager builds a manager over project "mer" with a worker-capable harness
// so a spawn that clears the pause guard can proceed. It returns the store so a
// test can flip the project or fleet pause bits.
func pauseManager() (*Manager, *fakeStore) {
	return capManager(domain.ProjectConfig{
		Worker:       domain.RoleOverride{Harness: domain.HarnessClaudeCode},
		Orchestrator: domain.RoleOverride{Harness: domain.HarnessClaudeCode},
		Prime:        domain.RoleOverride{Harness: domain.HarnessClaudeCode},
	})
}

// A worker spawn is refused with ErrProjectPaused when the project itself is
// paused, and the refusal creates no durable state.
func TestSpawn_ProjectPausedRefusesWorker(t *testing.T) {
	m, st := pauseManager()
	p := st.projects["mer"]
	p.Paused = true
	st.projects["mer"] = p
	before := len(st.sessions)

	_, _, _, err := m.Spawn(ctx, ports.SpawnConfig{ProjectID: "mer", Kind: domain.KindWorker})
	if !errors.Is(err, ErrProjectPaused) {
		t.Fatalf("spawn err = %v, want ErrProjectPaused", err)
	}
	if got := len(st.sessions); got != before {
		t.Fatalf("session count = %d, want %d — a paused refusal must create no row", got, before)
	}
}

// A worker spawn is refused when the fleet is paused even though the project's
// own bit is clear.
func TestSpawn_FleetPausedRefusesWorker(t *testing.T) {
	m, st := pauseManager()
	st.fleetPaused = true

	_, _, _, err := m.Spawn(ctx, ports.SpawnConfig{ProjectID: "mer", Kind: domain.KindWorker})
	if !errors.Is(err, ErrProjectPaused) {
		t.Fatalf("spawn err = %v, want ErrProjectPaused", err)
	}
}

// The pause guard is an authoritative safety gate. If the fleet flag cannot be
// read, a worker spawn must fail closed instead of proceeding as though the fleet
// were running.
func TestSpawn_FleetPauseReadErrorRefusesWorker(t *testing.T) {
	m, st := pauseManager()
	st.fleetPausedErr = errors.New("pause store unavailable")
	before := len(st.sessions)

	_, _, _, err := m.Spawn(ctx, ports.SpawnConfig{ProjectID: "mer", Kind: domain.KindWorker})
	if err == nil || !strings.Contains(err.Error(), "get fleet paused") {
		t.Fatalf("spawn err = %v, want fleet-pause read error", err)
	}
	if got := len(st.sessions); got != before {
		t.Fatalf("session count = %d, want %d — fail-closed refusal must create no row", got, before)
	}
}

// Orchestrators are exempt: a paused scope must not block orchestrator spawns,
// so supervision and alerting keep running.
func TestSpawn_PausedAllowsOrchestrator(t *testing.T) {
	m, st := pauseManager()
	p := st.projects["mer"]
	p.Paused = true
	st.projects["mer"] = p

	if _, _, _, err := m.Spawn(ctx, ports.SpawnConfig{ProjectID: "mer", Kind: domain.KindOrchestrator}); err != nil {
		t.Fatalf("orchestrator spawn under pause = %v, want nil", err)
	}
}

// Prime is the fleet's meta tier and must stay spawnable during pauses, just
// like project orchestrators.
func TestSpawn_PausedAllowsPrime(t *testing.T) {
	m, st := pauseManager()
	p := st.projects["mer"]
	p.Paused = true
	st.projects["mer"] = p

	if _, _, _, err := m.Spawn(ctx, ports.SpawnConfig{ProjectID: "mer", Kind: domain.KindPrime}); err != nil {
		t.Fatalf("prime spawn under pause = %v, want nil", err)
	}
}

// A forced spawn overrides the pause guard.
func TestSpawn_ForceOverridesPause(t *testing.T) {
	m, st := pauseManager()
	p := st.projects["mer"]
	p.Paused = true
	st.projects["mer"] = p

	if _, _, _, err := m.Spawn(ctx, ports.SpawnConfig{ProjectID: "mer", Kind: domain.KindWorker, Force: true}); err != nil {
		t.Fatalf("forced worker spawn under pause = %v, want nil", err)
	}
}

// With neither bit set a worker spawn proceeds normally.
func TestSpawn_NotPausedAllowsWorker(t *testing.T) {
	m, _ := pauseManager()
	if _, _, _, err := m.Spawn(ctx, ports.SpawnConfig{ProjectID: "mer", Kind: domain.KindWorker}); err != nil {
		t.Fatalf("unpaused worker spawn = %v, want nil", err)
	}
}
