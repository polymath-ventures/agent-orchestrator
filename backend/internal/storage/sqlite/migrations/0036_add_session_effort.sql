-- +goose Up
-- effort is the normalized effort the session was launched with. It is durable
-- for the same reason as model: worker-mix census and restore must use the
-- original native (harness, model, effort) tuple even when project config later
-- changes. Empty means no effort was resolved. Defaulting to '' keeps existing
-- rows valid without a backfill.
-- +goose StatementBegin
ALTER TABLE sessions ADD COLUMN effort TEXT NOT NULL DEFAULT '';
-- +goose StatementEnd

-- The sessions_cdc_update trigger is deliberately left alone: effort is fixed
-- at spawn and never changes independently during normal session lifecycle.

-- +goose Down
-- +goose StatementBegin
ALTER TABLE sessions DROP COLUMN effort;
-- +goose StatementEnd
