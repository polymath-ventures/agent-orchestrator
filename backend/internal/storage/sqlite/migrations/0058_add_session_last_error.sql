-- +goose Up
-- The latest supervised process launch failure is a durable session fact. It is kept
-- separate from activity_state so terminal state transitions do not erase the
-- diagnostic before an operator can inspect it.
ALTER TABLE sessions ADD COLUMN last_error TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE sessions DROP COLUMN last_error;
