## Context

The first `aong` change deliberately kept the binary thin: it shells out to the
co-installed `ao` executable and only composes behavior that `ao` exposes
publicly, plus user-service operations that `ao` cannot express. That boundary
is still right, but the command surface is too narrow. `aong` currently registers
only seven lifecycle commands and treats every other verb as CLI misuse, so it
is not yet the fork's complete invocation surface.

Issue #203 settles the product shape: `ao` remains upstream's CLI, `aong` is the
fork's operator/agent-facing surface, and machine-only entrypoints continue to
call `ao` directly. The implementation must preserve the lifecycle semantics
from the archived `add-aong-lifecycle-cli` design while changing the root
command behavior from allowlist to passthrough-by-default.

## Goals / Non-Goals

**Goals:**

- Make every non-overridden `ao` top-level command reachable through `aong`
  without maintaining a duplicate list of commands.
- Keep overridden verbs visible in one small table so the fork-specific
  divergences are auditable and liftable into upstream if desired.
- Preserve transparent process behavior on passthrough: stdin, stdout, stderr,
  signals where the process layer supports them, and exit status.
- Make `--verbose` useful for operators and reviewers by printing what `aong`
  will invoke or where it intentionally diverges.
- Extend `doctor` at the boundary `aong` owns: run `ao doctor`, then check
  loaded fork service units.

**Non-Goals:**

- Importing `backend/internal/cli`, daemon packages, storage, run-file helpers,
  or HTTP clients into `aong`.
- Changing `ao` command behavior, service units, daemon lifecycle semantics, or
  hidden machine entrypoints.
- Renaming the embedded `using-ao` skill directory unless implementation proves
  that compatibility is safe; the content can teach `aong` while the path stays
  stable.
- Making `aong` shadow or replace the installed `ao` binary.

## Decisions

### Passthrough is the fallback path, not a generated command catalog

`aong` should keep explicit Cobra commands only for overrides and for its own
root/help/version behavior. If root receives an argv whose first non-flag token
is not overridden, it resolves the sibling-first `ao` executable and execs/runs
`ao <original argv>` directly.

The rejected alternative is to enumerate `ao` commands and register mirror
Cobra commands in `aong`. That makes command help nicer on day one but recreates
the rot that caused this issue: the wrapper has to remember upstream's catalog.
Completeness should be a property of the fallback path, with tests proving that
the current `ao` command list remains reachable.

### Global `aong` flags are parsed only before the verb

`--verbose` is an `aong` global only when it appears before the verb. Once a
passthrough verb is selected, everything after that verb is handed to `ao`
untouched, including flags named `--verbose`.

The alternative is interspersed global parsing, but that would steal flags from
future `ao` commands and make passthrough less transparent. Requiring globals
before the verb is a common CLI convention and is easy to document in help.

### Passthrough needs a streaming runner separate from combined-output helpers

Existing `runAO` captures combined output because lifecycle overrides often add
their own explanatory text around `ao` output. Passthrough should use a new
dependency seam that binds stdin/stdout/stderr directly and returns the
underlying exit code without remapping. Tests can still record argv and provide
fake streams through the same dependency.

### `pause` is an override that exits as misuse after teaching the right verbs

`aong pause` must not silently alias `drain`; that would reintroduce the
dishonest name the lifecycle spec removed. It should also not look like a random
unknown command. Treat it as a deliberate `aong` divergence: print a concise
message explaining that `drain` gates and drains at idle while `stop-work`
terminates now, then exit with CLI misuse status.

### `doctor` wraps, then adds unit checks

`aong doctor` should run `ao doctor` with transparent output, then add checks for
`ao-web.service` and `ao-tmux.service` when a systemd user manager is present and
the units are loaded. This lives in `aong` because `ao doctor` cannot know
fork-specific units, while `aong status` already has the systemd detection seam.
Missing units on a plain/upstream-style host are informational, not failures.
Loaded but inactive/failed units make `aong doctor` fail after preserving the
`ao doctor` output.

## Risks / Trade-offs

- Passthrough root handling can accidentally catch true `aong` misuse → Keep
  root flag parsing strict before the verb, keep known override argument checks,
  and cover unknown flags/unexpected args in tests.
- Streaming passthrough is harder to assert than captured output → Add a small
  fake process seam that records argv, stdin use, stdout/stderr writes, and exit
  code independently of lifecycle override tests.
- `aong doctor` could become a second doctor implementation → Limit it to the
  unit layer `ao` lacks; everything else remains `ao doctor`.
- Documentation replacement could overcorrect descriptive `ao` references →
  Update only operator/agent instructions that tell someone what to type; leave
  architecture text, porcelain-boundary explanations, hooks, services, and
  hidden entrypoints as `ao`.

## Migration Plan

No data migration is required. Deployment already installs `aong` beside `ao`.
Rollback is reverting the command-layer change; operator docs would again point
at the narrower lifecycle surface.

## Open Questions

None blocking. The issue's decision defaults settle `--verbose`, the
before-verb global flag rule, passthrough help behavior, and the
machine-entrypoint exceptions.
