## Port Boundary Evidence

Reference anchors are the issue-design gap list in `design.md`; current anchors
below name the fitted implementation and tests that carry each reopened #6 edge.

- Cross-family reviewer defaults and taxonomy: implemented in
  `backend/internal/domain/projectconfig.go` and covered by
  `backend/internal/domain/projectconfig_test.go:138`.
- Reviewer fail-loud launch config: implemented in
  `backend/internal/review/review.go:457` and covered by
  `backend/internal/review/review_test.go:739`,
  `backend/internal/review/review_test.go:759`, and
  `backend/internal/review/review_test.go:778`.
- Slim role prompt scaffolds: implemented in
  `backend/internal/session_manager/prompt.go:276`,
  `backend/internal/session_manager/prompt.go:286`,
  `backend/internal/session_manager/prompt.go:296`, and
  `backend/internal/review/prompt.go:22`; covered by
  `backend/internal/session_manager/prompt_test.go:77`,
  `backend/internal/session_manager/prompt_test.go:103`,
  `backend/internal/session_manager/prompt_test.go:134`, and
  `backend/internal/review/prompt_test.go`.
- Absolute/shared role instruction files and preserved repo confinement:
  implemented in `backend/internal/session_manager/prompt.go:162` and covered by
  `backend/internal/session_manager/prompt_test.go:185`,
  `backend/internal/session_manager/prompt_test.go:214`,
  `backend/internal/session_manager/prompt_test.go:228`,
  `backend/internal/session_manager/prompt_test.go:243`,
  `backend/internal/session_manager/prompt_test.go:266`, and
  `backend/internal/session_manager/prompt_fifo_test.go:17`.
- Per-session policy provenance: implemented in
  `backend/internal/session_manager/manager.go:531`,
  `backend/internal/session_manager/manager.go:1342`, and
  `backend/internal/session_manager/prompt.go:230`; covered by
  `backend/internal/session_manager/manager_test.go:2398`.
- Reviewer health wiring: implemented in
  `backend/internal/daemon/agent_wiring.go` and covered by
  `backend/internal/daemon/agent_wiring_test.go`.
- Reviewer UI model pins and default label: implemented in
  `frontend/src/renderer/components/ProjectSettingsForm.tsx` and covered by
  `frontend/src/renderer/components/ProjectSettingsForm.test.tsx`.
- Prime operator surfaces: implemented in `backend/internal/cli/project.go:129`,
  `backend/internal/cli/project.go:350`,
  `frontend/src/api/schema.ts:1215`, and
  `frontend/src/api/schema.ts:2817`; covered by
  `backend/internal/cli/project_test.go:271`,
  `backend/internal/cli/role_test.go:30`,
  `backend/internal/roleprompt/roleprompt_test.go:36`, and
  `frontend/src/renderer/components/ProjectSettingsForm.test.tsx`.
- Final-review status and incident lessons: implemented in
  `ops/final-review-status-core.mjs`, `ops/final-review-status.mjs`, and
  `agent-instructions/source/30-polypowers.md:243`; covered by
  `ops/final-review-status.test.mjs` and
  `backend/internal/adapters/scm/github/provider_test.go:1467`.

## Preserved Current Behavior

- Fail-closed role rule loading remains in `LoadRoleRules`, including
  missing/empty/oversized files, FIFO rejection, and repo-relative symlink escape
  rejection.
- Repo-relative role files still use `os.Root` confinement. Absolute files are
  accepted only as intentional policy-file values and still fail closed on load
  errors.
- `ao role prompt` and HTTP/UI effective prompt inspection remain the
  operator-visible prompt surfaces, now including prime and reviewer roles.
- Existing SSE/drain/prime hardening is intentionally outside this parity diff
  except where prime role configuration/prompt inspection needed surface parity.

## Ruled-Dead Reference Features

The diff intentionally does not port label routing, behavior-version
convergence, attention/capacity dashboards, usage-based scheduling, or unrelated
notification delivery. The final-review status helper is limited to the
history-mined review/merge contract and does not reintroduce those larger
reference subsystems.
