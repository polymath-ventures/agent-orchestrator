package sessionmanager

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

type stashFailOnCallWorkspace struct {
	*fakeWorkspace
	failAt int
	count  int
	err    error
}

func (w *stashFailOnCallWorkspace) StashUncommitted(ctx context.Context, info ports.WorkspaceInfo) (string, error) {
	w.count++
	if w.count == w.failAt {
		return "", w.err
	}
	return w.fakeWorkspace.StashUncommitted(ctx, info)
}

// The reported outage: a terminated Prime row whose worktree still holds
// ao/prime. Nothing in the old code released it, so every replacement spawn hit
// "branch is already checked out in another worktree" until the restart budget
// was spent. Releasing stale role resources is what makes the replacement
// possible.
func TestReleaseStaleRoleResourcesReleasesCanonicalBranch(t *testing.T) {
	m, st, rt, ws := newLifecycleManager()

	st.sessions["prime-1"] = domain.SessionRecord{
		ID: "prime-1", Kind: domain.KindPrime,
		Metadata: domain.SessionMetadata{
			WorkspacePath:   "/data/worktrees/prime/prime-1",
			Branch:          "ao/prime",
			RuntimeHandleID: "leaked-tmux",
		},
		IsTerminated: true, Activity: domain.Activity{State: domain.ActivityExited},
	}
	// A leaked runtime that outlived the terminated row (agent /exit).
	rt.aliveByHandle = map[string]bool{"leaked-tmux": true}

	res, err := m.ReleaseStaleRoleResources(ctx, domain.PrimeTarget())
	if err != nil {
		t.Fatalf("ReleaseStaleRoleResources err = %v", err)
	}
	if len(res.Released) != 1 || res.Released[0] != "prime-1" {
		t.Fatalf("Released = %v, want prime-1", res.Released)
	}
	forced := false
	for _, call := range ws.calls {
		if strings.HasPrefix(call, "ForceDestroy:") {
			forced = true
		}
	}
	if !forced {
		t.Fatalf("the stale worktree holding the canonical branch was not released (calls: %v)", ws.calls)
	}
	if rt.destroyed != 1 {
		t.Fatalf("runtime destroyed = %d, want the leaked runtime reaped", rt.destroyed)
	}
}

// The ownership invariant carries into release: a terminated row sharing the
// live replacement's canonical path must not have that worktree destroyed.
func TestReleaseStaleRoleResourcesSkipsLiveOwnedWorkspace(t *testing.T) {
	m, st, _, ws := newLifecycleManager()

	const canonical = "/ws/mer/orchestrator/mer-orchestrator"
	st.sessions["mer-1"] = domain.SessionRecord{
		ID: "mer-1", ProjectID: "mer", Kind: domain.KindOrchestrator,
		Metadata:     domain.SessionMetadata{WorkspacePath: canonical, Branch: "ao/mer-orchestrator"},
		IsTerminated: true, Activity: domain.Activity{State: domain.ActivityExited},
	}
	st.sessions["mer-2"] = domain.SessionRecord{
		ID: "mer-2", ProjectID: "mer", Kind: domain.KindOrchestrator,
		Metadata: domain.SessionMetadata{WorkspacePath: canonical, Branch: "ao/mer-orchestrator"},
		Activity: domain.Activity{State: domain.ActivityActive},
	}

	res, err := m.ReleaseStaleRoleResources(ctx, domain.OrchestratorTarget("mer"))
	if err != nil {
		t.Fatalf("ReleaseStaleRoleResources err = %v", err)
	}
	for _, call := range ws.calls {
		if strings.HasPrefix(call, "ForceDestroy:") {
			t.Fatalf("call %q ran; the live replacement's worktree must be preserved", call)
		}
	}
	if len(res.Released) != 0 {
		t.Fatalf("Released = %v, want none", res.Released)
	}
}

// Release is scoped to its target: another project's Orchestrator and the fleet
// Prime are untouched.
func TestReleaseStaleRoleResourcesIsScopedToTarget(t *testing.T) {
	m, st, _, ws := newLifecycleManager()

	st.sessions["prime-1"] = domain.SessionRecord{
		ID: "prime-1", Kind: domain.KindPrime,
		Metadata:     domain.SessionMetadata{WorkspacePath: "/ws/prime-1", Branch: "ao/prime"},
		IsTerminated: true, Activity: domain.Activity{State: domain.ActivityExited},
	}
	st.sessions["other-1"] = domain.SessionRecord{
		ID: "other-1", ProjectID: "other", Kind: domain.KindOrchestrator,
		Metadata:     domain.SessionMetadata{WorkspacePath: "/ws/other-1", Branch: "ao/other-orchestrator"},
		IsTerminated: true, Activity: domain.Activity{State: domain.ActivityExited},
	}
	st.sessions["mer-worker"] = domain.SessionRecord{
		ID: "mer-worker", ProjectID: "mer", Kind: domain.KindWorker,
		Metadata:     domain.SessionMetadata{WorkspacePath: "/ws/mer-worker", Branch: "ao/mer-worker"},
		IsTerminated: true, Activity: domain.Activity{State: domain.ActivityExited},
	}

	res, err := m.ReleaseStaleRoleResources(ctx, domain.PrimeTarget())
	if err != nil {
		t.Fatalf("ReleaseStaleRoleResources err = %v", err)
	}
	if len(res.Released) != 1 || res.Released[0] != "prime-1" {
		t.Fatalf("Released = %v, want only prime-1 (release must be scoped to its target)", res.Released)
	}
	for _, call := range ws.calls {
		if strings.Contains(call, "other-1") || strings.Contains(call, "mer-worker") {
			t.Fatalf("call %q touched an out-of-target session", call)
		}
	}
}

// An invalid target is a programming error, not a silent no-op that would let a
// caller believe stale state was released.
func TestReleaseStaleRoleResourcesRejectsNonRoleTarget(t *testing.T) {
	m, _, _, _ := newLifecycleManager()
	if _, err := m.ReleaseStaleRoleResources(ctx, domain.RoleTarget{Kind: domain.KindWorker, ProjectID: "mer"}); err == nil {
		t.Fatal("ReleaseStaleRoleResources() = nil error for a worker target, want an error")
	}
}

// A stash failure must ABORT the release, not fall through to ForceDestroy.
// Force-destroying past a failed capture deletes uncommitted work permanently.
func TestReleaseStaleRoleResourcesRefusesReleaseWhenStashFails(t *testing.T) {
	m, st, _, ws := newLifecycleManager()
	ws.stashErr = errors.New("git stash exploded")

	st.sessions["prime-1"] = domain.SessionRecord{
		ID: "prime-1", Kind: domain.KindPrime,
		Metadata:     domain.SessionMetadata{WorkspacePath: "/ws/prime-1", Branch: "ao/prime"},
		IsTerminated: true, Activity: domain.Activity{State: domain.ActivityExited},
	}

	res, err := m.ReleaseStaleRoleResources(ctx, domain.PrimeTarget())
	if err != nil {
		t.Fatalf("ReleaseStaleRoleResources err = %v", err)
	}
	for _, call := range ws.calls {
		if strings.HasPrefix(call, "ForceDestroy:") {
			t.Fatalf("call %q ran after a failed stash; uncommitted work would be destroyed", call)
		}
	}
	if len(res.Released) != 0 {
		t.Fatalf("Released = %v, want none when work could not be captured", res.Released)
	}
}

// A stale worktree cannot be stashed and is exactly what release exists to
// clear, so that one failure mode must NOT block the release.
func TestReleaseStaleRoleResourcesProceedsWhenWorktreeIsStale(t *testing.T) {
	m, st, _, ws := newLifecycleManager()
	ws.stashErr = fmt.Errorf("worktree gone: %w", ports.ErrWorkspaceStale)

	st.sessions["prime-1"] = domain.SessionRecord{
		ID: "prime-1", Kind: domain.KindPrime,
		Metadata:     domain.SessionMetadata{WorkspacePath: "/ws/prime-1", Branch: "ao/prime"},
		IsTerminated: true, Activity: domain.Activity{State: domain.ActivityExited},
	}

	res, err := m.ReleaseStaleRoleResources(ctx, domain.PrimeTarget())
	if err != nil {
		t.Fatalf("ReleaseStaleRoleResources err = %v", err)
	}
	if len(res.Released) != 1 {
		t.Fatalf("Released = %v, want prime-1 released despite the stale worktree", res.Released)
	}
}

func TestReleaseStaleRoleResourcesWorkspaceProjectSurvivesCallerCancellationAfterPreservation(t *testing.T) {
	base := newFakeStore()
	st := &ctxHonoringWorktreeStore{fakeStore: base}
	cctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ws := &cancelAfterStashesWorkspace{
		ctxHonoringWorkspace: &ctxHonoringWorkspace{fakeWorkspace: &fakeWorkspace{}},
		cancel:               cancel,
		after:                2,
	}
	m := New(Deps{
		Runtime: &fakeRuntime{}, Agents: fakeAgents{}, Workspace: ws, Store: st,
		Messenger: &fakeMessenger{}, Lifecycle: &fakeLCM{store: base},
		LookPath: func(string) (string, error) { return "/bin/true", nil },
	})
	base.projects["mer"] = domain.ProjectRecord{ID: "mer", Path: "/repos/mer", Kind: domain.ProjectKindWorkspace, Config: testRoleAgents()}
	base.workspaceRepo["mer"] = []domain.WorkspaceRepoRecord{{
		ProjectID:    "mer",
		Name:         "api",
		RelativePath: "api",
	}}
	base.sessions["mer-orch"] = domain.SessionRecord{
		ID:           "mer-orch",
		ProjectID:    "mer",
		Kind:         domain.KindOrchestrator,
		Metadata:     domain.SessionMetadata{WorkspacePath: "/ws/mer-orch", Branch: "ao/mer-orchestrator"},
		IsTerminated: true,
		Activity:     domain.Activity{State: domain.ActivityExited},
	}
	base.worktrees["mer-orch"] = []domain.SessionWorktreeRecord{
		{SessionID: "mer-orch", RepoName: domain.RootWorkspaceRepoName, Branch: "ao/mer-orchestrator", WorktreePath: "/ws/mer-orch", State: "active"},
		{SessionID: "mer-orch", RepoName: "api", Branch: "ao/mer-orchestrator", WorktreePath: "/ws/mer-orch/api", State: "active"},
	}

	res, err := m.ReleaseStaleRoleResources(cctx, domain.OrchestratorTarget("mer"))
	if err != nil {
		t.Fatalf("ReleaseStaleRoleResources err = %v", err)
	}
	if cctx.Err() == nil {
		t.Fatal("setup: the second successful stash must have cancelled the caller context before force destroy")
	}
	if len(res.Released) != 1 || res.Released[0] != "mer-orch" {
		t.Fatalf("Released = %v, want mer-orch", res.Released)
	}
	if ws.destroyedLive != 2 {
		t.Errorf("workspace repo destroys that took effect = %d (skipped on cancelled ctx = %d); want both preserved repos released",
			ws.destroyedLive, ws.destroySkipped)
	}
	if st.deletedLive != 1 {
		t.Errorf("restore-marker clears that took effect = %d (skipped on cancelled ctx = %d)", st.deletedLive, st.deleteSkipped)
	}
}

func TestReleaseStaleRoleResourcesWorkspaceProjectRefusesReleaseWhenAnyRepoStashFails(t *testing.T) {
	m, st, _, baseWS := newLifecycleManager()
	ws := &stashFailOnCallWorkspace{fakeWorkspace: baseWS, failAt: 2, err: errors.New("git stash exploded")}
	m.workspace = ws
	st.projects["mer"] = domain.ProjectRecord{ID: "mer", Path: "/repos/mer", Kind: domain.ProjectKindWorkspace, Config: testRoleAgents()}
	st.workspaceRepo["mer"] = []domain.WorkspaceRepoRecord{{
		ProjectID:    "mer",
		Name:         "api",
		RelativePath: "api",
	}}
	st.sessions["mer-orch"] = domain.SessionRecord{
		ID:           "mer-orch",
		ProjectID:    "mer",
		Kind:         domain.KindOrchestrator,
		Metadata:     domain.SessionMetadata{WorkspacePath: "/ws/mer-orch", Branch: "ao/mer-orchestrator"},
		IsTerminated: true,
		Activity:     domain.Activity{State: domain.ActivityExited},
	}
	st.worktrees["mer-orch"] = []domain.SessionWorktreeRecord{
		{SessionID: "mer-orch", RepoName: domain.RootWorkspaceRepoName, Branch: "ao/mer-orchestrator", WorktreePath: "/ws/mer-orch", State: "active"},
		{SessionID: "mer-orch", RepoName: "api", Branch: "ao/mer-orchestrator", WorktreePath: "/ws/mer-orch/api", State: "active"},
	}

	res, err := m.ReleaseStaleRoleResources(ctx, domain.OrchestratorTarget("mer"))
	if err != nil {
		t.Fatalf("ReleaseStaleRoleResources err = %v", err)
	}
	for _, call := range ws.calls {
		if strings.HasPrefix(call, "ForceDestroy:") {
			t.Fatalf("call %q ran after a failed repo stash; uncommitted work would be destroyed", call)
		}
	}
	if len(res.Released) != 0 {
		t.Fatalf("Released = %v, want none when any repo's work could not be captured", res.Released)
	}
}
