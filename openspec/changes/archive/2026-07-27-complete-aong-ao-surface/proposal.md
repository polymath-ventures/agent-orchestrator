## Why

`aong` was introduced to give this fork an honest CLI surface for AO operations,
but the first slice only covered seven lifecycle verbs and made every other
`ao` command unreachable. Operators now have to remember which binary has which
verb, and unknown-but-valid `ao` commands fail as `aong` misuse instead of
remaining available through the fork's intended invocation surface.

## What Changes

- Make `aong` a complete surface over `ao` by default: any top-level verb that
  `aong` does not explicitly override is forwarded to `ao` with the same argv,
  stdin, stdout, stderr, and exit code.
- Keep the override table small and explicit. The existing lifecycle verbs
  remain overridden because they compose or diverge from `ao`'s lifecycle UX.
- Add a global `--verbose` flag, accepted before the verb, that reports the
  exact `ao` invocation for passthrough and wrapping overrides, and plainly
  marks commands that diverge instead of wrapping an equivalent `ao` verb.
- Make `aong pause` a deliberate instructional divergence rather than an
  unknown command or a silent alias for `drain`; it points operators at `drain`
  and `stop-work`.
- Make `aong doctor` pass through to `ao doctor` and add fork-owned user-service
  health checks for `ao-web.service` and `ao-tmux.service`.
- Update help and operator-facing documentation so `aong` is the fork's human
  and agent invocation surface while machine entrypoints continue to call `ao`
  directly.

## Capabilities

### New Capabilities

- None.

### Modified Capabilities

- `aong-lifecycle-cli`: widen `aong` from a lifecycle-only porcelain to a
  complete `ao` surface with passthrough-by-default, auditable verbose output,
  a deliberate `pause` redirect, and fork-specific doctor unit checks.

## Impact

- `backend/internal/aongcli/root.go`, `commands.go`, `ao.go`, and tests for the
  command registry, passthrough execution, stdio transparency, exit-code
  propagation, verbose reporting, `pause`, and `doctor`.
- `openspec/specs/aong-lifecycle-cli/spec.md` via a delta spec for the changed
  requirements.
- `docs/`, `CLAUDE.md`, and the embedded AO CLI skill under
  `backend/internal/skillassets/using-ao/`, limited to operator/agent-facing
  instructions that should use `aong`.
