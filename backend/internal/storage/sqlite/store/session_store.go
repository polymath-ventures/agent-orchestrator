package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite/gen"
)

// ---- sessions ----

// CreateSession assigns the non-recycling per-project identity
// ("{project}-{num}-{database-generation}") and inserts the record, returning
// it with ID populated. The generation read, next-num read, and insert
// run on the writer connection under writeMu, so two concurrent creates in the
// same project can't collide on num.
func (s *Store) CreateSession(ctx context.Context, rec domain.SessionRecord) (domain.SessionRecord, error) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()

	generation, err := s.sessionIDGeneration(ctx)
	if err != nil {
		return domain.SessionRecord{}, err
	}

	if rec.ProjectID == "" {
		if rec.Kind != domain.KindPrime {
			return domain.SessionRecord{}, fmt.Errorf("project id is required for %s sessions", rec.Kind)
		}
		return s.createProjectlessPrimeSession(ctx, rec, generation)
	}

	num, err := s.qw.NextSessionNum(ctx, rec.ProjectID)
	if err != nil {
		return domain.SessionRecord{}, fmt.Errorf("next session num for %s: %w", rec.ProjectID, err)
	}
	rec.ID = domain.SessionID(fmt.Sprintf("%s-%d-%s", rec.ProjectID, num, generation))
	if err := s.qw.InsertSession(ctx, recordToInsert(rec, num)); err != nil {
		return domain.SessionRecord{}, fmt.Errorf("insert session %s: %w", rec.ID, err)
	}
	return rec, nil
}

// sessionIDGenerationPattern is the exact shape migration 0059 mints for
// daemon_settings.session_id_generation: the lowercase-hex encoding of 8 random
// bytes (randomblob(8)). Reading it fails closed on anything else so a corrupt
// or absent token can never degrade a new identity back to the recyclable
// {project}-{num} form.
var sessionIDGenerationPattern = regexp.MustCompile(`^[0-9a-f]{16}$`)

func (s *Store) sessionIDGeneration(ctx context.Context) (string, error) {
	generation, err := s.qw.GetSessionIDGeneration(ctx)
	if err != nil {
		return "", fmt.Errorf("get session id generation: %w", err)
	}
	if !sessionIDGenerationPattern.MatchString(generation) {
		return "", fmt.Errorf("invalid session id generation %q: want the lowercase hex of randomblob(8)", generation)
	}
	return generation, nil
}

func (s *Store) createProjectlessPrimeSession(ctx context.Context, rec domain.SessionRecord, generation string) (domain.SessionRecord, error) {
	var num int64
	if err := s.writeDB.QueryRowContext(ctx, `SELECT COALESCE(MAX(num), 0) + 1 FROM sessions WHERE project_id IS NULL AND kind = 'prime'`).Scan(&num); err != nil {
		return domain.SessionRecord{}, fmt.Errorf("next projectless prime session num: %w", err)
	}
	rec.ID = domain.SessionID(fmt.Sprintf("prime-%d-%s", num, generation))
	params := recordToInsert(rec, num)
	if _, err := s.writeDB.ExecContext(ctx, insertProjectlessPrimeSessionSQL,
		params.ID,
		nil,
		params.Num,
		params.IssueID,
		params.Kind,
		params.Harness,
		params.DisplayName,
		params.ActivityState,
		params.ActivityLastAt,
		params.LastError,
		params.FirstSignalAt,
		params.IsTerminated,
		params.Branch,
		params.WorkspacePath,
		params.WorkspaceRepoPath,
		params.RuntimeHandleID,
		params.RuntimeLaunchID,
		params.AgentSessionID,
		params.Prompt,
		params.PreviewURL,
		params.PreviewRevision,
		params.Model,
		params.Effort,
		params.MixSelected,
		params.MixBucketModel,
		params.PromptPolicyHash,
		params.TerminateOnPRMerge,
		params.CleanupGeneration,
		params.CreatedAt,
		params.UpdatedAt,
	); err != nil {
		return domain.SessionRecord{}, fmt.Errorf("insert projectless prime session %s: %w", rec.ID, err)
	}
	return rec, nil
}

const insertProjectlessPrimeSessionSQL = `
INSERT INTO sessions (
    id, project_id, num, issue_id, kind, harness, display_name,
    activity_state, activity_last_at, last_error, first_signal_at, is_terminated,
    branch, workspace_path, workspace_repo_path, runtime_handle_id,
    runtime_launch_id, agent_session_id, prompt,
    preview_url, preview_revision, model, effort, mix_selected, mix_bucket_model,
    prompt_policy_hash, terminate_on_pr_merge, cleanup_generation, created_at, updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`

// UpdateSession writes the full mutable state of an existing session. The
// id/project/num/created_at are immutable and not touched here.
func (s *Store) UpdateSession(ctx context.Context, rec domain.SessionRecord) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	return s.qw.UpdateSession(ctx, recordToUpdate(rec))
}

// UpdateSessionFromActivitySignal applies a hook observation only while the
// session is still owned by the observed harness/generation.
func (s *Store) UpdateSessionFromActivitySignal(ctx context.Context, rec domain.SessionRecord) (bool, error) {
	activity := normalActivity(rec.Activity, rec.UpdatedAt)
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	rows, err := s.qw.UpdateSessionFromActivitySignal(ctx, gen.UpdateSessionFromActivitySignalParams{
		ActivityState:                activity.State,
		ActivityLastAt:               activity.LastActivityAt,
		FirstSignalAt:                timeToNullTime(rec.FirstSignalAt),
		AgentSessionID:               rec.Metadata.AgentSessionID,
		LatestUserPrompt:             rec.Metadata.LatestUserPrompt,
		LatestAssistantUpdate:        rec.Metadata.LatestAssistantUpdate,
		NativeTranscriptPath:         rec.Metadata.NativeTranscriptPath,
		UpdatedAt:                    rec.UpdatedAt,
		ID:                           rec.ID,
		ExpectedHarness:              rec.Harness,
		ExpectedSessionMode:          domain.NormalizeSessionMode(rec.Mode),
		ExpectedRuntimeLaunchID:      rec.Metadata.RuntimeLaunchID,
		ExpectedControllerGeneration: rec.Metadata.ControllerGeneration,
	})
	if err != nil {
		return false, fmt.Errorf("update session %s from activity signal: %w", rec.ID, err)
	}
	return rows > 0, nil
}

// RecordSessionLatestUserPrompt is the narrow post-delivery write owned by
// Send. It deliberately avoids rewriting stale owner/runtime facts.
func (s *Store) RecordSessionLatestUserPrompt(ctx context.Context, id domain.SessionID, prompt string, updatedAt time.Time) (bool, error) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	rows, err := s.qw.RecordSessionLatestUserPrompt(ctx, gen.RecordSessionLatestUserPromptParams{
		ID:               id,
		LatestUserPrompt: prompt,
		UpdatedAt:        updatedAt,
	})
	if err != nil {
		return false, fmt.Errorf("record latest user prompt for session %s: %w", id, err)
	}
	return rows > 0, nil
}

// SetSessionNamespaceKey initializes a worker's external namespace key exactly
// once. ok=false means the row is absent, is not a worker, or already has a key.
func (s *Store) SetSessionNamespaceKey(ctx context.Context, id domain.SessionID, key string) (bool, error) {
	if strings.TrimSpace(key) == "" {
		return false, errors.New("session namespace key is required")
	}
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	rows, err := s.qw.SetSessionNamespaceKey(ctx, gen.SetSessionNamespaceKeyParams{
		ID:           id,
		NamespaceKey: key,
	})
	if err != nil {
		return false, fmt.Errorf("set namespace key for session %s: %w", id, err)
	}
	return rows > 0, nil
}

// ClaimChatControllerGeneration makes generation the only Chat controller that
// may project provider events for this session. The narrow update avoids writing
// a stale full SessionRecord over lifecycle facts changed by another goroutine.
func (s *Store) ClaimChatControllerGeneration(
	ctx context.Context,
	id domain.SessionID,
	generation string,
	updatedAt time.Time,
) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	rows, err := s.qw.ClaimChatControllerGeneration(ctx, gen.ClaimChatControllerGenerationParams{
		ControllerGeneration: generation,
		UpdatedAt:            updatedAt,
		ID:                   id,
	})
	if err != nil {
		return fmt.Errorf("claim chat controller generation for %s: %w", id, err)
	}
	if rows == 0 {
		return fmt.Errorf("claim chat controller generation for %s: chat session not found", id)
	}
	return nil
}

// RenameSession updates only the user-facing display name for an existing
// session. It returns ok=false when the session id does not exist. The
// sessions_cdc_update trigger fans out a session_updated CDC event when the
// display name actually changes.
func (s *Store) RenameSession(ctx context.Context, id domain.SessionID, displayName string, updatedAt time.Time) (bool, error) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	rows, err := s.qw.RenameSession(ctx, gen.RenameSessionParams{
		ID:          id,
		DisplayName: displayName,
		UpdatedAt:   updatedAt,
	})
	if err != nil {
		return false, fmt.Errorf("rename session %s: %w", id, err)
	}
	return rows > 0, nil
}

// SetSessionPinned updates the pinned status of a session.
func (s *Store) SetSessionPinned(ctx context.Context, id domain.SessionID, isPinned bool, pinnedAt *time.Time, updatedAt time.Time) (bool, error) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	rows, err := s.qw.SetSessionPinned(ctx, gen.SetSessionPinnedParams{
		ID:        id,
		IsPinned:  isPinned,
		PinnedAt:  timePtrToNullTime(pinnedAt),
		UpdatedAt: updatedAt,
	})
	if err != nil {
		return false, fmt.Errorf("set session pinned %s: %w", id, err)
	}
	return rows > 0, nil
}

// SetSessionPreviewURL updates only the browser preview URL for an existing
// session. It returns ok=false when the session id does not exist. The
// sessions_cdc_update trigger fans out a session_updated CDC event when the
// preview URL actually changes.
func (s *Store) SetSessionPreviewURL(ctx context.Context, id domain.SessionID, previewURL string, updatedAt time.Time) (bool, error) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	rows, err := s.qw.SetSessionPreviewURL(ctx, gen.SetSessionPreviewURLParams{
		ID:         id,
		PreviewURL: previewURL,
		UpdatedAt:  updatedAt,
	})
	if err != nil {
		return false, fmt.Errorf("set preview url for session %s: %w", id, err)
	}
	return rows > 0, nil
}

// SetSessionTerminateOnPRMerge updates the user's merge-completion lifecycle
// policy. It returns ok=false when the session id does not exist.
func (s *Store) SetSessionTerminateOnPRMerge(ctx context.Context, id domain.SessionID, terminate bool, updatedAt time.Time) (bool, error) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	rows, err := s.qw.SetSessionTerminateOnPRMerge(ctx, gen.SetSessionTerminateOnPRMergeParams{
		ID:                 id,
		TerminateOnPRMerge: terminate,
		UpdatedAt:          updatedAt,
	})
	if err != nil {
		return false, fmt.Errorf("set terminate-on-pr-merge for session %s: %w", id, err)
	}
	return rows > 0, nil
}

// SetSessionAutoInjectReview persists a session's automatic review-injection policy.
func (s *Store) SetSessionAutoInjectReview(ctx context.Context, id domain.SessionID, autoInject bool, updatedAt time.Time) (bool, error) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	rows, err := s.qw.SetSessionAutoInjectReview(ctx, gen.SetSessionAutoInjectReviewParams{
		ID:               id,
		AutoInjectReview: autoInject,
		UpdatedAt:        updatedAt,
	})
	if err != nil {
		return false, fmt.Errorf("set auto-inject review for session %s: %w", id, err)
	}
	return rows > 0, nil
}

// SetSessionAutoInjectCI persists the default CI-failure injection policy that
// newly created PRs snapshot. Existing PR rows retain their original value.
func (s *Store) SetSessionAutoInjectCI(ctx context.Context, id domain.SessionID, autoInject bool, updatedAt time.Time) (bool, error) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	rows, err := s.qw.SetSessionAutoInjectCI(ctx, gen.SetSessionAutoInjectCIParams{
		ID:           id,
		AutoInjectCI: autoInject,
		UpdatedAt:    updatedAt,
	})
	if err != nil {
		return false, fmt.Errorf("set auto-inject CI for session %s: %w", id, err)
	}
	return rows > 0, nil
}

// SetSessionReviewerHarness persists the reviewer preference for one session.
func (s *Store) SetSessionReviewerHarness(ctx context.Context, id domain.SessionID, harness domain.ReviewerHarness, updatedAt time.Time) (bool, error) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	rows, err := s.qw.SetSessionReviewerHarness(ctx, gen.SetSessionReviewerHarnessParams{
		ReviewerHarness: harness,
		UpdatedAt:       updatedAt,
		ID:              id,
	})
	if err != nil {
		return false, fmt.Errorf("set reviewer harness for %s: %w", id, err)
	}
	return rows > 0, nil
}

// DeleteSession removes a session row, but only if it is still in seed state
// (no workspace, no runtime handle, no agent session id, no prompt, no handoff
// metadata, and not already terminated). Rows that have observable spawn output are immutable
// to preserve the no-resurrection guarantee — for those, callers fall back to
// MarkTerminated (lifecycle.Manager) instead.
//
// The deletion runs in a transaction. It first probes seed state with
// SessionIsSeed; only if that returns true does it clear the session's
// change_log rows (required because change_log FKs sessions(id) without
// ON DELETE CASCADE) and then delete the session row. For live or absent
// sessions the transaction commits with no rows touched — critically, the
// session_created / session_updated CDC events for live sessions are NOT
// destroyed when callers (e.g. RollbackSpawn's delete-then-kill fallback)
// invoke DeleteSession on a fully-spawned row.
//
// Returns deleted=true when a seed row was removed; deleted=false when the
// session id did not match a seed row (either it never existed, or it had
// already progressed past seed state). The latter case is benign — the caller
// should fall back to MarkTerminated.
func (s *Store) DeleteSession(ctx context.Context, id domain.SessionID) (bool, error) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	tx, err := s.writeDB.BeginTx(ctx, nil)
	if err != nil {
		return false, fmt.Errorf("begin delete seed session: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	q := s.qw.WithTx(tx)

	isSeed, err := q.SessionIsSeed(ctx, id)
	if err != nil {
		return false, fmt.Errorf("delete seed session: probe seed state for %s: %w", id, err)
	}
	if !isSeed {
		// Commit the empty tx so we don't leak a transaction. Critically, do
		// NOT touch change_log here — for a live session that contains real
		// session_created / session_updated CDC events.
		if err := tx.Commit(); err != nil {
			return false, fmt.Errorf("delete seed session: commit no-op: %w", err)
		}
		return false, nil
	}

	// Drop change_log rows for this session id first so the FK doesn't reject
	// the session DELETE. We do not touch project-level events (session_id IS
	// NULL) — those belong to the project, not this session. Both this DELETE
	// and the session DELETE below run via raw ExecContext to sidestep sqlc
	// 1.31's SQLite-parser bug, which strips trailing `?` placeholders and
	// string literals from DELETE statements (see queries/changelog.sql and
	// queries/sessions.sql for the documented workaround context).
	if _, err := tx.ExecContext(ctx, `DELETE FROM change_log WHERE session_id = ?`, id); err != nil {
		return false, fmt.Errorf("delete seed session: clear change log for %s: %w", id, err)
	}
	res, err := tx.ExecContext(ctx, `
DELETE FROM sessions
WHERE id = ?
  AND is_terminated = 0
  AND workspace_path = ''
  AND runtime_handle_id = ''
  AND agent_session_id = ''
  AND prompt = ''
  AND latest_user_prompt = ''
  AND latest_assistant_update = ''
  AND native_transcript_path = ''`, id)
	if err != nil {
		return false, fmt.Errorf("delete seed session %s: %w", id, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("delete seed session %s: rows affected: %w", id, err)
	}
	if err := tx.Commit(); err != nil {
		return false, fmt.Errorf("delete seed session: commit: %w", err)
	}
	return n > 0, nil
}

// GetSession returns the full record for a session, or ok=false if absent.
func (s *Store) GetSession(ctx context.Context, id domain.SessionID) (domain.SessionRecord, bool, error) {
	row, err := s.qr.GetSession(ctx, id)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.SessionRecord{}, false, nil
	}
	if err != nil {
		return domain.SessionRecord{}, false, fmt.Errorf("get session %s: %w", id, err)
	}
	return getSessionRowToRecord(row), true, nil
}

// ListSessions returns every session in a project, ordered by num.
func (s *Store) ListSessions(ctx context.Context, project domain.ProjectID) ([]domain.SessionRecord, error) {
	rows, err := s.qr.ListSessionsByProject(ctx, project)
	if err != nil {
		return nil, fmt.Errorf("list sessions for %s: %w", project, err)
	}
	return mapListSessionsByProjectRows(rows), nil
}

// ListAllSessions returns every session across all projects.
func (s *Store) ListAllSessions(ctx context.Context) ([]domain.SessionRecord, error) {
	rows, err := s.qr.ListAllSessions(ctx)
	if err != nil {
		return nil, fmt.Errorf("list all sessions: %w", err)
	}
	return mapListAllSessionsRows(rows), nil
}

func mapListSessionsByProjectRows(rows []gen.ListSessionsByProjectRow) []domain.SessionRecord {
	out := make([]domain.SessionRecord, 0, len(rows))
	for _, r := range rows {
		out = append(out, listSessionsByProjectRowToRecord(r))
	}
	return out
}

func mapListAllSessionsRows(rows []gen.ListAllSessionsRow) []domain.SessionRecord {
	out := make([]domain.SessionRecord, 0, len(rows))
	for _, r := range rows {
		out = append(out, listAllSessionsRowToRecord(r))
	}
	return out
}

func rowToRecord(row gen.GetSessionRow) domain.SessionRecord {
	return domain.SessionRecord{
		ID:              row.ID,
		ProjectID:       domain.ProjectID(strings.TrimSpace(string(row.ProjectID))),
		IssueID:         row.IssueID,
		Kind:            row.Kind,
		Harness:         row.Harness,
		Model:           row.Model,
		Effort:          domain.Effort(row.Effort),
		MixSelected:     row.MixSelected,
		MixBucketModel:  row.MixBucketModel,
		DisplayName:     row.DisplayName,
		NamespaceKey:    row.NamespaceKey,
		ReviewerHarness: row.ReviewerHarness,
		Mode:            domain.NormalizeSessionMode(row.SessionMode),
		Activity: domain.Activity{
			State:          row.ActivityState,
			LastActivityAt: row.ActivityLastAt,
		},
		LastError:          row.LastError,
		FirstSignalAt:      nullTimeToTime(row.FirstSignalAt),
		IsTerminated:       row.IsTerminated,
		IsPinned:           row.IsPinned,
		PinnedAt:           nullTimeToTimePtr(row.PinnedAt),
		TerminateOnPRMerge: row.TerminateOnPRMerge,
		AutoInjectReview:   row.AutoInjectReview,
		AutoInjectCI:       row.AutoInjectCI,
		Metadata: domain.SessionMetadata{
			Branch:                    row.Branch,
			WorkspacePath:             row.WorkspacePath,
			WorkspaceRepoPath:         row.WorkspaceRepoPath,
			DiffBaseSHA:               row.DiffBaseSha,
			DiffBaseRef:               row.DiffBaseRef,
			RuntimeHandleID:           row.RuntimeHandleID,
			RuntimeLaunchID:           row.RuntimeLaunchID,
			AgentSessionID:            row.AgentSessionID,
			Prompt:                    row.Prompt,
			LatestUserPrompt:          row.LatestUserPrompt,
			LatestAssistantUpdate:     row.LatestAssistantUpdate,
			NativeTranscriptPath:      row.NativeTranscriptPath,
			PromptPolicyHash:          row.PromptPolicyHash,
			PreviewURL:                row.PreviewURL,
			PreviewRevision:           row.PreviewRevision,
			BrowserCapabilityVerifier: row.BrowserCapabilityVerifier,

			ProviderConversationID: row.ProviderConversationID,
			ControllerGeneration:   row.ControllerGeneration,
		},
		CleanupGeneration: row.CleanupGeneration,
		CreatedAt:         row.CreatedAt,
		UpdatedAt:         row.UpdatedAt,
	}
}

func getSessionRowToRecord(row gen.GetSessionRow) domain.SessionRecord {
	return rowToRecord(row)
}

func listSessionsByProjectRowToRecord(row gen.ListSessionsByProjectRow) domain.SessionRecord {
	return rowToRecord(gen.GetSessionRow(row))
}

func listAllSessionsRowToRecord(row gen.ListAllSessionsRow) domain.SessionRecord {
	return rowToRecord(gen.GetSessionRow(row))
}

func recordToInsert(rec domain.SessionRecord, num int64) gen.InsertSessionParams {
	activity := normalActivity(rec.Activity, rec.CreatedAt)
	return gen.InsertSessionParams{
		ID:                        rec.ID,
		ProjectID:                 rec.ProjectID,
		Num:                       num,
		IssueID:                   rec.IssueID,
		Kind:                      rec.Kind,
		Harness:                   rec.Harness,
		Model:                     rec.Model,
		Effort:                    string(rec.Effort),
		MixSelected:               rec.MixSelected,
		MixBucketModel:            rec.MixBucketModel,
		ReviewerHarness:           rec.ReviewerHarness,
		DisplayName:               rec.DisplayName,
		ActivityState:             activity.State,
		ActivityLastAt:            activity.LastActivityAt,
		LastError:                 rec.LastError,
		FirstSignalAt:             timeToNullTime(rec.FirstSignalAt),
		IsTerminated:              rec.IsTerminated,
		IsPinned:                  rec.IsPinned,
		PinnedAt:                  timePtrToNullTime(rec.PinnedAt),
		Branch:                    rec.Metadata.Branch,
		WorkspacePath:             rec.Metadata.WorkspacePath,
		WorkspaceRepoPath:         rec.Metadata.WorkspaceRepoPath,
		DiffBaseSha:               rec.Metadata.DiffBaseSHA,
		DiffBaseRef:               rec.Metadata.DiffBaseRef,
		RuntimeHandleID:           rec.Metadata.RuntimeHandleID,
		RuntimeLaunchID:           rec.Metadata.RuntimeLaunchID,
		AgentSessionID:            rec.Metadata.AgentSessionID,
		Prompt:                    rec.Metadata.Prompt,
		LatestUserPrompt:          rec.Metadata.LatestUserPrompt,
		LatestAssistantUpdate:     rec.Metadata.LatestAssistantUpdate,
		NativeTranscriptPath:      rec.Metadata.NativeTranscriptPath,
		PromptPolicyHash:          rec.Metadata.PromptPolicyHash,
		PreviewURL:                rec.Metadata.PreviewURL,
		PreviewRevision:           rec.Metadata.PreviewRevision,
		TerminateOnPRMerge:        rec.TerminateOnPRMerge,
		CleanupGeneration:         rec.CleanupGeneration,
		BrowserCapabilityVerifier: rec.Metadata.BrowserCapabilityVerifier,
		AutoInjectReview:          rec.AutoInjectReview,
		AutoInjectCI:              rec.AutoInjectCI,

		SessionMode:            domain.NormalizeSessionMode(rec.Mode),
		ProviderConversationID: rec.Metadata.ProviderConversationID,
		ControllerGeneration:   rec.Metadata.ControllerGeneration,

		CreatedAt: rec.CreatedAt,
		UpdatedAt: rec.UpdatedAt,
	}
}

func recordToUpdate(rec domain.SessionRecord) gen.UpdateSessionParams {
	activity := normalActivity(rec.Activity, rec.UpdatedAt)
	return gen.UpdateSessionParams{
		ID:                        rec.ID,
		IssueID:                   rec.IssueID,
		Kind:                      rec.Kind,
		Harness:                   rec.Harness,
		Model:                     rec.Model,
		Effort:                    string(rec.Effort),
		MixSelected:               rec.MixSelected,
		MixBucketModel:            rec.MixBucketModel,
		ReviewerHarness:           rec.ReviewerHarness,
		DisplayName:               rec.DisplayName,
		ActivityState:             activity.State,
		ActivityLastAt:            activity.LastActivityAt,
		LastError:                 rec.LastError,
		FirstSignalAt:             timeToNullTime(rec.FirstSignalAt),
		IsTerminated:              rec.IsTerminated,
		IsPinned:                  rec.IsPinned,
		PinnedAt:                  timePtrToNullTime(rec.PinnedAt),
		Branch:                    rec.Metadata.Branch,
		WorkspacePath:             rec.Metadata.WorkspacePath,
		WorkspaceRepoPath:         rec.Metadata.WorkspaceRepoPath,
		DiffBaseSha:               rec.Metadata.DiffBaseSHA,
		DiffBaseRef:               rec.Metadata.DiffBaseRef,
		RuntimeHandleID:           rec.Metadata.RuntimeHandleID,
		RuntimeLaunchID:           rec.Metadata.RuntimeLaunchID,
		AgentSessionID:            rec.Metadata.AgentSessionID,
		Prompt:                    rec.Metadata.Prompt,
		LatestUserPrompt:          rec.Metadata.LatestUserPrompt,
		LatestAssistantUpdate:     rec.Metadata.LatestAssistantUpdate,
		NativeTranscriptPath:      rec.Metadata.NativeTranscriptPath,
		PromptPolicyHash:          rec.Metadata.PromptPolicyHash,
		PreviewURL:                rec.Metadata.PreviewURL,
		PreviewRevision:           rec.Metadata.PreviewRevision,
		TerminateOnPRMerge:        rec.TerminateOnPRMerge,
		CleanupGeneration:         rec.CleanupGeneration,
		BrowserCapabilityVerifier: rec.Metadata.BrowserCapabilityVerifier,
		AutoInjectReview:          rec.AutoInjectReview,
		AutoInjectCI:              rec.AutoInjectCI,

		ProviderConversationID: rec.Metadata.ProviderConversationID,
		ControllerGeneration:   rec.Metadata.ControllerGeneration,

		UpdatedAt: rec.UpdatedAt,
	}
}

// nullTimeToTime / timeToNullTime bridge the nullable first_signal_at column
// to the domain's zero-time convention (zero = no signal received yet).
func nullTimeToTime(t sql.NullTime) time.Time {
	if !t.Valid {
		return time.Time{}
	}
	return t.Time
}

func timeToNullTime(t time.Time) sql.NullTime {
	if t.IsZero() {
		return sql.NullTime{}
	}
	return sql.NullTime{Time: t, Valid: true}
}

func nullTimeToTimePtr(t sql.NullTime) *time.Time {
	if !t.Valid {
		return nil
	}
	return &t.Time
}

func timePtrToNullTime(t *time.Time) sql.NullTime {
	if t == nil {
		return sql.NullTime{}
	}
	return sql.NullTime{Time: *t, Valid: true}
}

func normalActivity(a domain.Activity, fallback time.Time) domain.Activity {
	if a.State == "" {
		a.State = domain.ActivityIdle
	}
	if a.LastActivityAt.IsZero() {
		a.LastActivityAt = fallback
	}
	if a.LastActivityAt.IsZero() {
		a.LastActivityAt = time.Now().UTC()
	}
	return a
}
