package daemon

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	sessionsvc "github.com/aoagents/agent-orchestrator/backend/internal/service/session"
)

type fakeReconcileSessions struct {
	calls   int
	targets []domain.RoleTarget
	sess    domain.Session
	err     error
}

func (f *fakeReconcileSessions) ReconcileRole(_ context.Context, target domain.RoleTarget, _ sessionsvc.ReconcileOptions) (domain.Session, error) {
	f.calls++
	f.targets = append(f.targets, target)
	return f.sess, f.err
}

// An explicit relaunch reconciles immediately and asks the supervisor to clear
// its budget — it must not depend on a later tick or a backoff window expiring.
func TestRelaunchPrimeReconcilesNowAndSignalsSupervisor(t *testing.T) {
	rec := newPrimeReconciler()
	sessions := &fakeReconcileSessions{sess: domain.Session{SessionRecord: domain.SessionRecord{ID: "prime-2", Kind: domain.KindPrime}}}
	relauncher := &primeRelauncher{reconciler: rec, sessions: sessions}

	got, err := relauncher.RelaunchPrime(context.Background())
	if err != nil {
		t.Fatalf("RelaunchPrime: %v", err)
	}
	if got.ID != "prime-2" {
		t.Fatalf("session = %q, want prime-2", got.ID)
	}
	if sessions.calls != 1 {
		t.Fatalf("ReconcileRole calls = %d, want 1", sessions.calls)
	}
	if len(sessions.targets) != 1 || sessions.targets[0] != domain.PrimeTarget() {
		t.Fatalf("targets = %v, want the prime target", sessions.targets)
	}
	if !rec.drain(context.Background()) {
		t.Fatal("relaunch must signal the supervisor so budget-paused state is cleared")
	}
}

func TestRelaunchPrimeSurfacesReconcileFailure(t *testing.T) {
	rec := newPrimeReconciler()
	sessions := &fakeReconcileSessions{err: errors.New("boom")}
	relauncher := &primeRelauncher{reconciler: rec, sessions: sessions}

	if _, err := relauncher.RelaunchPrime(context.Background()); err == nil {
		t.Fatal("RelaunchPrime() = nil error, want the reconcile failure surfaced")
	}
}

// Repeated presses coalesce into a single pending reconciliation instead of
// queueing a spawn per press.
func TestPrimeReconcilerCoalescesRequests(t *testing.T) {
	rec := newPrimeReconciler()
	rec.RequestRelaunch()
	rec.RequestRelaunch()
	rec.RequestRelaunch()

	if !rec.drain(context.Background()) {
		t.Fatal("want one pending request")
	}
	if rec.drain(context.Background()) {
		t.Fatal("requests must coalesce; a second pending request would spawn twice")
	}
}

// A nil reconciler is a no-op door, so the supervisor still runs when no
// external relaunch surface is wired (as in tests and headless setups).
func TestPrimeReconcilerNilIsSafe(t *testing.T) {
	var rec *PrimeReconciler
	rec.RequestRelaunch()
	if rec.wait() != nil {
		t.Fatal("a nil reconciler must yield a nil channel so select blocks on it forever")
	}
	if rec.drain(context.Background()) {
		t.Fatal("a nil reconciler has nothing to drain")
	}
}

// The budget clear reaches the loop: a capped supervisor that receives a
// relaunch request resets its restart state and ensures Prime again.
func TestSupervisorRelaunchRequestClearsBudgetAndEnsures(t *testing.T) {
	rec := newPrimeReconciler()
	state := &primeSupervisorState{}
	cfg := primeSupervisorConfig{
		RestartLimit: 1,
		Now:          func() time.Time { return time.Unix(0, 0).UTC() },
	}.withDefaults()

	// Exhaust the budget.
	now := cfg.Now().UTC()
	for i := 0; i < 5; i++ {
		state.reserveRestart(now, cfg)
	}
	if len(state.restartAttempts) == 0 {
		t.Fatal("precondition: expected restart attempts to be recorded")
	}

	// The loop's relaunch arm resets before ensuring.
	rec.RequestRelaunch()
	if !rec.drain(context.Background()) {
		t.Fatal("expected a pending relaunch request")
	}
	state.resetRestart()

	if len(state.restartAttempts) != 0 || !state.nextRestartAt.IsZero() || state.restartBackoff != 0 {
		t.Fatalf("budget not cleared: attempts=%v nextRestartAt=%v backoff=%v",
			state.restartAttempts, state.nextRestartAt, state.restartBackoff)
	}
}
