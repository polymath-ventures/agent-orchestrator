package session

import (
	"context"
	"fmt"
	"testing"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
	"github.com/aoagents/agent-orchestrator/backend/internal/service/prime"
)

// P1 — retire-before-spawn-preflight (GH #144).
//
// ReconcileRole(Clean:true) retires the running role session and only then calls
// the manager's Spawn, so a spawn precondition that fails deterministically —
// and that could have been checked without creating a workspace, a runtime, or a
// DB row — leaves the operator with no role session at all.
//
// The precondition modelled here is the tmux prerequisite check
// (session_manager/manager.go:470 -> validateRuntimePrerequisites,
// manager.go:3913): a bare exec.LookPath, no state touched, returning
// ports.ErrRuntimePrerequisite long before the seed row is inserted at
// manager.go:486. Whatever the reconciler consults, the observable contract is
// the same: when the replacement provably cannot be created, the session that is
// already running must still be running afterwards.
func TestReconcileRoleCleanKeepsActiveRoleWhenSpawnPreconditionsFail(t *testing.T) {
	preconditionErr := fmt.Errorf("spawn: %w: tmux is required for terminal sessions", ports.ErrRuntimePrerequisite)

	for _, tc := range []struct {
		name    string
		target  domain.RoleTarget
		active  domain.SessionRecord
		project domain.ProjectRecord
	}{
		{
			name:   "prime",
			target: domain.PrimeTarget(),
			active: domain.SessionRecord{ID: "prime-1", Kind: domain.KindPrime, Harness: domain.HarnessClaudeCode},
		},
		{
			name:    "orchestrator",
			target:  domain.OrchestratorTarget("mer"),
			active:  domain.SessionRecord{ID: "mer-1", ProjectID: "mer", Kind: domain.KindOrchestrator},
			project: domain.ProjectRecord{ID: "mer"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			st := newFakeStore()
			st.prime = domain.PrimeSettings{Enabled: true, Harness: domain.HarnessClaudeCode}.WithDefaults()
			if tc.project.ID != "" {
				st.projects[tc.project.ID] = tc.project
			}
			st.sessions[tc.active.ID] = tc.active

			fc := &fakeCommander{spawnErr: preconditionErr}
			svc := &Service{manager: fc, store: st}

			_, err := svc.ReconcileRole(context.Background(), tc.target, ReconcileOptions{Clean: true})
			if err == nil {
				t.Fatal("ReconcileRole() = nil error, want the unsatisfiable spawn precondition surfaced")
			}

			if len(fc.retired) != 0 {
				t.Fatalf("retired = %v: the active %s was retired even though spawn preconditions could not be satisfied (spawn err = %v); the operator is left with no role session at all",
					fc.retired, tc.target.Kind, err)
			}
			if len(fc.sent) != 0 {
				t.Fatalf("retire notices sent to %v: the active %s was told it is being replaced even though no replacement could be created",
					fc.sent, tc.target.Kind)
			}

			active, err := svc.activeRoleSessions(context.Background(), tc.target)
			if err != nil {
				t.Fatalf("activeRoleSessions: %v", err)
			}
			if len(active) != 1 || active[0].ID != tc.active.ID {
				t.Fatalf("active %s sessions = %v, want the untouched %q still running", tc.target.Kind, active, tc.active.ID)
			}
		})
	}
}

// primeRaceCommander parks the role spawn so a test can land a Prime settings
// write in the window ReconcileRole leaves open between planRole's settings
// snapshot (service.go:414) and the spawn (service.go:390). Parking is a
// channel handoff rather than a sleep: every store access on either side is
// ordered by a send/receive, so the test is deterministic under -race.
type primeRaceCommander struct {
	*fakeCommander
	store   *fakeStore
	entered chan struct{}
	release chan struct{}
}

func (f *primeRaceCommander) Spawn(ctx context.Context, cfg ports.SpawnConfig) (domain.SessionRecord, int, int, error) {
	close(f.entered)
	<-f.release
	rec, promptBytes, systemPromptBytes, err := f.fakeCommander.Spawn(ctx, cfg)
	if err != nil {
		return rec, promptBytes, systemPromptBytes, err
	}
	// The real manager persists the row it spawned; publishing it here is what
	// makes the post-condition ("is a Prime active?") observable.
	f.store.sessions[rec.ID] = rec
	return rec, promptBytes, systemPromptBytes, nil
}

// RetireForReplacement terminates the stored row the way the real manager does,
// so a disable pass that does find the Prime genuinely removes it.
func (f *primeRaceCommander) RetireForReplacement(ctx context.Context, id domain.SessionID) error {
	if err := f.fakeCommander.RetireForReplacement(ctx, id); err != nil {
		return err
	}
	if rec, ok := f.store.sessions[id]; ok {
		rec.IsTerminated = true
		f.store.sessions[id] = rec
	}
	return nil
}

// P2 — Prime settings race (GH #144).
//
// Prime settings writes are not serialized against role reconciliation.
// ReconcileRole snapshots settings in planRole (service.go:414) and spawns from
// that snapshot later (service.go:390); prime.Service.SetSettings
// (service/prime/service.go:72-81) persists the disable and immediately runs the
// supervisor's disable pass (daemon/prime_supervisor.go:212-226: look up the
// active Prime, retire it). If the disable lands inside that window it finds no
// active Prime — the reconcile has not spawned it yet — retires nothing, and the
// reconcile then spawns the Prime the operator just turned off.
//
// The daemon self-corrects on the next supervisor tick, so the defect is exactly
// this transient window: once the disable write and the in-flight reconcile have
// both returned, no active Prime may remain.
//
// The interleaving is pinned deterministically by a channel handoff in the fake
// commander rather than by timing: the spawn parks at fc.entered until the test
// has landed the disable write and closes fc.release. See the comment on the
// disable goroutine below for why that half cannot run inline.
func TestPrimeDisableDuringReconcileLeavesNoActivePrime(t *testing.T) {
	ctx := context.Background()

	st := newFakeStore()
	st.prime = domain.PrimeSettings{Enabled: true, Harness: domain.HarnessClaudeCode}.WithDefaults()

	fc := &primeRaceCommander{
		fakeCommander: &fakeCommander{spawnRecord: domain.SessionRecord{
			ID:       "prime-1",
			Kind:     domain.KindPrime,
			Harness:  domain.HarnessClaudeCode,
			Metadata: domain.SessionMetadata{Branch: "ao/prime"},
		}},
		store:   st,
		entered: make(chan struct{}),
		release: make(chan struct{}),
	}
	svc := &Service{manager: fc, store: st}

	// The settings write goes through the real use-case, including the
	// reconcile-now hook the daemon wires to the supervisor's disable pass.
	//
	// The hook the daemon installs (daemon.go:238) is PrimeReconciler.RequestRelaunch:
	// it only pokes the supervisor and returns, so the disable pass itself runs
	// on the supervisor goroutine, concurrently with any in-flight reconcile.
	// Modelled here as a goroutine for that reason — and because the fix
	// serializes the disable pass against reconciliation, so running it inline
	// on the goroutine that has to close fc.release would deadlock rather than
	// exercise the race.
	disabled := make(chan struct{})
	primeSvc := prime.New(prime.Deps{Store: st, OnSettingsChanged: func() {
		go func() {
			defer close(disabled)
			if _, err := svc.RetireActivePrime(ctx); err != nil {
				t.Errorf("disable pass: RetireActivePrime: %v", err)
			}
		}()
	}})

	reconciled := make(chan error, 1)
	go func() {
		_, err := svc.ReconcileRole(ctx, domain.PrimeTarget(), ReconcileOptions{})
		reconciled <- err
	}()

	// planRole has snapshotted Enabled=true; the spawn has not happened yet.
	<-fc.entered

	if _, err := primeSvc.SetSettings(ctx, domain.PrimeSettings{Enabled: false}.WithDefaults()); err != nil {
		close(fc.release)
		<-reconciled
		t.Fatalf("setup: disabling Prime: %v", err)
	}

	close(fc.release)
	reconcileErr := <-reconciled
	<-disabled

	settings, err := st.GetPrimeSettings(ctx)
	if err != nil {
		t.Fatalf("GetPrimeSettings: %v", err)
	}
	if settings.Enabled {
		t.Fatal("setup: Prime should be persisted as disabled")
	}

	active, ok, err := svc.ActivePrime(ctx)
	if err != nil {
		t.Fatalf("ActivePrime: %v", err)
	}
	if ok {
		t.Fatalf("Prime %q remained active after the disable completed (reconcile err = %v, retired = %v); the disable write and the in-flight reconcile have both returned, so no supervisor tick should be needed to remove it",
			active.ID, reconcileErr, fc.retired)
	}
}

// SetPrimeSettings lets the session test fakes stand in for prime.Store, so the
// settings half of the race above runs through the real prime use-case.
func (f *fakeStore) SetPrimeSettings(_ context.Context, settings domain.PrimeSettings) error {
	f.prime = settings
	return nil
}

// P2, second half — a completed disable must leave NO active Prime, not merely
// one fewer.
//
// ReconcileRole's clean path loops over every active role session because
// check-then-spawn is not atomic (service.go:360-363), so more than one active
// Prime row is reachable. A disable pass that retired only the newest would
// return "retired" while an older Prime kept running — the same end state the
// race above exists to prevent, reached by a different route.
func TestRetireActivePrimeRetiresEveryActivePrime(t *testing.T) {
	ctx := context.Background()

	st := newFakeStore()
	st.prime = domain.PrimeSettings{Enabled: false, Harness: domain.HarnessClaudeCode}.WithDefaults()
	for _, id := range []domain.SessionID{"prime-1", "prime-2"} {
		st.sessions[id] = domain.SessionRecord{
			ID:       id,
			Kind:     domain.KindPrime,
			Harness:  domain.HarnessClaudeCode,
			Metadata: domain.SessionMetadata{Branch: "ao/prime"},
		}
	}

	fc := &primeRaceCommander{fakeCommander: &fakeCommander{}, store: st}
	svc := &Service{manager: fc, store: st}

	retired, err := svc.RetireActivePrime(ctx)
	if err != nil {
		t.Fatalf("RetireActivePrime: %v", err)
	}
	if !retired {
		t.Fatal("RetireActivePrime reported nothing retired, want both Primes retired")
	}
	if len(fc.retired) != 2 {
		t.Fatalf("retired = %v, want both active Primes; a disable that stops at the newest leaves an older Prime running", fc.retired)
	}

	if active, ok, err := svc.ActivePrime(ctx); err != nil {
		t.Fatalf("ActivePrime: %v", err)
	} else if ok {
		t.Fatalf("Prime %q still active after the disable pass returned", active.ID)
	}
}

// Moving a spawn precondition earlier must not delete its telemetry.
//
// A role replacement refused at preflight is a spawn that did not happen, the
// same as one refused inside Spawn: the operator's Prime or orchestrator is
// missing or unreplaced either way. Before this, the refusal that the preflight
// gate now catches FIRST stopped emitting ao.session.spawn_failed entirely — the
// fix for one defect silently blinded the alerting for it.
func TestReconcileRolePreflightRejectionEmitsSpawnFailed(t *testing.T) {
	st := newFakeStore()
	st.prime = domain.PrimeSettings{Enabled: true, Harness: domain.HarnessClaudeCode}.WithDefaults()
	st.sessions["prime-1"] = domain.SessionRecord{ID: "prime-1", Kind: domain.KindPrime, Harness: domain.HarnessClaudeCode}

	sink := &fakeTelemetrySink{}
	fc := &fakeCommander{spawnErr: fmt.Errorf("spawn: %w: tmux is required for terminal sessions", ports.ErrRuntimePrerequisite)}
	svc := NewWithDeps(Deps{Manager: fc, Store: st, Telemetry: sink})

	if _, err := svc.ReconcileRole(context.Background(), domain.PrimeTarget(), ReconcileOptions{Clean: true}); err == nil {
		t.Fatal("ReconcileRole() = nil error, want the preflight refusal surfaced")
	}
	if len(fc.retired) != 0 {
		t.Fatalf("retired = %v: setup must exercise the PREFLIGHT rejection, before any teardown", fc.retired)
	}
	if len(sink.events) != 1 {
		t.Fatalf("telemetry events = %#v, want exactly one spawn_failed for the refused replacement", sink.events)
	}
	ev := sink.events[0]
	if ev.Name != "ao.session.spawn_failed" || ev.Source != "session_service" || ev.Level != ports.TelemetryLevelError {
		t.Fatalf("event = %+v, want an error-level ao.session.spawn_failed", ev)
	}
	if got := ev.Payload["kind"]; got != string(domain.KindPrime) {
		t.Fatalf("event payload kind = %#v, want prime", got)
	}
	if got := ev.Payload["fingerprint"]; got == "" {
		t.Fatalf("event payload fingerprint = %#v, want non-empty so the refusal groups with its Spawn-side twin", got)
	}
}
