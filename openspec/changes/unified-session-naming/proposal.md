## Why

A session's name is currently invented independently on every surface it appears
on. AO's sidebar shows whatever the dispatching Orc hand-typed into the required
`ao spawn --name` flag, while the harness names itself: a worker shown in AO as
`cc #1929 sqlite3` appears in Claude as `Fix CI failures in better-sqlite3
upgrade PR`, Prime gets an auto-derived codename instead of `AO Prime`, and
project Orcs are not named in the harness at all. Nothing AO knows about a
session reaches the name the operator reads on their phone, so the fleet is
unsupervisable from the surfaces the operator actually uses.

This is worth doing now because the operator drives the fleet from the Claude and
Codex mobile apps, where sessions from every project appear in one flat list and
the name is the only identifying cue.

## What Changes

- The daemon becomes the **single owner** of a session's display name, computing
  it once at the moment the session's identity is set — spawn or explicit
  rename — instead of accepting a name invented by the caller.
- A session's name is delivered into the **harness itself**, so the harness's own
  session list (and therefore the Claude / Codex desktop and mobile apps) shows
  the same string AO shows. Today no code path does this at all.
- Renaming a session from the AO sidebar updates the harness name on the **live**
  session rather than only the database row.
- A new agent-adapter capability expresses naming in two forms: a universal
  in-harness rename command, and an optional launch-time argument used to name a
  session atomically at start-up so nothing races the harness's own naming.
- **BREAKING (CLI/API surface, deliberately):** `ao spawn --name` stops being a
  required flag and becomes an explicit override. An omitted name is the signal
  that asks the daemon to compute the name. Shipped documentation stops teaching
  `--name`, because documentation that teaches the override defeats the owner.
- Naming failures are made non-destructive by contract: a name is cosmetic
  relative to the session's task, so a failed name delivery must never tear down
  a working session — while a session whose harness is actually dead must still
  fail its spawn rather than being forgiven as a naming hiccup.

## Capabilities

### New Capabilities

- `session-naming`: Who owns a session's name, how it is computed from the
  session's role and work item, how it is delivered to every surface including
  the harness, and what must remain true when delivery fails.

### Modified Capabilities

None. `fleet-prime-settings` continues to own Prime's display name as fleet
configuration — this change consumes that value rather than redefining it — and
`codex-fugu-harness` already specifies that fugu reuses the Codex adapter by
parameterization, so it inherits the new naming behavior without a requirement
change.

## Out of scope

Rebinding a live worker to a different work item. The prior fork had that
capability and the naming design was drafted against it, but this fork has no
path that changes a session's work item after spawn — no API route, no CLI
command, no store mutation. Specifying naming behavior for it would describe a
subject that does not exist, and building the rebind capability itself is a
separate feature. The single delivery path is the seam that would carry it: a
future rebind recomputes the name and calls the same routine spawn and rename
already share.

## Impact

- **Agent adapter port** — a new optional naming capability, implemented by the
  claude-code adapter and by the Codex adapter (which serves both `codex` and
  `codex-fugu`).
- **Session manager** — name computation for worker and Orc roles, and a single
  name-delivery path shared by spawn and rename.
- **Session service and HTTP API** — the rename path gains harness delivery.
- **CLI** — `ao spawn --name` becomes optional; `ao session rename` keeps the
  same display-name cap as the API.
- **Frontend** — no new UI, but the existing sidebar rename now has a visible
  effect inside the harness. Web-first: no Electron dependency.
- **Shipped agent documentation** — the daemon-embedded `using-ao` skill and role
  instruction/policy text stop teaching `ao spawn --name`.
- **No storage migration expected.** The display name is already persisted;
  the work is in who computes it and where it is delivered.
- **External dependency:** the naming mechanics of the `claude` and `codex` CLIs.
  The in-harness rename command this change rests on has been stable across many
  releases of both, so it is treated as a durable contract rather than something
  probed at runtime. The newer surfaces are deliberately kept non-load-bearing.
  GH #150 records the verified behavior and the versions it was verified against.
