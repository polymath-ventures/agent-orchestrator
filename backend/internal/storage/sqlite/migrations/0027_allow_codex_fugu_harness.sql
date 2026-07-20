-- Admit the fork-only codex-fugu harness in the sessions.harness CHECK.
--
-- This is a SEPARATE migration on purpose, and NOT an edit to 0007. goose tracks
-- applied migrations by version number, so any database already past 0007 (every
-- existing install — there are two dozen later migrations) would never re-run an
-- edited 0007, and codex-fugu would be admitted on fresh installs but silently
-- rejected on upgrades. 0007's own header note ("add new harnesses here by
-- extending this list") holds only while 0007 is the newest migration; once later
-- migrations exist, widening the allowlist requires a new migration like this one.
--
-- SQLite cannot ALTER a CHECK, so we surgically rewrite the stored CREATE TABLE
-- text in sqlite_master, matching the exact constraint 0007 leaves in place.
-- writable_schema edits run outside a transaction, and RESET forces an immediate
-- schema reparse on the connection. The migrate_test.go guards assert the live
-- constraint really widened (a byte mismatch in the source string no-ops silently).

-- +goose NO TRANSACTION
-- +goose Up
-- +goose StatementBegin
PRAGMA writable_schema = ON;
-- +goose StatementEnd
-- +goose StatementBegin
UPDATE sqlite_master
SET sql = replace(
    sql,
    'CHECK (harness IN ('''', ''claude-code'', ''codex'', ''aider'', ''opencode'', ''grok'', ''droid'', ''amp'', ''agy'', ''crush'', ''cursor'', ''qwen'', ''copilot'', ''goose'', ''auggie'', ''continue'', ''devin'', ''cline'', ''kimi'', ''kiro'', ''kilocode'', ''vibe'', ''pi'', ''autohand''))',
    'CHECK (harness IN ('''', ''claude-code'', ''codex'', ''codex-fugu'', ''aider'', ''opencode'', ''grok'', ''droid'', ''amp'', ''agy'', ''crush'', ''cursor'', ''qwen'', ''copilot'', ''goose'', ''auggie'', ''continue'', ''devin'', ''cline'', ''kimi'', ''kiro'', ''kilocode'', ''vibe'', ''pi'', ''autohand''))'
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
    'CHECK (harness IN ('''', ''claude-code'', ''codex'', ''codex-fugu'', ''aider'', ''opencode'', ''grok'', ''droid'', ''amp'', ''agy'', ''crush'', ''cursor'', ''qwen'', ''copilot'', ''goose'', ''auggie'', ''continue'', ''devin'', ''cline'', ''kimi'', ''kiro'', ''kilocode'', ''vibe'', ''pi'', ''autohand''))',
    'CHECK (harness IN ('''', ''claude-code'', ''codex'', ''aider'', ''opencode'', ''grok'', ''droid'', ''amp'', ''agy'', ''crush'', ''cursor'', ''qwen'', ''copilot'', ''goose'', ''auggie'', ''continue'', ''devin'', ''cline'', ''kimi'', ''kiro'', ''kilocode'', ''vibe'', ''pi'', ''autohand''))'
)
WHERE type = 'table' AND name = 'sessions';
-- +goose StatementEnd
-- +goose StatementBegin
PRAGMA writable_schema = RESET;
-- +goose StatementEnd
