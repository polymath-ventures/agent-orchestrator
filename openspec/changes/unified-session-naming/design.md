## Context

AO currently has no code path that names a session inside its harness. The
sidebar name comes from `ao spawn --name`, a required flag the dispatching Orc
fills in by hand, and the harness names itself independently. The result is two
unrelated names per session, one of which the operator sees on their phone.

This problem was solved once already, in the retired `agent-orchestrator-fscked`
fork, across its `f447fd954` (#147), `4b76949a1` (#163) and `c996e29e7` (#266).
That history is the most valuable input to this design, because it records three
failures that are not visible from the outside and that a fresh implementation
would rediscover the hard way:

1. **Argv slot contention.** Both `claude` and `codex` have a single positional
   startup slot. Spending it on a rename pushed the real prompt onto a post-start
   keystroke write; a second in-harness rename then collided with it, the pane
   received rename and prompt concatenated, and the worker never ran its task.
2. **A pane-readiness race.** Runtime creation returns as soon as the pane exists,
   which is before the harness has drawn its input box. Keystrokes sent into that
   gap are not queued — they land in whatever the harness reads first.
3. **Agents naming themselves.** `--name` documented as required, orchestrator
   policy telling Orcs to invent labels, and the daemon-embedded usage skill
   teaching the flag. An explicit name outranks a computed one, so the computed
   path was unreachable. Because Orcs dispatch with `--prompt "/address-issue
<id>"`, workers ended up literally named `/address-issue 148`.

GH #150 additionally records harness naming mechanics verified empirically against
`claude` 2.1.219 and `codex` 0.145.0, including two dead ends (`-rc` is not
`--remote-control`; `--remote-control`'s optional name is not the session name).
Treat that as settled input rather than re-deriving it.

The two mechanisms differ in how established they are, and the design leans on
that difference deliberately. The **in-harness `/rename <name>`** has been stable
across many releases of both CLIs — per the operator, as far back as they can
remember — so it is treated as a durable contract and is the universal path.
`claude`'s **`-n` launch flag** and the `nameSource` registry marker are newer
surfaces, so nothing is built to depend on them: `-n` is a droppable optimization
and `nameSource` is a verification aid, never a requirement.

Constraints: the daemon's API contract is generated, so DTO changes must go
through the spec generator. The fork is web-first, so nothing may depend on
Electron. Repo rules require test-first development.

## Goals / Non-Goals

**Goals:**

- One computed name per session, owned by the daemon, rendered identically on the
  AO sidebar, `ao session ls`, the harness TUI, and the Claude / Codex desktop
  and mobile session lists.
- One delivery path shared by spawn and rename, so the two cannot diverge.
- Adding a future harness cannot silently reintroduce the unnamed-session gap.
- The three historical failures above are prevented by construction and pinned by
  tests that fail if the prevention is removed.

**Non-Goals:**

- Encoding the harness in the name. That is deliberately excluded; the harness
  becomes visible as a sidebar glyph instead (separate ticket).
- Improving how a project's session prefix is derived at creation (separate
  ticket). This change consumes whatever prefix a project has.
- Renaming runtime multiplexer sessions or git branches.
- Upstreaming. The orchestrator and Prime roles are fork concepts.

## Decisions

### Compute the name in the daemon, keyed on an empty request field

The name is computed in the session manager at the point identity is set, and an
**empty** display name on the request is the signal that asks for computation.

_Why:_ the alternative — every caller computing its own name — is what produced
the divergence in the first place. There are three spawn entry points (tracker
intake, CLI, HTTP API) and they all funnel through one service, so the lookup and
the computation belong there, once.

_Alternative rejected:_ a `--auto-name` flag. It leaves the fabricated-name path
alive as the default, which is precisely the trap the prior fork fell into: a
name forged from the prompt is indistinguishable from an operator's name, so it
wins the override branch forever. Absence-as-signal cannot be faked by accident.

### One capability, two forms — universal rename, optional launch argument

A single agent-adapter naming capability exposes an in-harness rename command
(universal — every target harness accepts the identical inline `/rename <name>`)
and an optional launch argument (claude-code only, `-n <name>`).

_Why two forms rather than one:_ the prior fork used the post-start rename for
every harness, because it was choosing between prompt and rename for the single
positional slot. That constraint does not apply to `claude`'s `-n`, which is a
flag and competes with nothing. Naming in argv is atomic with process start, so
for claude-code the pane-readiness race is not mitigated — it is **absent**. Only
codex, which has no launch flag, needs the post-start write at spawn.

_Why this is safe:_ on claude-code, `-n` and `/rename` were verified to write the
same underlying fields, so the launch path and the rename path cannot drift into
two different names.

_Alternative rejected:_ naming every harness with the in-harness command only, for
uniformity. It would knowingly keep a race for a harness that offers a way to
avoid it entirely, buying symmetry with reliability.

_Alternative rejected:_ codex's `codex.thread.rename` app-server RPC instead of
the TUI command. It is a second transport, needing its own connection and failure
handling, to reach a name the TUI command already sets. Revisit only if the
keystroke path proves unreliable.

### Dispatch on capability, never on harness identity

The session manager asks whether an adapter implements the naming capability and
which forms it offers. It never switches on a harness name.

_Why:_ this is what makes the fix hold as harnesses are added. A `switch` on
harness name is a list that a future adapter is silently absent from; a capability
check makes the new adapter's own declaration the only thing that matters. The
codex adapter serves both `codex` and `codex-fugu` by parameterization, so one
implementation covers both without either being enumerated.

### One delivery function for spawn and rename

A single internal name-delivery routine is called by both, and it reads the name
off the session record rather than accepting it as an argument.

_Why:_ the failure this prevents is partial coverage — sidebar rename updating the
database while the harness keeps the old name, which is the current behavior. Two
call sites into one function means a surface cannot be forgotten, and the guard
behavior (skip terminated sessions, skip sessions with no runtime) is written
once. Reading the name from the record is what makes "the delivered name is
byte-identical to the displayed name" true by construction rather than by
convention: there is only one string, so there is nothing to keep in sync.

_Rebind is deliberately absent._ The prior fork had a work-item rebind path and
this design was drafted against it; this fork has none — no API route, no CLI
command, no store mutation changes a session's work item after spawn. The
delivery routine is the seam a future rebind would use, so nothing is lost by
not specifying naming for a capability that does not exist.

### Naming failure is forgiven only against a proven-live runtime

A failed name delivery does not fail a spawn when the runtime is confirmed alive,
and does fail it otherwise.

_Why the asymmetry is necessary:_ it looks like defensive over-engineering, and it
is not. Once the prompt rides argv, the name write is the **only** thing that
touches the pane during a claude-code spawn. Treating its failure as
unconditionally cosmetic means a harness that died between creation and naming
comes back as a live, idle session that never ran anything — the prior fork
shipped exactly that hole and had to close it in review. Liveness is the signal
that separates "cosmetic" from "dead".

### Prevent the docs regression with a test, not a convention

A test asserts that no shipped agent-facing guidance pairs a spawn instruction
with the name flag, and fails when a file it claims to cover is missing.

_Why a test here specifically:_ the daemon _embeds_ its usage skill and installs
it for agents to read. Documentation is therefore executable configuration on
this path — an agent that reads "`--name` is required" supplies a name, and the
override branch defeats the whole change. This is the one place where prose
regressing silently re-breaks the feature, which is what earns a guard. The
missing-file case matters because the prior fork's first version of this guard
skipped unreadable files and would have passed vacuously after a rename.

## Risks / Trade-offs

- **Harness CLI drift.** Low for the mechanism this design rests on: the
  in-harness `/rename <name>` has held stable across many releases of both CLIs.
  The residual risk sits on the newer surfaces → `-n` is an optimization the
  claude adapter can stop offering with no behavior change beyond reintroducing
  the spawn-time race that the universal path already tolerates, and no
  requirement depends on `nameSource`. Mechanics stay inside the adapters behind
  the capability, so any future change is one localized edit, and naming is
  non-fatal by contract so drift degrades a name rather than breaking spawns.
  Deliberately **not** mitigated with a startup probe or version check: that
  would add a gate running on every spawn forever to guard a contract that does
  not move, and the non-fatal contract already bounds the damage.
- **Keystroke delivery is inherently best-effort for codex.** A dropped write
  costs a name → accepted deliberately, and it is the whole point of putting the
  prompt in argv: the failure mode is inverted from "worker never ran its task"
  to "worker has AO's name but not the harness's". Renaming later re-issues it.
- **Readiness detection is heuristic** (evidence of harness output, not a
  protocol handshake) → bounded deadline, proceed anyway on timeout, and log the
  unconfirmed write. A harness that prints nothing must never hold a spawn open.
- **The 20-rune cap truncates title slugs**, so two workers on similarly-titled
  items can look alike → truncation preserves prefix and item number, which are
  the unique part; the item number always survives.
- **Removing a required flag is a CLI behavior change.** A caller relying on the
  error for a missing `--name` no longer gets it → the flag keeps working when
  supplied; only its requiredness is dropped. Shipped guidance is updated in the
  same change.

## Migration Plan

No storage migration is expected: the display name column already exists, and
this change alters who computes it and where else it is delivered. Existing
sessions keep their current names until something renames them; nothing
retroactively renames live sessions.

Rollback is per-layer and safe in either direction. Reverting the adapter
capability leaves AO's own names intact and simply stops harness delivery, which
is today's behavior. Reverting the CLI change restores a required flag.

## Open Questions

None blocking. The one question that was open during capture — which of claude's
name stores the mobile app renders — was resolved by observation before this
change was proposed: the name set by `-n` and by `/rename` is the surface the
phone shows, and the summary-style title an operator sees on an unnamed worker is
a server-side fallback that setting the name explicitly suppresses.
