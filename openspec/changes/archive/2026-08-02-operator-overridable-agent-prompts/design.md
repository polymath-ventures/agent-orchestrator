## Context

Issue-driven worker tasks currently have two owners. Tracker intake formats a complete issue message in `trackerintake`, while manual issue spawns fall through to `session_manager.buildTaskPrompt`; explicit prompts are also mutated there by an appended issue-context section. Project role rules affect only the separately delivered system prompt. The daemon has typed per-project configuration but no persisted fleet-settings model for ordinary project defaults.

## Goals / Non-Goals

**Goals:**

- Let an operator replace issue-driven worker task messages once for the daemon or for one project.
- Give intake and manual issue spawns one configured-template renderer.
- Keep explicit and configured task messages authoritative and free from fetched issue content.
- Preserve every legacy default task message when no override applies.
- Keep the result inspectable without conflating task messages with system prompts.

**Non-Goals:**

- A general template language or placeholders beyond `{issue}`.
- Wholesale replacement of AO's system-prompt scaffolds. Existing role-rule fields remain the append-only operator surface.
- Reworking tracker prefetch for the legacy non-override path.
- A new fleet-settings persistence/API subsystem.

## Decisions

### Use one typed field at both precedence layers

Add `workerTaskPrompt` to `domain.ProjectConfig`. Add the daemon environment setting `AO_WORKER_TASK_PROMPT` and decode it into the same typed default field at startup. Resolution is project value, then daemon default, then no template. The daemon passes that typed default into session-manager and tracker-intake construction; newly registered projects therefore inherit it without persisting copied values.

This uses the existing `DefaultProjectConfig`/`WithDefaults` model instead of creating a free-form map or a second global settings store. A new persistent global API was rejected because this issue needs one daemon-wide setting and that subsystem would be larger than the prompt problem.

### Render configured issue prompts in one small function

Place literal `{issue}` substitution and empty-render validation in the session prompt package and call it from both intake and session-manager paths. For canonical tracker IDs such as `github:owner/repo#242`, the placeholder value is the native issue token `242`; a manual `--issue 242` therefore produces the same message. Unknown ID shapes remain unchanged rather than introducing provider-specific parsing.

Template strings are otherwise opaque. Missing `{issue}` is allowed because a fixed global dispatch command can still be intentional. Whitespace-only configured values fail the spawn as configuration errors; unset empty values select the next precedence layer.

### Preserve legacy builders instead of unifying their unrelated prose

When no configured template resolves, tracker intake continues calling its existing `BuildIssuePrompt` and session-manager continues its current issue-context/fallback builder. This keeps their byte output unchanged. Only the configured path is unified because only that path has one specified contract.

### Treat explicit and configured prompts as complete task messages

`buildTaskPrompt` returns any explicit prompt unchanged. A resolved configured template is rendered as the complete message and likewise bypasses issue-context appending. Issue prefetch may still supply session naming data; it no longer mutates an authoritative task message.

### Extend inspection without replacing system prompts

The worker role-prompt response reports the resolved task template and its source when an override exists, alongside the existing exact system prompt. `ao role prompt` conditionally labels the task-template section for configured workers and preserves its prior output when no task override exists. The actual role/system prompt assembly and append-only role rules do not change.

## Risks / Trade-offs

- **[Risk] Multiline environment values are awkward in some service managers.** → Document shell-safe configuration and keep the per-project JSON/CLI path available.
- **[Risk] Canonical issue IDs and manually supplied references have different shapes.** → Normalize only the well-defined trailing `#<token>` form and regression-test intake/manual parity.
- **[Risk] A typo in a configured template can strand intake.** → Fail before creating session state with a clear configuration error; never fall back silently.
- **[Risk] Changing explicit-prompt enrichment removes context some callers received implicitly.** → This is intentional for authoritative prompts and is covered by a regression test; legacy promptless issue spawns retain enrichment.

## Migration Plan

No data migration is needed. Existing project JSON omits `workerTaskPrompt`, and an unset daemon environment leaves behavior unchanged. Rollback ignores the unknown stored field only if an older strict API is not asked to rewrite it; operators should clear the project field before downgrading when using config apply.

## Open Questions

None.
