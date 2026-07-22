package sessionmanager

import (
	"errors"
	"strings"
	"testing"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

// testMix is the 60/30/10 mix the apportionment assertions below are written
// against. All three buckets leave the model unset, so each bucket key is the
// harness alone.
func testMix() domain.WorkerMix {
	return domain.WorkerMix{
		{Harness: domain.HarnessClaudeCode, Weight: 60},
		{Harness: domain.HarnessCodex, Weight: 30},
		{Harness: domain.HarnessAider, Weight: 10},
	}
}

// mixManager builds a manager over a project whose only harness source is cfg,
// so a mix-only project genuinely has no worker role harness to fall back to.
func mixManager(cfg domain.ProjectConfig) (*Manager, *fakeStore) {
	st := newFakeStore()
	st.projects["mer"] = domain.ProjectRecord{ID: "mer", Config: cfg}
	m := New(Deps{
		Runtime: &fakeRuntime{}, Agents: fakeAgents{}, Workspace: &fakeWorkspace{}, Store: st,
		Messenger: &fakeMessenger{}, Lifecycle: &fakeLCM{store: st},
		LookPath: func(string) (string, error) { return "/bin/true", nil },
	})
	return m, st
}

func spawnUnpinnedWorker(t *testing.T, m *Manager) domain.SessionRecord {
	t.Helper()
	rec, err := m.Spawn(ctx, ports.SpawnConfig{ProjectID: "mer", Kind: domain.KindWorker})
	if err != nil {
		t.Fatalf("unpinned worker spawn: %v", err)
	}
	return rec
}

// A full apportionment cycle on a 60/30/10 mix lands exactly 6/3/1, which is the
// property that makes the realized fleet ratio equal the configured one. It also
// pins the census: each spawn must see the previous spawns' buckets, or the
// selector would return the same bucket every time.
func TestSpawn_UnpinnedWorkerConvergesOnConfiguredMix(t *testing.T) {
	m, _ := mixManager(domain.ProjectConfig{WorkerMix: testMix()})

	counts := map[domain.AgentHarness]int{}
	for i := 0; i < 10; i++ {
		counts[spawnUnpinnedWorker(t, m).Harness]++
	}
	want := map[domain.AgentHarness]int{
		domain.HarnessClaudeCode: 6,
		domain.HarnessCodex:      3,
		domain.HarnessAider:      1,
	}
	for harness, n := range want {
		if counts[harness] != n {
			t.Fatalf("after 10 spawns %s = %d, want %d (full distribution %v)", harness, counts[harness], n, counts)
		}
	}
}

// A mix bucket supplies the model as well as the harness, so the model persisted
// on the row is the bucket key the census counts on.
func TestSpawn_MixBucketSuppliesModel(t *testing.T) {
	m, st := mixManager(domain.ProjectConfig{WorkerMix: domain.WorkerMix{
		{Harness: domain.HarnessClaudeCode, Model: "opus", Weight: 100},
	}})

	rec := spawnUnpinnedWorker(t, m)
	if rec.Harness != domain.HarnessClaudeCode || rec.Model != "opus" {
		t.Fatalf("mix-selected bucket = (%q, %q), want (claude-code, opus)", rec.Harness, rec.Model)
	}
	if got := st.sessions["mer-1"].Model; got != "opus" {
		t.Fatalf("persisted model = %q, want opus", got)
	}
}

func TestSpawn_MixBucketEffortOverridesOrInheritsLaunchConfig(t *testing.T) {
	tests := []struct {
		name       string
		mixEffort  domain.Effort
		wantEffort domain.Effort
	}{
		{name: "explicit overrides", mixEffort: domain.EffortHigh, wantEffort: domain.EffortHigh},
		{name: "blank inherits", wantEffort: domain.EffortLow},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := newFakeStore()
			st.projects["mer"] = domain.ProjectRecord{ID: "mer", Config: domain.ProjectConfig{
				Worker: domain.RoleOverride{AgentConfig: domain.AgentConfig{Effort: domain.EffortLow}},
				WorkerMix: domain.WorkerMix{{
					Harness: domain.HarnessCodex, Model: "gpt-5-codex", Effort: tt.mixEffort, Weight: 100,
				}},
			}}
			agent := &recordingAgent{}
			m := modelManager(st, agent)

			rec := spawnUnpinnedWorker(t, m)
			if rec.Effort != tt.wantEffort {
				t.Fatalf("session effort = %q, want %q", rec.Effort, tt.wantEffort)
			}
			if got := st.sessions[rec.ID].Effort; got != tt.wantEffort {
				t.Fatalf("persisted effort = %q, want %q", got, tt.wantEffort)
			}
			if agent.lastConfig.Effort != tt.wantEffort {
				t.Fatalf("adapter effort = %q, want %q", agent.lastConfig.Effort, tt.wantEffort)
			}
		})
	}
}

func TestSpawn_MixCensusDistinguishesEffort(t *testing.T) {
	st := newFakeStore()
	st.projects["mer"] = domain.ProjectRecord{ID: "mer", Config: domain.ProjectConfig{
		WorkerMix: domain.WorkerMix{
			{Harness: domain.HarnessCodex, Model: "gpt-5-codex", Effort: domain.EffortLow, Weight: 50},
			{Harness: domain.HarnessCodex, Model: "gpt-5-codex", Effort: domain.EffortHigh, Weight: 50},
		},
	}}
	m := modelManager(st, &recordingAgent{})

	first := spawnUnpinnedWorker(t, m)
	second := spawnUnpinnedWorker(t, m)
	if first.Effort != domain.EffortLow || second.Effort != domain.EffortHigh {
		t.Fatalf("selected efforts = (%q, %q), want (low, high)", first.Effort, second.Effort)
	}
}

func TestSpawn_MixCandidateHealthDistinguishesEffort(t *testing.T) {
	st := newFakeStore()
	st.projects["mer"] = domain.ProjectRecord{ID: "mer", Config: domain.ProjectConfig{
		WorkerMix: domain.WorkerMix{
			{Harness: domain.HarnessCodex, Model: "gpt-5-codex", Effort: domain.EffortLow, Weight: 50},
			{Harness: domain.HarnessCodex, Model: "gpt-5-codex", Effort: domain.EffortHigh, Weight: 50},
		},
	}}
	m := modelManager(st, &recordingAgent{})
	m.health.MarkDown(workerMixCandidate(domain.HarnessCodex, "gpt-5-codex", domain.EffortLow), errors.New("low effort unavailable"))

	rec := spawnUnpinnedWorker(t, m)
	if rec.Effort != domain.EffortHigh {
		t.Fatalf("selected effort = %q, want healthy high bucket", rec.Effort)
	}
}

func TestMixHasBucketMatchesExactNormalizedTuple(t *testing.T) {
	mix := domain.WorkerMix{{
		Harness: domain.HarnessCodexFugu, Model: "fugu", Effort: domain.EffortMax, Weight: 100,
	}}
	if !mixHasBucket(mix, domain.HarnessCodexFugu, " fugu ", domain.EffortXHigh) {
		t.Fatal("max bucket did not match normalized xhigh tuple")
	}
	if mixHasBucket(mix, domain.HarnessCodexFugu, "fugu", domain.EffortHigh) {
		t.Fatal("same harness/model with different effort matched bucket")
	}
}

func TestSpawn_RejectsBucketsThatDuplicateAfterEffortInheritance(t *testing.T) {
	m, st := mixManager(domain.ProjectConfig{
		Worker: domain.RoleOverride{AgentConfig: domain.AgentConfig{Effort: domain.EffortHigh}},
		WorkerMix: domain.WorkerMix{
			{Harness: domain.HarnessCodex, Model: "gpt-5-codex", Weight: 50},
			{Harness: domain.HarnessCodex, Model: "gpt-5-codex", Effort: domain.EffortHigh, Weight: 50},
		},
	})

	_, err := m.Spawn(ctx, ports.SpawnConfig{ProjectID: "mer", Kind: domain.KindWorker})
	if err == nil || !strings.Contains(err.Error(), "duplicate bucket") {
		t.Fatalf("Spawn err = %v, want duplicate effective tuple", err)
	}
	if len(st.sessions) != 0 {
		t.Fatalf("sessions = %#v, want no state before duplicate rejection", st.sessions)
	}
}

// A project configuring a mix but no worker role harness is spawnable — the
// capability the client-side harness resolution currently blocks.
func TestSpawn_MixOnlyProjectIsSpawnable(t *testing.T) {
	m, _ := mixManager(domain.ProjectConfig{WorkerMix: testMix()})

	rec := spawnUnpinnedWorker(t, m)
	if rec.Harness != domain.HarnessClaudeCode {
		t.Fatalf("first mix selection = %q, want the 60%% bucket claude-code", rec.Harness)
	}
}

// An explicit pin bypasses selection entirely and launches exactly what was
// pinned, even when the mix would have chosen a different bucket.
func TestSpawn_PinnedSpawnBypassesMix(t *testing.T) {
	m, _ := mixManager(domain.ProjectConfig{WorkerMix: testMix()})

	rec, err := m.Spawn(ctx, ports.SpawnConfig{
		ProjectID: "mer", Kind: domain.KindWorker,
		Harness: domain.HarnessGrok, Model: "grok-4",
	})
	if err != nil {
		t.Fatal(err)
	}
	if rec.Harness != domain.HarnessGrok || rec.Model != "grok-4" {
		t.Fatalf("pinned spawn launched (%q, %q), want (grok, grok-4)", rec.Harness, rec.Model)
	}
}

// Pinning only a model still leaves harness selection to the mix. The explicit
// model overlays the selected bucket after the mix chooses the harness.
func TestSpawn_ModelOnlyPinUsesMixSelectedHarness(t *testing.T) {
	m, _ := mixManager(domain.ProjectConfig{
		WorkerMix: testMix(),
		Worker:    domain.RoleOverride{Harness: domain.HarnessGrok},
	})

	rec, err := m.Spawn(ctx, ports.SpawnConfig{ProjectID: "mer", Kind: domain.KindWorker, Model: "grok-4"})
	if err != nil {
		t.Fatal(err)
	}
	if rec.Harness != domain.HarnessClaudeCode || rec.Model != "grok-4" {
		t.Fatalf("model-only spawn = (%q, %q), want mix-selected claude-code and pinned model", rec.Harness, rec.Model)
	}
	if !rec.MixSelected {
		t.Fatal("model-only worker spawn on a mix project must record mixSelected")
	}
}

func TestSpawn_ModelOnlyPinConsumesSelectedBucketShare(t *testing.T) {
	m, st := mixManager(domain.ProjectConfig{
		WorkerMix: domain.WorkerMix{
			{Harness: domain.HarnessClaudeCode, Model: "configured-claude-model", Weight: 50},
			{Harness: domain.HarnessCodex, Model: "configured-codex-model", Weight: 50},
		},
	})

	first, err := m.Spawn(ctx, ports.SpawnConfig{
		ProjectID: "mer", Kind: domain.KindWorker, Model: "explicit-overlay-model",
	})
	if err != nil {
		t.Fatal(err)
	}
	if first.Harness != domain.HarnessClaudeCode || first.Model != "explicit-overlay-model" {
		t.Fatalf("model-only spawn = (%q, %q), want selected claude-code with explicit overlay", first.Harness, first.Model)
	}
	if got := st.sessions[first.ID].MixBucketModel; got != "configured-claude-model" {
		t.Fatalf("mix bucket model = %q, want selected configured bucket model", got)
	}

	second := spawnUnpinnedWorker(t, m)
	if second.Harness != domain.HarnessCodex {
		t.Fatalf("second selection = %q, want codex because the model-only spawn consumed claude share", second.Harness)
	}
}

// A pinned worker in a configured bucket consumes actual live capacity for that
// bucket. The selector balances the real fleet, not only rows selected by the
// mix itself.
func TestSpawn_PinnedWorkersInConfiguredBucketsConsumeMixShare(t *testing.T) {
	m, st := mixManager(domain.ProjectConfig{WorkerMix: testMix()})

	// Ten pinned claude-code workers. Counting them computes 60/(10+1) against
	// 30/(0+1), so the first unpinned spawn should select codex.
	for i := 0; i < 10; i++ {
		id := domain.SessionID("mer-pinned-" + string(rune('a'+i)))
		st.sessions[id] = domain.SessionRecord{
			ID: id, ProjectID: "mer", Kind: domain.KindWorker,
			Harness: domain.HarnessClaudeCode,
		}
	}

	if got := spawnUnpinnedWorker(t, m).Harness; got != domain.HarnessCodex {
		t.Fatalf("first selection = %q, want codex because pinned claude-code workers consume share", got)
	}
}

// The flag the census keys on is written at the one point the mix decides, so a
// mix-selected row carries it and a pinned row — even one landing on exactly a
// configured bucket — does not.
func TestSpawn_MixSelectionIsRecordedOnSession(t *testing.T) {
	m, st := mixManager(domain.ProjectConfig{WorkerMix: testMix()})

	unpinned := spawnUnpinnedWorker(t, m)
	if !unpinned.MixSelected {
		t.Fatal("unpinned mix spawn mixSelected = false, want true")
	}
	if !st.sessions[unpinned.ID].MixSelected {
		t.Fatal("persisted mixSelected = false, want true")
	}

	pinned, err := m.Spawn(ctx, ports.SpawnConfig{
		ProjectID: "mer", Kind: domain.KindWorker, Harness: domain.HarnessClaudeCode,
	})
	if err != nil {
		t.Fatal(err)
	}
	if pinned.MixSelected {
		t.Fatal("pinned spawn mixSelected = true, want false")
	}
	if st.sessions[pinned.ID].MixSelected {
		t.Fatal("persisted pinned mixSelected = true, want false")
	}
}

// A live worker outside the configured mix is ignored by the census: it has no
// bucket whose share can be debited.
func TestSpawn_MixCensusIgnoresWorkersOutsideConfiguredBuckets(t *testing.T) {
	m, st := mixManager(domain.ProjectConfig{WorkerMix: testMix()})

	for i := 0; i < 10; i++ {
		id := domain.SessionID("mer-outside-" + string(rune('a'+i)))
		st.sessions[id] = domain.SessionRecord{
			ID: id, ProjectID: "mer", Kind: domain.KindWorker,
			Harness: domain.HarnessGrok,
		}
	}

	if got := spawnUnpinnedWorker(t, m).Harness; got != domain.HarnessClaudeCode {
		t.Fatalf("first selection = %q, want claude-code because outside-mix rows do not consume bucket share", got)
	}
}

// Terminated sessions free their bucket's share: the census counts live workers
// only, so a mix whose 60%% bucket has been drained selects it again first.
func TestSpawn_MixCensusIgnoresTerminatedAndNonWorkerSessions(t *testing.T) {
	m, st := mixManager(domain.ProjectConfig{WorkerMix: testMix()})

	// Two claude-code sessions that must not count: one terminated, one an
	// orchestrator. Counting either would push the first selection off the 60%
	// bucket.
	st.sessions["mer-dead"] = domain.SessionRecord{
		ID: "mer-dead", ProjectID: "mer", Kind: domain.KindWorker,
		Harness: domain.HarnessClaudeCode, IsTerminated: true,
	}
	st.sessions["mer-orch"] = domain.SessionRecord{
		ID: "mer-orch", ProjectID: "mer", Kind: domain.KindOrchestrator,
		Harness: domain.HarnessClaudeCode,
	}

	if got := spawnUnpinnedWorker(t, m).Harness; got != domain.HarnessClaudeCode {
		t.Fatalf("first selection = %q, want claude-code — terminated and orchestrator rows must not count", got)
	}
}

// With no mix configured, resolution is the pre-existing role/project fallback,
// including its unresolvable-harness error.
func TestSpawn_NoMixLeavesResolutionUnchanged(t *testing.T) {
	m, _ := mixManager(domain.ProjectConfig{Worker: domain.RoleOverride{
		Harness:     domain.HarnessCodex,
		AgentConfig: domain.AgentConfig{Model: "gpt-5"},
	}})

	rec := spawnUnpinnedWorker(t, m)
	if rec.Harness != domain.HarnessCodex || rec.Model != "gpt-5" {
		t.Fatalf("no-mix spawn = (%q, %q), want the role override (codex, gpt-5)", rec.Harness, rec.Model)
	}
}
