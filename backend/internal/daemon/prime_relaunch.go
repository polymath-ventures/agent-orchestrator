package daemon

import (
	"context"
	"errors"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	sessionsvc "github.com/aoagents/agent-orchestrator/backend/internal/service/session"
)

// primeReconcileSessions is the reconciliation surface the relauncher needs.
type primeReconcileSessions interface {
	ReconcileRole(ctx context.Context, target domain.RoleTarget, opts sessionsvc.ReconcileOptions) (domain.Session, error)
}

type primeSettingsSessions interface {
	SetAndReconcilePrimeSettings(ctx context.Context, settings domain.PrimeSettings) error
}

// primeRelauncher implements the explicit user-initiated Prime relaunch.
//
// It does two things that the supervisor's automatic path deliberately will
// not: it clears budget-paused replacement state, and it reconciles now instead
// of waiting for the next tick. Automatic replacement stays damped against
// crash loops; only a deliberate operator action bypasses the pause.
type primeRelauncher struct {
	reconciler *PrimeReconciler
	sessions   primeReconcileSessions
}

func (p *primeRelauncher) RelaunchPrime(ctx context.Context) (domain.Session, error) {
	// Never report success with an empty session: the controller would answer
	// 200 with no session and the UI would believe Prime had relaunched.
	if p == nil || p.sessions == nil {
		return domain.Session{}, errors.New("prime relaunch is not available: reconciliation is not wired")
	}
	// Clear the budget and wake the loop first, so the supervisor stops
	// refusing to act even if the reconcile below races it. Reconciliation is
	// idempotent and serialized on the role target, so at most one Prime is
	// created regardless of which caller wins.
	p.reconciler.RequestRelaunch()
	return p.sessions.ReconcileRole(ctx, domain.PrimeTarget(), sessionsvc.ReconcileOptions{})
}

type primeSettingsReconciler struct {
	reconciler *PrimeReconciler
	sessions   primeSettingsSessions
}

func (p *primeSettingsReconciler) SetAndReconcilePrimeSettings(ctx context.Context, settings domain.PrimeSettings) error {
	if p == nil || p.sessions == nil {
		return errors.New("prime settings reconciliation is not available: reconciliation is not wired")
	}
	p.reconciler.RequestRelaunch()
	return p.sessions.SetAndReconcilePrimeSettings(ctx, settings)
}
