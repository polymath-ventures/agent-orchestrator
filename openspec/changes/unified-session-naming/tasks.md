## 1. Re-verify harness mechanics before writing code

- [ ] 1.1 Confirm the installed `claude` version still accepts `-n <name>` at launch and `/rename <name>` inline, and that both land in `~/.claude/sessions/<pid>.json` (`name`, with no `nameSource` field) — record the version in the PR
- [ ] 1.2 Confirm the installed `codex` version still accepts `/rename <name>` inline and persists `thread_name` to `~/.codex/session_index.jsonl` — record the version in the PR
- [ ] 1.3 If either mechanism has changed, stop and update the design before proceeding rather than working around it in the adapter

## 2. Naming grammar (pure functions, no I/O)

- [ ] 2.1 Failing tests for the worker name grammar: prefix + item number + title slug, and slug omitted when the title is unavailable
- [ ] 2.2 Failing tests for the orchestrator name grammar (prefix + role suffix) and for Prime taking its name from fleet Prime settings
- [ ] 2.3 Failing tests for the length cap: over-long names keep prefix, item number, and role suffix intact and clip only the trailing slug
- [ ] 2.4 Failing test that a worker with no resolvable work item still gets a non-empty name
- [ ] 2.5 Implement the name builders and slug/cap helpers until 2.1–2.4 pass

## 3. `AgentNamer` port capability

- [ ] 3.1 Failing test asserting the session manager dispatches on the naming capability, not on harness identity (a fake adapter declaring the capability is named; one that does not is left alone)
- [ ] 3.2 Define the optional capability interface: universal in-harness rename command plus optional launch-argument form
- [ ] 3.3 Failing test for the claude-code adapter returning the launch-argument form, and for name sanitization (control characters, newlines, collapsed whitespace)
- [ ] 3.4 Implement claude-code's launch-argument and in-harness rename forms
- [ ] 3.5 Failing test for the Codex adapter's in-harness rename form, asserting it covers `codex-fugu` through the same parameterized Plugin
- [ ] 3.6 Implement the Codex adapter's in-harness rename form
- [ ] 3.7 Regression test: no adapter places a rename in the harness's positional startup slot

## 4. Daemon ownership of the name

- [ ] 4.1 Failing test that an empty display name on a spawn request yields the computed name, and a supplied name is honored as an override
- [ ] 4.2 Failing test that a spawn prompt never becomes the display name
- [ ] 4.3 Remove the prompt-derived display-name default and make the CLI's `--name` optional while keeping its length validation
- [ ] 4.4 Resolve the work item title on every spawn path (tracker intake, CLI, HTTP API) in the shared service, bounded, degrading to the head-only name and logging the reason on any failure
- [ ] 4.5 Failing test that a tracker outage or unresolvable work item degrades the name instead of failing the spawn
- [ ] 4.6 Wire the computed name into spawn

## 5. One delivery path

- [ ] 5.1 Failing tests for a single name-delivery routine: prefers the launch-argument form at spawn, falls back to the in-harness command, no-ops for an adapter without the capability
- [ ] 5.2 Failing tests that it skips harness delivery for terminated sessions and for sessions with no live runtime
- [ ] 5.3 Implement the delivery routine and call it from spawn
- [ ] 5.4 Failing test that renaming a live session delivers the new name to the harness, then route the rename path through the delivery routine
- [ ] 5.5 Failing test that rebinding a live worker recomputes the name and delivers it, then route the rebind path through the same routine
- [ ] 5.6 Assert the delivered string is byte-identical to the persisted display name on every path

## 6. Spawn-safety invariants (each test must fail when its guard is removed)

- [ ] 6.1 Failing test that the positional startup argument carries the prompt, and that a named session with a prompt still runs its task rather than coming up waiting for input
- [ ] 6.2 Failing test that a post-start write waits for harness readiness, and that a harness producing no readiness evidence before the deadline still gets the write and does not fail the spawn
- [ ] 6.3 Implement bounded readiness waiting driven by a real timer, so an injected/frozen test clock cannot spin the wait until the context dies
- [ ] 6.4 Failing test that a naming failure against a confirmed-live runtime keeps the session and logs, and that a naming failure with unconfirmable liveness rolls the spawn back
- [ ] 6.5 Implement the liveness-gated forgiveness and confirm the prompt write remains fatal

## 7. Shipped guidance and its guard

- [ ] 7.1 Failing guard test that no shipped agent-facing guidance pairs a spawn instruction with the name flag, and that the guard fails when a file it claims to cover is missing
- [ ] 7.2 Update the daemon-embedded usage skill (command reference and examples) to stop passing and stop requiring the name flag
- [ ] 7.3 Update role instructions and orchestrator policy so dispatch passes the work item and omits the name, stating why the daemon owns it
- [ ] 7.4 Remove any remaining guidance telling a session to rename itself; legitimate renames go through the daemon rename path

## 8. Contract and end-to-end verification

- [ ] 8.1 Regenerate the API contract and commit generated OpenAPI/TypeScript together if any DTO changed
- [ ] 8.2 Confirm the sidebar rename flow works in web mode against a live daemon
- [ ] 8.3 Drive a real worker, a project Orc, and Prime on claude-code and confirm the identical name in the AO sidebar, `ao session ls`, the TUI, and `~/.claude/sessions/<pid>.json` with no `nameSource: "derived"` field
- [ ] 8.4 Drive the same three roles on codex and codex-fugu and confirm the identical name in the AO sidebar, `ao session ls`, the TUI, and `~/.codex/session_index.jsonl`
- [ ] 8.5 Confirm on the operator's phone that a spawned worker shows the AO name rather than a summary-style title
- [ ] 8.6 Confirm a live sidebar rename changes the name inside the running harness for each harness
- [ ] 8.7 Run the full local CI gate and record the harness versions from 1.1–1.2 plus the simpler alternative rejected, per repo PR rules
