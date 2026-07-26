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
// Every step is best effort and logged: this runs after the runtime is
// destroyed but before the row is terminated and its markers cleared, so no
// failure here may abort Kill and leave the row un-terminated.
func (m *Manager) destroyUncoveredChildWorktrees(ctx context.Context, rec domain.SessionRecord) {
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
			// Read the live set HERE rather than reusing the one the ownership
			// check above resolved against: that snapshot predates the runtime
			// teardown, and a concurrent role reconcile can retire the captured
			// owner and spawn a replacement while Destroy runs. The replacement
			// would be missing from the stale snapshot, and its worktree would
			// read as uncovered. This is a deliberately bounded check-then-act —
			// the window is now the same order as every other check in this path
			// — rather than serializing Kill with role reconciliation under a
			// lock, which is a much larger change to a path that is not
			// lock-serialized with reconcile at all today.
			live, liveErr := m.liveSessions(ctx)
			if liveErr != nil {
				m.logger.Warn("kill: re-reading live sessions failed; no child worktrees reclaimed",
					"sessionID", rec.ID, "error", liveErr)
				return
			}
			paths, complete := m.liveWorktreePaths(ctx, live, rec.ID)
			if !complete {
				// Coverage is what separates "held by nobody" from "held by a
				// live session". Without it in full there is no uncovered child,
				// only unknown ones, and destroying on a guess deletes a
				// directory a live session is working in. Nothing is reclaimed
				// this pass; the rows are cleared regardless, so what survives is
				// a directory an operator can still see and remove — strictly
				// better than a worktree yanked out from under a running agent.
				//
				// Clearing rather than keeping the rows for a later retry is
				// deliberate: older session_worktrees rows may not carry their
				// own repo path, so consumers that need registry resolution can
				// still hard-error on a repo that was deregistered meanwhile.
				// A transient failure here costs a visible directory, not a
				// broken cleanup path.
				m.logger.Warn("kill: live worktree coverage is incomplete; no child worktrees reclaimed (a child may be held by a live session)",
					"sessionID", rec.ID, "project", rec.ProjectID)
				return
			}
			occupied = paths
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
				// Same shape as an indeterminate coverage read, and cleared for
				// the same reason recorded there.
				return
			}
		}
		repoPath := row.RepoPath
		if repoPath == "" {
			repoPath = repoPaths[row.RepoName]
		}
		if repoPath == "" {
			// Older rows may not carry the repo path, and the repo was
			// deregistered after this session spawned. Destroy resolves the
			// repo from RepoPath and silently falls back to the PROJECT repo
			// when it is empty, which makes `git worktree remove` fail against
			// the wrong repo, skips the dirty check, and os.RemoveAll's the
			// directory anyway — deleting uncommitted work and leaving a
			// dangling registration in the real repo. Refusing is the only safe
			// move; the path is logged so an operator can remove it by hand.
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
// The second return reports whether coverage is COMPLETE. It is false as soon
// as one live session's rows fail to load, and the caller must then destroy
// nothing. Coverage is consumed as "this path is held by nobody, so reclaim
// it", so a partial map under-reports occupancy and turns a transient DB error
// into deleting a worktree a live session is working in. Failing closed costs a
// directory left on disk, which an operator can still see and remove; failing
// open costs somebody's uncommitted work.
func (m *Manager) liveWorktreePaths(ctx context.Context, live []domain.SessionRecord, exclude domain.SessionID) (map[string]domain.SessionID, bool) {
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
			m.logger.Warn("worktree coverage: listing a live session's worktrees failed; coverage is indeterminate",
				"sessionID", other.ID, "error", err)
			return occupied, false
		}
		for _, row := range rows {
			if path := normalizeWorkspacePath(row.WorktreePath); path != "" {
				occupied[path] = other.ID
			}
		}
	}
	return occupied, true
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
