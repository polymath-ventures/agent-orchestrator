-- Admit the daemon prime supervisor restart-cap alert.

-- +goose NO TRANSACTION
-- +goose Up
-- +goose StatementBegin
PRAGMA writable_schema = ON;
-- +goose StatementEnd
-- +goose StatementBegin
UPDATE sqlite_master
SET sql = replace(
    sql,
    '''low_quota''',
    '''low_quota'', ''model_unreachable'', ''model_recovered'', ''prime_restart_capped'''
)
WHERE type = 'table' AND name = 'notifications';
-- +goose StatementEnd
-- +goose StatementBegin
PRAGMA writable_schema = RESET;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
PRAGMA writable_schema = ON;
-- +goose StatementEnd
-- +goose StatementBegin
UPDATE sqlite_master
SET sql = replace(
    sql,
    '''low_quota'', ''model_unreachable'', ''model_recovered'', ''prime_restart_capped''',
    '''low_quota'''
)
WHERE type = 'table' AND name = 'notifications';
-- +goose StatementEnd
-- +goose StatementBegin
PRAGMA writable_schema = RESET;
-- +goose StatementEnd
