package session

import (
	"context"
	"strings"
	"testing"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
)

// The reported outage, at the service level: the newest role row is terminated
// and nothing else is active. Reconciliation must still produce a live role
// session — previously both spawn paths only looked at active rows, so the
// terminated row's worktree kept holding the canonical branch and every attempt
// failed.
func TestReconcileRoleRelaunchesWhenNewestRowIsTerminated(t *testing.T) {
	for _, tc := range []struct {
		name   string
		target domain.RoleTarget
		seed   domain.SessionRecord
		spawn  domain.SessionRecord
	}{
		{
			name:   "prime",
			target: domain.PrimeTarget(),
			seed:   domain.SessionRecord{ID: "prime-1", Kind: domain.KindPrime, IsTerminated: true, Metadata: domain.SessionMetadata{Branch: "ao/prime"}},
			spawn:  domain.SessionRecord{ID: "prime-2", Kind: domain.KindPrime, Metadata: domain.SessionMetadata{Branch: "ao/prime"}},
		},
		{
			name:   "orchestrator",
			target: domain.OrchestratorTarget("mer"),
			seed:   domain.SessionRecord{ID: "mer-1", ProjectID: "mer", Kind: domain.KindOrchestrator, IsTerminated: true},
			spawn:  domain.SessionRecord{ID: "mer-2", ProjectID: "mer", Kind: domain.KindOrchestrator},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			st := newFakeStore()
			st.prime = domain.PrimeSettings{Enabled: true}.WithDefaults()
			st.projects["mer"] = domain.ProjectRecord{ID: "mer"}
			st.sessions[tc.seed.ID] = tc.seed

			fc := &fakeCommander{spawnRecord: tc.spawn}
			svc := &Service{manager: fc, store: st}

			got, err := svc.ReconcileRole(context.Background(), tc.target, ReconcileOptions{})
			if err != nil {
				t.Fatalf("ReconcileRole: %v", err)
			}
			if got.ID != tc.spawn.ID {
				t.Fatalf("session = %q, want the replacement %q", got.ID, tc.spawn.ID)
			}
			if len(fc.released) != 1 || fc.released[0] != tc.target {
				t.Fatalf("released = %v, want the stale resources for %v released before spawning", fc.released, tc.target)
			}
		})
	}
}

// Reconciliation is idempotent: a healthy active role session is returned
// as-is, and nothing is spawned or released.
func TestReconcileRoleIsIdempotentWhenHealthy(t *testing.T) {
	st := newFakeStore()
	st.prime = domain.PrimeSettings{Enabled: true}.WithDefaults()
	st.sessions["prime-1"] = domain.SessionRecord{ID: "prime-1", Kind: domain.KindPrime}

	fc := &fakeCommander{spawnRecord: domain.SessionRecord{ID: "prime-2", Kind: domain.KindPrime}}
	svc := &Service{manager: fc, store: st}

	got, err := svc.ReconcileRole(context.Background(), domain.PrimeTarget(), ReconcileOptions{})
	if err != nil {
		t.Fatalf("ReconcileRole: %v", err)
	}
	if got.ID != "prime-1" {
		t.Fatalf("session = %q, want the existing active prime", got.ID)
	}
	if fc.spawned {
		t.Fatal("a healthy role session must not be replaced")
	}
	if len(fc.released) != 0 {
		t.Fatalf("released = %v, want no release when nothing is being replaced", fc.released)
	}
}

// Clean retires the active role session first, then releases stale resources,
// then spawns.
func TestReconcileRoleCleanRetiresThenReleasesThenSpawns(t *testing.T) {
	st := newFakeStore()
	st.prime = domain.PrimeSettings{Enabled: true}.WithDefaults()
	st.sessions["prime-1"] = domain.SessionRecord{ID: "prime-1", Kind: domain.KindPrime}

	fc := &fakeCommander{spawnRecord: domain.SessionRecord{ID: "prime-2", Kind: domain.KindPrime, Metadata: domain.SessionMetadata{Branch: "ao/prime"}}}
	svc := &Service{manager: fc, store: st}

	if _, err := svc.ReconcileRole(context.Background(), domain.PrimeTarget(), ReconcileOptions{Clean: true}); err != nil {
		t.Fatalf("ReconcileRole: %v", err)
	}
	if len(fc.retired) != 1 || fc.retired[0] != "prime-1" {
		t.Fatalf("retired = %v, want prime-1", fc.retired)
	}
	if len(fc.released) != 1 {
		t.Fatalf("released = %v, want the stale-resource release to run", fc.released)
	}
	if !fc.spawned {
		t.Fatal("a replacement must be spawned")
	}
}

// A non-role target is a client error, not a silent no-op.
func TestReconcileRoleRejectsNonRoleTarget(t *testing.T) {
	svc := &Service{manager: &fakeCommander{}, store: newFakeStore()}
	if _, err := svc.ReconcileRole(context.Background(), domain.RoleTarget{Kind: domain.KindWorker, ProjectID: "mer"}, ReconcileOptions{}); err == nil {
		t.Fatal("ReconcileRole() = nil error for a worker target, want an error")
	}
}

// A release failure must surface rather than being papered over with a spawn
// that is about to fail on the branch the release did not free.
func TestReconcileRolePropagatesReleaseFailure(t *testing.T) {
	st := newFakeStore()
	st.prime = domain.PrimeSettings{Enabled: true}.WithDefaults()
	st.sessions["prime-1"] = domain.SessionRecord{ID: "prime-1", Kind: domain.KindPrime, IsTerminated: true}

	fc := &fakeCommander{
		spawnRecord: domain.SessionRecord{ID: "prime-2", Kind: domain.KindPrime},
		releaseErr:  context.DeadlineExceeded,
	}
	svc := &Service{manager: fc, store: st}

	if _, err := svc.ReconcileRole(context.Background(), domain.PrimeTarget(), ReconcileOptions{}); err == nil {
		t.Fatal("ReconcileRole() = nil error, want the release failure surfaced")
	}
	if fc.spawned {
		t.Fatal("no replacement may be spawned when stale resources could not be released")
	}
}

// Persisted settings are the single source of truth for Prime lifecycle, so a
// relaunch must not resurrect a Prime the operator disabled. Found by exercising
// the live endpoint: it previously fell through to a spawn and failed with an
// unrelated "agent harness required".
func TestReconcileRoleRefusesPrimeWhenDisabled(t *testing.T) {
	st := newFakeStore()
	st.prime = domain.PrimeSettings{Enabled: false}.WithDefaults()

	fc := &fakeCommander{spawnRecord: domain.SessionRecord{ID: "prime-1", Kind: domain.KindPrime}}
	svc := &Service{manager: fc, store: st}

	_, err := svc.ReconcileRole(context.Background(), domain.PrimeTarget(), ReconcileOptions{})
	if err == nil {
		t.Fatal("ReconcileRole() = nil error while Prime is disabled, want a refusal")
	}
	if !strings.Contains(err.Error(), "disabled") {
		t.Fatalf("error = %q, want it to explain Prime is disabled", err)
	}
	if fc.spawned {
		t.Fatal("no Prime may be spawned while Prime is disabled")
	}
	if len(fc.released) != 0 {
		t.Fatalf("released = %v, want no stale-resource release for a disabled role", fc.released)
	}
}

// An enabled Prime still reconciles normally.
func TestReconcileRoleAllowsPrimeWhenEnabled(t *testing.T) {
	st := newFakeStore()
	st.prime = domain.PrimeSettings{Enabled: true, Harness: domain.HarnessClaudeCode}.WithDefaults()

	fc := &fakeCommander{spawnRecord: domain.SessionRecord{ID: "prime-1", Kind: domain.KindPrime, Harness: domain.HarnessClaudeCode}}
	svc := &Service{manager: fc, store: st}

	if _, err := svc.ReconcileRole(context.Background(), domain.PrimeTarget(), ReconcileOptions{}); err != nil {
		t.Fatalf("ReconcileRole: %v", err)
	}
	if !fc.spawned {
		t.Fatal("an enabled Prime must reconcile into existence")
	}
}
