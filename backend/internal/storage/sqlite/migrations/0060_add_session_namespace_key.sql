-- +goose Up
-- The exact external namespace key is persisted once for new workers. Existing
-- rows stay empty so their stored workspace, runtime handle, and branch remain
-- authoritative and no live resource is renamed by migration.
ALTER TABLE sessions ADD COLUMN namespace_key TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE sessions DROP COLUMN namespace_key;
