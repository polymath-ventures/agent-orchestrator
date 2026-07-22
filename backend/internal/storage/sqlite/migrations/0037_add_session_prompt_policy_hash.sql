-- +goose Up
-- prompt_policy_hash records the assembled system prompt/policy identity a
-- session received at spawn/restore time. Empty means the session predates the
-- field or no system prompt was assembled.
-- +goose StatementBegin
ALTER TABLE sessions ADD COLUMN prompt_policy_hash TEXT NOT NULL DEFAULT '';
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE sessions DROP COLUMN prompt_policy_hash;
-- +goose StatementEnd
