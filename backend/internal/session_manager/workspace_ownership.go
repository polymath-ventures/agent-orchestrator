package sessionmanager

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

// skipReasonWorkspaceInUse is the public reason for an ownership skip. It is
// deliberately distinct from "workspace teardown failed": nothing failed, and
// reporting it as a failure sends operators hunting for a broken worktree.
const skipReasonWorkspaceInUse = "workspace is in use by an active session"

// workspaceOwnedByLiveSession reports whether the workspace recorded on rec is
// currently held by some *other*, live session — in which case rec is a
// historical row that no longer owns that workspace and must not tear it down.
//
// Two arms, for two different ways a stale row can point at live state:
//
//   - Path. Role workspaces are canonical, so successive Orchestrator rows for
//     one project share one worktree path. A terminated row therefore records
//     the exact path the live replacement is running in. This is the arm that
//     prevents deleting a live worktree.
//   - Canonical role branch. Role branches are canonical too, so a terminated
//     role row can name the live role's branch even when its recorded path has
//     gone stale. Scoped to role sessions on both sides: worker branches are
//     not canonical, and two workers re-using one feature branch in separate
//     worktrees must still be able to clean up independently.
func workspaceOwnedByLiveSession(rec domain.SessionRecord, live []domain.SessionRecord) (domain.SessionID, bool) {
	path := normalizeWorkspacePath(rec.Metadata.WorkspacePath)
	branch := rec.Metadata.Branch
	if path == "" && branch == "" {
		return "", false
	}
	recTarget, recIsRole := domain.RoleTargetForSession(rec)
	for _, other := range live {
		if other.ID == rec.ID || other.IsTerminated {
			continue
		}
		if path != "" && normalizeWorkspacePath(other.Metadata.WorkspacePath) == path {
			return other.ID, true
		}
		if branch == "" || !recIsRole || other.Metadata.Branch != branch {
			continue
		}
		// Scope the branch arm to the SAME role target. Branch names are derived
		// from a project's session prefix, so two projects that share a prefix
		// (explicitly configured, or colliding on the first 12 chars of their
		// ids) generate the same orchestrator branch. Without this check they
		// would preserve each other's stale worktrees, leaving the canonical
		// branch occupied — the very outage this change exists to end.
		if otherTarget, ok := domain.RoleTargetForSession(other); ok && otherTarget == recTarget {
			return other.ID, true
		}
	}
	return "", false
}

// destroyUncoveredChildWorktrees reclaims the child worktrees of rec that no
// live session occupies, at the one moment their location is still known.
//
// workspaceOwnedByLiveSession answers a per-SESSION question from the root path
// and the branch, the only two workspace fields a session row carries. A
// workspace project's *children* live nowhere but session_worktrees, so
// "somebody live holds the root" says nothing about a child the live owner does
// not have. Kill clears every marker (it must: a surviving marker lets
// RestoreAll resurrect the killed session into the live owner's worktree,
// #2319), so a child not reclaimed here can never be found again — no HTTP,
// CLI, or read-model surface exposes session_worktrees.
//
// Destroying it here rather than recording it for later cleanup is what removes
// the orphan instead of tracking it: the row is only inventory because the
// directory outlived it, and an uncovered child is by definition held by
// nobody, so there is nothing to wait for.
//
// Every step is best effort and logged: this runs before the runtime is
// destroyed and the row terminated, so no failure here may abort Kill and leave
// the old agent process alive.
func (m *Manager) destroyUncoveredChildWorktrees(ctx context.Context, rec domain.SessionRecord, live []domain.SessionRecord) {
	rows, err := m.store.ListSessionWorktrees(ctx, rec.ID)
	if err != nil {
		m.logger.Warn("kill: listing worktree rows failed; uncovered children not reclaimed",
			"sessionID", rec.ID, "error", err)
		return
	}
	if len(rows) == 0 {
		return
	}
	var occupied map[string]domain.SessionID
	var repoPaths map[string]string
	for _, row := range rows {
		// The root is the shared, still-owned worktree: never a candidate. This
		// is a guard, not an optimization — the live owner holds it by
		// definition of the branch this runs in.
		if row.RepoName == domain.RootWorkspaceRepoName || row.RepoName == "" {
			continue
		}
		path := normalizeWorkspacePath(row.WorktreePath)
		if path == "" {
			continue
		}
		if occupied == nil {
			occupied = m.liveWorktreePaths(ctx, live, rec.ID)
		}
		if owner, covered := occupied[path]; covered {
			m.logger.Debug("kill: child worktree occupied by a live session; preserving",
				"sessionID", rec.ID, "repo", row.RepoName, "path", path, "owner", owner)
			continue
		}
		if repoPaths == nil {
			repoPaths, err = m.workspaceRepoPaths(ctx, rec.ProjectID)
			if err != nil {
				m.logger.Warn("kill: resolving workspace repos failed; uncovered children not reclaimed",
					"sessionID", rec.ID, "error", err)
				return
			}
		}
		repoPath := repoPaths[row.RepoName]
		if repoPath == "" {
			// The repo was deregistered after this session spawned, so its
			// canonical path is unrecoverable. Destroy resolves the repo from
			// RepoPath and silently falls back to the PROJECT repo when it is
			// empty, which makes `git worktree remove` fail against the wrong
			// repo, skips the dirty check, and os.RemoveAll's the directory
			// anyway — deleting uncommitted work and leaving a dangling
			// registration in the real repo. Refusing is the only safe move;
			// the path is logged so an operator can remove it by hand.
			m.logger.Warn("kill: child worktree left behind: its repo is no longer registered, so it cannot be removed safely",
				"sessionID", rec.ID, "repo", row.RepoName, "path", row.WorktreePath, "project", rec.ProjectID)
			continue
		}
		info := ports.WorkspaceInfo{
			Path:      row.WorktreePath,
			Branch:    firstNonEmptyString(row.Branch, rec.Metadata.Branch),
			SessionID: rec.ID,
			ProjectID: rec.ProjectID,
			RepoPath:  repoPath,
		}
		if err := m.workspace.Destroy(ctx, info); err != nil {
			if errors.Is(err, ports.ErrWorkspaceDirty) {
				// Uncommitted work: the same refusal every interactive teardown
				// path honors. Preserved, not forced.
				m.logger.Warn("kill: child worktree has uncommitted changes; preserved",
					"sessionID", rec.ID, "repo", row.RepoName, "path", row.WorktreePath)
				continue
			}
			m.logger.Warn("kill: reclaiming uncovered child worktree failed",
				"sessionID", rec.ID, "repo", row.RepoName, "path", row.WorktreePath, "error", err)
			continue
		}
		m.logger.Info("kill: reclaimed child worktree no live session occupies",
			"sessionID", rec.ID, "repo", row.RepoName, "path", row.WorktreePath)
	}
}

// workspaceRepoPaths maps a workspace project's registered child repo names to
// their canonical repo paths. Unlike sessionWorktreeRowsToRepoInfos it never
// fails on a row it cannot place: a caller that must tolerate deregistered
// repos looks the name up and decides for itself what a miss means.
func (m *Manager) workspaceRepoPaths(ctx context.Context, projectID domain.ProjectID) (map[string]string, error) {
	project, err := m.loadProject(ctx, projectID)
	if err != nil {
		return nil, err
	}
	if project.Kind.WithDefault() != domain.ProjectKindWorkspace {
		return map[string]string{}, nil
	}
	repos, err := m.store.ListWorkspaceRepos(ctx, string(projectID))
	if err != nil {
		return nil, err
	}
	out := make(map[string]string, len(repos))
	for _, repo := range repos {
		out[repo.Name] = filepath.Join(project.Path, filepath.FromSlash(repo.RelativePath))
	}
	return out, nil
}

// liveWorktreePaths collects every worktree path live sessions occupy, keyed by
// cleaned path: each live session's recorded root plus every path in its own
// session_worktrees rows. The session being torn down is excluded — it is still
// live in the store at that point, and a row cannot cover itself.
//
// A live session whose rows fail to load contributes only its root path.
// Under-reporting coverage is the safe direction here: it leaves a directory
// alone, whereas over-reporting would destroy a worktree somebody is using.
func (m *Manager) liveWorktreePaths(ctx context.Context, live []domain.SessionRecord, exclude domain.SessionID) map[string]domain.SessionID {
	occupied := make(map[string]domain.SessionID)
	for _, other := range live {
		if other.ID == exclude || other.IsTerminated {
			continue
		}
		if path := normalizeWorkspacePath(other.Metadata.WorkspacePath); path != "" {
			occupied[path] = other.ID
		}
		rows, err := m.store.ListSessionWorktrees(ctx, other.ID)
		if err != nil {
			m.logger.Warn("worktree coverage: listing a live session's worktrees failed; counting its root only",
				"sessionID", other.ID, "error", err)
			continue
		}
		for _, row := range rows {
			if path := normalizeWorkspacePath(row.WorktreePath); path != "" {
				occupied[path] = other.ID
			}
		}
	}
	return occupied
}

// normalizeWorkspacePath canonicalizes a recorded workspace path for comparison.
// Rows can record the same worktree with different spellings (a trailing
// separator, an uncleaned "..") and a raw string compare would miss the match —
// which, in this predicate, means concluding "not owned" and destroying a live
// worktree. Cleaning is deliberately lexical: this runs against DB rows for
// sessions whose directories may already be gone, so it must not touch the
// filesystem.
func normalizeWorkspacePath(path string) string {
	if path == "" {
		return ""
	}
	return filepath.Clean(path)
}

// liveSessions returns every non-terminated session, the set the ownership
// predicate resolves against. Every teardown path consults this one view, so
// "who owns this workspace" is answered in exactly one place.
func (m *Manager) liveSessions(ctx context.Context) ([]domain.SessionRecord, error) {
	recs, err := m.store.ListAllSessions(ctx)
	if err != nil {
		return nil, fmt.Errorf("list sessions for workspace ownership: %w", err)
	}
	out := make([]domain.SessionRecord, 0, len(recs))
	for _, rec := range recs {
		if !rec.IsTerminated {
			out = append(out, rec)
		}
	}
	return out, nil
}
