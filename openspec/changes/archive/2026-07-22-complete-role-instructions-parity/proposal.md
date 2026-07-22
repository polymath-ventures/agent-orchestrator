## Why

Issue #6 was reopened after PR #36 because the first role-instructions pass delivered useful
visibility and fail-closed rules loading, but did not preserve the reference fork's accumulated
reviewer-independence, role-configuration, and slim-prompt parity work. The operator ruling for the
reopened pass is PORT -> FIT -> IMPROVE -> FIX: lift the reference behavior where it is the proven
bug-fix corpus, adapt only current-tree seams, preserve current improvements, and apply only the
explicit deltas the reference never had.

The reopened ticket identifies the root defect: the current "Project default" reviewer setting
resolves to the worker's own harness when no explicit reviewer is configured, which recreates the
self-review anti-pattern the reference fork already fixed. The same parity pass also needs to
restore the role/family taxonomy, fail-loud reviewer config resolution, absolute/shared policy file
support, per-session prompt-policy recording, reviewer health monitoring, reviewer model-pin UI
behavior, prime role operator surfaces, and the slim base-prompt scaffold.

## What Changes

- Replace the gap-listed role/reviewer regions using the PORT -> FIT -> IMPROVE -> FIX strategy:
  transplant reference behavior, adapt it to current config/spawn/review/UI paths, preserve current
  verified-better behavior, and record every reference-only or current-only decision in the PR.
- Introduce an explicit agent-family taxonomy so review independence is representable and the
  default reviewer can be chosen from a different family than the worker, with `ProviderUnknown`
  treated as unclassified/advisory rather than a blocking family match.
- Replace the dangling "Project default" reviewer sentinel with a label and resolution path that
  names the real cross-family default and validates configured reviewer harnesses before use.
- Fail loudly when reviewer agent-config/model-pin resolution errors occur; do not silently drop a
  configured reviewer model to a zero-value launch config.
- Adopt the slim-scaffold prompt pattern for role prompt assembly: Go keeps only the minimal identity
  and mechanics scaffold, while operator-controlled files carry substantive role doctrine.
- Permit absolute/shared operator instruction file paths when intentionally configured, while
  preserving the current tree's verified-better fail-closed loading, `os.Root` confinement for
  repo-relative rules, and effective-prompt inspector.
- Record the injected prompt-policy identity for each session, at least as a hash, so an operator can
  later tell which policy content a session received without relying on ephemeral prompt artifacts.
- Bring reviewer harnesses into install/auth health monitoring and expose reviewer model/effort pins
  in the UI through the dynamic model picker without stale scalar values crossing harness switches.
- Add the prime role to the operator control and prompt-inspection surfaces where the current role
  vocabulary already supports role-specific prompts.
- Prove each history-mined edge case from the issue survives in the port, or explicitly document why
  it no longer applies.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `role-instructions`: Complete the reopened parity contract for operator-controlled role
  instructions, effective-prompt visibility, reviewer independence, reviewer/prime configuration,
  prompt-policy provenance, and slim role scaffolds.

## Impact

- Backend project configuration, agent harness taxonomy, reviewer harness resolution, reviewer
  launch config, prompt assembly, session creation records, agent health wiring, and role prompt
  visibility routes.
- CLI role/project commands for reviewer, orchestrator, worker, and prime role policy surfaces.
- Electron/React project settings, reviewer defaults, reviewer model pins, dynamic model picker
  plumbing, and role prompt inspector.
- OpenAPI and TypeScript schema output when DTOs or route surfaces change; generated artifacts must
  be regenerated from source.
- Tests must cite the reference implementation and issue history checklist, including the
  self-review default incident, status-context drift, merge-park distinction, stale PR status
  lookup, malformed review timestamp handling, foreground reviewer execution, and role-instruction
  loading race/size behavior.
