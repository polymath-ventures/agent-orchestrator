package sessionmanager

import (
	"context"
	"errors"
	"testing"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

// The fakes below are deliberately context-honoring: a real tmux destroy, git
// worktree teardown, and durable terminal-state write all fail immediately once
// the context they are handed is cancelled. The plain package fakes ignore ctx,
// so they cannot observe the difference between a rollback that took effect and
// one that was a no-op. These narrow wrappers make that difference observable
// without changing the shared fakes the rest of the package relies on.

// ctxHonoringRuntime fails Destroy on an already-cancelled context, recording
// that the runtime session was NOT torn down.
type ctxHonoringRuntime struct {
	*fakeRuntime
	destroyedLive  int
	destroySkipped int
}

func (r *ctxHonoringRuntime) Destroy(ctx context.Context, handle ports.RuntimeHandle) error {
	if err := ctx.Err(); err != nil {
		r.destroySkipped++
		return err
	}
	r.destroyedLive++
	return r.fakeRuntime.Destroy(ctx, handle)
}

// ctxHonoringWorkspace fails Destroy on an already-cancelled context, recording
// that the worktree was left on disk.
type ctxHonoringWorkspace struct {
	*fakeWorkspace
	destroyedLive  int
	destroySkipped int
}

func (w *ctxHonoringWorkspace) Destroy(ctx context.Context, info ports.WorkspaceInfo) error {
	if err := ctx.Err(); err != nil {
		w.destroySkipped++
		return err
	}
	w.destroyedLive++
	return w.fakeWorkspace.Destroy(ctx, info)
}

// disconnectingLCM models an HTTP client that goes away mid-spawn: MarkSpawned
// is the in-flight step when the request context is cancelled, so it returns
// ctx.Err() and drives Spawn into its post-Create rollback. MarkTerminated then
// honors the context the rollback hands it, so a rollback that reuses the dead
// caller context leaves the row un-terminated.
type disconnectingLCM struct {
	*fakeLCM
	cancel           context.CancelFunc
	terminatedLive   int
	terminateSkipped int
}

func (l *disconnectingLCM) MarkSpawned(ctx context.Context, _ domain.SessionID, _ domain.SessionMetadata) error {
	l.cancel()
	return ctx.Err()
}

func (l *disconnectingLCM) MarkTerminated(ctx context.Context, id domain.SessionID) error {
	if err := ctx.Err(); err != nil {
		l.terminateSkipped++
		return err
	}
	l.terminatedLive++
	return l.fakeLCM.MarkTerminated(ctx, id)
}

// A client disconnect during spawn cancels the caller's request context. The
// compensating rollback must still run to completion: the tmux session is
// destroyed, the worktree is torn down, and the row is parked terminated. Doing
// that teardown on the caller's cancelled context leaves exactly the orphan the
// rollback exists to prevent — a live runtime and a worktree on disk with no
// correct durable record.
//
// Scope note: markMixCandidateDown and verifyLaunchCommandRunning intentionally
// read the caller's context (a caller-cancelled attempt is not a candidate
// fault, and a cancelled probe is what enters rollback at all). This test says
// nothing about those two; it only requires the compensating teardown to survive.
func TestSpawnRollbackSurvivesCallerCancellation(t *testing.T) {
	st := newFakeStore()
	st.projects["mer"] = domain.ProjectRecord{ID: "mer", Config: testRoleAgents()}
	rt := &ctxHonoringRuntime{fakeRuntime: &fakeRuntime{}}
	ws := &ctxHonoringWorkspace{fakeWorkspace: &fakeWorkspace{}}
	cctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	lcm := &disconnectingLCM{fakeLCM: &fakeLCM{store: st}, cancel: cancel}
	m := New(Deps{
		Runtime: rt, Agents: fakeAgents{}, Workspace: ws, Store: st,
		Messenger: &fakeMessenger{}, Lifecycle: lcm,
		LookPath: func(string) (string, error) { return "/bin/true", nil },
	})

	_, _, _, err := m.Spawn(cctx, ports.SpawnConfig{
		ProjectID: "mer", Kind: domain.KindWorker, Harness: domain.HarnessClaudeCode,
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("spawn err = %v, want it to fail with the cancelled caller context", err)
	}
	if rt.created != 1 {
		t.Fatalf("runtime created = %d, want the spawn to have reached runtime.Create", rt.created)
	}

	if rt.destroyedLive != 1 {
		t.Errorf("runtime destroys that took effect = %d (skipped on cancelled ctx = %d); the tmux session is still live after rollback",
			rt.destroyedLive, rt.destroySkipped)
	}
	if ws.destroyedLive != 1 {
		t.Errorf("workspace destroys that took effect = %d (skipped on cancelled ctx = %d); the worktree is still on disk after rollback",
			ws.destroyedLive, ws.destroySkipped)
	}
	if lcm.terminatedLive != 1 {
		t.Errorf("terminal-state writes that took effect = %d (skipped on cancelled ctx = %d); the failed spawn was never parked terminated",
			lcm.terminatedLive, lcm.terminateSkipped)
	}

	rows, listErr := st.ListAllSessions(context.Background())
	if listErr != nil {
		t.Fatal(listErr)
	}
	for _, row := range rows {
		if !row.IsTerminated {
			t.Errorf("session %s survived the rolled-back spawn as a non-terminated row", row.ID)
		}
	}
}
