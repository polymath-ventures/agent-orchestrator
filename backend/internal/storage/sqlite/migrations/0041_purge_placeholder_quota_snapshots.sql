-- +goose Up
-- Purge the hardcoded "no signal" placeholder quota rows seeded by the
-- pre-probe implementation (GH #97). Quota is now written only by daemon probes
-- and the Stop-hook freshness path; no-signal placeholders are never written
-- again, and un-probed/failed harness states are carried inline by the widget
-- from the QuotaProber's in-memory status. These rows would otherwise render as
-- stale "unknown / no signal / none" chips forever.
-- +goose StatementBegin
DELETE FROM quota_snapshots WHERE signal_quality = 'none';
-- +goose StatementEnd

-- +goose Down
-- No-op: the deleted rows were fabricated placeholders with no real data, so
-- there is nothing meaningful to restore. Rolling back the schema does not
-- recreate them.
-- +goose StatementBegin
SELECT 1;
-- +goose StatementEnd
