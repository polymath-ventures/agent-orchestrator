package gitworktree

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

// TestWorkspaceIntegrationCanonicalRoleBranchIsReusableAfterRelease reproduces
// the reported outage at the git layer and proves the release fixes it.
//
// A role session that was killed from its terminal leaves its worktree holding
// the canonical role branch. Until that worktree is released, every replacement
// spawn fails with ErrBranchCheckedOutElsewhere — which is exactly what the
// daemon log showed for ao/prime, once per attempt, until the restart budget
// was exhausted.
//
// Ignored runtime residue under .claude/ is present throughout, because that is
// what the live worktree looked like. Git tolerates ignored files (verified
// directly: `git worktree remove` succeeds with only ignored content), so it
// must not be what stops the release.
func TestWorkspaceIntegrationCanonicalRoleBranchIsReusableAfterRelease(t *testing.T) {
	git := requireGit(t)
	tmp := t.TempDir()
	repo := setupOriginClone(t, git, tmp)
	root := filepath.Join(tmp, "managed")
	ws, err := New(Options{Binary: git, ManagedRoot: root, RepoResolver: StaticRepoResolver{"proj": repo}})
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	ctx := context.Background()
	const canonicalBranch = "ao/proj-orchestrator"

	// The dead role session's worktree.
	stale, err := ws.Create(ctx, ports.WorkspaceConfig{
		ProjectID: "proj", SessionID: "orch-1", Branch: canonicalBranch,
	})
	if err != nil {
		t.Fatalf("create stale role worktree: %v", err)
	}

	// AO-managed runtime residue that version control ignores.
	claudeDir := filepath.Join(stale.Path, ".claude")
	if err := os.MkdirAll(claudeDir, 0o755); err != nil {
		t.Fatalf("mkdir .claude: %v", err)
	}
	if err := os.WriteFile(filepath.Join(claudeDir, "settings.local.json"), []byte("{}\n"), 0o600); err != nil {
		t.Fatalf("write residue: %v", err)
	}

	// RED: while the stale worktree holds the branch, a replacement cannot take it.
	if _, err := ws.Create(ctx, ports.WorkspaceConfig{
		ProjectID: "proj", SessionID: "orch-2", Branch: canonicalBranch,
	}); !errors.Is(err, ErrBranchCheckedOutElsewhere) {
		t.Fatalf("replacement create err = %v, want ErrBranchCheckedOutElsewhere", err)
	}

	// Release the stale resource, the way reconciliation does before spawning.
	if err := ws.ForceDestroy(ctx, ports.WorkspaceInfo{
		Path: stale.Path, Branch: canonicalBranch, SessionID: "orch-1", ProjectID: "proj", RepoPath: stale.RepoPath,
	}); err != nil {
		t.Fatalf("force destroy stale role worktree: %v", err)
	}
	if _, err := os.Stat(stale.Path); !os.IsNotExist(err) {
		t.Fatalf("stale worktree path still present: %v", err)
	}

	// GREEN: the replacement now takes the canonical branch.
	replacement, err := ws.Create(ctx, ports.WorkspaceConfig{
		ProjectID: "proj", SessionID: "orch-2", Branch: canonicalBranch,
	})
	if err != nil {
		t.Fatalf("replacement create after release: %v", err)
	}
	if replacement.Branch != canonicalBranch {
		t.Fatalf("replacement branch = %q, want %q", replacement.Branch, canonicalBranch)
	}
	if replacement.Path == stale.Path {
		// Not a failure per se for canonical layouts, but the path must exist.
		if _, err := os.Stat(replacement.Path); err != nil {
			t.Fatalf("replacement path missing: %v", err)
		}
	}
}

// Ordinary (non-force) teardown must succeed when the only content is
// version-control-ignored runtime residue. This pins the behavior that was
// mis-diagnosed in the ticket: had git actually refused here, cleanup really
// would have been blocked by .claude/ residue.
func TestWorkspaceIntegrationDestroySucceedsWithIgnoredResidue(t *testing.T) {
	git := requireGit(t)
	tmp := t.TempDir()
	repo := setupOriginClone(t, git, tmp)
	root := filepath.Join(tmp, "managed")
	ws, err := New(Options{Binary: git, ManagedRoot: root, RepoResolver: StaticRepoResolver{"proj": repo}})
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	ctx := context.Background()

	info, err := ws.Create(ctx, ports.WorkspaceConfig{ProjectID: "proj", SessionID: "sess-ig", Branch: "ao/ignored"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	// Commit a .gitignore covering .claude/ so the residue below is genuinely
	// ignored. Without this the test could pass for the wrong reason.
	if err := os.WriteFile(filepath.Join(info.Path, ".gitignore"), []byte(".claude/\n"), 0o600); err != nil {
		t.Fatalf("write gitignore: %v", err)
	}
	runGit(t, git, info.Path, "add", ".gitignore")
	runGit(t, git, info.Path, "commit", "-m", "ignore runtime residue")

	claudeDir := filepath.Join(info.Path, ".claude")
	if err := os.MkdirAll(claudeDir, 0o755); err != nil {
		t.Fatalf("mkdir .claude: %v", err)
	}
	if err := os.WriteFile(filepath.Join(claudeDir, "settings.local.json"), []byte("{}\n"), 0o600); err != nil {
		t.Fatalf("write residue: %v", err)
	}

	// Precondition: git must consider the worktree clean, and must see the
	// residue only under --ignored. If this ever changes, the assertion below
	// would be testing something else entirely.
	if out := gitOutput(t, git, info.Path, "status", "--porcelain"); out != "" {
		t.Fatalf("precondition failed: worktree is not clean to git: %q", out)
	}
	if out := gitOutput(t, git, info.Path, "status", "--porcelain", "--ignored"); out == "" {
		t.Fatalf("precondition failed: residue is not reported as ignored")
	}

	if err := ws.Destroy(ctx, ports.WorkspaceInfo{
		Path: info.Path, Branch: info.Branch, SessionID: "sess-ig", ProjectID: "proj", RepoPath: info.RepoPath,
	}); err != nil {
		t.Fatalf("Destroy with only ignored residue = %v, want success", err)
	}
	if _, err := os.Stat(info.Path); !os.IsNotExist(err) {
		t.Fatalf("worktree path still present: %v", err)
	}
}

func gitOutput(t *testing.T, git, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command(git, append([]string{"-C", dir}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v (%s)", args, err, out)
	}
	return strings.TrimSpace(string(out))
}
