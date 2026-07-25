package sessionmanager

import (
	"bytes"
	"log/slog"
	"strings"
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

// primeSpawnManager builds a manager over the supplied stored Prime settings and
// returns it alongside the buffer its logger writes to.
func primeSpawnManager(t *testing.T, settings domain.PrimeSettings) (*Manager, *bytes.Buffer) {
	t.Helper()
	st := newFakeStore()
	st.prime = settings
	logs := &bytes.Buffer{}
	m := New(Deps{
		Runtime:   &fakeRuntime{},
		Agents:    singleAgent{agent: &recordingAgent{}},
		Workspace: &fakeWorkspace{},
		Store:     st,
		Messenger: &fakeMessenger{},
		Lifecycle: &fakeLCM{store: st},
		DataDir:   t.TempDir(),
		LookPath:  func(string) (string, error) { return "/bin/true", nil },
		Logger:    slog.New(slog.NewTextHandler(logs, nil)),
	})
	return m, logs
}

// The unattended default is applied on READ rather than migrated into stored
// rows, because an empty stored mode means "never configured" (Prime exposes no
// CLI flag or UI control for the field) and the behavior it produced was the
// stall this default exists to end. What it must not be is silent: the operator
// has to be able to see, from the daemon log, that the Prime now running took
// its permission mode from the daemon rather than from settings, and how to
// change it.
//
// The signal is emitted where the mode takes effect — the spawn — not inside
// WithDefaults, which runs on every settings read and would drown the log.
func TestSpawn_PrimeLogsWhenPermissionModeComesFromTheDaemonDefault(t *testing.T) {
	m, logs := primeSpawnManager(t, domain.PrimeSettings{
		Enabled: true, DisplayName: "Fleet Lead", Harness: domain.HarnessCodex,
	})

	if _, _, _, err := m.Spawn(ctx, ports.SpawnConfig{Kind: domain.KindPrime}); err != nil {
		t.Fatalf("Spawn projectless Prime: %v", err)
	}
	got := logs.String()
	if !strings.Contains(got, "no permission mode stored in Prime settings") {
		t.Fatalf("logs = %q, want the applied-default notice; an operator cannot otherwise tell the Prime is running unattended by daemon default", got)
	}
	if !strings.Contains(got, string(domain.PermissionModeBypassPermissions)) {
		t.Fatalf("logs = %q, want the applied permission mode named", got)
	}
	if !strings.Contains(got, "/prime/settings") {
		t.Fatalf("logs = %q, want the override route so the notice is actionable", got)
	}
}

// The notice describes a default that was APPLIED. An operator who stored a mode
// explicitly — including the "default" that restores prompting — configured it
// themselves, and telling them the daemon chose it would be wrong, not merely
// noisy.
func TestSpawn_PrimeDoesNotLogTheDefaultNoticeWhenPermissionsAreStored(t *testing.T) {
	for _, stored := range []domain.PermissionMode{domain.PermissionModeDefault, domain.PermissionModeBypassPermissions} {
		t.Run(string(stored), func(t *testing.T) {
			m, logs := primeSpawnManager(t, domain.PrimeSettings{
				Enabled: true, DisplayName: "Fleet Lead", Harness: domain.HarnessCodex,
				AgentConfig: domain.AgentConfig{Permissions: stored},
			})

			if _, _, _, err := m.Spawn(ctx, ports.SpawnConfig{Kind: domain.KindPrime}); err != nil {
				t.Fatalf("Spawn projectless Prime: %v", err)
			}
			if got := logs.String(); strings.Contains(got, "no permission mode stored in Prime settings") {
				t.Fatalf("logs = %q, want no applied-default notice: %q was stored explicitly", got, stored)
			}
		})
	}
}

// Preflight is a speculative pass over the same preconditions, and a role
// reconcile runs it and then spawns. Emitting the notice on both would report
// one launch twice.
func TestPreflight_PrimeDoesNotLogTheAppliedDefaultNotice(t *testing.T) {
	m, logs := primeSpawnManager(t, domain.PrimeSettings{
		Enabled: true, DisplayName: "Fleet Lead", Harness: domain.HarnessCodex,
	})

	if err := m.Preflight(ctx, ports.SpawnConfig{Kind: domain.KindPrime}); err != nil {
		t.Fatalf("Preflight projectless Prime: %v", err)
	}
	if got := logs.String(); strings.Contains(got, "no permission mode stored in Prime settings") {
		t.Fatalf("logs = %q, want the notice only from the pass that actually launches", got)
	}
}

// A role reconcile preflights and then spawns, running the model-validation gate
// twice for ONE launch. The verdict must be identical on both passes — that is
// the whole point of sharing the code — but the operator-facing warning must be
// emitted once, by the pass that actually launches. Two copies of "model
// validation unavailable; continuing spawn" for a single reconcile reads as two
// launches with two problems.
func TestPreflightAndSpawn_LogTheModelValidationWarningOnce(t *testing.T) {
	st := newFakeStore()
	st.projects["mer"] = domain.ProjectRecord{ID: "mer", Config: domain.ProjectConfig{
		Orchestrator: domain.RoleOverride{Harness: domain.HarnessCodex, AgentConfig: domain.AgentConfig{Model: "gpt-5-codex"}},
	}}
	validator := &fakeSpawnSelectionValidator{result: ports.ModelValidationResult{
		Status: ports.ModelValidationProbeUnavailable, Message: "no fresh cached catalog",
	}}
	logs := &bytes.Buffer{}
	m := New(Deps{
		Runtime: &fakeRuntime{}, Agents: fakeAgents{}, Workspace: &fakeWorkspace{}, Store: st,
		Messenger: &fakeMessenger{}, Lifecycle: &fakeLCM{store: st}, ModelValidator: validator,
		LookPath: func(string) (string, error) { return "/bin/true", nil },
		Logger:   slog.New(slog.NewTextHandler(logs, nil)),
	})

	cfg := ports.SpawnConfig{ProjectID: "mer", Kind: domain.KindOrchestrator}
	if err := m.Preflight(ctx, cfg); err != nil {
		t.Fatalf("Preflight: %v", err)
	}
	if got := strings.Count(logs.String(), "model validation unavailable"); got != 0 {
		t.Fatalf("preflight logged the warning %d time(s): %q; the speculative pass must stay quiet", got, logs.String())
	}
	if _, _, _, err := m.Spawn(ctx, cfg); err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	if got := strings.Count(logs.String(), "model validation unavailable"); got != 1 {
		t.Fatalf("warning logged %d time(s) across one preflight+spawn: %q, want exactly 1", got, logs.String())
	}
	// Suppression is only about the log line: both passes must still consult the
	// validator and reach the same verdict.
	if len(validator.calls) != 2 {
		t.Fatalf("validator calls = %#v, want one per pass; the speculative pass must still run the identical gate", validator.calls)
	}
}
