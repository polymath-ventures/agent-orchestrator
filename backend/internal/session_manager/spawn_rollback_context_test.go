package sessionmanager

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

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

func (w *ctxHonoringWorkspace) ForceDestroy(ctx context.Context, info ports.WorkspaceInfo) error {
	if err := ctx.Err(); err != nil {
		w.destroySkipped++
		return err
	}
	w.destroyedLive++
	return w.fakeWorkspace.ForceDestroy(ctx, info)
}

type cancelAfterStashesWorkspace struct {
	*ctxHonoringWorkspace
	cancel context.CancelFunc
	after  int
	count  int
}

func (w *cancelAfterStashesWorkspace) StashUncommitted(ctx context.Context, info ports.WorkspaceInfo) (string, error) {
	ref, err := w.fakeWorkspace.StashUncommitted(ctx, info)
	if err == nil {
		w.count++
		if w.count == w.after {
			w.cancel()
		}
	}
	return ref, err
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

// disconnectingNameMessenger models the client going away during spawn's
// post-start name write: the pane write is the in-flight step when the request
// context is cancelled, so it fails with ctx.Err() and drives Spawn into the
// name-delivery rollback branch.
type disconnectingNameMessenger struct {
	*fakeMessenger
	cancel context.CancelFunc
}

func (m *disconnectingNameMessenger) Send(ctx context.Context, id domain.SessionID, msg string) error {
	m.cancel()
	_ = m.fakeMessenger.Send(ctx, id, msg)
	return ctx.Err()
}

// Name delivery is the spawn step added after the compensating-context rule, and
// it carries the same rollback obligation as its siblings: a spawn that fails to
// name a session it cannot confirm alive must destroy the runtime it just
// created. Doing that teardown on the caller's cancelled context leaves the tmux
// session live with no correct durable record — the orphan the rollback exists
// to prevent.
//
// This drives the rollback through the name-delivery failure specifically rather
// than through MarkSpawned, because that branch is the one the MarkSpawned test
// above cannot reach.
func TestSpawnNameDeliveryRollbackSurvivesCallerCancellation(t *testing.T) {
	st := newFakeStore()
	st.projects["mer"] = domain.ProjectRecord{ID: "mer", Config: testRoleAgents()}
	// aliveByHandle leaves "h1" unset, so the post-failure liveness probe cannot
	// confirm the pane and forgiveSpawnNameFailure refuses to keep the session.
	// That is what makes this the rollback branch rather than the forgiven one; a
	// real tmux probe on a dead caller context fails the same way, so a
	// disconnect lands here as the ordinary outcome rather than a corner case.
	rt := &ctxHonoringRuntime{fakeRuntime: &fakeRuntime{}}
	cctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	m := New(Deps{
		Runtime: rt, Agents: agentsFor{agent: renameOnlyAgent{}}, Workspace: &fakeWorkspace{}, Store: st,
		Messenger: &disconnectingNameMessenger{fakeMessenger: &fakeMessenger{}, cancel: cancel},
		Lifecycle: &fakeLCM{store: st},
		LookPath:  func(string) (string, error) { return "/bin/true", nil },
	})

	_, _, _, err := m.Spawn(cctx, ports.SpawnConfig{
		ProjectID: "mer", Kind: domain.KindWorker, Harness: domain.HarnessClaudeCode,
		IssueID: "170", IssueTitle: "Name delivery rollback",
	})
	if err == nil || !strings.Contains(err.Error(), "deliver name") {
		t.Fatalf("spawn err = %v, want the name-delivery branch to reject the spawn", err)
	}
	if cctx.Err() == nil {
		t.Fatal("setup: the failing name write must have cancelled the caller context")
	}
	if rt.created != 1 {
		t.Fatalf("runtime created = %d, want the spawn to have reached runtime.Create", rt.created)
	}

	// The branch's other two rollback steps already detach inside their helpers,
	// and the workspace/terminal-state halves are pinned by the MarkSpawned test
	// above; the inline runtime destroy is the one this branch got wrong.
	if rt.destroyedLive != 1 {
		t.Errorf("runtime destroys that took effect = %d (skipped on cancelled ctx = %d); the tmux session is still live after the name-delivery rollback",
			rt.destroyedLive, rt.destroySkipped)
	}
}

// Restore creates a runtime session exactly like spawn does, so it carries the
// same rollback obligation: a relaunch that fails after runtime.Create must
// destroy the pane it just made. The caller here is the one that goes away — a
// disconnecting HTTP restore request, or the boot restore pass whose context is
// cancelled during shutdown — and its cancellation is what makes MarkSpawned
// fail in the first place. Tearing down on that dead context leaves a live
// agent pane attached to a session row that restore reports as failed, which is
// the orphan RestoreAll and Cleanup then have to guess about.
func TestRestoreRollbackSurvivesCallerCancellation(t *testing.T) {
	st := newFakeStore()
	st.projects["mer"] = domain.ProjectRecord{ID: "mer", Config: testRoleAgents()}
	seedTerminal(st, "mer-1", domain.SessionMetadata{WorkspacePath: "/ws/mer-1", Branch: "b", AgentSessionID: "agent-x"})
	rt := &ctxHonoringRuntime{fakeRuntime: &fakeRuntime{}}
	cctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	lcm := &disconnectingLCM{fakeLCM: &fakeLCM{store: st}, cancel: cancel}
	m := New(Deps{
		Runtime: rt, Agents: fakeAgents{}, Workspace: &fakeWorkspace{}, Store: st,
		Messenger: &fakeMessenger{}, Lifecycle: lcm,
		LookPath: func(string) (string, error) { return "/bin/true", nil },
	})

	_, err := m.RestoreWithMode(cctx, "mer-1")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("restore err = %v, want it to fail with the cancelled caller context", err)
	}
	if rt.created != 1 {
		t.Fatalf("runtime created = %d, want the restore to have reached runtime.Create", rt.created)
	}
	if rt.destroyedLive != 1 {
		t.Errorf("runtime destroys that took effect = %d (skipped on cancelled ctx = %d); the relaunched tmux session is still live after the failed restore",
			rt.destroyedLive, rt.destroySkipped)
	}
}

// partialWorktreeStore fails the bookkeeping write for one repo of a multi-repo
// workspace project, after earlier repos have already been recorded, and cancels
// the caller as it does so — the client disconnect is why the write failed.
// DeleteSessionWorktrees honors the context it is handed, so a compensation run
// on the caller's dead context leaves the recorded rows behind.
type partialWorktreeStore struct {
	*fakeStore
	failRepo      string
	cancel        context.CancelFunc
	deletedLive   int
	deleteSkipped int
}

func (s *partialWorktreeStore) UpsertSessionWorktree(ctx context.Context, row domain.SessionWorktreeRecord) error {
	if row.RepoName == s.failRepo {
		s.cancel()
		return ctx.Err()
	}
	return s.fakeStore.UpsertSessionWorktree(ctx, row)
}

func (s *partialWorktreeStore) DeleteSessionWorktrees(ctx context.Context, id domain.SessionID) error {
	if err := ctx.Err(); err != nil {
		s.deleteSkipped++
		return err
	}
	s.deletedLive++
	return s.fakeStore.DeleteSessionWorktrees(ctx, id)
}

// ctxHonoringWorkspaceProject fails the workspace-project destroy on an
// already-cancelled context, recording that the on-disk project was left behind.
type ctxHonoringWorkspaceProject struct {
	*fakeWorkspace
	projectDestroyedLive  int
	projectDestroySkipped int
}

func (w *ctxHonoringWorkspaceProject) DestroyWorkspaceProject(ctx context.Context, info ports.WorkspaceProjectInfo) error {
	if err := ctx.Err(); err != nil {
		w.projectDestroySkipped++
		return err
	}
	w.projectDestroyedLive++
	return w.fakeWorkspace.DestroyWorkspaceProject(ctx, info)
}

// A workspace project whose worktree bookkeeping fails partway through must be
// compensated on both halves: the on-disk project is destroyed AND the
// session_worktree rows already recorded for earlier repos are deleted, so the
// store does not go on describing worktrees that are no longer there. Both
// halves run on the compensating context, so piping the caller's cancelled
// context back in leaks the project directory and leaves the rows behind.
//
// The fake store has no ON DELETE CASCADE, so this pins the explicit delete
// directly. In production the cascade off the seed row usually does this job;
// the explicit delete is what covers the case where the seed row is parked
// terminated rather than deleted and the cascade never fires.
func TestPartialWorkspaceProjectRollbackSurvivesCallerCancellation(t *testing.T) {
	base := newFakeStore()
	base.projects["mer"] = domain.ProjectRecord{
		ID: "mer", Path: "/repo/mer", Kind: domain.ProjectKindWorkspace, Config: testRoleAgents(),
	}
	base.workspaceRepo["mer"] = []domain.WorkspaceRepoRecord{{Name: "api", RelativePath: "services/api"}}
	cctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	// The root repo's row lands first; "api" is the later repo whose write fails.
	st := &partialWorktreeStore{fakeStore: base, failRepo: "api", cancel: cancel}
	ws := &ctxHonoringWorkspaceProject{fakeWorkspace: &fakeWorkspace{}}
	m := New(Deps{
		Runtime: &fakeRuntime{}, Agents: fakeAgents{}, Workspace: ws, Store: st,
		Messenger: &fakeMessenger{}, Lifecycle: &fakeLCM{store: base},
		LookPath: func(string) (string, error) { return "/bin/true", nil },
	})

	_, _, _, err := m.Spawn(cctx, ports.SpawnConfig{ProjectID: "mer", Kind: domain.KindWorker, Harness: domain.HarnessClaudeCode})
	if err == nil || !strings.Contains(err.Error(), `record workspace worktree "api"`) {
		t.Fatalf("spawn err = %v, want the later repo's bookkeeping write to fail", err)
	}
	if cctx.Err() == nil {
		t.Fatal("setup: the failing bookkeeping write must have cancelled the caller context")
	}

	if ws.projectDestroyedLive != 1 {
		t.Errorf("workspace-project destroys that took effect = %d (skipped on cancelled ctx = %d); the half-built project is still on disk",
			ws.projectDestroyedLive, ws.projectDestroySkipped)
	}
	rows, listErr := base.ListSessionWorktrees(context.Background(), "mer-1")
	if listErr != nil {
		t.Fatal(listErr)
	}
	if len(rows) != 0 {
		t.Errorf("surviving session_worktrees rows = %v (row deletes that took effect = %d, skipped on cancelled ctx = %d); the store still describes worktrees of a destroyed project",
			rows, st.deletedLive, st.deleteSkipped)
	}
}

// cleanupContext is the single place the detach rule lives, so its three
// properties are pinned directly: teardown ignores the caller's cancellation, it
// still carries request-scoped values, and it stays bounded.
func TestCleanupContextDetachesButStaysBounded(t *testing.T) {
	type reqKey struct{}
	caller, cancel := context.WithCancel(context.WithValue(context.Background(), reqKey{}, "req-42"))
	cleanupCtx, done := cleanupContext(caller)
	defer done()
	cancel()

	if err := cleanupCtx.Err(); err != nil {
		t.Fatalf("cleanup ctx err = %v, want teardown to survive the caller's cancellation", err)
	}
	if got := cleanupCtx.Value(reqKey{}); got != "req-42" {
		t.Errorf("request-scoped value = %v, want it preserved so teardown still logs/traces", got)
	}
	deadline, ok := cleanupCtx.Deadline()
	if !ok {
		t.Fatal("cleanup ctx has no deadline; a wedged teardown could hang forever")
	}
	if remaining := time.Until(deadline); remaining > spawnCleanupTimeout {
		t.Errorf("deadline is %v out, want at most spawnCleanupTimeout (%v)", remaining, spawnCleanupTimeout)
	}
}

// Teardown helpers call each other, and context.WithoutCancel drops the parent's
// deadline along with its cancellation. Without reuse, each nesting level would
// restart the clock and the real bound would be a multiple of the constant, so a
// re-derive must hand back the same context rather than a fresh budget.
func TestCleanupContextReusesAnExistingCleanupBudget(t *testing.T) {
	outer, cancelOuter := cleanupContext(context.Background())
	defer cancelOuter()
	outerDeadline, _ := outer.Deadline()

	inner, cancelInner := cleanupContext(outer)
	defer cancelInner()
	innerDeadline, ok := inner.Deadline()
	if !ok {
		t.Fatal("nested cleanup ctx lost its deadline")
	}
	if !innerDeadline.Equal(outerDeadline) {
		t.Errorf("nested deadline = %v, outer = %v; nesting must not restart the cleanup budget", innerDeadline, outerDeadline)
	}
	// The no-op cancel must not kill the shared budget its caller still needs.
	cancelInner()
	if err := outer.Err(); err != nil {
		t.Errorf("outer cleanup ctx err = %v after the nested cancel; the shared budget was torn down early", err)
	}
}

// ctxHonoringDeleteStore fails the seed-row delete on an already-cancelled
// context, recording that the row was left behind.
type ctxHonoringDeleteStore struct {
	*fakeStore
	deletedLive   int
	deleteSkipped int
}

func (s *ctxHonoringDeleteStore) DeleteSession(ctx context.Context, id domain.SessionID) (bool, error) {
	if err := ctx.Err(); err != nil {
		s.deleteSkipped++
		return false, err
	}
	s.deletedLive++
	return s.fakeStore.DeleteSession(ctx, id)
}

// ctxHonoringCleaningAgent records whether the agent workspace-cleanup hook
// actually ran, or was skipped because the context it received was already dead.
type ctxHonoringCleaningAgent struct {
	fakeAgent
	ranLive int
	skipped int
}

func (a *ctxHonoringCleaningAgent) CleanupWorkspace(ctx context.Context, _ ports.WorkspaceHookConfig) error {
	if err := ctx.Err(); err != nil {
		a.skipped++
		return err
	}
	a.ranLive++
	return nil
}

// cleanupAgentWorkspace is shared between compensation and requested teardown,
// so it is the one teardown helper that must NOT detach on its own. On a Kill —
// where tearing the session down IS the request — a caller that gives up is
// entitled to abandon the agent cleanup hook with it.
func TestKillLetsTheCallerAbandonAgentWorkspaceCleanup(t *testing.T) {
	st := newFakeStore()
	st.projects["mer"] = domain.ProjectRecord{ID: "mer", Config: testRoleAgents()}
	st.sessions["mer-1"] = mkLive("mer-1")
	agent := &ctxHonoringCleaningAgent{}
	m := New(Deps{
		Runtime: &fakeRuntime{}, Agents: singleAgent{agent: agent}, Workspace: &fakeWorkspace{}, Store: st,
		Messenger: &fakeMessenger{}, Lifecycle: &fakeLCM{store: st},
		LookPath: func(string) (string, error) { return "/bin/true", nil },
	})

	cctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := m.Kill(cctx, "mer-1"); err != nil {
		t.Logf("kill returned %v (the fakes ignore ctx for the destroy steps)", err)
	}
	// skipped == 1 proves the hook was reached and declined the dead context. A
	// bare ranLive == 0 would also hold if the hook were never called at all,
	// which would make this test vacuous.
	if agent.skipped != 1 || agent.ranLive != 0 {
		t.Errorf("agent cleanup ran=%d skipped=%d, want ran=0 skipped=1; a requested teardown must stay cancellable",
			agent.ranLive, agent.skipped)
	}
}

func TestKillSingleRepoCleanupSurvivesCallerCancellationAfterRuntimeDestroy(t *testing.T) {
	base := newFakeStore()
	base.projects["mer"] = domain.ProjectRecord{ID: "mer", Config: testRoleAgents()}
	base.sessions["mer-1"] = mkLive("mer-1")
	st := &ctxHonoringWorktreeStore{fakeStore: base}
	cctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	rt := &cancelOnRuntimeDestroy{fakeRuntime: &fakeRuntime{}, cancel: cancel}
	ws := &ctxHonoringWorkspace{fakeWorkspace: &fakeWorkspace{}}
	lcm := &ctxHonoringLCM{fakeLCM: &fakeLCM{store: base}}
	m := New(Deps{
		Runtime: rt, Agents: fakeAgents{}, Workspace: ws, Store: st,
		Messenger: &fakeMessenger{}, Lifecycle: lcm,
		LookPath: func(string) (string, error) { return "/bin/true", nil },
	})

	if freed, err := m.Kill(cctx, "mer-1"); err != nil || !freed {
		t.Fatalf("Kill freed=%v err=%v, want successful cleanup despite the caller going away", freed, err)
	}
	if cctx.Err() == nil {
		t.Fatal("setup: the runtime destroy must have cancelled the caller context")
	}
	if ws.destroyedLive != 1 {
		t.Errorf("workspace destroys that took effect = %d (skipped on cancelled ctx = %d); the worktree is still on disk after the runtime is gone",
			ws.destroyedLive, ws.destroySkipped)
	}
	if st.deletedLive != 1 {
		t.Errorf("restore-marker clears that took effect = %d (skipped on cancelled ctx = %d); the next boot can replay stale restore inventory",
			st.deletedLive, st.deleteSkipped)
	}
	if lcm.terminatedLive != 1 {
		t.Errorf("terminal-state writes that took effect = %d (skipped on cancelled ctx = %d); the killed row is still live with a dead runtime",
			lcm.terminatedLive, lcm.terminateSkipped)
	}
	if !base.sessions["mer-1"].IsTerminated {
		t.Error("the killed row is still live after its runtime was destroyed; the next boot reads that as crash recovery")
	}
}

func TestKillOwnedElsewhereCleanupSurvivesCallerCancellationAfterRuntimeDestroy(t *testing.T) {
	base := newFakeStore()
	st := &ctxHonoringWorktreeStore{fakeStore: base}
	cctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	rt := &cancelOnRuntimeDestroy{fakeRuntime: &fakeRuntime{}, cancel: cancel}
	ws := &ctxHonoringWorkspace{fakeWorkspace: &fakeWorkspace{}}
	lcm := &ctxHonoringLCM{fakeLCM: &fakeLCM{store: base}}
	m := New(Deps{
		Runtime: rt, Agents: fakeAgents{}, Workspace: ws, Store: st,
		Messenger: &fakeMessenger{}, Lifecycle: lcm,
		LookPath: func(string) (string, error) { return "/bin/true", nil },
	})
	canonical := seedUncoveredChildKill(base)

	if freed, err := m.Kill(cctx, "mer-orch-1"); err != nil || freed {
		t.Fatalf("Kill freed=%v err=%v, want owned root preserved and cleanup to complete despite the caller going away", freed, err)
	}
	if cctx.Err() == nil {
		t.Fatal("setup: the runtime destroy must have cancelled the caller context")
	}
	if ws.destroyedLive != 1 {
		t.Errorf("child worktree destroys that took effect = %d (skipped on cancelled ctx = %d); the uncovered child at %s is orphaned",
			ws.destroyedLive, ws.destroySkipped, canonical+"/web")
	}
	if st.deletedLive != 1 {
		t.Errorf("restore-marker clears that took effect = %d (skipped on cancelled ctx = %d); the next boot's RestoreAll can resurrect the killed row into the replacement's worktree",
			st.deletedLive, st.deleteSkipped)
	}
	if lcm.terminatedLive != 1 {
		t.Errorf("terminal-state writes that took effect = %d (skipped on cancelled ctx = %d); the killed row is still live with a dead runtime",
			lcm.terminatedLive, lcm.terminateSkipped)
	}
	if !base.sessions["mer-orch-1"].IsTerminated {
		t.Error("the killed row is still live after its runtime was destroyed; the next boot reads that as crash recovery")
	}
	if rows := base.worktrees["mer-orch-1"]; len(rows) != 0 {
		t.Errorf("surviving restore markers = %#v, want none", rows)
	}
}

// cancelOnRuntimeDestroy models the caller going away at the exact moment the
// runtime is reaped: the destroy itself succeeds — the pane really is gone —
// and the request context is dead by the time it returns.
type cancelOnRuntimeDestroy struct {
	*fakeRuntime
	cancel context.CancelFunc
}

func (r *cancelOnRuntimeDestroy) Destroy(ctx context.Context, handle ports.RuntimeHandle) error {
	err := r.fakeRuntime.Destroy(ctx, handle)
	r.cancel()
	return err
}

// ctxHonoringWorktreeStore fails the restore-marker clear on an
// already-cancelled context, recording that the markers were left behind.
type ctxHonoringWorktreeStore struct {
	*fakeStore
	deletedLive   int
	deleteSkipped int
}

func (s *ctxHonoringWorktreeStore) DeleteSessionWorktrees(ctx context.Context, id domain.SessionID) error {
	if err := ctx.Err(); err != nil {
		s.deleteSkipped++
		return err
	}
	s.deletedLive++
	return s.fakeStore.DeleteSessionWorktrees(ctx, id)
}

// ctxHonoringLCM fails the terminal-state write on an already-cancelled
// context, recording that the row was left live.
type ctxHonoringLCM struct {
	*fakeLCM
	terminatedLive   int
	terminateSkipped int
}

func (l *ctxHonoringLCM) MarkTerminated(ctx context.Context, id domain.SessionID) error {
	if err := ctx.Err(); err != nil {
		l.terminateSkipped++
		return err
	}
	l.terminatedLive++
	return l.fakeLCM.MarkTerminated(ctx, id)
}

// Retiring a role row whose workspace a live replacement owns reaps the row's
// runtime and then does three pieces of bookkeeping: reclaim the multi-repo
// children nobody occupies, clear the restore markers, and park the row
// terminated. A disconnected HTTP caller is the ordinary way that context dies,
// and once the runtime is gone all three steps are cleanup — none of them is
// the caller's to abandon.
//
// Run on the caller's context they fail together, and the row is left LIVE with
// a dead runtime: on the next boot reconcileLive reads that as crash recovery
// and stashes and force-destroys the shared root and covered children out from
// under the live replacement that legitimately owns them. Reporting the failure
// faithfully does not undo it, so the steps run detached (but bounded) instead.
func TestRetireForReplacementCleanupSurvivesCallerCancellation(t *testing.T) {
	base := newFakeStore()
	st := &ctxHonoringWorktreeStore{fakeStore: base}
	cctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	rt := &cancelOnRuntimeDestroy{fakeRuntime: &fakeRuntime{}, cancel: cancel}
	ws := &ctxHonoringWorkspace{fakeWorkspace: &fakeWorkspace{}}
	lcm := &ctxHonoringLCM{fakeLCM: &fakeLCM{store: base}}
	m := New(Deps{
		Runtime: rt, Agents: fakeAgents{}, Workspace: ws, Store: st,
		Messenger: &fakeMessenger{}, Lifecycle: lcm,
		LookPath: func(string) (string, error) { return "/bin/true", nil },
	})
	// mer-orch-1 shares its canonical root with the live mer-orch-2 and owns one
	// child worktree (…/web) the replacement does not occupy.
	canonical := seedUncoveredChildKill(base)

	if err := m.RetireForReplacement(cctx, "mer-orch-1"); err != nil {
		t.Fatalf("RetireForReplacement err = %v, want the retire to complete despite the caller going away", err)
	}
	if cctx.Err() == nil {
		t.Fatal("setup: the runtime destroy must have cancelled the caller context")
	}
	if rt.destroyed != 1 {
		t.Fatalf("runtime destroys = %d, want 1 (the precondition for this test)", rt.destroyed)
	}

	if ws.destroyedLive != 1 {
		t.Errorf("child worktree destroys that took effect = %d (skipped on cancelled ctx = %d); the uncovered child at %s is orphaned, and clearing its marker below makes it unfindable",
			ws.destroyedLive, ws.destroySkipped, canonical+"/web")
	}
	if st.deletedLive != 1 {
		t.Errorf("restore-marker clears that took effect = %d (skipped on cancelled ctx = %d); the next boot's RestoreAll can resurrect the retired row into the replacement's worktree",
			st.deletedLive, st.deleteSkipped)
	}
	if lcm.terminatedLive != 1 {
		t.Errorf("terminal-state writes that took effect = %d (skipped on cancelled ctx = %d); the retired row is still live with a dead runtime",
			lcm.terminatedLive, lcm.terminateSkipped)
	}
	if !base.sessions["mer-orch-1"].IsTerminated {
		t.Error("the retired row is still live after its runtime was destroyed; the next boot reads that as a crash and tears down the live replacement's shared root")
	}
	if rows := base.worktrees["mer-orch-1"]; len(rows) != 0 {
		t.Errorf("surviving restore markers = %#v, want none", rows)
	}
}

func TestRetireForReplacementWorkspaceProjectCleanupSurvivesCallerCancellation(t *testing.T) {
	base := newFakeStore()
	st := &ctxHonoringWorktreeStore{fakeStore: base}
	cctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	rt := &cancelOnRuntimeDestroy{fakeRuntime: &fakeRuntime{}, cancel: cancel}
	ws := &ctxHonoringWorkspace{fakeWorkspace: &fakeWorkspace{}}
	lcm := &ctxHonoringLCM{fakeLCM: &fakeLCM{store: base}}
	m := New(Deps{
		Runtime: rt, Agents: fakeAgents{}, Workspace: ws, Store: st,
		Messenger: &fakeMessenger{}, Lifecycle: lcm,
		LookPath: func(string) (string, error) { return "/bin/true", nil },
	})
	base.projects["mer"] = domain.ProjectRecord{ID: "mer", Path: "/repos/mer", Kind: domain.ProjectKindWorkspace, Config: testRoleAgents()}
	base.workspaceRepo["mer"] = []domain.WorkspaceRepoRecord{{
		ProjectID:    "mer",
		Name:         "api",
		RelativePath: "api",
	}}
	base.sessions["mer-orch"] = domain.SessionRecord{
		ID:        "mer-orch",
		ProjectID: "mer",
		Kind:      domain.KindOrchestrator,
		Metadata:  domain.SessionMetadata{WorkspacePath: "/ws/mer-orch", Branch: "ao/mer-orchestrator", RuntimeHandleID: "orch-handle"},
		Activity:  domain.Activity{State: domain.ActivityActive},
	}
	base.worktrees["mer-orch"] = []domain.SessionWorktreeRecord{
		{SessionID: "mer-orch", RepoName: domain.RootWorkspaceRepoName, Branch: "ao/mer-orchestrator", WorktreePath: "/ws/mer-orch", State: "active"},
		{SessionID: "mer-orch", RepoName: "api", Branch: "ao/mer-orchestrator", WorktreePath: "/ws/mer-orch/api", State: "active"},
	}

	if err := m.RetireForReplacement(cctx, "mer-orch"); err != nil {
		t.Fatalf("RetireForReplacement err = %v, want workspace-project cleanup to complete despite the caller going away", err)
	}
	if cctx.Err() == nil {
		t.Fatal("setup: the runtime destroy must have cancelled the caller context")
	}
	if rt.destroyed != 1 {
		t.Fatalf("runtime destroys = %d, want 1 (the precondition for this test)", rt.destroyed)
	}

	if ws.destroyedLive != 2 {
		t.Errorf("workspace repo destroys that took effect = %d (skipped on cancelled ctx = %d); the workspace-project repos are orphaned after replacement",
			ws.destroyedLive, ws.destroySkipped)
	}
	if st.deletedLive != 1 {
		t.Errorf("restore-marker clears that took effect = %d (skipped on cancelled ctx = %d); RestoreAll can resurrect the retired row into a replacement worktree",
			st.deletedLive, st.deleteSkipped)
	}
	if lcm.terminatedLive != 1 {
		t.Errorf("terminal-state writes that took effect = %d (skipped on cancelled ctx = %d); the retired row is still live with a dead runtime",
			lcm.terminatedLive, lcm.terminateSkipped)
	}
	if !base.sessions["mer-orch"].IsTerminated {
		t.Error("the retired workspace-project row is still live after its runtime was destroyed")
	}
	if rows := base.worktrees["mer-orch"]; len(rows) != 0 {
		t.Errorf("surviving restore markers = %#v, want none", rows)
	}
}

func TestRetireForReplacementSingleRepoCleanupSurvivesCallerCancellation(t *testing.T) {
	base := newFakeStore()
	st := &ctxHonoringWorktreeStore{fakeStore: base}
	cctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	rt := &cancelOnRuntimeDestroy{fakeRuntime: &fakeRuntime{}, cancel: cancel}
	ws := &ctxHonoringWorkspace{fakeWorkspace: &fakeWorkspace{}}
	lcm := &ctxHonoringLCM{fakeLCM: &fakeLCM{store: base}}
	m := New(Deps{
		Runtime: rt, Agents: fakeAgents{}, Workspace: ws, Store: st,
		Messenger: &fakeMessenger{}, Lifecycle: lcm,
		LookPath: func(string) (string, error) { return "/bin/true", nil },
	})
	base.projects["mer"] = domain.ProjectRecord{ID: "mer", Config: testRoleAgents()}
	base.sessions["mer-orch"] = domain.SessionRecord{
		ID:        "mer-orch",
		ProjectID: "mer",
		Kind:      domain.KindOrchestrator,
		Metadata:  domain.SessionMetadata{WorkspacePath: "/ws/mer-orch", Branch: "ao/mer-orchestrator", RuntimeHandleID: "orch-handle"},
		Activity:  domain.Activity{State: domain.ActivityActive},
	}
	base.worktrees["mer-orch"] = []domain.SessionWorktreeRecord{{
		SessionID:    "mer-orch",
		RepoName:     domain.RootWorkspaceRepoName,
		Branch:       "ao/mer-orchestrator",
		WorktreePath: "/ws/mer-orch",
		State:        "active",
	}}

	if err := m.RetireForReplacement(cctx, "mer-orch"); err != nil {
		t.Fatalf("RetireForReplacement err = %v, want single-repo cleanup to complete despite the caller going away", err)
	}
	if cctx.Err() == nil {
		t.Fatal("setup: the runtime destroy must have cancelled the caller context")
	}
	if rt.destroyed != 1 {
		t.Fatalf("runtime destroys = %d, want 1 (the precondition for this test)", rt.destroyed)
	}
	if ws.destroyedLive != 1 {
		t.Errorf("workspace destroys that took effect = %d (skipped on cancelled ctx = %d); the single-repo worktree is orphaned after replacement",
			ws.destroyedLive, ws.destroySkipped)
	}
	if st.deletedLive != 1 {
		t.Errorf("restore-marker clears that took effect = %d (skipped on cancelled ctx = %d)", st.deletedLive, st.deleteSkipped)
	}
	if lcm.terminatedLive != 1 {
		t.Errorf("terminal-state writes that took effect = %d (skipped on cancelled ctx = %d)", lcm.terminatedLive, lcm.terminateSkipped)
	}
	if !base.sessions["mer-orch"].IsTerminated {
		t.Error("the retired single-repo row is still live after its runtime was destroyed")
	}
}

func TestRetireForReplacementWorkspaceProjectCallerCancellationBeforeRuntimeDestroyLeavesSessionRetryable(t *testing.T) {
	base := newFakeStore()
	st := &ctxHonoringWorktreeStore{fakeStore: base}
	cctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	rt := &ctxHonoringRuntime{fakeRuntime: &fakeRuntime{}}
	ws := &cancelAfterStashesWorkspace{
		ctxHonoringWorkspace: &ctxHonoringWorkspace{fakeWorkspace: &fakeWorkspace{}},
		cancel:               cancel,
		after:                2,
	}
	lcm := &ctxHonoringLCM{fakeLCM: &fakeLCM{store: base}}
	m := New(Deps{
		Runtime: rt, Agents: fakeAgents{}, Workspace: ws, Store: st,
		Messenger: &fakeMessenger{}, Lifecycle: lcm,
		LookPath: func(string) (string, error) { return "/bin/true", nil },
	})
	base.projects["mer"] = domain.ProjectRecord{ID: "mer", Path: "/repos/mer", Kind: domain.ProjectKindWorkspace, Config: testRoleAgents()}
	base.workspaceRepo["mer"] = []domain.WorkspaceRepoRecord{{
		ProjectID:    "mer",
		Name:         "api",
		RelativePath: "api",
	}}
	base.sessions["mer-orch"] = domain.SessionRecord{
		ID:        "mer-orch",
		ProjectID: "mer",
		Kind:      domain.KindOrchestrator,
		Metadata:  domain.SessionMetadata{WorkspacePath: "/ws/mer-orch", Branch: "ao/mer-orchestrator", RuntimeHandleID: "orch-handle"},
		Activity:  domain.Activity{State: domain.ActivityActive},
	}
	base.worktrees["mer-orch"] = []domain.SessionWorktreeRecord{
		{SessionID: "mer-orch", RepoName: domain.RootWorkspaceRepoName, Branch: "ao/mer-orchestrator", WorktreePath: "/ws/mer-orch", State: "active"},
		{SessionID: "mer-orch", RepoName: "api", Branch: "ao/mer-orchestrator", WorktreePath: "/ws/mer-orch/api", State: "active"},
	}

	if err := m.RetireForReplacement(cctx, "mer-orch"); !errors.Is(err, context.Canceled) {
		t.Fatalf("RetireForReplacement err = %v, want the cancelled caller to abort before runtime destroy", err)
	}
	if cctx.Err() == nil {
		t.Fatal("setup: the second successful stash must have cancelled the caller context before runtime destroy")
	}
	if rt.destroyedLive != 0 || rt.destroySkipped != 1 {
		t.Errorf("runtime destroys that took effect = %d (skipped on cancelled ctx = %d); cancellation before runtime destroy must leave the row retryable",
			rt.destroyedLive, rt.destroySkipped)
	}
	if ws.destroyedLive != 0 {
		t.Errorf("workspace repo destroys that took effect = %d (skipped on cancelled ctx = %d); no workspace cleanup should run before the runtime is reaped",
			ws.destroyedLive, ws.destroySkipped)
	}
	if st.deletedLive != 0 {
		t.Errorf("restore-marker clears that took effect = %d (skipped on cancelled ctx = %d)", st.deletedLive, st.deleteSkipped)
	}
	if lcm.terminatedLive != 0 {
		t.Errorf("terminal-state writes that took effect = %d (skipped on cancelled ctx = %d)", lcm.terminatedLive, lcm.terminateSkipped)
	}
	if base.sessions["mer-orch"].IsTerminated {
		t.Error("session was marked terminated even though runtime destroy never took effect")
	}
	if rows := base.worktrees["mer-orch"]; len(rows) != 2 {
		t.Errorf("surviving restore markers = %#v, want root and child retained for retry", rows)
	}
}

// failingDestroyWorkspaceProject reports that the on-disk teardown failed, so
// the worktrees named by the recorded rows are still there.
type failingDestroyWorkspaceProject struct {
	*fakeWorkspace
	err error
}

func (w *failingDestroyWorkspaceProject) DestroyWorkspaceProject(context.Context, ports.WorkspaceProjectInfo) error {
	return w.err
}

// The bookkeeping rows are the only record of where a workspace project's
// worktrees live. When the disk teardown fails they must survive the rollback:
// deleting them would strand directories that no later cleanup pass can find.
func TestPartialWorkspaceProjectRollbackKeepsRowsWhenDestroyFails(t *testing.T) {
	base := newFakeStore()
	base.projects["mer"] = domain.ProjectRecord{
		ID: "mer", Path: "/repo/mer", Kind: domain.ProjectKindWorkspace, Config: testRoleAgents(),
	}
	base.workspaceRepo["mer"] = []domain.WorkspaceRepoRecord{{Name: "api", RelativePath: "services/api"}}
	cctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	st := &partialWorktreeStore{fakeStore: base, failRepo: "api", cancel: cancel}
	ws := &failingDestroyWorkspaceProject{fakeWorkspace: &fakeWorkspace{}, err: errors.New("worktree still checked out")}
	m := New(Deps{
		Runtime: &fakeRuntime{}, Agents: fakeAgents{}, Workspace: ws, Store: st,
		Messenger: &fakeMessenger{}, Lifecycle: &fakeLCM{store: base},
		LookPath: func(string) (string, error) { return "/bin/true", nil },
	})

	_, _, _, err := m.Spawn(cctx, ports.SpawnConfig{ProjectID: "mer", Kind: domain.KindWorker, Harness: domain.HarnessClaudeCode})
	if err == nil || !strings.Contains(err.Error(), `record workspace worktree "api"`) {
		t.Fatalf("spawn err = %v, want the later repo's bookkeeping write to fail", err)
	}
	if st.deletedLive != 0 {
		t.Errorf("worktree rows were deleted %d times despite the disk teardown failing; the leftover project is now unfindable", st.deletedLive)
	}
	rows, listErr := base.ListSessionWorktrees(context.Background(), "mer-1")
	if listErr != nil {
		t.Fatal(listErr)
	}
	if len(rows) == 0 {
		t.Error("no session_worktrees rows survived; nothing records the worktrees still on disk")
	}
}
