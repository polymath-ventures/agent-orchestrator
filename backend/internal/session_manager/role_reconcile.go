package sessionmanager

import (
	"context"
	"errors"
	"fmt"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
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

type releaseWorkspaceTarget struct {
	info     ports.WorkspaceInfo
	repoName string
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
	rows, ok, rowErr := m.workspaceProjectRows(ctx, rec)
	if rowErr != nil {
		return fmt.Errorf("workspace rows: %w", rowErr)
	}
	targets := []releaseWorkspaceTarget{{info: ws}}
	if ok {
		targets = make([]releaseWorkspaceTarget, 0, len(rows))
		for i := len(rows) - 1; i >= 0; i-- {
			targets = append(targets, releaseWorkspaceTarget{
				info:     workspaceInfoFromRepoInfo(rows[i]),
				repoName: rows[i].RepoName,
			})
		}
	}
	for _, target := range targets {
		if err := m.stashBeforeRelease(ctx, rec, target.info, target.repoName); err != nil {
			return err
		}
	}

	cleanupCtx, cancelCleanup := cleanupContext(ctx)
	defer cancelCleanup()
	for _, target := range targets {
		if err := m.workspace.ForceDestroy(cleanupCtx, target.info); err != nil {
			if target.repoName != "" {
				return fmt.Errorf("force destroy %s: %w", target.repoName, err)
			}
			return fmt.Errorf("force destroy: %w", err)
		}
	}
	m.cleanupAgentWorkspace(cleanupCtx, rec, rec.Metadata.WorkspacePath)
	if err := m.store.DeleteSessionWorktrees(cleanupCtx, rec.ID); err != nil {
		return fmt.Errorf("clear restore markers: %w", err)
	}
	return nil
}

// stashBeforeRelease captures uncommitted work before a force-release, and
// refuses the release when capture fails for any reason other than the
// worktree being stale.
//
// A stale worktree is exactly the state release exists to clear, so it cannot
// be stashed and must not block. Every OTHER stash failure — a transient git
// error, a full disk, a permissions problem — means work may still be there,
// and force-destroying past it would delete it permanently. That is the same
// contract RetireForReplacement follows; diverging from it here would make
// "work is stashed before release" a comment rather than a guarantee.
func (m *Manager) stashBeforeRelease(ctx context.Context, rec domain.SessionRecord, info ports.WorkspaceInfo, repoName string) error {
	if _, err := m.workspace.StashUncommitted(ctx, info); err != nil {
		if !errors.Is(err, ports.ErrWorkspaceStale) {
			if repoName != "" {
				return fmt.Errorf("stash %s before release: %w", repoName, err)
			}
			return fmt.Errorf("stash before release: %w", err)
		}
		m.logger.Warn("release stale role resources: stale workspace; skipping preserve",
			"sessionID", rec.ID, "repo", repoName, "path", info.Path, "error", err)
	}
	return nil
}
