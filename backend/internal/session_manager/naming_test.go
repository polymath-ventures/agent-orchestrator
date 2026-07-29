package sessionmanager

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

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
	st.sessions["mer-1"] = liveNamedSession("ao #7 renamed", domain.ActivityIdle)

	if err := m.DeliverName(ctx, "mer-1"); err != nil {
		t.Fatal(err)
	}
	if len(msg.msgs) != 1 || msg.msgs[0] != "/rename ao #7 renamed" {
		t.Fatalf("writes = %v, want the persisted name delivered verbatim", msg.msgs)
	}
}

func liveNamedSession(name string, state domain.ActivityState) domain.SessionRecord {
	return domain.SessionRecord{
		ID:          "mer-1",
		ProjectID:   "mer",
		Harness:     domain.HarnessClaudeCode,
		DisplayName: name,
		Metadata:    domain.SessionMetadata{RuntimeHandleID: "h1"},
		Activity:    domain.Activity{State: state},
	}
}

// A rename is unsolicited relative to whatever the agent is doing. A harness
// that takes mid-turn input steers on it; one that does not queues it as the
// next prompt. Either way the rename would reach the model as text, so a
// cosmetic write never interrupts a turn — or answers a pending dialog.
func TestDeliverNameSkipsASessionThatIsNotIdle(t *testing.T) {
	for _, state := range []domain.ActivityState{domain.ActivityActive, domain.ActivityBlocked, domain.ActivityWaitingInput} {
		t.Run(string(state), func(t *testing.T) {
			m, st, _, msg := newNamingManager(renameOnlyAgent{})
			st.sessions["mer-1"] = liveNamedSession("ao #7 renamed", state)

			if err := m.DeliverName(ctx, "mer-1"); err != nil {
				t.Fatal(err)
			}
			if len(msg.msgs) != 0 {
				t.Fatalf("writes = %v, want none while the session is %s", msg.msgs, state)
			}
		})
	}
}

// The agent process is wrapped by AO's supervisor, so when it exits its runtime
// session goes with it. A name typed into a dead session would not be TUI text,
// so delivery requires positive proof the runtime is still alive.
func TestDeliverNameRefusesWhenThePaneIsNoLongerRunningTheAgent(t *testing.T) {
	m, st, rt, msg := newNamingManager(renameOnlyAgent{})
	st.sessions["mer-1"] = liveNamedSession("ao #7 renamed", domain.ActivityIdle)
	rt.workloadAliveByHandle = map[string]bool{"h1": false}

	if err := m.DeliverName(ctx, "mer-1"); err != nil {
		t.Fatal(err)
	}
	if len(msg.msgs) != 0 {
		t.Fatalf("writes = %v, want none once the runtime is gone", msg.msgs)
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

// readyGatedAgent names after start and gates that write on a terminal marker,
// the way codex does.
type readyGatedAgent struct {
	renameOnlyAgent
	hints ports.PromptReadinessHints
}

func (a readyGatedAgent) PromptReadinessHints(context.Context, ports.LaunchConfig) (ports.PromptReadinessHints, error) {
	return a.hints, nil
}

// Runtime creation returns as soon as the pane exists, which is before the
// harness has drawn an input box. Keystrokes sent into that gap are not queued
// by a box that does not exist yet.
func TestSpawnWaitsForHarnessReadinessBeforeTheNameWrite(t *testing.T) {
	m, _, rt, msg := newNamingManager(readyGatedAgent{hints: ports.PromptReadinessHints{
		Patterns:     []string{"READY"},
		PollInterval: time.Millisecond,
		Timeout:      5 * time.Second,
		Lines:        80,
	}})
	rt.outputs = []string{"", "", "READY"}

	if _, _, _, err := m.Spawn(ctx, ports.SpawnConfig{ProjectID: "mer", Kind: domain.KindWorker, IssueID: "150", IssueTitle: "Unified session naming"}); err != nil {
		t.Fatal(err)
	}
	if rt.outputCalls < 3 {
		t.Fatalf("pane reads before the name write = %d, want the write to wait for the readiness marker", rt.outputCalls)
	}
	if len(msg.msgs) != 1 || !strings.HasPrefix(msg.msgs[0], "/rename ") {
		t.Fatalf("writes = %v, want the rename delivered after readiness", msg.msgs)
	}
}

// A harness that prints nothing recognizable must never hold a spawn open. The
// write goes out anyway and the degraded path is observable.
func TestSpawnProceedsWhenAHarnessNeverReportsReadiness(t *testing.T) {
	m, _, rt, msg := newNamingManager(readyGatedAgent{hints: ports.PromptReadinessHints{
		Patterns:     []string{"NEVER-PRINTED"},
		PollInterval: time.Millisecond,
		Timeout:      50 * time.Millisecond,
		Lines:        80,
	}})
	rt.outputs = []string{"still starting"}

	if _, _, _, err := m.Spawn(ctx, ports.SpawnConfig{ProjectID: "mer", Kind: domain.KindWorker, IssueID: "150", IssueTitle: "Unified session naming"}); err != nil {
		t.Fatalf("spawn failed waiting on a silent harness: %v", err)
	}
	if len(msg.msgs) != 1 || !strings.HasPrefix(msg.msgs[0], "/rename ") {
		t.Fatalf("writes = %v, want the rename issued anyway after the deadline", msg.msgs)
	}
}

// The readiness deadline runs on a real timer, so a test clock frozen elsewhere
// cannot spin the wait until the context dies.
func TestReadinessWaitIsBoundedByWallClock(t *testing.T) {
	m, _, rt, _ := newNamingManager(readyGatedAgent{hints: ports.PromptReadinessHints{
		Patterns:     []string{"NEVER-PRINTED"},
		PollInterval: time.Millisecond,
		Timeout:      50 * time.Millisecond,
		Lines:        80,
	}})
	// A clock stopped at a fixed instant: if the wait consulted it, the deadline
	// would never arrive.
	frozen := time.Now()
	m.clock = func() time.Time { return frozen }
	rt.outputs = []string{"still starting"}

	done := make(chan error, 1)
	go func() {
		_, _, _, err := m.Spawn(ctx, ports.SpawnConfig{ProjectID: "mer", Kind: domain.KindWorker, IssueID: "150"})
		done <- err
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("spawn did not return; the readiness wait is not bounded by wall-clock time")
	}
}

// Naming must never displace, delay, or concatenate with the initial prompt: a
// named session with a prompt still runs its task rather than coming up idle.
func TestSpawnDeliversBothTheNameAndTheAfterStartPrompt(t *testing.T) {
	m, _, _, msg := newNamingManager(afterStartPromptAgent{})

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
	if len(msg.msgs) != 2 {
		t.Fatalf("writes = %v, want the rename then the prompt", msg.msgs)
	}
	if msg.msgs[0] != "/rename "+rec.DisplayName {
		t.Fatalf("first write = %q, want the rename", msg.msgs[0])
	}
	if msg.msgs[1] != "/address-issue 150" {
		t.Fatalf("second write = %q, want the initial prompt intact", msg.msgs[1])
	}
}

// afterStartPromptAgent takes its prompt through the pane rather than argv, so
// both a name and a prompt have to be written after start.
type afterStartPromptAgent struct{ renameOnlyAgent }

func (afterStartPromptAgent) GetPromptDeliveryStrategy(context.Context, ports.LaunchConfig) (ports.PromptDeliveryStrategy, error) {
	return ports.PromptDeliveryAfterStart, nil
}

// goesActiveWhileWaitingAgent models a worker whose prompt rides argv: by the
// time the harness reports ready, it is already working on its task.
type goesActiveWhileWaitingAgent struct {
	renameOnlyAgent
	store *fakeStore
	id    domain.SessionID
}

func (a goesActiveWhileWaitingAgent) PromptReadinessHints(context.Context, ports.LaunchConfig) (ports.PromptReadinessHints, error) {
	a.store.mu.Lock()
	rec := a.store.sessions[a.id]
	rec.Activity = domain.Activity{State: domain.ActivityActive}
	a.store.sessions[a.id] = rec
	a.store.mu.Unlock()
	return ports.PromptReadinessHints{}, nil
}

// The spawn write is solicited — it is part of creating the session, into a TUI
// drawn moments earlier — so it must NOT inherit the rename path's idle-only
// policy. A codex worker takes its prompt in argv and is routinely mid-turn by
// the time the harness reports ready; refusing there would leave exactly the
// sessions this change exists for permanently unnamed.
func TestSpawnDeliversTheNameEvenWhenTheAgentIsAlreadyWorking(t *testing.T) {
	st := newFakeStore()
	st.projects["mer"] = domain.ProjectRecord{
		ID:     "mer",
		Config: domain.ProjectConfig{SessionPrefix: "ao", Worker: domain.RoleOverride{Harness: domain.HarnessClaudeCode}},
	}
	rt := &fakeRuntime{aliveByHandle: map[string]bool{"h1": true}}
	msg := &fakeMessenger{}
	m := New(Deps{
		Runtime:   rt,
		Agents:    agentsFor{agent: goesActiveWhileWaitingAgent{store: st, id: "mer-1"}},
		Workspace: &fakeWorkspace{},
		Store:     st,
		Messenger: msg,
		Lifecycle: &fakeLCM{store: st},
		LookPath:  func(string) (string, error) { return "/bin/true", nil },
	})

	rec, _, _, err := m.Spawn(ctx, ports.SpawnConfig{ProjectID: "mer", Kind: domain.KindWorker, IssueID: "150", IssueTitle: "Unified session naming"})
	if err != nil {
		t.Fatal(err)
	}
	want := "/rename " + rec.DisplayName
	if len(msg.msgs) != 1 || msg.msgs[0] != want {
		t.Fatalf("writes = %v, want the spawn-time name %q delivered despite the active turn", msg.msgs, want)
	}
}

// The remaining fail-closed branches: a runtime that cannot answer, and a probe
// that errors. Without proof the agent is there, nothing is typed.
func TestDeliverNameRefusesWhenTheProbeErrors(t *testing.T) {
	m, st, rt, msg := newNamingManager(renameOnlyAgent{})
	st.sessions["mer-1"] = liveNamedSession("ao #7 renamed", domain.ActivityIdle)
	rt.workloadAliveErr = errors.New("tmux unreachable")

	if err := m.DeliverName(ctx, "mer-1"); err != nil {
		t.Fatal(err)
	}
	if len(msg.msgs) != 0 {
		t.Fatalf("writes = %v, want none when the probe cannot answer", msg.msgs)
	}
}

// awaitsUserWhileWaitingAgent models a worker that hits a permission dialog
// while AO is still waiting for its harness to report ready.
type awaitsUserWhileWaitingAgent struct {
	renameOnlyAgent
	store *fakeStore
	id    domain.SessionID
	state domain.ActivityState
}

func (a awaitsUserWhileWaitingAgent) PromptReadinessHints(context.Context, ports.LaunchConfig) (ports.PromptReadinessHints, error) {
	a.store.mu.Lock()
	rec := a.store.sessions[a.id]
	rec.Activity = domain.Activity{State: a.state}
	a.store.sessions[a.id] = rec
	a.store.mu.Unlock()
	return ports.PromptReadinessHints{}, nil
}

// A spawn tolerates an active session but must still refuse one awaiting the
// human: the paste plus Enter that delivers a name would answer the dialog.
func TestSpawnDoesNotWriteANameIntoAPendingDecision(t *testing.T) {
	for _, state := range []domain.ActivityState{domain.ActivityBlocked, domain.ActivityWaitingInput} {
		t.Run(string(state), func(t *testing.T) {
			st := newFakeStore()
			st.projects["mer"] = domain.ProjectRecord{
				ID:     "mer",
				Config: domain.ProjectConfig{SessionPrefix: "ao", Worker: domain.RoleOverride{Harness: domain.HarnessClaudeCode}},
			}
			rt := &fakeRuntime{aliveByHandle: map[string]bool{"h1": true}}
			msg := &fakeMessenger{}
			m := New(Deps{
				Runtime:   rt,
				Agents:    agentsFor{agent: awaitsUserWhileWaitingAgent{store: st, id: "mer-1", state: state}},
				Workspace: &fakeWorkspace{},
				Store:     st,
				Messenger: msg,
				Lifecycle: &fakeLCM{store: st},
				LookPath:  func(string) (string, error) { return "/bin/true", nil },
			})

			if _, _, _, err := m.Spawn(ctx, ports.SpawnConfig{ProjectID: "mer", Kind: domain.KindWorker, IssueID: "150"}); err != nil {
				t.Fatal(err)
			}
			if len(msg.msgs) != 0 {
				t.Fatalf("writes = %v, want none while the session is %s", msg.msgs, state)
			}
		})
	}
}

// A restore relaunches the harness, so its name resets to whatever it derives
// for itself. Without re-delivery, a session renamed before teardown comes back
// with the old harness name — a divergence reintroduced by the lifecycle event
// meant to preserve the session.
// Deliberately a launch-named adapter: a resume command carries no launch-time
// name flag, so the harness that CAN be named in argv is exactly the one whose
// restore would be skipped by an adapter-capability check.
func TestRestoreRedeliversThePersistedName(t *testing.T) {
	m, st, _, msg := newNamingManager(launchNamedAgent{})
	rec := liveNamedSession("ao #7 renamed", domain.ActivityIdle)
	rec.IsTerminated = true
	rec.Activity = domain.Activity{State: domain.ActivityExited}
	rec.Metadata.WorkspacePath = "/ws/mer-1"
	rec.Metadata.Branch = "ao/mer-1/root"
	rec.Metadata.AgentSessionID = "native-1"
	st.sessions["mer-1"] = rec

	if _, err := m.RestoreWithMode(ctx, "mer-1"); err != nil {
		t.Fatal(err)
	}
	want := "/rename ao #7 renamed"
	found := false
	for _, sent := range msg.msgs {
		if sent == want {
			found = true
		}
	}
	if !found {
		t.Fatalf("writes = %v, want the persisted name %q re-delivered on restore", msg.msgs, want)
	}
}
