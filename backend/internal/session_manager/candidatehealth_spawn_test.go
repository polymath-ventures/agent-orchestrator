package sessionmanager

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/aoagents/agent-orchestrator/backend/internal/candidatehealth"
	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

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

	_, err := m.Spawn(ctx, ports.SpawnConfig{ProjectID: "mer", Kind: domain.KindWorker})
	if !errors.Is(err, ports.ErrAgentBinaryNotFound) {
		t.Fatalf("spawn err = %v, want ErrAgentBinaryNotFound", err)
	}
	if !tr.IsDown(workerMixCandidate(domain.HarnessClaudeCode, "")) {
		t.Fatal("a mix-selected binary-missing spawn must mark the bucket down")
	}
}

// A mix-selected spawn that fails because the runtime refuses to create marks the
// selected bucket down: the runtime rejecting the launch is attributable to the
// candidate.
func TestSpawn_MixSelectedRuntimeRefusedMarksDown(t *testing.T) {
	tr := candidatehealth.New(candidatehealth.Config{Source: "session_manager"})
	m, rt, _ := healthMixManager(t, domain.ProjectConfig{WorkerMix: singleBucketMix()}, tr, nil)
	rt.createErr = errors.New("runtime refused to create")

	_, err := m.Spawn(ctx, ports.SpawnConfig{ProjectID: "mer", Kind: domain.KindWorker})
	if err == nil {
		t.Fatal("expected runtime-refused spawn to fail")
	}
	if !tr.IsDown(workerMixCandidate(domain.HarnessClaudeCode, "")) {
		t.Fatal("a mix-selected runtime-refused spawn must mark the bucket down")
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

	_, err := m.Spawn(ctx, ports.SpawnConfig{ProjectID: "mer", Kind: domain.KindWorker})
	if !errors.Is(err, ErrUnknownHarness) {
		t.Fatalf("spawn err = %v, want ErrUnknownHarness", err)
	}
	if tr.IsDown(workerMixCandidate(domain.HarnessClaudeCode, "")) {
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

	_, err := m.Spawn(ctx, ports.SpawnConfig{ProjectID: "mer", Kind: domain.KindWorker})
	if err == nil {
		t.Fatal("expected workspace failure to fail the spawn")
	}
	if tr.IsDown(workerMixCandidate(domain.HarnessClaudeCode, "")) {
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
	_, err := m.Spawn(cctx, ports.SpawnConfig{ProjectID: "mer", Kind: domain.KindWorker})
	if !errors.Is(err, ports.ErrAgentBinaryNotFound) {
		t.Fatalf("spawn err = %v, want it to reach the agent-binary check", err)
	}
	if tr.IsDown(workerMixCandidate(domain.HarnessClaudeCode, "")) {
		t.Fatal("a caller-cancelled attempt must not mark the bucket down")
	}
}

// A down bucket is excluded from selection and its share redistributes onto the
// surviving healthy buckets: with the 60%% bucket down, every unpinned spawn goes
// to the 40%% bucket rather than substituting a harness the mix never listed.
func TestSpawn_DownBucketExcludedAndRedistributes(t *testing.T) {
	tr := candidatehealth.New(candidatehealth.Config{Source: "session_manager"})
	m, _, _ := healthMixManager(t, domain.ProjectConfig{WorkerMix: twoBucketMix()}, tr, nil)
	tr.MarkDown(workerMixCandidate(domain.HarnessClaudeCode, ""), errors.New("binary gone"))

	for i := 0; i < 10; i++ {
		rec := spawnUnpinnedWorker(t, m)
		if rec.Harness != domain.HarnessCodex {
			t.Fatalf("selection %d = %q, want codex — the 60%% bucket is down and excluded", i, rec.Harness)
		}
	}
}

// When every bucket in the mix is down, selection has no candidate and the spawn
// fails loudly with ErrWorkerMixExhausted rather than falling back to a harness
// outside the mix.
func TestSpawn_AllBucketsDownFailsLoudly(t *testing.T) {
	tr := candidatehealth.New(candidatehealth.Config{Source: "session_manager"})
	m, _, _ := healthMixManager(t, domain.ProjectConfig{WorkerMix: twoBucketMix()}, tr, nil)
	tr.MarkDown(workerMixCandidate(domain.HarnessClaudeCode, ""), errors.New("binary gone"))
	tr.MarkDown(workerMixCandidate(domain.HarnessCodex, ""), errors.New("runtime refused"))

	_, err := m.Spawn(ctx, ports.SpawnConfig{ProjectID: "mer", Kind: domain.KindWorker})
	if !errors.Is(err, ErrWorkerMixExhausted) {
		t.Fatalf("spawn err = %v, want ErrWorkerMixExhausted", err)
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

	cand := workerMixCandidate(domain.HarnessClaudeCode, "")
	tr.MarkDown(cand, errors.New("was broken"))
	if !tr.IsDown(cand) {
		t.Fatal("precondition: the bucket should be down")
	}

	if _, err := m.Spawn(ctx, ports.SpawnConfig{ProjectID: "mer", Kind: domain.KindWorker, Harness: domain.HarnessClaudeCode}); err != nil {
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

	_, err := m.Spawn(ctx, ports.SpawnConfig{ProjectID: "mer", Kind: domain.KindWorker, Harness: domain.HarnessClaudeCode})
	if !errors.Is(err, ports.ErrAgentBinaryNotFound) {
		t.Fatalf("spawn err = %v, want ErrAgentBinaryNotFound", err)
	}
	if tr.IsDown(workerMixCandidate(domain.HarnessClaudeCode, "")) {
		t.Fatal("a pinned spawn failure must not mark any candidate down")
	}
}
