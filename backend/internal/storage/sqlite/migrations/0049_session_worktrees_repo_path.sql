-- +goose Up
-- +goose StatementBegin
ALTER TABLE session_worktrees ADD COLUMN repo_path TEXT NOT NULL DEFAULT '';
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE session_worktrees DROP COLUMN repo_path;
-- +goose StatementEnd
