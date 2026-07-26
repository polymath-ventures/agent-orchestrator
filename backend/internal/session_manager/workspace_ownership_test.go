package sessionmanager

import (
	"errors"
	"strings"
	"testing"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
)

// Role workspaces are canonical: successive Orchestrator rows for one project
// share a single worktree path. That means a *historical terminated* row points
// at the worktree the *live replacement* is running in. Cleanup must not read
// "terminated row + recorded path" as permission to delete that path.
//
// Today this is masked only by the ignored-residue teardown failure that the
// next phase fixes; without this guard, fixing teardown would ship a build that
// deletes the live orchestrator's worktree.
func TestCleanup_SkipsWorkspaceOwnedByLiveReplacement(t *testing.T) {
	m, st, _, ws := newManager()

	const canonical = "/ws/mer/orchestrator/mer-orchestrator"
	// Two historical terminated orchestrator rows, both pointing at the
	// canonical path the live replacement now occupies.
	st.sessions["mer-1"] = domain.SessionRecord{
		ID: "mer-1", ProjectID: "mer", Kind: domain.KindOrchestrator,
		Metadata:     domain.SessionMetadata{WorkspacePath: canonical, Branch: "ao/mer-orchestrator"},
		IsTerminated: true, Activity: domain.Activity{State: domain.ActivityExited},
	}
	st.sessions["mer-2"] = domain.SessionRecord{
		ID: "mer-2", ProjectID: "mer", Kind: domain.KindOrchestrator,
		Metadata:     domain.SessionMetadata{WorkspacePath: canonical, Branch: "ao/mer-orchestrator"},
		IsTerminated: true, Activity: domain.Activity{State: domain.ActivityExited},
	}
	// The live replacement, on the same canonical path and branch.
	st.sessions["mer-3"] = domain.SessionRecord{
		ID: "mer-3", ProjectID: "mer", Kind: domain.KindOrchestrator,
		Metadata: domain.SessionMetadata{WorkspacePath: canonical, Branch: "ao/mer-orchestrator"},
		Activity: domain.Activity{State: domain.ActivityActive},
	}

	res, err := m.Cleanup(ctx, "mer")
	if err != nil {
		t.Fatal(err)
	}
	if ws.destroyed != 0 {
		t.Fatalf("destroyed %d workspaces; the live replacement's canonical worktree must never be destroyed", ws.destroyed)
	}
	if len(res.Cleaned) != 0 {
		t.Fatalf("cleaned = %v, want none", res.Cleaned)
	}
	if len(res.Skipped) != 2 {
		t.Fatalf("skipped = %v, want both historical rows skipped", res.Skipped)
	}
	for _, skip := range res.Skipped {
		if skip.Reason != "workspace is in use by an active session" {
			t.Errorf("session %s skip reason = %q, want the ownership reason (an ownership skip is not a teardown failure)", skip.SessionID, skip.Reason)
		}
	}
}

// The branch arm of the guard: a terminated role row whose canonical role
// branch is held by an active role session must not have its worktree torn
// down, even when the recorded paths differ (a stale recorded path must not
// become a licence to delete).
func TestCleanup_SkipsRoleWorkspaceWhenCanonicalBranchIsLive(t *testing.T) {
	m, st, _, ws := newManager()

	st.sessions["prime-1"] = domain.SessionRecord{
		ID: "prime-1", Kind: domain.KindPrime,
		Metadata:     domain.SessionMetadata{WorkspacePath: "/ws/prime/prime-1", Branch: "ao/prime"},
		IsTerminated: true, Activity: domain.Activity{State: domain.ActivityExited},
	}
	st.sessions["prime-2"] = domain.SessionRecord{
		ID: "prime-2", Kind: domain.KindPrime,
		Metadata: domain.SessionMetadata{WorkspacePath: "/ws/prime/prime-2", Branch: "ao/prime"},
		Activity: domain.Activity{State: domain.ActivityActive},
	}

	res, err := m.Cleanup(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	if ws.destroyed != 0 {
		t.Fatalf("destroyed %d workspaces; a worktree on a live canonical role branch must be preserved", ws.destroyed)
	}
	if len(res.Skipped) != 1 || res.Skipped[0].SessionID != "prime-1" {
		t.Fatalf("skipped = %v, want prime-1", res.Skipped)
	}
	if res.Skipped[0].Reason != "workspace is in use by an active session" {
		t.Fatalf("reason = %q, want the ownership reason", res.Skipped[0].Reason)
	}
}

// The guard must not become a leak: a terminated role row that nobody live
// holds is still reclaimed. Ownership is the invariant, not "role".
func TestCleanup_ReclaimsUnownedRoleWorkspace(t *testing.T) {
	m, st, _, ws := newManager()

	st.sessions["prime-1"] = domain.SessionRecord{
		ID: "prime-1", Kind: domain.KindPrime,
		Metadata:     domain.SessionMetadata{WorkspacePath: "/ws/prime/prime-1", Branch: "ao/prime"},
		IsTerminated: true, Activity: domain.Activity{State: domain.ActivityExited},
	}

	res, err := m.Cleanup(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	if ws.destroyed != 1 {
		t.Fatalf("destroyed = %d, want 1; an unowned role workspace must still be reclaimed", ws.destroyed)
	}
	if len(res.Cleaned) != 1 || res.Cleaned[0] != "prime-1" {
		t.Fatalf("cleaned = %v, want prime-1", res.Cleaned)
	}
	if len(res.Skipped) != 0 {
		t.Fatalf("skipped = %v, want none", res.Skipped)
	}
}

// A terminated worker sharing no path with anything live is untouched by the
// new guard — worker cleanup semantics must not change.
func TestCleanup_WorkerCleanupUnaffectedByOwnershipGuard(t *testing.T) {
	m, st, _, ws := newManager()

	seedTerminal(st, "mer-1", domain.SessionMetadata{WorkspacePath: "/ws/mer-1"})
	st.sessions["mer-2"] = mkLive("mer-2")

	res, err := m.Cleanup(ctx, "mer")
	if err != nil {
		t.Fatal(err)
	}
	if ws.destroyed != 1 {
		t.Fatalf("destroyed = %d, want 1", ws.destroyed)
	}
	if len(res.Cleaned) != 1 || res.Cleaned[0] != "mer-1" {
		t.Fatalf("cleaned = %v, want mer-1", res.Cleaned)
	}
}

// Two workers that happen to record the same branch (a re-used feature branch)
// must not block each other's cleanup: the branch arm of the guard is scoped to
// role sessions, where the branch is canonical and shared by design.
func TestCleanup_SharedWorkerBranchDoesNotBlockCleanup(t *testing.T) {
	m, st, _, ws := newManager()

	st.sessions["mer-1"] = domain.SessionRecord{
		ID: "mer-1", ProjectID: "mer", Kind: domain.KindWorker,
		Metadata:     domain.SessionMetadata{WorkspacePath: "/ws/mer-1", Branch: "feat/shared"},
		IsTerminated: true, Activity: domain.Activity{State: domain.ActivityExited},
	}
	st.sessions["mer-2"] = domain.SessionRecord{
		ID: "mer-2", ProjectID: "mer", Kind: domain.KindWorker,
		Metadata: domain.SessionMetadata{WorkspacePath: "/ws/mer-2", Branch: "feat/shared"},
		Activity: domain.Activity{State: domain.ActivityActive},
	}

	res, err := m.Cleanup(ctx, "mer")
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Cleaned) != 1 || res.Cleaned[0] != "mer-1" {
		t.Fatalf("cleaned = %v, want mer-1 (a shared worker branch is not an ownership conflict)", res.Cleaned)
	}
	if ws.destroyed != 1 {
		t.Fatalf("destroyed = %d, want 1", ws.destroyed)
	}
}

// The predicate itself, exercised directly: it answers "does this terminated
// row still own the workspace recorded on it?" and every teardown path consults
// this one implementation.
func TestWorkspaceOwnedByLiveSession(t *testing.T) {
	live := []domain.SessionRecord{
		{
			ID: "orch-live", ProjectID: "mer", Kind: domain.KindOrchestrator,
			Metadata: domain.SessionMetadata{WorkspacePath: "/ws/canonical", Branch: "ao/mer-orchestrator"},
		},
	}

	tests := []struct {
		name string
		rec  domain.SessionRecord
		want bool
	}{
		{
			name: "same path is owned",
			rec: domain.SessionRecord{
				ID: "orch-old", ProjectID: "mer", Kind: domain.KindOrchestrator,
				Metadata: domain.SessionMetadata{WorkspacePath: "/ws/canonical"},
			},
			want: true,
		},
		{
			name: "same canonical role branch is owned",
			rec: domain.SessionRecord{
				ID: "orch-older", ProjectID: "mer", Kind: domain.KindOrchestrator,
				Metadata: domain.SessionMetadata{WorkspacePath: "/ws/stale", Branch: "ao/mer-orchestrator"},
			},
			want: true,
		},
		{
			name: "unrelated path and branch is not owned",
			rec: domain.SessionRecord{
				ID: "worker-old", ProjectID: "mer", Kind: domain.KindWorker,
				Metadata: domain.SessionMetadata{WorkspacePath: "/ws/other", Branch: "feat/x"},
			},
			want: false,
		},
		{
			name: "a row does not own against itself",
			rec: domain.SessionRecord{
				ID: "orch-live", ProjectID: "mer", Kind: domain.KindOrchestrator,
				Metadata: domain.SessionMetadata{WorkspacePath: "/ws/canonical", Branch: "ao/mer-orchestrator"},
			},
			want: false,
		},
		{
			name: "empty path is not owned",
			rec: domain.SessionRecord{
				ID: "no-ws", ProjectID: "mer", Kind: domain.KindOrchestrator,
			},
			want: false,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			owner, got := workspaceOwnedByLiveSession(tc.rec, live)
			if got != tc.want {
				t.Fatalf("workspaceOwnedByLiveSession() = %v (owner %q), want %v", got, owner, tc.want)
			}
		})
	}
}

// Retiring a historical role row must not destroy a worktree that a live
// replacement already occupies. Cleanup is not the only teardown path that can
// reach a canonical role workspace, so the ownership predicate gates this one
// too — the row is still marked terminated, just without the destroy.
func TestRetireForReplacementSkipsWorkspaceOwnedByLiveSession(t *testing.T) {
	m, st, rt, ws := newLifecycleManager()

	const canonical = "/ws/mer/orchestrator/mer-orchestrator"
	st.sessions["mer-orch-1"] = domain.SessionRecord{
		ID: "mer-orch-1", ProjectID: "mer", Kind: domain.KindOrchestrator,
		Metadata: domain.SessionMetadata{WorkspacePath: canonical, Branch: "ao/mer-orchestrator", RuntimeHandleID: "old-handle"},
		Activity: domain.Activity{State: domain.ActivityActive},
	}
	// A replacement is already live on the same canonical path.
	st.sessions["mer-orch-2"] = domain.SessionRecord{
		ID: "mer-orch-2", ProjectID: "mer", Kind: domain.KindOrchestrator,
		Metadata: domain.SessionMetadata{WorkspacePath: canonical, Branch: "ao/mer-orchestrator", RuntimeHandleID: "new-handle"},
		Activity: domain.Activity{State: domain.ActivityActive},
	}

	if err := m.RetireForReplacement(ctx, "mer-orch-1"); err != nil {
		t.Fatalf("RetireForReplacement err = %v", err)
	}

	for _, call := range ws.calls {
		if strings.HasPrefix(call, "ForceDestroy:") {
			t.Fatalf("workspace call %q ran; the live replacement's worktree must not be destroyed (calls: %v)", call, ws.calls)
		}
	}
	if !st.sessions["mer-orch-1"].IsTerminated {
		t.Fatal("the retired row must still be marked terminated")
	}
	// Its own leaked runtime is still reaped — only the shared workspace is spared.
	if rt.destroyed != 1 || rt.destroyedIDs[0] != "old-handle" {
		t.Fatalf("runtime destroyed = %d ids=%v, want old-handle", rt.destroyed, rt.destroyedIDs)
	}
}

// Branch names are derived from a project's session prefix, so two projects
// that share a prefix generate the same orchestrator branch. The branch arm
// must be scoped to the SAME role target, or they preserve each other's stale
// worktrees and the canonical branch stays occupied — reproducing the outage
// this change exists to end.
func TestWorkspaceOwnershipBranchArmIsScopedToRoleTarget(t *testing.T) {
	live := []domain.SessionRecord{{
		ID: "beta-orch", ProjectID: "beta", Kind: domain.KindOrchestrator,
		Metadata: domain.SessionMetadata{WorkspacePath: "/ws/beta", Branch: "ao/shared-orchestrator"},
	}}
	// Same branch string, DIFFERENT project — not an ownership conflict.
	stale := domain.SessionRecord{
		ID: "acme-orch", ProjectID: "acme", Kind: domain.KindOrchestrator,
		Metadata: domain.SessionMetadata{WorkspacePath: "/ws/acme", Branch: "ao/shared-orchestrator"},
	}
	if owner, owned := workspaceOwnedByLiveSession(stale, live); owned {
		t.Fatalf("owned by %q; a different project's identical branch name is not an ownership conflict", owner)
	}

	// Same branch AND same target — genuinely owned.
	sameTarget := domain.SessionRecord{
		ID: "beta-orch-old", ProjectID: "beta", Kind: domain.KindOrchestrator,
		Metadata: domain.SessionMetadata{WorkspacePath: "/ws/beta-old", Branch: "ao/shared-orchestrator"},
	}
	if _, owned := workspaceOwnedByLiveSession(sameTarget, live); !owned {
		t.Fatal("same role target on the same canonical branch must be treated as owned")
	}
}

// Rows can record the same worktree with different spellings. A raw string
// compare would miss the match, conclude "not owned", and destroy a live
// worktree — so comparison is normalized.
func TestWorkspaceOwnershipNormalizesPaths(t *testing.T) {
	live := []domain.SessionRecord{{
		ID: "orch-live", ProjectID: "mer", Kind: domain.KindOrchestrator,
		Metadata: domain.SessionMetadata{WorkspacePath: "/ws/mer/orchestrator"},
	}}
	for _, spelling := range []string{
		"/ws/mer/orchestrator/",
		"/ws/mer/./orchestrator",
		"/ws/mer/sibling/../orchestrator",
	} {
		rec := domain.SessionRecord{
			ID: "orch-old", ProjectID: "mer", Kind: domain.KindOrchestrator,
			Metadata: domain.SessionMetadata{WorkspacePath: spelling},
		}
		if _, owned := workspaceOwnedByLiveSession(rec, live); !owned {
			t.Errorf("path %q not recognized as the live worktree; it would be destroyed", spelling)
		}
	}
}

// Killing a stale role row must not destroy the worktree a live replacement
// already occupies on the shared canonical path.
func TestKillSkipsWorkspaceOwnedByLiveSession(t *testing.T) {
	m, st, rt, ws := newLifecycleManager()

	const canonical = "/ws/mer/orchestrator/mer-orchestrator"
	st.sessions["mer-1"] = domain.SessionRecord{
		ID: "mer-1", ProjectID: "mer", Kind: domain.KindOrchestrator,
		Metadata: domain.SessionMetadata{WorkspacePath: canonical, Branch: "ao/mer-orchestrator", RuntimeHandleID: "old-handle"},
		Activity: domain.Activity{State: domain.ActivityActive},
	}
	st.sessions["mer-2"] = domain.SessionRecord{
		ID: "mer-2", ProjectID: "mer", Kind: domain.KindOrchestrator,
		Metadata: domain.SessionMetadata{WorkspacePath: canonical, Branch: "ao/mer-orchestrator", RuntimeHandleID: "new-handle"},
		Activity: domain.Activity{State: domain.ActivityActive},
	}

	if _, err := m.Kill(ctx, "mer-1"); err != nil {
		t.Fatalf("Kill err = %v", err)
	}
	for _, call := range ws.calls {
		if strings.HasPrefix(call, "Destroy:") || strings.HasPrefix(call, "ForceDestroy:") {
			t.Fatalf("call %q ran; the live replacement's worktree must be preserved", call)
		}
	}
	if !st.sessions["mer-1"].IsTerminated {
		t.Fatal("the killed row must still be marked terminated")
	}
	if rt.destroyed != 1 || rt.destroyedIDs[0] != "old-handle" {
		t.Fatalf("runtime destroyed = %d ids=%v, want old-handle", rt.destroyed, rt.destroyedIDs)
	}
}

// #144 P3 — child worktree orphaned on Kill.
//
// When the ROOT workspace is owned by a live replacement, Kill skips every
// teardown but still runs DeleteSessionWorktrees for the WHOLE session.
// session_worktrees rows are the only per-repo record there is (SessionMetadata
// records the root path only) and no HTTP, CLI, or read-model surface exposes
// them, so a multi-repo child the live owner does NOT occupy is orphaned the
// moment its marker is cleared. Cleanup then skips the same session for the
// same root-only ownership reason, so nothing ever reclaims that directory.
//
// The fix is to reclaim the uncovered child right there, while its row still
// names it: the root is shared, but a child nobody live occupies is held by
// nobody. Markers are still cleared wholesale — keeping a partial subset breaks
// every consumer of these rows, which assume they describe a whole project.
func TestKillDestroysChildWorktreeNotCoveredByLiveOwner(t *testing.T) {
	m, st, _, ws := newLifecycleManager()

	const canonical = "/ws/mer/orchestrator/mer-orchestrator"
	st.projects["mer"] = domain.ProjectRecord{
		ID: "mer", Path: "/repos/mer", Kind: domain.ProjectKindWorkspace, Config: testRoleAgents(),
	}
	st.workspaceRepo["mer"] = []domain.WorkspaceRepoRecord{
		{ProjectID: "mer", Name: "api", RelativePath: "api"},
		{ProjectID: "mer", Name: "web", RelativePath: "web"},
	}

	st.sessions["mer-orch-1"] = domain.SessionRecord{
		ID: "mer-orch-1", ProjectID: "mer", Kind: domain.KindOrchestrator,
		Metadata: domain.SessionMetadata{WorkspacePath: canonical, Branch: "ao/mer-orchestrator", RuntimeHandleID: "old-handle"},
		Activity: domain.Activity{State: domain.ActivityActive},
	}
	st.worktrees["mer-orch-1"] = []domain.SessionWorktreeRecord{
		{SessionID: "mer-orch-1", RepoName: domain.RootWorkspaceRepoName, Branch: "ao/mer-orchestrator", WorktreePath: canonical, State: "active"},
		{SessionID: "mer-orch-1", RepoName: "api", Branch: "ao/mer-orchestrator", WorktreePath: canonical + "/api", State: "active"},
		{SessionID: "mer-orch-1", RepoName: "web", Branch: "ao/mer-orchestrator", WorktreePath: canonical + "/web", State: "active"},
	}

	// The live replacement on the same canonical root. It covers the root and
	// "api"; nothing it holds points at .../web.
	st.sessions["mer-orch-2"] = domain.SessionRecord{
		ID: "mer-orch-2", ProjectID: "mer", Kind: domain.KindOrchestrator,
		Metadata: domain.SessionMetadata{WorkspacePath: canonical, Branch: "ao/mer-orchestrator", RuntimeHandleID: "new-handle"},
		Activity: domain.Activity{State: domain.ActivityActive},
	}
	st.worktrees["mer-orch-2"] = []domain.SessionWorktreeRecord{
		{SessionID: "mer-orch-2", RepoName: domain.RootWorkspaceRepoName, Branch: "ao/mer-orchestrator", WorktreePath: canonical, State: "active"},
		{SessionID: "mer-orch-2", RepoName: "api", Branch: "ao/mer-orchestrator", WorktreePath: canonical + "/api", State: "active"},
	}

	if _, err := m.Kill(ctx, "mer-orch-1"); err != nil {
		t.Fatalf("Kill err = %v", err)
	}

	// The uncovered child is reclaimed — and nothing else is.
	if ws.destroyed != 1 {
		t.Fatalf("Destroy calls = %d, want exactly 1 (the uncovered child); calls = %v", ws.destroyed, ws.calls)
	}
	if got := ws.lastDestroyInfo.Path; got != canonical+"/web" {
		t.Fatalf("destroyed %q, want the uncovered child %q; the live owner holds only %v",
			got, canonical+"/web", st.worktrees["mer-orch-2"])
	}
	if got := ws.lastDestroyInfo.RepoPath; got != "/repos/mer/web" {
		t.Fatalf("destroy repo path = %q, want the child repo /repos/mer/web; removing a child worktree against the wrong repo skips the dirty check and deletes uncommitted work",
			got)
	}
	// The shared root and the covered child are never touched.
	for _, call := range ws.calls {
		if call == "Destroy:mer-orchestrator" || call == "Destroy:api" || strings.HasPrefix(call, "ForceDestroy:") {
			t.Fatalf("call %q ran; the live replacement's worktrees must be preserved (calls: %v)", call, ws.calls)
		}
	}
	if !st.sessions["mer-orch-1"].IsTerminated {
		t.Fatal("the killed row must still be marked terminated")
	}
	// Markers are cleared wholesale: a surviving partial subset breaks the
	// consumers of these rows and lets RestoreAll resurrect the killed session.
	if rows := st.worktrees["mer-orch-1"]; len(rows) != 0 {
		t.Fatalf("worktree rows survived: %#v, want none", rows)
	}
}

// seedUncoveredChildKill builds the #144 P3 fixture: a killed orchestrator with
// a root and two child worktrees, and a live replacement on the same canonical
// root that covers the root and "api" but NOT ".../web". Returns the canonical
// root path.
func seedUncoveredChildKill(st *fakeStore) string {
	const canonical = "/ws/mer/orchestrator/mer-orchestrator"
	st.projects["mer"] = domain.ProjectRecord{
		ID: "mer", Path: "/repos/mer", Kind: domain.ProjectKindWorkspace, Config: testRoleAgents(),
	}
	st.workspaceRepo["mer"] = []domain.WorkspaceRepoRecord{
		{ProjectID: "mer", Name: "api", RelativePath: "api"},
		{ProjectID: "mer", Name: "web", RelativePath: "web"},
	}
	st.sessions["mer-orch-1"] = domain.SessionRecord{
		ID: "mer-orch-1", ProjectID: "mer", Kind: domain.KindOrchestrator,
		Metadata: domain.SessionMetadata{WorkspacePath: canonical, Branch: "ao/mer-orchestrator", RuntimeHandleID: "old-handle"},
		Activity: domain.Activity{State: domain.ActivityActive},
	}
	st.worktrees["mer-orch-1"] = []domain.SessionWorktreeRecord{
		{SessionID: "mer-orch-1", RepoName: domain.RootWorkspaceRepoName, Branch: "ao/mer-orchestrator", WorktreePath: canonical, State: "active"},
		{SessionID: "mer-orch-1", RepoName: "api", Branch: "ao/mer-orchestrator", WorktreePath: canonical + "/api", State: "active"},
		{SessionID: "mer-orch-1", RepoName: "web", Branch: "ao/mer-orchestrator", WorktreePath: canonical + "/web", State: "active"},
	}
	st.sessions["mer-orch-2"] = domain.SessionRecord{
		ID: "mer-orch-2", ProjectID: "mer", Kind: domain.KindOrchestrator,
		Metadata: domain.SessionMetadata{WorkspacePath: canonical, Branch: "ao/mer-orchestrator", RuntimeHandleID: "new-handle"},
		Activity: domain.Activity{State: domain.ActivityActive},
	}
	st.worktrees["mer-orch-2"] = []domain.SessionWorktreeRecord{
		{SessionID: "mer-orch-2", RepoName: domain.RootWorkspaceRepoName, Branch: "ao/mer-orchestrator", WorktreePath: canonical, State: "active"},
		{SessionID: "mer-orch-2", RepoName: "api", Branch: "ao/mer-orchestrator", WorktreePath: canonical + "/api", State: "active"},
	}
	return canonical
}

// The child reclaim must be ordered AFTER the runtime teardown.
//
// Reclaiming first removes a directory out from under an agent process that is
// still running in it — the killed session's own harness — which is both a data
// hazard and the precondition for the failure the next test pins.
func TestKillReclaimsChildWorktreeOnlyAfterRuntimeTeardown(t *testing.T) {
	m, st, rt, ws := newLifecycleManager()
	var sharedLog []string
	rt.sharedLog = &sharedLog
	ws.sharedLog = &sharedLog
	canonical := seedUncoveredChildKill(st)

	if _, err := m.Kill(ctx, "mer-orch-1"); err != nil {
		t.Fatalf("Kill err = %v", err)
	}

	runtimeIdx, childIdx := -1, -1
	for i, call := range sharedLog {
		if call == "RuntimeDestroy:old-handle" && runtimeIdx < 0 {
			runtimeIdx = i
		}
		if strings.HasPrefix(call, "Destroy:") && childIdx < 0 {
			childIdx = i
		}
	}
	if childIdx < 0 {
		t.Fatalf("no child worktree destroy in %v; the uncovered child at %s must still be reclaimed", sharedLog, canonical+"/web")
	}
	if runtimeIdx < 0 {
		t.Fatalf("no runtime destroy in %v", sharedLog)
	}
	if runtimeIdx > childIdx {
		t.Fatalf("runtime destroy (pos %d) ran after the child worktree destroy (pos %d) in %v; the killed session's agent was still running in the tree being removed",
			runtimeIdx, childIdx, sharedLog)
	}
}

// A failed runtime destroy aborts Kill with the session still LIVE. Nothing may
// have been reclaimed by then: the child worktree belongs to a session that is
// still running, and Kill's caller is told the kill did not happen.
func TestKillReclaimsNoChildWorktreesWhenRuntimeDestroyFails(t *testing.T) {
	m, st, rt, ws := newLifecycleManager()
	seedUncoveredChildKill(st)
	rt.destroyErr = errors.New("tmux: server not responding")

	if _, err := m.Kill(ctx, "mer-orch-1"); err == nil {
		t.Fatal("Kill err = nil, want the runtime destroy failure surfaced")
	}
	if ws.destroyed != 0 {
		t.Fatalf("Destroy ran %d times (%v); the session is still live because its runtime survived, so its worktrees must be untouched",
			ws.destroyed, ws.calls)
	}
	if st.sessions["mer-orch-1"].IsTerminated {
		t.Fatal("the row was terminated despite the runtime destroy failing")
	}
	if rows := st.worktrees["mer-orch-1"]; len(rows) != 3 {
		t.Fatalf("worktree rows = %#v, want all 3 preserved for the retry", rows)
	}
}

// Coverage is what separates "held by nobody" from "held by a live session". If
// one live session's worktree rows cannot be loaded, the coverage map
// UNDER-reports occupancy, and an under-reported map read as permission to
// destroy deletes a worktree a live session is working in — on nothing worse
// than a transient DB error. Reclaim must fail closed.
//
// Failing closed must not cost the directory instead. session_worktrees is the
// ONLY record of a child worktree's location, so clearing the rows after
// reclaiming nothing turns a transient error into a permanent, unnameable
// orphan — worse than the leak this whole change exists to fix. Nothing was
// destroyed, so the rows still describe the complete workspace and are kept for
// a later Cleanup to retry.
func TestKillKeepsRetryableInventoryWhenLiveCoverageIsIndeterminate(t *testing.T) {
	m, st, _, ws := newLifecycleManager()
	canonical := seedUncoveredChildKill(st)
	// The live owner's rows are unreadable. It may well hold .../web — nothing
	// here can tell, and that is exactly the point.
	st.listWTErr = map[domain.SessionID]error{"mer-orch-2": errors.New("database is locked")}

	if _, err := m.Kill(ctx, "mer-orch-1"); err != nil {
		t.Fatalf("Kill err = %v; indeterminate coverage must not fail the kill", err)
	}
	if ws.destroyed != 0 {
		t.Fatalf("Destroy ran %d times (%v); %s looks uncovered only because the live owner's rows failed to load",
			ws.destroyed, ws.calls, canonical+"/web")
	}
	if !st.sessions["mer-orch-1"].IsTerminated {
		t.Fatal("the killed row must still be marked terminated")
	}
	// The inventory survives: without it nothing in the system can name
	// .../web again.
	rows := st.worktrees["mer-orch-1"]
	if len(rows) != 3 {
		t.Fatalf("worktree rows = %#v, want all 3 kept; a reclaim that destroyed nothing must not destroy the only record of the directories", rows)
	}
	found := false
	for _, row := range rows {
		if row.WorktreePath == canonical+"/web" {
			found = true
		}
	}
	if !found {
		t.Fatalf("kept rows %#v do not name the unreclaimed child %s", rows, canonical+"/web")
	}

	// Kept rows are inventory, never a restore marker: RestoreAll must not
	// resurrect the killed session into the live owner's worktree (#2319).
	ws.calls = nil
	if err := m.RestoreAll(ctx); err != nil {
		t.Fatalf("RestoreAll err = %v", err)
	}
	for _, call := range ws.calls {
		if strings.HasPrefix(call, "Restore:") {
			t.Fatalf("RestoreAll ran %q; kept inventory must not be restorable (calls: %v)", call, ws.calls)
		}
	}
	if !st.sessions["mer-orch-1"].IsTerminated {
		t.Fatal("the killed session must stay terminated across RestoreAll")
	}

	// Retryable, which is the whole point of keeping them: once the transient
	// error clears and nobody live holds the root, Cleanup reclaims the child.
	st.listWTErr = nil
	if err := m.RetireForReplacement(ctx, "mer-orch-2"); err != nil {
		t.Fatalf("RetireForReplacement err = %v", err)
	}
	ws.calls = nil
	if _, err := m.Cleanup(ctx, "mer"); err != nil {
		t.Fatalf("Cleanup err = %v", err)
	}
	reclaimed := false
	for _, call := range ws.calls {
		if call == "Destroy:web" {
			reclaimed = true
		}
	}
	if !reclaimed {
		t.Fatalf("cleanup calls = %v; the kept rows must let a later cleanup reclaim %s", ws.calls, canonical+"/web")
	}
}

// The retention above is conditioned on the rows being non-restorable. A
// shutdown-saved session carries "removed" rows, the kind RestoreAll actually
// replays, and keeping those would relaunch the killed session inside the live
// replacement's worktree (#2319). Retryability is worth a directory an operator
// can still see; it is not worth that.
func TestKillClearsRestorableMarkersEvenWhenCoverageIsIndeterminate(t *testing.T) {
	m, st, _, ws := newLifecycleManager()
	canonical := seedUncoveredChildKill(st)
	for i := range st.worktrees["mer-orch-1"] {
		st.worktrees["mer-orch-1"][i].State = "removed"
	}
	st.sessions["mer-orch-2"] = domain.SessionRecord{
		ID: "mer-orch-2", ProjectID: "mer", Kind: domain.KindOrchestrator,
		Metadata: domain.SessionMetadata{WorkspacePath: canonical, Branch: "ao/mer-orchestrator", RuntimeHandleID: "new-handle"},
		Activity: domain.Activity{State: domain.ActivityActive},
	}
	st.listWTErr = map[domain.SessionID]error{"mer-orch-2": errors.New("database is locked")}

	if _, err := m.Kill(ctx, "mer-orch-1"); err != nil {
		t.Fatalf("Kill err = %v", err)
	}
	if ws.destroyed != 0 {
		t.Fatalf("Destroy ran %d times (%v); coverage was indeterminate", ws.destroyed, ws.calls)
	}
	if rows := st.worktrees["mer-orch-1"]; len(rows) != 0 {
		t.Fatalf("restorable rows survived: %#v; RestoreAll would relaunch the killed session in the live replacement's worktree", rows)
	}
}

// The coverage read must be FRESH. Kill resolves ownership, then destroys the
// runtime, and only then reclaims — and a role reconcile can retire the owner
// and spawn a replacement while that teardown runs. Coverage computed from the
// pre-teardown snapshot would not contain the replacement, so the child it is
// working in would read as held by nobody and be destroyed underneath it.
func TestKillResolvesChildCoverageFromLiveSessionsReadAfterTeardown(t *testing.T) {
	m, st, _, ws := newLifecycleManager()
	canonical := seedUncoveredChildKill(st)

	// Between Kill's ownership read (1) and its coverage read (2), the captured
	// owner is retired and a replacement takes over the canonical root — and,
	// unlike its predecessor, it holds .../web.
	st.beforeListAll = func(call int) {
		if call != 2 {
			return
		}
		retired := st.sessions["mer-orch-2"]
		retired.IsTerminated = true
		st.sessions["mer-orch-2"] = retired
		st.sessions["mer-orch-3"] = domain.SessionRecord{
			ID: "mer-orch-3", ProjectID: "mer", Kind: domain.KindOrchestrator,
			Metadata: domain.SessionMetadata{WorkspacePath: canonical, Branch: "ao/mer-orchestrator", RuntimeHandleID: "newer-handle"},
			Activity: domain.Activity{State: domain.ActivityActive},
		}
		st.worktrees["mer-orch-3"] = []domain.SessionWorktreeRecord{
			{SessionID: "mer-orch-3", RepoName: domain.RootWorkspaceRepoName, Branch: "ao/mer-orchestrator", WorktreePath: canonical, State: "active"},
			{SessionID: "mer-orch-3", RepoName: "api", Branch: "ao/mer-orchestrator", WorktreePath: canonical + "/api", State: "active"},
			{SessionID: "mer-orch-3", RepoName: "web", Branch: "ao/mer-orchestrator", WorktreePath: canonical + "/web", State: "active"},
		}
	}

	if _, err := m.Kill(ctx, "mer-orch-1"); err != nil {
		t.Fatalf("Kill err = %v", err)
	}
	if ws.destroyed != 0 {
		t.Fatalf("Destroy ran %d times (%v); %s is held by the replacement that appeared during teardown",
			ws.destroyed, ws.calls, canonical+"/web")
	}
	if !st.sessions["mer-orch-1"].IsTerminated {
		t.Fatal("the killed row must still be marked terminated")
	}
	// Coverage was complete, so this is the ordinary path: markers cleared.
	if rows := st.worktrees["mer-orch-1"]; len(rows) != 0 {
		t.Fatalf("worktree rows survived: %#v, want none", rows)
	}
}

// The uncovered child's repo can be DEREGISTERED, and then its canonical repo
// path is unrecoverable. Kill must not guess: ports.Workspace.Destroy resolves
// the repo from RepoPath and silently falls back to the PROJECT repo when it is
// empty, which makes `git worktree remove` fail against the wrong repo, skips
// the dirty check entirely, and os.RemoveAll's the directory anyway — verified
// against the real gitworktree adapter, which returned nil while deleting
// staged work. Leaving the directory for an operator is the only safe move.
func TestKillLeavesUncoveredChildOfDeregisteredRepoAlone(t *testing.T) {
	m, st, _, ws := newLifecycleManager()

	const canonical = "/ws/mer/orchestrator/mer-orchestrator"
	st.projects["mer"] = domain.ProjectRecord{
		ID: "mer", Path: "/repos/mer", Kind: domain.ProjectKindWorkspace, Config: testRoleAgents(),
	}
	// "web" was deregistered after mer-orch-1 spawned; only "api" remains.
	st.workspaceRepo["mer"] = []domain.WorkspaceRepoRecord{
		{ProjectID: "mer", Name: "api", RelativePath: "api"},
	}

	st.sessions["mer-orch-1"] = domain.SessionRecord{
		ID: "mer-orch-1", ProjectID: "mer", Kind: domain.KindOrchestrator,
		Metadata: domain.SessionMetadata{WorkspacePath: canonical, Branch: "ao/mer-orchestrator", RuntimeHandleID: "old-handle"},
		Activity: domain.Activity{State: domain.ActivityActive},
	}
	st.worktrees["mer-orch-1"] = []domain.SessionWorktreeRecord{
		{SessionID: "mer-orch-1", RepoName: domain.RootWorkspaceRepoName, Branch: "ao/mer-orchestrator", WorktreePath: canonical, State: "active"},
		{SessionID: "mer-orch-1", RepoName: "web", Branch: "ao/mer-orchestrator", WorktreePath: canonical + "/web", State: "active"},
	}
	st.sessions["mer-orch-2"] = domain.SessionRecord{
		ID: "mer-orch-2", ProjectID: "mer", Kind: domain.KindOrchestrator,
		Metadata: domain.SessionMetadata{WorkspacePath: canonical, Branch: "ao/mer-orchestrator", RuntimeHandleID: "new-handle"},
		Activity: domain.Activity{State: domain.ActivityActive},
	}

	if _, err := m.Kill(ctx, "mer-orch-1"); err != nil {
		t.Fatalf("Kill err = %v", err)
	}
	if ws.destroyed != 0 {
		t.Fatalf("Destroy ran %d times (%v); a worktree whose repo is deregistered cannot be removed safely and must be left for an operator",
			ws.destroyed, ws.calls)
	}
	if !st.sessions["mer-orch-1"].IsTerminated {
		t.Fatal("the killed row must still be marked terminated")
	}
}

// The half of #144 P3 that must NOT regress: when the live owner covers every
// one of the killed session's worktrees, all markers are still cleared, so
// RestoreAll cannot resurrect the killed session into the live replacement's
// worktree (#2319). These are shutdown-saved rows (State "removed"), the kind
// RestoreAll actually replays, so the guard is exercised end to end.
func TestKillClearsChildWorktreeMarkersCoveredByLiveOwner(t *testing.T) {
	m, st, _, ws := newLifecycleManager()

	const canonical = "/ws/mer/orchestrator/mer-orchestrator"
	st.projects["mer"] = domain.ProjectRecord{
		ID: "mer", Path: "/repos/mer", Kind: domain.ProjectKindWorkspace, Config: testRoleAgents(),
	}
	st.workspaceRepo["mer"] = []domain.WorkspaceRepoRecord{
		{ProjectID: "mer", Name: "api", RelativePath: "api"},
	}

	// Shutdown-saved orchestrator: terminated, restore markers written.
	st.sessions["mer-orch-1"] = domain.SessionRecord{
		ID: "mer-orch-1", ProjectID: "mer", Kind: domain.KindOrchestrator,
		Metadata:     domain.SessionMetadata{WorkspacePath: canonical, Branch: "ao/mer-orchestrator", RuntimeHandleID: "old-handle"},
		IsTerminated: true, Activity: domain.Activity{State: domain.ActivityExited},
	}
	st.worktrees["mer-orch-1"] = []domain.SessionWorktreeRecord{
		{SessionID: "mer-orch-1", RepoName: domain.RootWorkspaceRepoName, Branch: "ao/mer-orchestrator", WorktreePath: canonical, PreservedRef: "refs/ao/preserved/root", State: "removed"},
		{SessionID: "mer-orch-1", RepoName: "api", Branch: "ao/mer-orchestrator", WorktreePath: canonical + "/api", PreservedRef: "refs/ao/preserved/api", State: "removed"},
	}

	// A replacement is already live on the canonical root and covers every one
	// of those paths.
	st.sessions["mer-orch-2"] = domain.SessionRecord{
		ID: "mer-orch-2", ProjectID: "mer", Kind: domain.KindOrchestrator,
		Metadata: domain.SessionMetadata{WorkspacePath: canonical, Branch: "ao/mer-orchestrator", RuntimeHandleID: "new-handle"},
		Activity: domain.Activity{State: domain.ActivityActive},
	}
	st.worktrees["mer-orch-2"] = []domain.SessionWorktreeRecord{
		{SessionID: "mer-orch-2", RepoName: domain.RootWorkspaceRepoName, Branch: "ao/mer-orchestrator", WorktreePath: canonical, State: "active"},
		{SessionID: "mer-orch-2", RepoName: "api", Branch: "ao/mer-orchestrator", WorktreePath: canonical + "/api", State: "active"},
	}

	if _, err := m.Kill(ctx, "mer-orch-1"); err != nil {
		t.Fatalf("Kill err = %v", err)
	}
	if rows := st.worktrees["mer-orch-1"]; len(rows) != 0 {
		t.Fatalf("restore markers survived for worktrees the live owner covers: %#v; RestoreAll would resurrect the killed session into the live replacement's worktree", rows)
	}

	if err := m.RestoreAll(ctx); err != nil {
		t.Fatalf("RestoreAll err = %v", err)
	}
	for _, call := range ws.calls {
		if strings.HasPrefix(call, "Restore:") {
			t.Fatalf("RestoreAll ran %q; a killed session must not be restored into the live replacement's worktree (calls: %v)", call, ws.calls)
		}
	}
	if !st.sessions["mer-orch-1"].IsTerminated {
		t.Fatal("the killed session must stay terminated across RestoreAll")
	}
}

// The repo-path lookup is the other way coverage work can bail out, and it has
// the same shape as an indeterminate coverage read: nothing was destroyed, so
// clearing the markers as well would turn a transient failure into a permanent
// orphan rather than something a later Cleanup can retry.
func TestKillKeepsRetryableInventoryWhenRepoPathsCannotBeResolved(t *testing.T) {
	m, st, _, ws := newLifecycleManager()
	canonical := seedUncoveredChildKill(st)
	// The live owner's rows load fine, so .../web really is uncovered — but the
	// project read that would name its canonical repo path fails.
	st.getProjectErr = errors.New("database is locked")

	if _, err := m.Kill(ctx, "mer-orch-1"); err != nil {
		t.Fatalf("Kill err = %v; an unresolvable repo path must not fail the kill", err)
	}
	if ws.destroyed != 0 {
		t.Fatalf("Destroy ran %d times (%v); no child can be reclaimed when its repo path is unknown",
			ws.destroyed, ws.calls)
	}
	if !st.sessions["mer-orch-1"].IsTerminated {
		t.Fatal("the killed row must still be marked terminated")
	}
	rows := st.worktrees["mer-orch-1"]
	if len(rows) != 3 {
		t.Fatalf("worktree rows = %#v, want all 3 kept; a reclaim that destroyed nothing must not destroy the only record of %s",
			rows, canonical+"/web")
	}

	// Kept rows stay inventory, never a restore marker.
	ws.calls = nil
	if err := m.RestoreAll(ctx); err != nil {
		t.Fatalf("RestoreAll err = %v", err)
	}
	for _, call := range ws.calls {
		if strings.HasPrefix(call, "Restore:") {
			t.Fatalf("RestoreAll ran %q; kept inventory must not be restorable (calls: %v)", call, ws.calls)
		}
	}
}
