-- name: UpsertSessionInitialContext :exec
INSERT INTO session_initial_contexts (session_id, snapshot_json, created_at)
VALUES (?, ?, ?)
ON CONFLICT(session_id) DO UPDATE SET
    snapshot_json = excluded.snapshot_json,
    created_at = excluded.created_at;

-- name: GetSessionInitialContext :one
SELECT snapshot_json FROM session_initial_contexts WHERE session_id = ?;
