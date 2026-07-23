-- +goose Up
-- +goose StatementBegin
ALTER TABLE daemon_settings ADD COLUMN prime_settings TEXT NOT NULL DEFAULT '{}';
-- +goose StatementEnd

-- Prime is the only projectless session kind. SQLite cannot drop NOT NULL or
-- alter a column FK in place, so mirror the established writable_schema pattern
-- used by earlier CHECK-widening migrations.
-- +goose StatementBegin
PRAGMA writable_schema = ON;
-- +goose StatementEnd
-- +goose StatementBegin
UPDATE sqlite_master
SET sql = replace(
    sql,
    'project_id              TEXT NOT NULL REFERENCES projects (id)',
    'project_id              TEXT REFERENCES projects (id)'
)
WHERE type = 'table' AND name = 'sessions';
-- +goose StatementEnd
-- +goose StatementBegin
UPDATE sqlite_master
SET sql = replace(
    sql,
    'UNIQUE (project_id, num)' || char(10) || ')',
    'UNIQUE (project_id, num),' || char(10) || '    CHECK (kind = ''prime'' OR (project_id IS NOT NULL AND project_id <> ''''))' || char(10) || ')'
)
WHERE type = 'table' AND name = 'sessions';
-- +goose StatementEnd
-- +goose StatementBegin
UPDATE sqlite_master
SET sql = replace(
    sql,
    'project_id TEXT NOT NULL REFERENCES projects (id)',
    'project_id TEXT REFERENCES projects (id)'
)
WHERE type = 'table' AND name = 'change_log';
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
    'UNIQUE (project_id, num),' || char(10) || '    CHECK (kind = ''prime'' OR (project_id IS NOT NULL AND project_id <> ''''))' || char(10) || ')',
    'UNIQUE (project_id, num)' || char(10) || ')'
)
WHERE type = 'table' AND name = 'sessions';
-- +goose StatementEnd
-- +goose StatementBegin
UPDATE sqlite_master
SET sql = replace(
    sql,
    'project_id              TEXT REFERENCES projects (id)',
    'project_id              TEXT NOT NULL REFERENCES projects (id)'
)
WHERE type = 'table' AND name = 'sessions';
-- +goose StatementEnd
-- +goose StatementBegin
UPDATE sqlite_master
SET sql = replace(
    sql,
    'project_id TEXT REFERENCES projects (id)',
    'project_id TEXT NOT NULL REFERENCES projects (id)'
)
WHERE type = 'table' AND name = 'change_log';
-- +goose StatementEnd
-- +goose StatementBegin
PRAGMA writable_schema = RESET;
-- +goose StatementEnd
