-- +goose Up
-- runtime_token scopes activity hooks to the runtime generation that launched
-- the agent; launch_command is the argv[0] the launch-process liveness probe
-- matches on. Both are durable because their consumers outlive the process
-- that learned them: stale-hook rejection must survive a daemon restart (an
-- unpersisted token reloads empty and the guard silently disarms), and the
-- reaper's liveness sweep runs against rehydrated sessions (an empty command
-- degrades the probe to "any child of the pane shell counts as alive").
-- Defaulting to '' keeps existing rows valid without backfill; both consumers
-- already treat empty as "no signal" and fail open.
-- +goose StatementBegin
ALTER TABLE sessions ADD COLUMN runtime_token TEXT NOT NULL DEFAULT '';
-- +goose StatementEnd
-- +goose StatementBegin
ALTER TABLE sessions ADD COLUMN launch_command TEXT NOT NULL DEFAULT '';
-- +goose StatementEnd

-- The sessions_cdc_update trigger is deliberately left alone: both columns are
-- runtime plumbing set at spawn/restore, never a user-visible change on their
-- own, so they can never be the sole reason a row needs CDC fanout.

-- +goose Down
-- +goose StatementBegin
ALTER TABLE sessions DROP COLUMN runtime_token;
-- +goose StatementEnd
-- +goose StatementBegin
ALTER TABLE sessions DROP COLUMN launch_command;
-- +goose StatementEnd
