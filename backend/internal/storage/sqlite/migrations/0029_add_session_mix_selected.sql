-- +goose Up
-- mix_selected records that the worker mix chose this session's
-- (harness, model), rather than a caller pinning it. It is durable because it
-- cannot be recovered later: a pinned spawn naming exactly a configured bucket
-- produces a row identical to a mix-selected one, so a census grouping on
-- (harness, model) alone counts pinned workers against the bucket's share and
-- starves it. The fact is only knowable at the moment the mix decides, so it is
-- written there. FALSE means not mix-selected, which is the correct reading of
-- every row that predates this column and so needs no backfill.
-- +goose StatementBegin
ALTER TABLE sessions ADD COLUMN mix_selected BOOLEAN NOT NULL DEFAULT FALSE;
-- +goose StatementEnd

-- The sessions_cdc_update trigger is deliberately left alone: mix_selected is
-- set once at spawn and never updated, so it can never be the sole reason a row
-- changes.

-- +goose Down
-- +goose StatementBegin
ALTER TABLE sessions DROP COLUMN mix_selected;
-- +goose StatementEnd
