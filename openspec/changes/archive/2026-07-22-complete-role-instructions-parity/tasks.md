## 1. Freeze the port boundary

- [x] 1.1 Record the reference-to-current file map for every reopened #6 gap in the implementation
      plan and PR body, citing exact reference `file:line` evidence.
- [x] 1.2 Identify and preserve current verified-better behavior: fail-closed role rules loading,
      `os.Root` repo-relative confinement, `ao role prompt`, HTTP/UI prompt inspection, and existing
      SSE/drain/prime hardening outside this issue's scope.
- [x] 1.3 Confirm the ruled-dead reference features stay out of the diff: label routing,
      behavior-version convergence, attention/capacity dashboards, usage-based scheduling, and
      unrelated notification delivery.

## 2. Restore reviewer independence and launch correctness

- [x] 2.1 Port/adapt the `AgentFamily` taxonomy and make reviewer resolution choose a different
      family than the worker by default.
- [x] 2.2 Replace the "Project default" sentinel/label with resolved cross-family default behavior
      across API, CLI, and UI.
- [x] 2.3 Propagate reviewer agent-config/model resolution errors instead of dropping configured
      pins to zero values.
- [x] 2.4 Preserve `ProviderUnknown` as unclassified/advisory so unknown harnesses do not create a
      false same-family block.

## 3. Complete role policy control and provenance

- [x] 3.1 Adopt the slim-scaffold prompt pattern for worker, orchestrator, reviewer, and prime where
      prompt assembly is owned by AO.
- [x] 3.2 Support intentional absolute/shared role instruction files while preserving fail-closed
      loading and repo-relative confinement for repo policy files.
- [x] 3.3 Record per-session injected policy provenance, at least by stable hash, and expose it where
      session diagnostics need it.
- [x] 3.4 Add prime role configuration and prompt-inspector coverage.

## 4. Fit reviewer health and UI model pins

- [x] 4.1 Include reviewer harnesses in install/auth health monitoring with the same actionable vs
      advisory state semantics as worker harnesses.
- [x] 4.2 Replace reviewer model-pin free text/stale scalar behavior with the dynamic model picker
      and harness-local saved pairs.
- [x] 4.3 Regenerate OpenAPI and TypeScript schema output for any changed DTOs or routes.

## 5. Verify incident lessons and prepare review

- [x] 5.1 Add regression coverage for status-context literal drift, merge-park vs failed-review
      state, stale PR lookup, malformed review timestamps, and foreground reviewer execution.
- [x] 5.2 Add prompt-loader coverage for missing/empty/oversized files, absolute paths, and
      TOCTOU-resistant size checks.
- [x] 5.3 Run focused backend/frontend tests after each phase and update this task list only when
      behavior is verified.
- [x] 5.4 Run the repository's full CI commands, rebase on fresh `origin/main`, push, open the PR,
      and complete independent final review before declaring merge-ready.
