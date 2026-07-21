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
    '''model_recovered''',
    '''model_recovered'', ''prime_restart_capped'''
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
    '''model_recovered'', ''prime_restart_capped''',
    '''model_recovered'''
)
WHERE type = 'table' AND name = 'notifications';
-- +goose StatementEnd
-- +goose StatementBegin
PRAGMA writable_schema = RESET;
-- +goose StatementEnd
