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

// The launch-probe branch rolls back through rollbackSpawnSeedRow, whose job is
// to DELETE the row of a spawn that never became observable rather than park a
// terminated phantom. Asserting only "nothing is live" would pass either way,
// because the delete's own fallback (markSpawnFailedTerminated) is separately
// detached — so this pins the delete itself reaching a usable context.
func TestSpawnSeedRowRollbackSurvivesCallerCancellation(t *testing.T) {
	base := newFakeStore()
	base.projects["mer"] = domain.ProjectRecord{ID: "mer", Config: testRoleAgents()}
	st := &ctxHonoringDeleteStore{fakeStore: base}
	rt := &ctxHonoringRuntime{fakeRuntime: &fakeRuntime{processAliveByHandle: map[string]bool{"h1": false}}}
	cctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ws := &cancelOnDestroyWorkspace{fakeWorkspace: &fakeWorkspace{}, cancel: cancel}
	m := New(Deps{
		Runtime: rt, Agents: fakeAgents{}, Workspace: ws, Store: st,
		Messenger: &fakeMessenger{}, Lifecycle: &fakeLCM{store: base},
		LookPath: func(string) (string, error) { return "/bin/true", nil },
	})
	m.launchProbe = launchProbeConfig{retryDelay: time.Millisecond, attempts: 1}

	_, _, _, err := m.Spawn(cctx, ports.SpawnConfig{ProjectID: "mer", Kind: domain.KindWorker, Harness: domain.HarnessClaudeCode})
	if err == nil {
		t.Fatal("expected the launch-process probe to reject the spawn")
	}
	if cctx.Err() == nil {
		t.Fatal("setup: rollback must have cancelled the caller context")
	}
	if rt.destroyedLive != 1 {
		t.Errorf("runtime destroys that took effect = %d (skipped = %d); the tmux session is still live",
			rt.destroyedLive, rt.destroySkipped)
	}
	if st.deletedLive != 1 {
		t.Errorf("seed-row deletes that took effect = %d (skipped on cancelled ctx = %d); the abandoned spawn's row was never reclaimed",
			st.deletedLive, st.deleteSkipped)
	}
	rows, listErr := base.ListAllSessions(context.Background())
	if listErr != nil {
		t.Fatal(listErr)
	}
	for _, row := range rows {
		if !row.IsTerminated {
			t.Errorf("session %s survived the rolled-back spawn as a non-terminated row", row.ID)
		}
	}
}

// The restore path's post-MarkSpawned probe branch has already flipped the row
// live, so destroying the runtime without parking the row terminated leaves a
// live-looking session with no runtime behind it — the same orphan class, on the
// relaunch side. The pre-MarkSpawned branch has nothing to undo and is excluded.
func TestRestoreLaunchProbeFailureParksTheRelaunchedRowTerminated(t *testing.T) {
	st := newFakeStore()
	st.projects["mer"] = domain.ProjectRecord{ID: "mer", Config: testRoleAgents()}
	seedTerminal(st, "mer-1", domain.SessionMetadata{WorkspacePath: "/ws/mer-1", Branch: "b", AgentSessionID: "agent-x"})
	// First probe passes so MarkSpawned runs; the second rejects the relaunch.
	rt := &ctxHonoringRuntime{fakeRuntime: &fakeRuntime{processAliveSeq: []bool{true, false}}}
	m := New(Deps{
		Runtime: rt, Agents: fakeAgents{}, Workspace: &fakeWorkspace{}, Store: st,
		Messenger: &fakeMessenger{}, Lifecycle: &fakeLCM{store: st},
		LookPath: func(string) (string, error) { return "/bin/true", nil },
	})
	m.launchProbe = launchProbeConfig{retryDelay: time.Millisecond, attempts: 1}

	if _, err := m.RestoreWithMode(context.Background(), "mer-1"); err == nil {
		t.Fatal("expected the post-MarkSpawned probe to reject the relaunch")
	}
	if rt.destroyedLive != 1 {
		t.Fatalf("runtime destroys that took effect = %d, want the relaunched session destroyed", rt.destroyedLive)
	}
	rows, listErr := st.ListAllSessions(context.Background())
	if listErr != nil {
		t.Fatal(listErr)
	}
	for _, row := range rows {
		if !row.IsTerminated {
			t.Errorf("session %s is live after its relaunch was rejected and its runtime destroyed", row.ID)
		}
	}
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

// The complementary half: on the ROLLBACK path the same hook must still run,
// because rollbackPreparedSpawnWorkspace hands it a detached cleanup context.
// Together with the Kill test above this pins the split — compensation detaches,
// requested teardown does not — so neither side can be "fixed" into the other.
func TestSpawnRollbackStillRunsAgentWorkspaceCleanupAfterCallerCancellation(t *testing.T) {
	st := newFakeStore()
	st.projects["mer"] = domain.ProjectRecord{ID: "mer", Config: testRoleAgents()}
	agent := &ctxHonoringCleaningAgent{}
	cctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	m := New(Deps{
		Runtime:   &fakeRuntime{processAliveByHandle: map[string]bool{"h1": false}},
		Agents:    singleAgent{agent: agent},
		Workspace: &cancelOnDestroyWorkspace{fakeWorkspace: &fakeWorkspace{}, cancel: cancel},
		Store:     st,
		Messenger: &fakeMessenger{}, Lifecycle: &fakeLCM{store: st},
		LookPath: func(string) (string, error) { return "/bin/true", nil },
	})
	m.launchProbe = launchProbeConfig{retryDelay: time.Millisecond, attempts: 1}

	if _, _, _, err := m.Spawn(cctx, ports.SpawnConfig{ProjectID: "mer", Kind: domain.KindWorker, Harness: domain.HarnessClaudeCode}); err == nil {
		t.Fatal("expected the launch-process probe to reject the spawn")
	}
	if cctx.Err() == nil {
		t.Fatal("setup: rollback must have cancelled the caller context")
	}
	if agent.ranLive != 1 {
		t.Errorf("agent cleanup runs that took effect = %d (skipped on cancelled ctx = %d); the spawn's agent-side workspace state was left behind",
			agent.ranLive, agent.skipped)
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
