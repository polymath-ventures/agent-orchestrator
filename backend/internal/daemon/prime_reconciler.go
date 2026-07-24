package daemon

import "context"

// PrimeReconciler is the door an explicit user action uses to reach the Prime
// supervisor's desired-state loop.
//
// The restart budget deliberately damps a tight crash loop, but it is
// process-local state owned by the supervisor goroutine, so before this there
// was no way to clear it: an operator's only recourse was to disable Prime and
// hope a supervisor tick landed inside the disabled window. That is the
// documented "hold Prime disabled long enough for a tick" workaround.
//
// Requests are coalesced through a one-slot channel: several relaunch presses
// collapse into a single reconciliation rather than queueing spawns.
type PrimeReconciler struct {
	requests chan struct{}
}

func newPrimeReconciler() *PrimeReconciler {
	return &PrimeReconciler{requests: make(chan struct{}, 1)}
}

// RequestRelaunch clears budget-paused replacement state and wakes the
// supervisor immediately. It never blocks: a pending request already carries
// the same meaning.
func (r *PrimeReconciler) RequestRelaunch() {
	if r == nil {
		return
	}
	select {
	case r.requests <- struct{}{}:
	default:
	}
}

// wait returns the request channel for the supervisor loop to select on.
func (r *PrimeReconciler) wait() <-chan struct{} {
	if r == nil {
		return nil
	}
	return r.requests
}

// drain reports whether a relaunch was requested without blocking. Used by
// tests and by the loop's non-select paths.
func (r *PrimeReconciler) drain(ctx context.Context) bool {
	if r == nil {
		return false
	}
	select {
	case <-r.requests:
		return true
	case <-ctx.Done():
		return false
	default:
		return false
	}
}
