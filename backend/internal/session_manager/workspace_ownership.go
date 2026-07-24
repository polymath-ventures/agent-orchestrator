package sessionmanager

import (
	"context"
	"fmt"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
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
	path := rec.Metadata.WorkspacePath
	branch := rec.Metadata.Branch
	if path == "" && branch == "" {
		return "", false
	}
	for _, other := range live {
		if other.ID == rec.ID || other.IsTerminated {
			continue
		}
		if path != "" && other.Metadata.WorkspacePath == path {
			return other.ID, true
		}
		if branch != "" && rec.Kind.IsRole() && other.Kind.IsRole() && other.Metadata.Branch == branch {
			return other.ID, true
		}
	}
	return "", false
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
