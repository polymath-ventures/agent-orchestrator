package sessionmanager

import (
	"testing"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
)

// A projectless Prime row whose WorkspaceRepoPath never round-tripped leaves
// teardown with neither a project id nor a repo path. gitworktree then fails
// with "project id is required" — a plain error, so cleanup reports the generic
// "workspace teardown failed", skips the row, and the stale worktree keeps
// holding the canonical ao/prime branch. Every replacement spawn then fails
// with "branch is already checked out in another worktree" until the restart
// budget is exhausted. That is the reported outage.
//
// The repo path is derivable from the role identity alone — singleRepoOverridePath
// already does it for the restore paths. Teardown must derive it the same way
// rather than trusting a row field that may be empty.
func TestWorkspaceInfoDerivesFleetPrimeRepoPath(t *testing.T) {
	const dataDir = "/data"

	prime := domain.SessionRecord{
		ID:   "prime-1",
		Kind: domain.KindPrime,
		// Projectless, and the repo path was never persisted.
		Metadata: domain.SessionMetadata{
			WorkspacePath: "/data/worktrees/prime/prime-1",
			Branch:        "ao/prime",
		},
	}

	info := workspaceInfoForTeardown(prime, dataDir)
	if info.RepoPath != fleetPrimeRepoPath(dataDir) {
		t.Fatalf("RepoPath = %q, want the derived fleet prime repo %q; without it teardown cannot resolve a repo and the stale worktree keeps holding %q",
			info.RepoPath, fleetPrimeRepoPath(dataDir), prime.Metadata.Branch)
	}
}

// A persisted repo path still wins: derivation is a fallback, not an override.
func TestWorkspaceInfoPrefersPersistedRepoPath(t *testing.T) {
	prime := domain.SessionRecord{
		ID:   "prime-1",
		Kind: domain.KindPrime,
		Metadata: domain.SessionMetadata{
			WorkspacePath:     "/data/worktrees/prime/prime-1",
			Branch:            "ao/prime",
			WorkspaceRepoPath: "/somewhere/else/repo",
		},
	}
	if got := workspaceInfoForTeardown(prime, "/data").RepoPath; got != "/somewhere/else/repo" {
		t.Fatalf("RepoPath = %q, want the persisted path to win", got)
	}
}

// A project-owned session is unaffected: it resolves its repo through its
// project id exactly as before.
func TestWorkspaceInfoLeavesProjectSessionsAlone(t *testing.T) {
	worker := domain.SessionRecord{
		ID: "mer-1", ProjectID: "mer", Kind: domain.KindWorker,
		Metadata: domain.SessionMetadata{WorkspacePath: "/ws/mer-1", Branch: "ao/mer-1"},
	}
	info := workspaceInfoForTeardown(worker, "/data")
	if info.RepoPath != "" {
		t.Fatalf("RepoPath = %q, want empty so the project id resolves the repo", info.RepoPath)
	}
	if info.ProjectID != "mer" {
		t.Fatalf("ProjectID = %q, want mer", info.ProjectID)
	}
}
