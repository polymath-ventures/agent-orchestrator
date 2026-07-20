package sessionmanager

import (
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

// Pinning only a model also bypasses the mix: the mix cannot honor a model it
// did not choose the harness for, so the request falls to config resolution.
func TestSpawn_PinnedModelAloneBypassesMix(t *testing.T) {
	m, _ := mixManager(domain.ProjectConfig{
		WorkerMix: testMix(),
		Worker:    domain.RoleOverride{Harness: domain.HarnessGrok},
	})

	rec, err := m.Spawn(ctx, ports.SpawnConfig{ProjectID: "mer", Kind: domain.KindWorker, Model: "grok-4"})
	if err != nil {
		t.Fatal(err)
	}
	if rec.Harness != domain.HarnessGrok || rec.Model != "grok-4" {
		t.Fatalf("model-pinned spawn = (%q, %q), want the configured worker harness and the pinned model", rec.Harness, rec.Model)
	}
}

// The spec's "pinned spawns do not consume mix share": interleaving pinned
// spawns must leave the unpinned selection sequence byte-identical to the
// sequence the same project produces with no pinned spawns at all.
//
// Both pin shapes matter, and only the second is load-bearing. A pin naming a
// harness outside the mix is invisible to a census that filters on bucket keys
// alone; a pin naming exactly a configured bucket is not, so it is the case
// that forces the mix-selected flag to exist rather than the census inferring
// attribution from (harness, model).
func TestSpawn_PinnedSpawnsDoNotPerturbMixSequence(t *testing.T) {
	pins := map[string]ports.SpawnConfig{
		"harness outside the mix": {ProjectID: "mer", Kind: domain.KindWorker, Harness: domain.HarnessGrok},
		"exactly a configured bucket": {
			ProjectID: "mer", Kind: domain.KindWorker, Harness: domain.HarnessClaudeCode,
		},
	}
	for name, pin := range pins {
		t.Run(name, func(t *testing.T) {
			baselineManager, _ := mixManager(domain.ProjectConfig{WorkerMix: testMix()})
			var baseline []domain.AgentHarness
			for i := 0; i < 6; i++ {
				baseline = append(baseline, spawnUnpinnedWorker(t, baselineManager).Harness)
			}

			m, _ := mixManager(domain.ProjectConfig{WorkerMix: testMix()})
			var interleaved []domain.AgentHarness
			for i := 0; i < 6; i++ {
				// A pinned spawn before every unpinned one.
				if _, err := m.Spawn(ctx, pin); err != nil {
					t.Fatal(err)
				}
				interleaved = append(interleaved, spawnUnpinnedWorker(t, m).Harness)
			}

			for i := range baseline {
				if baseline[i] != interleaved[i] {
					t.Fatalf("selection %d = %q with pinned spawns, want %q (baseline %v, interleaved %v)",
						i, interleaved[i], baseline[i], baseline, interleaved)
				}
			}
		})
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

// A live, non-terminated worker sitting in a configured bucket must still be
// invisible to the census when a user pinned it: only the mix's own selections
// consume mix share.
func TestSpawn_MixCensusIgnoresPinnedSessionsInConfiguredBuckets(t *testing.T) {
	m, st := mixManager(domain.ProjectConfig{WorkerMix: testMix()})

	// Ten pinned claude-code workers. Counting them would compute
	// 60/(10+1) = 5.45 against 30/(0+1) and starve the 60%% bucket.
	for i := 0; i < 10; i++ {
		id := domain.SessionID("mer-pinned-" + string(rune('a'+i)))
		st.sessions[id] = domain.SessionRecord{
			ID: id, ProjectID: "mer", Kind: domain.KindWorker,
			Harness: domain.HarnessClaudeCode,
		}
	}

	if got := spawnUnpinnedWorker(t, m).Harness; got != domain.HarnessClaudeCode {
		t.Fatalf("first selection = %q, want claude-code — pinned rows must not consume mix share", got)
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
