## 1. Naming grammar (pure functions, no I/O)

- [x] 1.1 Failing tests for the worker name grammar: prefix + item number + title slug, and slug omitted when the title is unavailable
- [x] 1.2 Failing tests for the orchestrator name grammar (prefix + role suffix) and for Prime taking its name from fleet Prime settings
- [x] 1.3 Failing tests for the length cap: over-long names keep prefix, item number, and role suffix intact and clip only the trailing slug
- [x] 1.4 Failing test that a worker with no resolvable work item still gets a non-empty name
- [x] 1.5 Implement the name builders and slug/cap helpers until 1.1–1.4 pass

## 2. `AgentNamer` port capability

- [x] 2.1 Failing test asserting the session manager dispatches on the naming capability, not on harness identity (a fake adapter declaring the capability is named; one that does not is left alone)
- [x] 2.2 Define the optional capability interface: universal in-harness rename command plus optional launch-argument form
- [x] 2.3 Failing test for the Codex adapter's in-harness rename form, asserting it covers `codex-fugu` through the same parameterized Plugin
- [x] 2.4 Implement the Codex adapter's in-harness rename form
- [x] 2.5 Failing test for the claude-code adapter's in-harness rename form, and for name sanitization on both adapters (control characters, newlines, collapsed whitespace)
- [x] 2.6 Implement claude-code's in-harness rename form — the universal path works on every harness before any optimization is added
- [x] 2.7 Failing test for claude-code's launch-argument form, then implement it as the spawn-time optimization
- [x] 2.8 Regression test: no adapter places a rename in the harness's positional startup slot

## 3. Daemon ownership of the name

- [x] 3.1 Failing test that an empty display name on a spawn request yields the computed name, and a supplied name is honored as an override
- [x] 3.2 Failing test that a spawn prompt never becomes the display name
- [x] 3.3 Remove the prompt-derived display-name default and make the CLI's `--name` optional while keeping its length validation
- [x] 3.4 Resolve the work item title on every spawn path (tracker intake, CLI, HTTP API) in the shared service, bounded, degrading to the head-only name and logging the reason on any failure
- [x] 3.5 Failing test that a tracker outage or unresolvable work item degrades the name instead of failing the spawn
- [x] 3.6 Wire the computed name into spawn

## 4. One delivery path

Rebind is out of scope: this fork has no path that changes a session's work item
after spawn. See the proposal's Out of scope section.

- [x] 4.1 Failing tests for a single name-delivery routine: prefers the launch-argument form at spawn when offered, falls back to the in-harness command, no-ops for an adapter without the capability
- [x] 4.2 Failing tests that it skips harness delivery for terminated sessions and for sessions with no live runtime
- [x] 4.3 Implement the delivery routine and call it from spawn
- [x] 4.4 Failing test that renaming a live session delivers the new name to the harness, then route the rename path through the delivery routine
- [x] 4.6 Assert the delivered string is byte-identical to the persisted display name on every path

## 5. Spawn-safety invariants (each test must fail when its guard is removed)

- [x] 5.1 Failing test that the positional startup argument carries the prompt, and that a named session with a prompt still runs its task rather than coming up waiting for input
- [x] 5.2 Failing test that a post-start write waits for harness readiness, and that a harness producing no readiness evidence before the deadline still gets the write and does not fail the spawn
- [x] 5.3 Implement bounded readiness waiting driven by a real timer, so an injected/frozen test clock cannot spin the wait until the context dies
- [x] 5.4 Failing test that a naming failure against a confirmed-live runtime keeps the session and logs, and that a naming failure with unconfirmable liveness rolls the spawn back
- [x] 5.5 Implement the liveness-gated forgiveness and confirm the prompt write remains fatal

## 6. Shipped guidance and its guard

- [x] 6.1 Failing guard test that no shipped agent-facing guidance pairs a spawn instruction with the name flag, and that the guard fails when a file it claims to cover is missing
- [x] 6.2 Update the daemon-embedded usage skill (command reference and examples) to stop passing and stop requiring the name flag
- [x] 6.3 Confirm role instructions and orchestrator policy carry no spawn-naming guidance in this fork (they do not; the guard covers the shipped skill, which is the only agent-facing artifact that described the flag)
- [x] 6.4 Confirm no shipped guidance tells a session to rename itself

## 7. Contract and end-to-end verification

- [ ] 7.1 Regenerate the API contract and commit generated OpenAPI/TypeScript together if any DTO changed
- [ ] 7.2 Confirm the sidebar rename flow works in web mode against a live daemon
- [ ] 7.3 Drive a real worker, a project Orc, and Prime on claude-code and confirm the identical name in the AO sidebar, `ao session ls`, the TUI, and `~/.claude/sessions/<pid>.json` — that store is the data the desktop/mobile surface renders, so no separate app check is needed
- [ ] 7.4 Drive the same three roles on codex and codex-fugu and confirm the identical name in the AO sidebar, `ao session ls`, the TUI, and `~/.codex/session_index.jsonl`
- [ ] 7.5 Confirm a live sidebar rename changes the name inside the running harness for each harness
- [ ] 7.6 Confirm the universal path alone is sufficient: with claude-code's launch-argument form disabled, a claude session still ends up correctly named via the in-harness command
- [ ] 7.7 Run the full local CI gate; in the PR, record the harness versions exercised and name the simpler alternative rejected, per repo rules
