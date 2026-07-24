package sessionmanager

import (
	"context"
	"fmt"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
)

// ReleaseResult reports what releasing stale role resources reclaimed and what
// it deliberately left alone.
type ReleaseResult struct {
	// Released are terminated role sessions whose workspace was reclaimed.
	Released []domain.SessionID
	// Preserved are terminated role sessions whose workspace is still owned by
	// a live session and was therefore left in place.
	Preserved []domain.SessionID
}

// ReleaseStaleRoleResources prepares a role target for a replacement: it reaps
// runtimes leaked by terminated sessions of that role and releases the
// workspaces those sessions still hold, so the canonical role branch is free
// for the replacement to check out.
//
// This is the fix for the reported outage. Nothing previously retired a
// *terminated* role row — both role spawn paths only ever considered active
// rows — so a Prime killed from its terminal left its worktree holding
// ao/prime, and every replacement spawn failed with "branch is already checked
// out in another worktree" until the restart budget was exhausted.
//
// Two invariants hold throughout:
//
//   - Ownership. A terminated row whose workspace is still held by a live
//     session is preserved, never destroyed. Role paths and role branches are
//     canonical and therefore shared across successive role rows.
//   - Work preservation. Uncommitted work is stashed before the workspace is
//     force-released, the same contract RetireForReplacement follows.
//
// Best-effort per session: one session's failure is logged and does not abort
// the pass, because a single wedged row must not be able to keep the whole role
// unrecoverable — that is the failure mode this function exists to end.
func (m *Manager) ReleaseStaleRoleResources(ctx context.Context, target domain.RoleTarget) (ReleaseResult, error) {
	if err := target.Validate(); err != nil {
		return ReleaseResult{}, err
	}
	recs, err := m.store.ListAllSessions(ctx)
	if err != nil {
		return ReleaseResult{}, fmt.Errorf("release stale %s: list sessions: %w", target, err)
	}
	live := make([]domain.SessionRecord, 0, len(recs))
	for _, rec := range recs {
		if !rec.IsTerminated {
			live = append(live, rec)
		}
	}

	result := ReleaseResult{}
	for _, rec := range recs {
		if !rec.IsTerminated {
			continue
		}
		if recTarget, ok := domain.RoleTargetForSession(rec); !ok || recTarget != target {
			continue
		}
		// Reap first: a leaked runtime holding the workspace open would make
		// the release below fight a live process.
		if err := m.reconcileReap(ctx, rec); err != nil {
			m.logger.Warn("release stale role resources: reap failed", "sessionID", rec.ID, "target", target.String(), "error", err)
		}
		if rec.Metadata.WorkspacePath == "" {
			continue
		}
		if owner, inUse := workspaceOwnedByLiveSession(rec, live); inUse {
			m.logger.Info("release stale role resources: workspace owned by a live session; preserving",
				"sessionID", rec.ID, "target", target.String(), "owner", owner)
			result.Preserved = append(result.Preserved, rec.ID)
			continue
		}
		if err := m.releaseRoleWorkspace(ctx, rec); err != nil {
			m.logger.Warn("release stale role resources: workspace release failed",
				"sessionID", rec.ID, "target", target.String(), "path", rec.Metadata.WorkspacePath, "error", err)
			continue
		}
		result.Released = append(result.Released, rec.ID)
	}
	return result, nil
}

// releaseRoleWorkspace captures any uncommitted work, then force-releases the
// worktree so its branch is free. The row is already terminated, so there is no
// lifecycle transition to make here — only the workspace is reclaimed.
func (m *Manager) releaseRoleWorkspace(ctx context.Context, rec domain.SessionRecord) error {
	ws := workspaceInfoForTeardown(rec, m.dataDir)
	if rows, ok, rowErr := m.workspaceProjectRows(ctx, rec); rowErr != nil {
		return fmt.Errorf("workspace rows: %w", rowErr)
	} else if ok {
		for i := len(rows) - 1; i >= 0; i-- {
			info := workspaceInfoFromRepoInfo(rows[i])
			if _, err := m.workspace.StashUncommitted(ctx, info); err != nil {
				m.logger.Warn("release stale role resources: stash failed; releasing anyway",
					"sessionID", rec.ID, "repo", rows[i].RepoName, "error", err)
			}
			if err := m.workspace.ForceDestroy(ctx, info); err != nil {
				return fmt.Errorf("force destroy %s: %w", rows[i].RepoName, err)
			}
		}
	} else {
		if _, err := m.workspace.StashUncommitted(ctx, ws); err != nil {
			// A stale or unreadable worktree cannot be stashed, and that is
			// precisely the state we are here to clear. Log and continue.
			m.logger.Warn("release stale role resources: stash failed; releasing anyway",
				"sessionID", rec.ID, "path", ws.Path, "error", err)
		}
		if err := m.workspace.ForceDestroy(ctx, ws); err != nil {
			return fmt.Errorf("force destroy: %w", err)
		}
	}
	m.cleanupAgentWorkspace(ctx, rec, rec.Metadata.WorkspacePath)
	if err := m.store.DeleteSessionWorktrees(ctx, rec.ID); err != nil {
		return fmt.Errorf("clear restore markers: %w", err)
	}
	return nil
}
