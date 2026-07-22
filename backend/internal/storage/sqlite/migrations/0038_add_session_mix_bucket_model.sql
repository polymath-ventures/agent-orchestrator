-- +goose Up
-- mix_bucket_model preserves the configured worker-mix bucket model selected for
-- a mix-selected session. The launched session model may differ when a worker
-- spawn supplies an explicit model without a harness, but the census still has
-- to debit the bucket the mix selected.
ALTER TABLE sessions ADD COLUMN mix_bucket_model TEXT NOT NULL DEFAULT '';

UPDATE sessions SET mix_bucket_model = model WHERE mix_selected = TRUE;

-- +goose Down
ALTER TABLE sessions DROP COLUMN mix_bucket_model;
