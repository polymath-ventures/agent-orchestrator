-- name: UpsertModelHealth :one
INSERT INTO model_health (
    project_id, harness, model, status, reason, message, observed_at, updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(project_id, harness, model) DO UPDATE SET
    status = excluded.status,
    reason = excluded.reason,
    message = excluded.message,
    observed_at = excluded.observed_at,
    updated_at = excluded.updated_at
RETURNING *;

-- name: ListModelHealthByProject :many
SELECT *
FROM model_health
WHERE project_id = ?
ORDER BY harness ASC, model ASC;
