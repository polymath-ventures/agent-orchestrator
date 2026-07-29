-- +goose Up
-- runtime_token and launch_command were added by 0032_session_runtime_probe for
-- the fork's RuntimeToken generation-fence and LaunchCommand liveness sweep.
-- The upstream sync (GH #220) replaced both mechanisms with upstream's
-- runtime_launch_id (0051) + AgentExitDetector, so these columns are now dead:
-- nothing reads them. Drop them so the schema matches the code. SQLite ALTER
-- ... DROP COLUMN is supported here (see 0049's Down).
-- +goose StatementBegin
ALTER TABLE sessions DROP COLUMN runtime_token;
-- +goose StatementEnd
-- +goose StatementBegin
ALTER TABLE sessions DROP COLUMN launch_command;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE sessions ADD COLUMN runtime_token TEXT NOT NULL DEFAULT '';
-- +goose StatementEnd
-- +goose StatementBegin
ALTER TABLE sessions ADD COLUMN launch_command TEXT NOT NULL DEFAULT '';
-- +goose StatementEnd
