## Context

The first `role-prompt-transparency` change is archived into the canonical `role-instructions`
spec. It added useful operator-facing surfaces, but the reopened issue supersedes the earlier
greenfield plan. The implementation must start from the reference fork where the ticket identifies
the reference as the proven behavior source, then fit that code into current upstream-shaped seams.

The parity boundary is the issue's verified gap list:

- Cross-family reviewer default and family taxonomy: reference `domain/projectconfig.go:173-201`
  plus `domain/agent_family.go:24-35`; current path starts at `domain/projectconfig.go:100-112`.
- Reviewer config fail-loud behavior: reference `review/review.go:608-612`; current path starts at
  `review/review.go:470-473`.
- Slim prompt scaffold: reference `session_manager/manager.go:3740-3800`; current path starts at
  `session_manager/prompt.go:265-441`.
- Absolute role instruction files: reference `session_manager/manager.go:3598-3612`; current path
  starts at `prompt.go:232-235`.
- Per-session policy provenance: reference `session_manager/manager.go:3544-3575`; current tree is
  missing it.
- Reviewer health wiring: reference `daemon/agent_health_wiring.go:73-75`; current tree is missing
  it.
- Reviewer UI model pins and "Project default" labeling: reference `ProjectSettingsForm.tsx`
  regions cited by the issue; current path starts at the settings form reviewer rows.
- Prime operator surfaces and role-prompt inspector coverage: reference `cli/project.go:446` and
  the settings form role surfaces; current CLI/UI omit prime in the relevant paths.
- Role override envelope and prompt-policy placement are current-vs-reference decisions to fit, not
  copy blindly.

## Goals / Non-Goals

**Goals:**

- Make unconfigured reviewer selection cross-family by construction, with labels and API/CLI/UI
  behavior that explain the actual default instead of hiding behind an empty sentinel.
- Express agent family independently of individual harness strings, and preserve the historical
  `ProviderUnknown` rule: unknown/unclassified harnesses do not create a hard independence block.
- Make reviewer launch config resolution fail loudly for configured invalid pins, matching the
  reference's propagated error behavior.
- Move role prompt content toward a slim Go scaffold plus operator-controlled policy files, while
  preserving the current fail-closed loader and effective prompt inspector.
- Support intentional absolute/shared instruction paths without weakening repo-relative
  confinement.
- Record per-session prompt-policy provenance and include reviewer harnesses in health monitoring.
- Add prime to the operator-facing configuration and prompt-inspection role vocabulary.
- Verify the history-mined edge cases in tests or a PR checklist with file/line evidence.

**Non-Goals:**

- Reintroducing behavior-version convergence, label routing, attention/capacity dashboards,
  usage-based scheduling, or any other ruled-dead reference entanglement.
- Weakening the already-merged effective-prompt inspector, fail-closed misconfiguration behavior, or
  `os.Root` confinement for repo-relative policy files.
- Changing Slack/notification delivery surfaces owned by other issues.
- Merging without a clean independent review and explicit authorization.

## Decisions

### D1 - Port family taxonomy before reviewer default logic

Reviewer independence cannot be enforced or explained while harness strings are the only vocabulary.
Port the reference's `AgentFamily` concept first, then make project reviewer resolution return a
different-family reviewer by default. Unknown/unclassified families remain advisory so new harnesses
are not blocked by absence of taxonomy.

### D2 - Enforce independence when setting review status, not only when checking it

The issue's history warns that fail-closed enforcement at check time bricked the merge queue when
default-branch workflows lacked author-family context. Keep merge gates robust by recording enough
review status metadata when a verdict is set, then have later checks consume that recorded contract.

### D3 - Treat "clean but human-merge parked" as its own status

Do not overload review failure for sensitive or human-authorization stops. Preserve a distinct
merge-park state so a clean review that still needs a human does not permanently look failed.

### D4 - One status/review contract source

Avoid literal drift between final-review writers and merge-gate readers by centralizing status
context names and accepted review-state constants in one shared package or documented constant set.

### D5 - Slim prompts keep mechanics in Go and doctrine in operator files

Use Go prompt builders for identity, ordering, and runtime mechanics only. Substantive doctrine
belongs in configured operator instruction files that are visible through the prompt inspector.
Preserve the agent-facing guard that prevents agents from revealing standing instructions in their
own output; the inspector is an operator-owned daemon surface.

### D6 - Absolute policy files are explicit, not a repo-relative bypass

Repo-relative policy files keep current `os.Root` confinement. Absolute/shared paths are accepted
only through the role instruction file surface, load fail-closed, and are recorded in prompt-policy
provenance. This preserves current safety while enabling shared operator policy outside a checkout.

## Risks / Trade-offs

- **Porting reference code can drag ruled-dead features.** Fit only the files and regions cited by
  the gap list; remove label routing, convergence, attention, capacity, usage scheduling, and
  unrelated notification behavior while fitting.
- **Reviewer default changes can surprise old configs.** Existing explicit reviewer choices keep
  winning. Only the unconfigured/default sentinel changes, and the UI label must name the resolved
  behavior.
- **Prompt-policy hashes are not human-readable policy.** The hash is provenance, not visibility.
  Effective prompt inspection remains the human-readable source.
- **Absolute files broaden where config can point.** Load fail-closed and record provenance so a bad
  shared path is loud and inspectable.

## Verification Plan

- Add failing tests for unconfigured reviewer default selecting a different family, unknown family
  permissiveness, configured invalid reviewer harness/model errors propagating, and the UI label
  naming the resolved default.
- Add regression coverage for status-context drift, merge-park distinction, stale PR status lookup,
  malformed review timestamps, and foreground reviewer execution.
- Add prompt loader tests for absolute paths, missing/empty/oversized fail-closed behavior, and
  TOCTOU-resistant open/fstat/read ordering where the loader owns that boundary.
- Add API/CLI/UI tests for prime role prompt inspection and role policy config.
- Run `npm run api` after DTO changes and the repo's backend/frontend gates before review.
