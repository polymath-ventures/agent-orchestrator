-- +goose Up
-- model is the model the session was launched with. It is durable because the
-- worker-mix census groups live sessions on (harness, model): re-deriving each
-- session's model from project config at census time is wrong whenever that
-- config changed after the session launched, which is exactly when the census
-- matters. Empty means no model was resolved. Defaulting to '' keeps existing
-- rows valid without backfill.
-- +goose StatementBegin
ALTER TABLE sessions ADD COLUMN model TEXT NOT NULL DEFAULT '';
-- +goose StatementEnd

-- The sessions_cdc_update trigger is deliberately left alone: model is set once
-- at spawn and never updated, so it can never be the sole reason a row changes.

-- +goose Down
-- +goose StatementBegin
ALTER TABLE sessions DROP COLUMN model;
-- +goose StatementEnd
