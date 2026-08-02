## Why

AO currently owns every issue-driven worker task message and always injects issue content. Operators cannot replace that dispatch contract globally, so self-contained workflows such as `/address-issue {issue}` receive duplicated or malformed context and can miss operator-required lifecycle gates.

## What Changes

- Add one typed worker task-prompt template setting with literal `{issue}` substitution.
- Resolve the template as per-project configuration first, then the daemon-wide operator default, then AO's unchanged built-in prompt behavior.
- Make tracker auto-intake and manual `ao spawn --issue` render configured templates through the same function.
- Treat configured templates and explicit prompts as complete task messages: do not append issue bodies or `## Issue Context`.
- Reject configured templates that are empty or render empty instead of silently falling back.
- Keep existing system-prompt role rules append-only; wholesale system-prompt replacement remains outside this change.
- Record the fork feature in the upstream-sync preservation checklist.

## Capabilities

### New Capabilities

- `agent-task-prompts`: Operator-configured global and per-project worker task-message replacement, issue rendering, precedence, inspection boundary, and legacy compatibility.

### Modified Capabilities

None.

## Impact

- Typed project configuration and daemon-wide environment configuration.
- Session task-prompt assembly and tracker-intake spawn wiring.
- Project config CLI/API/supervisor surfaces and generated OpenAPI/TypeScript schemas.
- Prompt regression tests, intake parity tests, operator documentation, and `docs/fork.md`.
