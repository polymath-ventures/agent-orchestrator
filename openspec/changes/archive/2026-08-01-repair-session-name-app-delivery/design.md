## Context

The original unified naming implementation added two delivery mechanisms:
Claude's `-n` launch flag and the universal in-harness `/rename` command. The
spec accidentally made `-n` a replacement for `/rename`, but the issue records
the opposite contract: `/rename` is the durable path every harness uses, while
`-n` is only an early naming optimization.

AO also uses the same activity words for different risk profiles. An operator
rename or restore write is unsolicited, so an idle prompt is unsafe: the next
Enter can submit text to the agent. A spawn-owned write is different because AO
just created the session and is completing its setup. By the time the harness
reports ready, the startup prompt may already be active or complete, and either
state still needs the app-visible name.

## Goals / Non-Goals

**Goals:**

- Make the in-harness rename path universal for spawn delivery whenever the
  adapter supports it.
- Keep spawn naming safe across active sessions, idle prompts, and blocked
  permission dialogs.
- Preserve stricter guards for operator rename and restore redelivery.

**Non-Goals:**

- Add CLI version probes or feature detection for `/rename`.
- Change the public API, storage model, or session-name grammar.
- Make harness naming a hard failure after the agent is proven alive and
  working.

## Decisions

### D1 - `/rename` remains universal after readiness

Spawn passes the computed name to any launch argument an adapter offers, then
redelivers the same persisted name through `InHarnessRenameCommand` after prompt
readiness. This keeps early UI surfaces named without making the newer launch
flag responsible for desktop/mobile app persistence.

Alternative rejected: skip post-start rename when a launch argument exists. That
was smaller, but it is exactly the behavior that left app-visible names stale.

### D2 - Spawn uses the solicited delivery guard only when waiting_input is distinguishable

Spawn delivery uses the guard path that allows active and `waiting_input` states
only for harnesses that can report pending decisions as the distinct `blocked`
state. Harnesses that collapse permission prompts into `waiting_input` keep the
stricter nudge policy, so a spawn-owned `/rename` cannot answer a dialog that
AO cannot distinguish from an idle prompt. The spawn write is AO completing
creation of the session, not a later operator action.

Alternative rejected: use the idle-only rename guard for spawn. That protects
the pane but loses names for workers that are already active or have already
returned to a distinguishable prompt by the time their harness reports
readiness.

### D3 - Restore and operator rename stay stricter

Restore redelivery and operator rename remain unsolicited writes and use the
guard policies that skip idle prompts as well as active/blocked sessions. A
restore can rely on launch-time naming where available and may redeliver later;
it must not type `/rename` into a prompt the operator may be about to use.

## Risks / Trade-offs

- **Claude turn interference:** verified live against Claude Code 2.1.220 that
  spawn-time `/rename` after readiness updates the registry name while the task
  prompt is in flight and does not appear as literal prompt text.
- **Cosmetic delivery failure:** name delivery remains non-fatal only when the
  runtime is still alive; otherwise spawn still fails closed.
