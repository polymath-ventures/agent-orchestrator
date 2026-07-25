package sessionmanager

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

// renameOnlyAgent names its session with the universal in-harness command, like
// codex: no launch-time flag, so the name is written after the pane exists.
type renameOnlyAgent struct{ fakeAgent }

func (renameOnlyAgent) InHarnessRenameCommand(name string) (string, bool) {
	safe, ok := ports.DeliverableName(name)
	if !ok {
		return "", false
	}
	return "/rename " + safe, true
}

func (renameOnlyAgent) LaunchNameArgs(string) []string { return nil }

// launchNamedAgent also names at launch, like claude-code, and records the name
// it was handed in argv.
type launchNamedAgent struct {
	renameOnlyAgent
	lastLaunchName *string
}

func (a launchNamedAgent) LaunchNameArgs(name string) []string {
	safe, ok := ports.DeliverableName(name)
	if !ok {
		return nil
	}
	return []string{"-n", safe}
}

func (a launchNamedAgent) GetLaunchCommand(_ context.Context, cfg ports.LaunchConfig) ([]string, error) {
	if a.lastLaunchName != nil {
		*a.lastLaunchName = cfg.DisplayName
	}
	return append([]string{"launch"}, a.LaunchNameArgs(cfg.DisplayName)...), nil
}

type agentsFor struct{ agent ports.Agent }

func (a agentsFor) Agent(domain.AgentHarness) (ports.Agent, bool) { return a.agent, true }

func newNamingManager(agent ports.Agent) (*Manager, *fakeStore, *fakeRuntime, *fakeMessenger) {
	st := newFakeStore()
	st.projects["mer"] = domain.ProjectRecord{
		ID:     "mer",
		Config: domain.ProjectConfig{SessionPrefix: "ao", Worker: domain.RoleOverride{Harness: domain.HarnessClaudeCode}, Orchestrator: domain.RoleOverride{Harness: domain.HarnessClaudeCode}},
	}
	rt := &fakeRuntime{aliveByHandle: map[string]bool{"h1": true}}
	msg := &fakeMessenger{}
	m := New(Deps{
		Runtime:   rt,
		Agents:    agentsFor{agent: agent},
		Workspace: &fakeWorkspace{},
		Store:     st,
		Messenger: msg,
		Lifecycle: &fakeLCM{store: st},
		LookPath:  func(string) (string, error) { return "/bin/true", nil },
	})
	return m, st, rt, msg
}

// An omitted display name is the signal that asks the daemon to compute one. It
// must never come back empty, and it must never be the prompt: a prompt-derived
// name is indistinguishable from an operator's and would win the override branch
// forever, which is exactly how the prior implementation ended up with workers
// literally named `/address-issue 148`.
func TestSpawnComputesTheDisplayNameWhenOmitted(t *testing.T) {
	m, _, _, _ := newNamingManager(renameOnlyAgent{})

	rec, _, _, err := m.Spawn(ctx, ports.SpawnConfig{
		ProjectID:  "mer",
		Kind:       domain.KindWorker,
		IssueID:    "150",
		IssueTitle: "Unified session naming",
		Prompt:     "/address-issue 150",
	})
	if err != nil {
		t.Fatal(err)
	}
	if rec.DisplayName != "ao #150 Unified" {
		t.Fatalf("display name = %q, want %q", rec.DisplayName, "ao #150 Unified")
	}
	if strings.Contains(rec.DisplayName, "/address-issue") {
		t.Fatalf("display name %q contains the prompt", rec.DisplayName)
	}
}

func TestSpawnHonorsAnExplicitDisplayName(t *testing.T) {
	m, _, _, _ := newNamingManager(renameOnlyAgent{})

	rec, _, _, err := m.Spawn(ctx, ports.SpawnConfig{
		ProjectID:   "mer",
		Kind:        domain.KindWorker,
		IssueID:     "150",
		IssueTitle:  "Unified session naming",
		DisplayName: "operator pick",
	})
	if err != nil {
		t.Fatal(err)
	}
	if rec.DisplayName != "operator pick" {
		t.Fatalf("display name = %q, want the explicit override", rec.DisplayName)
	}
}

func TestSpawnNamesAnOrchestratorFromTheProjectPrefix(t *testing.T) {
	m, _, _, _ := newNamingManager(renameOnlyAgent{})

	rec, _, _, err := m.Spawn(ctx, ports.SpawnConfig{ProjectID: "mer", Kind: domain.KindOrchestrator})
	if err != nil {
		t.Fatal(err)
	}
	if rec.DisplayName != "ao Orc" {
		t.Fatalf("display name = %q, want %q", rec.DisplayName, "ao Orc")
	}
}

// A tracker outage costs a title, never a spawn.
func TestSpawnDegradesTheNameWhenTheTitleIsUnavailable(t *testing.T) {
	m, _, _, _ := newNamingManager(renameOnlyAgent{})

	rec, _, _, err := m.Spawn(ctx, ports.SpawnConfig{ProjectID: "mer", Kind: domain.KindWorker, IssueID: "150"})
	if err != nil {
		t.Fatalf("spawn failed on an unresolvable title: %v", err)
	}
	if rec.DisplayName != "ao #150" {
		t.Fatalf("display name = %q, want the head-only name %q", rec.DisplayName, "ao #150")
	}
}

// A harness with a launch-time naming flag is named atomically with process
// start, so the spawn must not follow up with a post-start write it does not
// need — that write is the pane-readiness race this avoids entirely.
func TestSpawnPrefersTheLaunchArgumentAndSkipsThePostStartWrite(t *testing.T) {
	var launched string
	m, _, _, msg := newNamingManager(launchNamedAgent{lastLaunchName: &launched})

	rec, _, _, err := m.Spawn(ctx, ports.SpawnConfig{ProjectID: "mer", Kind: domain.KindWorker, IssueID: "150", IssueTitle: "Unified session naming"})
	if err != nil {
		t.Fatal(err)
	}
	if launched != rec.DisplayName {
		t.Fatalf("launch config display name = %q, want the persisted name %q", launched, rec.DisplayName)
	}
	for _, sent := range msg.msgs {
		if strings.HasPrefix(sent, "/rename") {
			t.Fatalf("post-start writes = %v, want no rename when argv carried the name", msg.msgs)
		}
	}
}

// A harness with only the in-harness form is named after start, and the string
// written into the pane is byte-identical to the persisted display name.
func TestSpawnDeliversTheNameToAHarnessWithoutALaunchArgument(t *testing.T) {
	m, _, _, msg := newNamingManager(renameOnlyAgent{})

	rec, _, _, err := m.Spawn(ctx, ports.SpawnConfig{ProjectID: "mer", Kind: domain.KindWorker, IssueID: "150", IssueTitle: "Unified session naming"})
	if err != nil {
		t.Fatal(err)
	}
	want := "/rename " + rec.DisplayName
	if len(msg.msgs) == 0 || msg.msgs[0] != want {
		t.Fatalf("post-start writes = %v, want first write %q", msg.msgs, want)
	}
}

// A harness that declares no naming capability keeps AO's own name and is never
// written to blindly.
func TestSpawnLeavesANamelessHarnessAlone(t *testing.T) {
	m, _, _, msg := newNamingManager(fakeAgent{})

	rec, _, _, err := m.Spawn(ctx, ports.SpawnConfig{ProjectID: "mer", Kind: domain.KindWorker, IssueID: "150", IssueTitle: "Unified session naming"})
	if err != nil {
		t.Fatal(err)
	}
	if rec.DisplayName != "ao #150 Unified" {
		t.Fatalf("display name = %q, want AO's own computed name", rec.DisplayName)
	}
	if len(msg.msgs) != 0 {
		t.Fatalf("writes to a harness with no naming capability = %v, want none", msg.msgs)
	}
}

// A name is cosmetic relative to the session's task, so a failed delivery
// against a live runtime keeps the session.
func TestSpawnKeepsTheSessionWhenNamingFailsAgainstALiveRuntime(t *testing.T) {
	m, _, rt, msg := newNamingManager(renameOnlyAgent{})
	msg.err = errors.New("pane write failed")
	rt.aliveByHandle["h1"] = true

	rec, _, _, err := m.Spawn(ctx, ports.SpawnConfig{ProjectID: "mer", Kind: domain.KindWorker, IssueID: "150", IssueTitle: "Unified session naming"})
	if err != nil {
		t.Fatalf("spawn failed on a cosmetic naming error against a live runtime: %v", err)
	}
	if rec.DisplayName != "ao #150 Unified" {
		t.Fatalf("display name = %q, want AO's computed name retained", rec.DisplayName)
	}
	if rt.destroyed != 0 {
		t.Fatalf("runtime destroyed %d time(s), want the live session kept", rt.destroyed)
	}
}

// Once the prompt rides argv, the name write is the only thing that touches the
// pane during a claude-code spawn. Forgiving its failure unconditionally would
// report a harness that died before doing any work as a live, idle session.
func TestSpawnFailsWhenNamingFailsAndLivenessCannotBeConfirmed(t *testing.T) {
	m, _, rt, msg := newNamingManager(renameOnlyAgent{})
	msg.err = errors.New("pane write failed")
	rt.aliveByHandle["h1"] = false

	if _, _, _, err := m.Spawn(ctx, ports.SpawnConfig{ProjectID: "mer", Kind: domain.KindWorker, IssueID: "150", IssueTitle: "Unified session naming"}); err == nil {
		t.Fatal("spawn succeeded with a failed name write against an unconfirmable runtime, want failure")
	}
	if rt.destroyed == 0 {
		t.Fatal("runtime was not destroyed, want the spawn rolled back")
	}
}

// The rename path reaches the running harness, not just the database row.
func TestDeliverNameWritesThePersistedNameToALiveSession(t *testing.T) {
	m, st, _, msg := newNamingManager(renameOnlyAgent{})
	st.sessions["mer-1"] = domain.SessionRecord{
		ID:          "mer-1",
		ProjectID:   "mer",
		Harness:     domain.HarnessClaudeCode,
		DisplayName: "ao #7 renamed",
		Metadata:    domain.SessionMetadata{RuntimeHandleID: "h1"},
		Activity:    domain.Activity{State: domain.ActivityIdle},
	}

	if err := m.DeliverName(ctx, "mer-1"); err != nil {
		t.Fatal(err)
	}
	if len(msg.msgs) != 1 || msg.msgs[0] != "/rename ao #7 renamed" {
		t.Fatalf("writes = %v, want the persisted name delivered verbatim", msg.msgs)
	}
}

func TestDeliverNameSkipsSessionsWithNoLiveRuntime(t *testing.T) {
	for _, tc := range []struct {
		name string
		rec  domain.SessionRecord
	}{
		{"terminated", domain.SessionRecord{ID: "mer-1", ProjectID: "mer", DisplayName: "ao #7", IsTerminated: true, Metadata: domain.SessionMetadata{RuntimeHandleID: "h1"}}},
		{"no runtime handle", domain.SessionRecord{ID: "mer-1", ProjectID: "mer", DisplayName: "ao #7"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m, st, _, msg := newNamingManager(renameOnlyAgent{})
			st.sessions["mer-1"] = tc.rec

			if err := m.DeliverName(ctx, "mer-1"); err != nil {
				t.Fatal(err)
			}
			if len(msg.msgs) != 0 {
				t.Fatalf("writes = %v, want none", msg.msgs)
			}
		})
	}
}
