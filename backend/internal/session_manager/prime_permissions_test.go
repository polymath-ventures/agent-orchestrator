package sessionmanager

import (
	"testing"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

// #144 — the unattended permission mode fleet Prime defaults to must survive the
// whole spawn path, not just the domain type.
//
// Prime is projectless: it has no ProjectConfig.AgentConfig to inherit a
// permission mode from, so whatever the stored settings resolve to is what the
// harness is launched with. An empty mode emits no --permission-mode flag, and
// an enabled Prime then blocks on the first tool prompt with nobody at its pane
// to answer. domain-level tests pin DefaultPrimeSettings/WithDefaults; this one
// pins the only thing that actually matters operationally — that the value
// reaches ports.LaunchConfig, and therefore the launched agent.
//
// Deliberately spawns through the real Manager.Spawn and reads the LaunchConfig
// the agent adapter was handed. Nothing here recomputes the expected mode from
// settings; the settings row stored below leaves Permissions unset, exactly as
// an operator who enabled Prime without touching permissions would.
func TestSpawn_ProjectlessPrimeLaunchesWithUnattendedPermissions(t *testing.T) {
	st := newFakeStore()
	st.prime = domain.PrimeSettings{
		Enabled:     true,
		DisplayName: "Fleet Lead",
		Harness:     domain.HarnessCodex,
	}
	if st.prime.AgentConfig.Permissions != "" {
		t.Fatalf("fixture stores an explicit permission mode (%q); the point is to exercise the daemon default",
			st.prime.AgentConfig.Permissions)
	}
	agent := &recordingAgent{}
	m := New(Deps{
		Runtime:   &fakeRuntime{},
		Agents:    singleAgent{agent: agent},
		Workspace: &fakeWorkspace{},
		Store:     st,
		Messenger: &fakeMessenger{},
		Lifecycle: &fakeLCM{store: st},
		DataDir:   t.TempDir(),
		LookPath:  func(string) (string, error) { return "/bin/true", nil },
	})

	if _, _, _, err := m.Spawn(ctx, ports.SpawnConfig{Kind: domain.KindPrime}); err != nil {
		t.Fatalf("Spawn projectless Prime: %v", err)
	}
	if agent.launchCalls != 1 {
		t.Fatalf("launch calls = %d, want 1; the assertion below is only meaningful if the real launch happened", agent.launchCalls)
	}
	if got := agent.lastLaunch.Permissions; got != ports.PermissionModeBypassPermissions {
		t.Fatalf("launched Prime with permission mode %q, want %q; an unattended Prime with no permission mode blocks on the first tool prompt",
			got, ports.PermissionModeBypassPermissions)
	}
	// The launch config's Config carries the same mode, so an adapter reading
	// either field agrees.
	if got := agent.lastLaunch.Config.Permissions; got != ports.PermissionModeBypassPermissions {
		t.Fatalf("launch agent config permission mode = %q, want %q", got, ports.PermissionModeBypassPermissions)
	}
}
