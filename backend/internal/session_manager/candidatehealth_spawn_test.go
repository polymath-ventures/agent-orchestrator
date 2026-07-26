package sessionmanager

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"sync"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/candidatehealth"
	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

// launchCmdAgent is a fake whose GetLaunchCommand fails with a caller-supplied
// error, exercising the pre-validateAgentBinary failure path where an adapter
// (e.g. copilot) reports ErrAgentBinaryNotFound from GetLaunchCommand itself.
type launchCmdAgent struct {
	fakeAgent
	err error
}

func (a launchCmdAgent) GetLaunchCommand(context.Context, ports.LaunchConfig) ([]string, error) {
	return nil, a.err
}

type launchCmdAgents struct{ err error }

func (a launchCmdAgents) Agent(domain.AgentHarness) (ports.Agent, bool) {
	return launchCmdAgent{err: a.err}, true
}

func launchCmdFailManager(t *testing.T, tr *candidatehealth.Tracker, launchErr error) *Manager {
	t.Helper()
	st := newFakeStore()
	st.projects["mer"] = domain.ProjectRecord{ID: "mer", Config: domain.ProjectConfig{WorkerMix: singleBucketMix()}}
	return New(Deps{
		Runtime: &fakeRuntime{}, Agents: launchCmdAgents{err: launchErr}, Workspace: &fakeWorkspace{}, Store: st,
		Messenger: &fakeMessenger{}, Lifecycle: &fakeLCM{store: st},
		LookPath: func(string) (string, error) { return "/bin/true", nil }, Health: tr,
	})
}

// healthRecordingSink captures candidate-health telemetry so the wiring tests
// can assert on the events the Tracker emits through the manager.
type healthRecordingSink struct {
	mu     sync.Mutex
	events []ports.TelemetryEvent
}

func (s *healthRecordingSink) Emit(_ context.Context, ev ports.TelemetryEvent) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events = append(s.events, ev)
}

func (s *healthRecordingSink) Close(context.Context) error { return nil }

func (s *healthRecordingSink) count(name string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for _, ev := range s.events {
		if ev.Name == name {
			n++
		}
	}
	return n
}

func singleBucketMix() domain.WorkerMix {
	return domain.WorkerMix{{Harness: domain.HarnessClaudeCode, Weight: 100}}
}

// twoBucketMix is a 60/40 claude-code/codex mix: enough buckets to show a down
// bucket's share redistributing onto the survivor.
func twoBucketMix() domain.WorkerMix {
	return domain.WorkerMix{
		{Harness: domain.HarnessClaudeCode, Weight: 60},
		{Harness: domain.HarnessCodex, Weight: 40},
	}
}

// binaryMissingLookPath resolves tmux (so the runtime-prerequisite check passes)
// but fails every other binary, forcing the launch-attributable
// agent-binary-missing failure path.
func binaryMissingLookPath() func(string) (string, error) {
	return func(bin string) (string, error) {
		if bin == "tmux" {
			return "/usr/bin/tmux", nil
		}
		return "", errors.New("executable not found in PATH")
	}
}

// healthMixManager builds a manager over a mix-configured project with a
// caller-supplied candidate-health tracker, so a test can inspect down state and
// emitted events. lookPath defaults to always-resolves when nil.
func healthMixManager(t *testing.T, cfg domain.ProjectConfig, tr *candidatehealth.Tracker, lookPath func(string) (string, error)) (*Manager, *fakeRuntime, *fakeWorkspace) {
	t.Helper()
	st := newFakeStore()
	st.projects["mer"] = domain.ProjectRecord{ID: "mer", Config: cfg}
	rt := &fakeRuntime{}
	ws := &fakeWorkspace{}
	if lookPath == nil {
		lookPath = func(string) (string, error) { return "/bin/true", nil }
	}
	m := New(Deps{
		Runtime: rt, Agents: fakeAgents{}, Workspace: ws, Store: st,
		Messenger: &fakeMessenger{}, Lifecycle: &fakeLCM{store: st},
		LookPath: lookPath, Health: tr,
	})
	return m, rt, ws
}

// A mix-selected spawn that fails because the agent binary is not on PATH marks
// the selected bucket down: the binary being missing is direct evidence the
// candidate cannot launch.
func TestSpawn_MixSelectedBinaryMissingMarksDown(t *testing.T) {
	tr := candidatehealth.New(candidatehealth.Config{Source: "session_manager"})
	m, _, _ := healthMixManager(t, domain.ProjectConfig{WorkerMix: singleBucketMix()}, tr, binaryMissingLookPath())

	_, _, _, err := m.Spawn(ctx, ports.SpawnConfig{ProjectID: "mer", Kind: domain.KindWorker})
	if !errors.Is(err, ports.ErrAgentBinaryNotFound) {
		t.Fatalf("spawn err = %v, want ErrAgentBinaryNotFound", err)
	}
	if !tr.IsDown(workerMixCandidate(domain.HarnessClaudeCode, "", "")) {
		t.Fatal("a mix-selected binary-missing spawn must mark the bucket down")
	}
}

// GetLaunchCommand itself can report ErrAgentBinaryNotFound (e.g. copilot
// resolves its binary there, not just via PATH). That path must mark the bucket
// down too, or a broken bucket stays healthy and gets reselected forever.
func TestSpawn_MixSelectedLaunchCommandBinaryMissingMarksDown(t *testing.T) {
	tr := candidatehealth.New(candidatehealth.Config{Source: "session_manager"})
	m := launchCmdFailManager(t, tr, fmt.Errorf("copilot: %w", ports.ErrAgentBinaryNotFound))

	_, _, _, err := m.Spawn(ctx, ports.SpawnConfig{ProjectID: "mer", Kind: domain.KindWorker})
	if !errors.Is(err, ports.ErrAgentBinaryNotFound) {
		t.Fatalf("spawn err = %v, want ErrAgentBinaryNotFound", err)
	}
	if !tr.IsDown(workerMixCandidate(domain.HarnessClaudeCode, "", "")) {
		t.Fatal("GetLaunchCommand returning ErrAgentBinaryNotFound must mark the bucket down")
	}
}

// A non-sentinel GetLaunchCommand failure (a prompt/config error, not the
// candidate's binary) is NOT a candidate fault and must not mark the bucket down.
func TestSpawn_MixSelectedLaunchCommandGenericErrorDoesNotMarkDown(t *testing.T) {
	tr := candidatehealth.New(candidatehealth.Config{Source: "session_manager"})
	m := launchCmdFailManager(t, tr, errors.New("prompt template build failed"))

	_, _, _, err := m.Spawn(ctx, ports.SpawnConfig{ProjectID: "mer", Kind: domain.KindWorker})
	if err == nil {
		t.Fatal("expected spawn to fail")
	}
	if tr.IsDown(workerMixCandidate(domain.HarnessClaudeCode, "", "")) {
		t.Fatal("a non-binary launch-command error must not mark the bucket down")
	}
}

// A mix-selected spawn that fails because the runtime refuses to create marks the
// selected bucket down: the runtime rejecting the launch is attributable to the
// candidate.
func TestSpawn_MixSelectedRuntimeRefusedMarksDown(t *testing.T) {
	tr := candidatehealth.New(candidatehealth.Config{Source: "session_manager"})
	m, rt, _ := healthMixManager(t, domain.ProjectConfig{WorkerMix: singleBucketMix()}, tr, nil)
	rt.createErr = errors.New("runtime refused to create")

	_, _, _, err := m.Spawn(ctx, ports.SpawnConfig{ProjectID: "mer", Kind: domain.KindWorker})
	if err == nil {
		t.Fatal("expected runtime-refused spawn to fail")
	}
	if !tr.IsDown(workerMixCandidate(domain.HarnessClaudeCode, "", "")) {
		t.Fatal("a mix-selected runtime-refused spawn must mark the bucket down")
	}
}

// Workspace preparation runs the selected agent's launch-specific setup after a
// bucket was chosen. A failure here is attributable to the candidate and should
// mark it down before rollback.
func TestSpawn_MixSelectedWorkspacePreparationFailureMarksDown(t *testing.T) {
	tr := candidatehealth.New(candidatehealth.Config{Source: "session_manager"})
	st := newFakeStore()
	st.projects["mer"] = domain.ProjectRecord{ID: "mer", Config: domain.ProjectConfig{WorkerMix: singleBucketMix()}}
	agent := &hookErrorCleaningAgent{hookErr: errors.New("hooks install failed")}
	m := New(Deps{
		Runtime: &fakeRuntime{}, Agents: singleAgent{agent: agent}, Workspace: &fakeWorkspace{}, Store: st,
		Messenger: &fakeMessenger{}, Lifecycle: &fakeLCM{store: st},
		LookPath: func(string) (string, error) { return "/bin/true", nil }, Health: tr,
	})

	_, _, _, err := m.Spawn(ctx, ports.SpawnConfig{ProjectID: "mer", Kind: domain.KindWorker})
	if err == nil {
		t.Fatal("expected workspace preparation failure")
	}
	if !tr.IsDown(workerMixCandidate(domain.HarnessClaudeCode, "", "")) {
		t.Fatal("a mix-selected workspace-preparation failure must mark the bucket down")
	}
}

// After-start prompt delivery is part of launching the selected bucket. If the
// pane write fails after runtime start, the candidate should be marked down
// before cleanup/termination obscures the launch attempt.
func TestSpawn_MixSelectedAfterStartPromptFailureMarksDown(t *testing.T) {
	tr := candidatehealth.New(candidatehealth.Config{Source: "session_manager"})
	st := newFakeStore()
	st.projects["mer"] = domain.ProjectRecord{ID: "mer", Config: domain.ProjectConfig{WorkerMix: singleBucketMix()}}
	agent := &recordingAgent{}
	m := New(Deps{
		Runtime: &fakeRuntime{}, Agents: singleAgent{agent: afterStartAgent{recordingAgent: agent}}, Workspace: &fakeWorkspace{}, Store: st,
		Messenger: &fakeMessenger{err: errors.New("pane unavailable")}, Lifecycle: &fakeLCM{store: st},
		LookPath: func(string) (string, error) { return "/bin/true", nil }, Health: tr,
	})

	_, _, _, err := m.Spawn(ctx, ports.SpawnConfig{ProjectID: "mer", Kind: domain.KindWorker, Prompt: "fix the button"})
	if err == nil {
		t.Fatal("expected after-start prompt delivery failure")
	}
	if !tr.IsDown(workerMixCandidate(domain.HarnessClaudeCode, "", "")) {
		t.Fatal("a mix-selected after-start prompt failure must mark the bucket down")
	}
}

// A configuration error (unknown harness) is not attributable to the candidate:
// the harness or model is misconfigured, not broken, so the bucket stays healthy.
func TestSpawn_MixSelectedUnknownHarnessDoesNotMarkDown(t *testing.T) {
	st := newFakeStore()
	st.projects["mer"] = domain.ProjectRecord{ID: "mer", Config: domain.ProjectConfig{WorkerMix: singleBucketMix()}}
	tr := candidatehealth.New(candidatehealth.Config{Source: "session_manager"})
	m := New(Deps{
		Runtime: &fakeRuntime{}, Agents: missingAgents{}, Workspace: &fakeWorkspace{}, Store: st,
		Messenger: &fakeMessenger{}, Lifecycle: &fakeLCM{store: st},
		LookPath: func(string) (string, error) { return "/bin/true", nil }, Health: tr,
	})

	_, _, _, err := m.Spawn(ctx, ports.SpawnConfig{ProjectID: "mer", Kind: domain.KindWorker})
	if !errors.Is(err, ErrUnknownHarness) {
		t.Fatalf("spawn err = %v, want ErrUnknownHarness", err)
	}
	if tr.IsDown(workerMixCandidate(domain.HarnessClaudeCode, "", "")) {
		t.Fatal("a configuration error must not mark the bucket down")
	}
}

// An environmental error (here the workspace failing to materialize) is not
// attributable to the candidate: the harness never got a chance to launch, so
// the bucket stays healthy. Marking down here would take a bucket out of
// rotation because the disk was full.
func TestSpawn_MixSelectedEnvironmentalErrorDoesNotMarkDown(t *testing.T) {
	tr := candidatehealth.New(candidatehealth.Config{Source: "session_manager"})
	m, _, ws := healthMixManager(t, domain.ProjectConfig{WorkerMix: singleBucketMix()}, tr, nil)
	ws.createErr = errors.New("no space left on device")

	_, _, _, err := m.Spawn(ctx, ports.SpawnConfig{ProjectID: "mer", Kind: domain.KindWorker})
	if err == nil {
		t.Fatal("expected workspace failure to fail the spawn")
	}
	if tr.IsDown(workerMixCandidate(domain.HarnessClaudeCode, "", "")) {
		t.Fatal("an environmental error must not mark the bucket down")
	}
}

// When the caller's own context is already cancelled, the attempt is abandoned,
// not the candidate's fault — even though it fails on an otherwise-attributable
// path. Attribution keys on caller-context state, not on the error identity.
func TestSpawn_MixSelectedCallerCanceledDoesNotMarkDown(t *testing.T) {
	tr := candidatehealth.New(candidatehealth.Config{Source: "session_manager"})
	m, _, _ := healthMixManager(t, domain.ProjectConfig{WorkerMix: singleBucketMix()}, tr, binaryMissingLookPath())

	cctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, _, _, err := m.Spawn(cctx, ports.SpawnConfig{ProjectID: "mer", Kind: domain.KindWorker})
	if !errors.Is(err, ports.ErrAgentBinaryNotFound) {
		t.Fatalf("spawn err = %v, want it to reach the agent-binary check", err)
	}
	if tr.IsDown(workerMixCandidate(domain.HarnessClaudeCode, "", "")) {
		t.Fatal("a caller-cancelled attempt must not mark the bucket down")
	}
}

// A down bucket's share is debit-preserved, not removed from the mix. The first
// spawn can go to the healthy survivor while the down bucket's debit is lower,
// but once D'Hondt selects the down bucket's slot the spawn fails loudly instead
// of silently redistributing that share forever.
func TestSpawn_DownBucketDebitPreservedAndFailsWhenSelected(t *testing.T) {
	tr := candidatehealth.New(candidatehealth.Config{Source: "session_manager"})
	m, _, _ := healthMixManager(t, domain.ProjectConfig{WorkerMix: twoBucketMix()}, tr, nil)
	tr.MarkDown(workerMixCandidate(domain.HarnessClaudeCode, "", ""), errors.New("binary gone"))

	first := spawnUnpinnedWorker(t, m)
	if first.Harness != domain.HarnessCodex {
		t.Fatalf("first selection = %q, want codex while down bucket carries one skip debit", first.Harness)
	}

	_, _, _, err := m.Spawn(ctx, ports.SpawnConfig{ProjectID: "mer", Kind: domain.KindWorker})
	if !errors.Is(err, ErrWorkerMixBucketDown) {
		t.Fatalf("second spawn err = %v, want ErrWorkerMixBucketDown when down bucket's slot is selected", err)
	}
}

// When every bucket in the mix is down, selection fails with an exhausted-mix
// error rather than falling back to a harness outside the mix.
func TestSpawn_AllBucketsDownFailsLoudly(t *testing.T) {
	tr := candidatehealth.New(candidatehealth.Config{Source: "session_manager"})
	m, _, _ := healthMixManager(t, domain.ProjectConfig{WorkerMix: twoBucketMix()}, tr, nil)
	tr.MarkDown(workerMixCandidate(domain.HarnessClaudeCode, "", ""), errors.New("binary gone"))
	tr.MarkDown(workerMixCandidate(domain.HarnessCodex, "", ""), errors.New("runtime refused"))

	_, _, _, err := m.Spawn(ctx, ports.SpawnConfig{ProjectID: "mer", Kind: domain.KindWorker})
	if !errors.Is(err, ErrWorkerMixExhausted) {
		t.Fatalf("spawn err = %v, want ErrWorkerMixExhausted", err)
	}
}

func TestSpawn_ModelOnlyFailureMarksExplicitModelCandidateDown(t *testing.T) {
	tr := candidatehealth.New(candidatehealth.Config{Source: "session_manager"})
	m, rt, _ := healthMixManager(t, domain.ProjectConfig{
		WorkerMix: domain.WorkerMix{{
			Harness: domain.HarnessCodex, Model: "gpt-5.4-codex", Weight: 100,
		}},
	}, tr, nil)
	rt.createErr = errors.New("runtime refused explicit model")

	_, _, _, err := m.Spawn(ctx, ports.SpawnConfig{ProjectID: "mer", Kind: domain.KindWorker, Model: "gpt-5.5-codex"})
	if err == nil {
		t.Fatal("expected explicit-model launch failure")
	}
	actual := workerMixCandidate(domain.HarnessCodex, "gpt-5.5-codex", "")
	configured := workerMixCandidate(domain.HarnessCodex, "gpt-5.4-codex", "")
	if !tr.IsDown(actual) {
		t.Fatal("explicit model candidate was not marked down")
	}
	if tr.IsDown(configured) {
		t.Fatal("configured bucket model was marked down instead of the explicit model candidate")
	}

	rt.createErr = nil
	_, _, _, err = m.Spawn(ctx, ports.SpawnConfig{ProjectID: "mer", Kind: domain.KindWorker, Model: "gpt-5.5-codex"})
	if !errors.Is(err, ErrWorkerMixExhausted) {
		t.Fatalf("repeat explicit-model spawn err = %v, want ErrWorkerMixExhausted for the all-down overlaid mix", err)
	}

	_, _, _, err = m.Spawn(ctx, ports.SpawnConfig{
		ProjectID: "mer", Kind: domain.KindWorker, Harness: domain.HarnessCodex, Model: "gpt-5.5-codex",
	})
	if err != nil {
		t.Fatalf("pinned exact overlay recovery spawn: %v", err)
	}
	if tr.IsDown(actual) {
		t.Fatal("successful exact overlay spawn did not recover the explicit model candidate")
	}
	_, _, _, err = m.Spawn(ctx, ports.SpawnConfig{ProjectID: "mer", Kind: domain.KindWorker, Model: "gpt-5.5-codex"})
	if err != nil {
		t.Fatalf("model-only spawn after recovery: %v", err)
	}
}

func TestSpawn_ModelOnlyDownOverlayDebitSelectsHealthyBucket(t *testing.T) {
	tr := candidatehealth.New(candidatehealth.Config{Source: "session_manager"})
	m, rt, _ := healthMixManager(t, domain.ProjectConfig{
		WorkerMix: domain.WorkerMix{
			{Harness: domain.HarnessClaudeCode, Model: "configured-claude-model", Weight: 50},
			{Harness: domain.HarnessCodex, Model: "configured-codex-model", Weight: 50},
		},
	}, tr, nil)
	rt.createErr = errors.New("runtime refused explicit model")

	_, _, _, err := m.Spawn(ctx, ports.SpawnConfig{ProjectID: "mer", Kind: domain.KindWorker, Model: "explicit-overlay-model"})
	if err == nil {
		t.Fatal("expected first explicit-overlay launch failure")
	}
	rt.createErr = nil

	rec, _, _, err := m.Spawn(ctx, ports.SpawnConfig{ProjectID: "mer", Kind: domain.KindWorker, Model: "explicit-overlay-model"})
	if err != nil {
		t.Fatalf("second explicit-overlay spawn: %v", err)
	}
	if rec.Harness != domain.HarnessCodex {
		t.Fatalf("second explicit-overlay harness = %q, want codex after claude overlay debit", rec.Harness)
	}
}

// A successful spawn on a bucket's exact identity recovers a previously-down
// bucket and emits exactly one recovered event. A down bucket is excluded from
// unpinned selection, so the reachable "successful attempt on that exact
// candidate" is a pin onto the bucket's identity.
func TestSpawn_SuccessfulSpawnRecoversDownBucket(t *testing.T) {
	sink := &healthRecordingSink{}
	tr := candidatehealth.New(candidatehealth.Config{Source: "session_manager", Telemetry: sink})
	m, _, _ := healthMixManager(t, domain.ProjectConfig{WorkerMix: singleBucketMix()}, tr, nil)

	cand := workerMixCandidate(domain.HarnessClaudeCode, "", "")
	tr.MarkDown(cand, errors.New("was broken"))
	if !tr.IsDown(cand) {
		t.Fatal("precondition: the bucket should be down")
	}

	if _, _, _, err := m.Spawn(ctx, ports.SpawnConfig{ProjectID: "mer", Kind: domain.KindWorker, Harness: domain.HarnessClaudeCode}); err != nil {
		t.Fatal(err)
	}
	if tr.IsDown(cand) {
		t.Fatal("a successful spawn on the bucket must recover it")
	}
	if got := sink.count(candidatehealth.EventCandidateRecovered); got != 1 {
		t.Fatalf("recovered events = %d, want exactly 1", got)
	}
}

// A pinned spawn is never a mix candidate: even when it fails on an
// otherwise-attributable path and lands on a bucket's exact identity, it must not
// mark that bucket down. A user's bad pin is not evidence the candidate is broken.
func TestSpawn_PinnedFailureDoesNotMarkDown(t *testing.T) {
	tr := candidatehealth.New(candidatehealth.Config{Source: "session_manager"})
	m, _, _ := healthMixManager(t, domain.ProjectConfig{WorkerMix: singleBucketMix()}, tr, binaryMissingLookPath())

	_, _, _, err := m.Spawn(ctx, ports.SpawnConfig{ProjectID: "mer", Kind: domain.KindWorker, Harness: domain.HarnessClaudeCode})
	if !errors.Is(err, ports.ErrAgentBinaryNotFound) {
		t.Fatalf("spawn err = %v, want ErrAgentBinaryNotFound", err)
	}
	if tr.IsDown(workerMixCandidate(domain.HarnessClaudeCode, "", "")) {
		t.Fatal("a pinned spawn failure must not mark any candidate down")
	}
}

// cancelOnDestroyWorkspace cancels the caller's context from inside workspace
// teardown, modelling the ordinary case where the caller goes away (or its
// deadline lapses) while rollback is still in flight. Deterministic on purpose:
// the invariant under test is about ordering, not about how long teardown takes.
type cancelOnDestroyWorkspace struct {
	*fakeWorkspace
	cancel context.CancelFunc
}

func (w *cancelOnDestroyWorkspace) Destroy(ctx context.Context, info ports.WorkspaceInfo) error {
	w.cancel()
	return w.fakeWorkspace.Destroy(ctx, info)
}

// probeFailureMixManager builds a mix-configured manager whose launch-process
// probe reports the given sequence of liveness answers, and whose workspace
// teardown cancels cancelCtx. processAliveSeq drives which of Spawn's two probe
// branches rejects the spawn.
func probeFailureMixManager(t *testing.T, tr *candidatehealth.Tracker, seq []bool, cancel context.CancelFunc) *Manager {
	t.Helper()
	st := newFakeStore()
	st.projects["mer"] = domain.ProjectRecord{ID: "mer", Config: domain.ProjectConfig{WorkerMix: singleBucketMix()}}
	m := New(Deps{
		Runtime:   &fakeRuntime{processAliveSeq: seq},
		Agents:    fakeAgents{},
		Workspace: &cancelOnDestroyWorkspace{fakeWorkspace: &fakeWorkspace{}, cancel: cancel},
		Store:     st,
		Messenger: &fakeMessenger{}, Lifecycle: &fakeLCM{store: st},
		LookPath: func(string) (string, error) { return "/bin/true", nil }, Health: tr,
	})
	m.launchProbe = launchProbeConfig{retryDelay: time.Millisecond, attempts: 1}
	return m
}

// A launch-process probe failure is attributable to the candidate, and
// attribution is decided at FAILURE time. Rollback runs on a context detached
// from the caller's, so teardown legitimately outlives a caller that has gone
// away — but that must not retroactively suppress the mark-down. Marking down
// after cleanup would let the caller's departure erase evidence the bucket
// cannot launch, and a broken bucket that stays healthy is reselected forever.
func TestSpawn_MixSelectedFirstLaunchProbeFailureMarksDownWhenCallerDiesDuringRollback(t *testing.T) {
	tr := candidatehealth.New(candidatehealth.Config{Source: "session_manager"})
	cctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	m := probeFailureMixManager(t, tr, []bool{false}, cancel)

	_, _, _, err := m.Spawn(cctx, ports.SpawnConfig{ProjectID: "mer", Kind: domain.KindWorker})
	if err == nil {
		t.Fatal("expected the launch-process probe to reject the spawn")
	}
	if cctx.Err() == nil {
		t.Fatal("setup: rollback must have cancelled the caller context")
	}
	if !tr.IsDown(workerMixCandidate(domain.HarnessClaudeCode, "", "")) {
		t.Fatal("a launch-probe failure must mark the bucket down even when the caller context dies during rollback")
	}
}

// Same invariant for the post-MarkSpawned probe: the first probe passes, the
// second rejects the spawn, and its rollback outlives the caller.
func TestSpawn_MixSelectedSecondLaunchProbeFailureMarksDownWhenCallerDiesDuringRollback(t *testing.T) {
	tr := candidatehealth.New(candidatehealth.Config{Source: "session_manager"})
	cctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	m := probeFailureMixManager(t, tr, []bool{true, false}, cancel)

	_, _, _, err := m.Spawn(cctx, ports.SpawnConfig{ProjectID: "mer", Kind: domain.KindWorker})
	if err == nil {
		t.Fatal("expected the post-MarkSpawned launch-process probe to reject the spawn")
	}
	if cctx.Err() == nil {
		t.Fatal("setup: rollback must have cancelled the caller context")
	}
	if !tr.IsDown(workerMixCandidate(domain.HarnessClaudeCode, "", "")) {
		t.Fatal("a launch-probe failure must mark the bucket down even when the caller context dies during rollback")
	}
}

// Preflight promises to create nothing and change nothing — that is the whole
// reason a role reconcile may ask it before tearing down the running session.
// For a WORKER the promise cannot hold: resolveSpawnTarget resolves the mix
// bucket, and selectMixBucket debits the candidate-health skip ledger for a
// bucket it finds down. A speculative question would then permanently alter the
// share accounting for a spawn that never happens.
//
// So the exported entry point refuses the kind outright rather than documenting
// a rule its callers have to remember.
func TestPreflight_RefusesWorkerAndLeavesCandidateHealthUntouched(t *testing.T) {
	tr := candidatehealth.New(candidatehealth.Config{Source: "session_manager"})
	m, _, _ := healthMixManager(t, domain.ProjectConfig{WorkerMix: singleBucketMix()}, tr, nil)
	// The single bucket is down, so any mix resolution debits a skip against it.
	down := workerMixCandidate(domain.HarnessClaudeCode, "", "")
	tr.MarkDown(down, errors.New("provider outage"))

	before := skipDebits(tr)

	err := m.Preflight(ctx, ports.SpawnConfig{ProjectID: "mer", Kind: domain.KindWorker})
	if !errors.Is(err, ErrPreflightWorkerUnsupported) {
		t.Fatalf("Preflight(worker) err = %v, want ErrPreflightWorkerUnsupported", err)
	}
	if after := skipDebits(tr); !maps.Equal(before, after) {
		t.Fatalf("skip debits went from %v to %v across a speculative worker preflight; Preflight must not touch candidate health",
			before, after)
	}

	// The guard is scoped to workers: the role kinds Preflight exists for are
	// still answered on their merits.
	if err := m.Preflight(ctx, ports.SpawnConfig{ProjectID: "mer", Kind: domain.KindOrchestrator}); errors.Is(err, ErrPreflightWorkerUnsupported) {
		t.Fatalf("Preflight(orchestrator) was refused as a worker: %v", err)
	}
}

// skipDebits snapshots the candidate-health skip ledger so a test can assert an
// operation left it exactly as it found it.
func skipDebits(tr *candidatehealth.Tracker) map[string]int {
	out := map[string]int{}
	tr.ForEachSkipped(func(c candidatehealth.Candidate, skipped int) {
		out[c.String()] = skipped
	})
	return out
}
