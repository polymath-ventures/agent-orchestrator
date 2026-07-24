-- Widen the sessions.harness CHECK to also allow the 'fake' harness (the
-- deterministic, LLM-free e2e test harness). This forward-ports upstream's
-- migration after the fork's already-applied codex-fugu widening.

-- +goose NO TRANSACTION
-- +goose Up
-- +goose StatementBegin
PRAGMA writable_schema = ON;
-- +goose StatementEnd
-- +goose StatementBegin
UPDATE sqlite_master
SET sql = replace(
    sql,
    'CHECK (harness IN ('''', ''claude-code'', ''codex'', ''codex-fugu'', ''aider'', ''opencode'', ''grok'', ''droid'', ''amp'', ''agy'', ''crush'', ''cursor'', ''qwen'', ''copilot'', ''goose'', ''auggie'', ''continue'', ''devin'', ''cline'', ''kimi'', ''kiro'', ''kilocode'', ''vibe'', ''pi'', ''autohand''))',
    'CHECK (harness IN ('''', ''claude-code'', ''codex'', ''codex-fugu'', ''aider'', ''opencode'', ''grok'', ''droid'', ''amp'', ''agy'', ''crush'', ''cursor'', ''qwen'', ''copilot'', ''goose'', ''auggie'', ''continue'', ''devin'', ''cline'', ''kimi'', ''kiro'', ''kilocode'', ''vibe'', ''pi'', ''autohand'', ''fake''))'
)
WHERE type = 'table' AND name = 'sessions';
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
    'CHECK (harness IN ('''', ''claude-code'', ''codex'', ''codex-fugu'', ''aider'', ''opencode'', ''grok'', ''droid'', ''amp'', ''agy'', ''crush'', ''cursor'', ''qwen'', ''copilot'', ''goose'', ''auggie'', ''continue'', ''devin'', ''cline'', ''kimi'', ''kiro'', ''kilocode'', ''vibe'', ''pi'', ''autohand'', ''fake''))',
    'CHECK (harness IN ('''', ''claude-code'', ''codex'', ''codex-fugu'', ''aider'', ''opencode'', ''grok'', ''droid'', ''amp'', ''agy'', ''crush'', ''cursor'', ''qwen'', ''copilot'', ''goose'', ''auggie'', ''continue'', ''devin'', ''cline'', ''kimi'', ''kiro'', ''kilocode'', ''vibe'', ''pi'', ''autohand''))'
)
WHERE type = 'table' AND name = 'sessions';
-- +goose StatementEnd
-- +goose StatementBegin
PRAGMA writable_schema = RESET;
-- +goose StatementEnd
