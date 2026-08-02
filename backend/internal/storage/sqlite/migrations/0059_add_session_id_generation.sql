-- +goose Up
-- A session's external namespaces (workspace path, tmux session, VCS branch,
-- and — transitively — the Claude project directory) are derived from its AO
-- session ID, which is MAX(num)+1 per project. That counter recycles when the
-- database is rebuilt, so a fresh session can inherit a previous session's
-- on-host state (GH #244, #249).
--
-- session_id_generation is a per-database-generation token minted once here.
-- Composed into every newly-minted session ID as `{project}-{num}-{generation}`,
-- it makes identity unique over the host's lifetime: a rebuilt database mints a
-- new token, so a restarted counter cannot reproduce a surviving generation's
-- IDs. randomblob is non-deterministic, so it is set with an UPDATE rather than
-- as an ADD COLUMN default (SQLite requires constant column defaults).
ALTER TABLE daemon_settings ADD COLUMN session_id_generation TEXT NOT NULL DEFAULT '';

-- +goose StatementBegin
UPDATE daemon_settings SET session_id_generation = lower(hex(randomblob(16))) WHERE id = 1;
-- +goose StatementEnd

-- +goose Down
ALTER TABLE daemon_settings DROP COLUMN session_id_generation;
