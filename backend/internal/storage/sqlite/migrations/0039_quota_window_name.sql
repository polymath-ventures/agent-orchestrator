-- +goose Up
-- window_name distinguishes simultaneous provider quota windows (for example
-- Codex primary and secondary windows) without overloading the model field.
-- +goose StatementBegin
ALTER TABLE quota_snapshots ADD COLUMN window_name TEXT NOT NULL DEFAULT '';

DROP INDEX IF EXISTS idx_quota_snapshots_window;
DROP INDEX IF EXISTS idx_quota_snapshots_latest;

CREATE UNIQUE INDEX idx_quota_snapshots_window
    ON quota_snapshots(harness, account_id, model, window_name, window_start, window_end);

CREATE INDEX idx_quota_snapshots_latest
    ON quota_snapshots(harness, account_id, model, window_name, observed_at DESC);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS idx_quota_snapshots_window;
DROP INDEX IF EXISTS idx_quota_snapshots_latest;

CREATE UNIQUE INDEX idx_quota_snapshots_window
    ON quota_snapshots(harness, account_id, model, window_start, window_end);

CREATE INDEX idx_quota_snapshots_latest
    ON quota_snapshots(harness, account_id, model, observed_at DESC);

ALTER TABLE quota_snapshots DROP COLUMN window_name;
-- +goose StatementEnd
