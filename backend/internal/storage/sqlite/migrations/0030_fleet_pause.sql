-- +goose Up
-- Pause is a bit, not config surgery. The per-project pause flag lives in its
-- own `projects.paused` column rather than inside the `projects.config` JSON
-- blob so that pausing/resuming a project never rewrites the operator-authored
-- config — a pause/resume cycle leaves the config column byte-identical.
-- UpsertProject (the config-save path) deliberately omits `paused`; only
-- SetProjectPaused writes it.
-- +goose StatementBegin
ALTER TABLE projects ADD COLUMN paused INTEGER NOT NULL DEFAULT 0;
-- +goose StatementEnd

-- daemon_settings is a single-row table (CHECK (id = 1)) holding daemon-global
-- flags. fleet_paused is independent of the project rows on purpose: enforcement
-- reads this flag directly, so a project registered *while the fleet is paused*
-- is gated from its first moment. Seeded unpaused so existing daemons keep
-- running with no backfill.
-- +goose StatementBegin
CREATE TABLE daemon_settings (
    id           INTEGER PRIMARY KEY CHECK (id = 1),
    fleet_paused INTEGER NOT NULL DEFAULT 0
);
-- +goose StatementEnd

-- +goose StatementBegin
INSERT INTO daemon_settings (id, fleet_paused) VALUES (1, 0);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE daemon_settings;
-- +goose StatementEnd

-- +goose StatementBegin
ALTER TABLE projects DROP COLUMN paused;
-- +goose StatementEnd
