-- +goose Up
-- +goose StatementBegin
CREATE TABLE quota_snapshots (
    id TEXT PRIMARY KEY,
    harness TEXT NOT NULL,
    account_id TEXT NOT NULL DEFAULT '',
    model TEXT NOT NULL DEFAULT '',
    window_start TIMESTAMP,
    window_end TIMESTAMP,
    used REAL,
    remaining REAL,
    limit_value REAL,
    signal_quality TEXT NOT NULL CHECK (signal_quality IN ('exact', 'estimated', 'none')),
    source TEXT NOT NULL,
    basis TEXT NOT NULL DEFAULT '',
    observed_at TIMESTAMP NOT NULL
);

CREATE UNIQUE INDEX idx_quota_snapshots_window
    ON quota_snapshots(harness, account_id, model, window_start, window_end);

CREATE INDEX idx_quota_snapshots_latest
    ON quota_snapshots(harness, account_id, model, observed_at DESC);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS idx_quota_snapshots_latest;
DROP INDEX IF EXISTS idx_quota_snapshots_window;
DROP TABLE IF EXISTS quota_snapshots;
-- +goose StatementEnd
