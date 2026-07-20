-- name: UpsertQuotaSnapshot :one
INSERT INTO quota_snapshots (
    id, harness, account_id, model, window_start, window_end,
    used, remaining, limit_value, signal_quality, source, basis, observed_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(harness, account_id, model, window_start, window_end) DO UPDATE SET
    used = excluded.used,
    remaining = excluded.remaining,
    limit_value = excluded.limit_value,
    signal_quality = excluded.signal_quality,
    source = excluded.source,
    basis = excluded.basis,
    observed_at = excluded.observed_at
RETURNING *;

-- name: ListLatestQuotaSnapshots :many
SELECT q.*
FROM quota_snapshots q
WHERE q.id = (
    SELECT latest.id
    FROM quota_snapshots latest
    WHERE latest.harness = q.harness
      AND latest.account_id = q.account_id
      AND latest.model = q.model
    ORDER BY latest.observed_at DESC, latest.window_end DESC, latest.window_start DESC, latest.id DESC
    LIMIT 1
)
ORDER BY q.harness ASC, q.account_id ASC, q.model ASC;
