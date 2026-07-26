package sessionmanager

import (
	"strings"
	"testing"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
)

// Regression guards for #144 P3.
//
// session_worktrees rows do double duty: they are per-directory records AND the
// reconstruction record for a COMPLETE workspace project. Every consumer of
// them assumes the latter — workspaceProjectRows needs len > 1,
// sessionWorktreeRowsToRepoInfos hard-errors on a repo missing from the
// registry, and restoreWorkspaceProjectRows requires a __root__ row. So a Kill
// that keeps a PARTIAL subset of rows as "cleanup inventory" breaks those
// consumers, which is why the original code cleared everything and only logged.
//
// The fix reclaims uncovered children at Kill time and still clears every
// marker, so these shapes cannot arise. These two tests pin that: they build
// the exact fixture that broke the preserve-rows approach — a killed session
// whose uncovered children name DEREGISTERED repos, the case where rows can
// never be resolved again — and assert the consumers stay healthy.

// The preserve-rows approach left two unresolvable rows behind, which sent
// Cleanup into `session worktree row %q no longer matches workspace registry`
// permanently: the repos are gone from the registry, so it could never recover.
func TestCleanupSucceedsAfterKillOfSessionWithDeregisteredChildRepos(t *testing.T) {
	m, st, _, ws := newLifecycleManager()

	const canonical = "/ws/mer/orchestrator/mer-orchestrator"
	st.projects["mer"] = domain.ProjectRecord{
		ID: "mer", Path: "/repos/mer", Kind: domain.ProjectKindWorkspace, Config: testRoleAgents(),
	}
	// Both children were deregistered after mer-orch-1 spawned. Nothing can
	// resolve them, so any consumer that insists on resolving them is stuck.
	st.workspaceRepo["mer"] = []domain.WorkspaceRepoRecord{}

	st.sessions["mer-orch-1"] = domain.SessionRecord{
		ID: "mer-orch-1", ProjectID: "mer", Kind: domain.KindOrchestrator,
		Metadata: domain.SessionMetadata{WorkspacePath: canonical, Branch: "ao/mer-orchestrator", RuntimeHandleID: "old-handle"},
		Activity: domain.Activity{State: domain.ActivityActive},
	}
	st.worktrees["mer-orch-1"] = []domain.SessionWorktreeRecord{
		{SessionID: "mer-orch-1", RepoName: domain.RootWorkspaceRepoName, Branch: "ao/mer-orchestrator", WorktreePath: canonical, State: "active"},
		{SessionID: "mer-orch-1", RepoName: "web", Branch: "ao/mer-orchestrator", WorktreePath: canonical + "/web", State: "active"},
		{SessionID: "mer-orch-1", RepoName: "docs", Branch: "ao/mer-orchestrator", WorktreePath: canonical + "/docs", State: "active"},
	}
	// The live replacement that makes Kill take the owned-elsewhere branch.
	st.sessions["mer-orch-2"] = domain.SessionRecord{
		ID: "mer-orch-2", ProjectID: "mer", Kind: domain.KindOrchestrator,
		Metadata: domain.SessionMetadata{WorkspacePath: canonical, Branch: "ao/mer-orchestrator", RuntimeHandleID: "new-handle"},
		Activity: domain.Activity{State: domain.ActivityActive},
	}

	if _, err := m.Kill(ctx, "mer-orch-1"); err != nil {
		t.Fatalf("Kill err = %v", err)
	}
	if rows := st.worktrees["mer-orch-1"]; len(rows) != 0 {
		t.Fatalf("worktree rows survived Kill: %#v; a partial subset breaks every consumer of these rows", rows)
	}

	// Retire the live owner so nothing owns the canonical root any more, then
	// let Cleanup reclaim both terminated rows.
	if err := m.RetireForReplacement(ctx, "mer-orch-2"); err != nil {
		t.Fatalf("RetireForReplacement err = %v", err)
	}
	ws.calls = nil

	res, err := m.Cleanup(ctx, "mer")
	if err != nil {
		t.Fatalf("Cleanup err = %v", err)
	}
	for _, skip := range res.Skipped {
		if strings.Contains(skip.Reason, "teardown failed") {
			t.Fatalf("Cleanup skipped %s with %q; the deregistered child rows must not be able to wedge cleanup (skipped = %v)",
				skip.SessionID, skip.Reason, res.Skipped)
		}
	}
	if len(res.Cleaned) != 2 {
		t.Fatalf("cleaned = %v, skipped = %v; want both sessions reclaimed", res.Cleaned, res.Skipped)
	}
}

// The preserve-rows approach left rows with no __root__ entry, and
// restoreWorkspaceProjectRows loops workspace.Restore over every row BEFORE it
// checks for the root — materializing child directories on disk and only then
// failing with "workspace project root worktree row missing".
//
// Scoped to exactly that property: leftover rows must not drive per-child
// Restore calls. Whether the restore itself succeeds is deliberately NOT
// asserted — RestoreWithMode gates only on rec.IsTerminated and consults
// neither workspace ownership nor markers, so it will happily relaunch this
// killed session into the worktree the live replacement still owns. That gap
// predates this change and is tracked separately in GH #168; pinning "restore
// succeeds" here would assert the unsafe outcome is correct.
func TestRestoreAfterKillOfSessionWithDeregisteredChildReposLeavesNoStrayWorktrees(t *testing.T) {
	m, st, _, ws := newLifecycleManager()

	const canonical = "/ws/mer/orchestrator/mer-orchestrator"
	st.projects["mer"] = domain.ProjectRecord{
		ID: "mer", Path: "/repos/mer", Kind: domain.ProjectKindWorkspace, Config: testRoleAgents(),
	}
	st.workspaceRepo["mer"] = []domain.WorkspaceRepoRecord{}

	st.sessions["mer-orch-1"] = domain.SessionRecord{
		ID: "mer-orch-1", ProjectID: "mer", Kind: domain.KindOrchestrator,
		Metadata: domain.SessionMetadata{
			WorkspacePath: canonical, Branch: "ao/mer-orchestrator", RuntimeHandleID: "old-handle",
			Prompt: "keep working", AgentSessionID: "agent-1",
		},
		Activity: domain.Activity{State: domain.ActivityActive},
	}
	st.worktrees["mer-orch-1"] = []domain.SessionWorktreeRecord{
		{SessionID: "mer-orch-1", RepoName: domain.RootWorkspaceRepoName, Branch: "ao/mer-orchestrator", WorktreePath: canonical, State: "active"},
		{SessionID: "mer-orch-1", RepoName: "web", Branch: "ao/mer-orchestrator", WorktreePath: canonical + "/web", State: "active"},
		{SessionID: "mer-orch-1", RepoName: "docs", Branch: "ao/mer-orchestrator", WorktreePath: canonical + "/docs", State: "active"},
	}
	st.sessions["mer-orch-2"] = domain.SessionRecord{
		ID: "mer-orch-2", ProjectID: "mer", Kind: domain.KindOrchestrator,
		Metadata: domain.SessionMetadata{WorkspacePath: canonical, Branch: "ao/mer-orchestrator", RuntimeHandleID: "new-handle"},
		Activity: domain.Activity{State: domain.ActivityActive},
	}

	if _, err := m.Kill(ctx, "mer-orch-1"); err != nil {
		t.Fatalf("Kill err = %v", err)
	}
	ws.calls = nil

	// The outcome is not asserted (see above); only the side effects are.
	_, _ = m.RestoreWithMode(ctx, "mer-orch-1")
	// No per-child Restore may run: with no resolvable child repos, any attempt
	// would materialize stray directories before failing on the missing root.
	for _, call := range ws.calls {
		if call == "Restore:web" || call == "Restore:docs" {
			t.Fatalf("call %q ran; restoring an unresolvable child creates a stray worktree directory (calls: %v)", call, ws.calls)
		}
	}
}
