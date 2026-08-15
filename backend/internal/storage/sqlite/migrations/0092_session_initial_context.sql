-- +goose Up
CREATE TABLE IF NOT EXISTS session_initial_contexts (
    session_id TEXT PRIMARY KEY REFERENCES sessions(id) ON DELETE CASCADE,
    snapshot_json TEXT NOT NULL CHECK (json_valid(snapshot_json)),
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- +goose Down
DROP TABLE IF EXISTS session_initial_contexts;
