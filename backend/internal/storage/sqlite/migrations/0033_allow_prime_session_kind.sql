-- Admit the daemon-only prime role in sessions.kind and enforce its active
-- singleton invariant in storage. Prime is spawned only by the env-gated
-- supervisor, but the database owns the global "one live prime" fact.

-- +goose NO TRANSACTION
-- +goose Up
-- +goose StatementBegin
PRAGMA writable_schema = ON;
-- +goose StatementEnd
-- +goose StatementBegin
UPDATE sqlite_master
SET sql = replace(
    sql,
    'CHECK (kind IN (''worker'', ''orchestrator''))',
    'CHECK (kind IN (''worker'', ''orchestrator'', ''prime''))'
)
WHERE type = 'table' AND name = 'sessions';
-- +goose StatementEnd
-- +goose StatementBegin
PRAGMA writable_schema = RESET;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE UNIQUE INDEX IF NOT EXISTS idx_sessions_active_prime_singleton ON sessions (kind)
WHERE kind = 'prime' AND is_terminated = 0;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS idx_sessions_active_prime_singleton;
-- +goose StatementEnd
-- +goose StatementBegin
PRAGMA writable_schema = ON;
-- +goose StatementEnd
-- +goose StatementBegin
UPDATE sqlite_master
SET sql = replace(
    sql,
    'CHECK (kind IN (''worker'', ''orchestrator'', ''prime''))',
    'CHECK (kind IN (''worker'', ''orchestrator''))'
)
WHERE type = 'table' AND name = 'sessions';
-- +goose StatementEnd
-- +goose StatementBegin
PRAGMA writable_schema = RESET;
-- +goose StatementEnd
