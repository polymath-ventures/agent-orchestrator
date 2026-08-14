## 1. Headless Runtime Packaging

- [x] 1.1 Add failing ops guards for immutable-release runtime construction, pre-activation validation, and the stable install-prefix runtime symlink.
- [x] 1.2 Update `ops/deploy.sh` to reuse the shared ACP runtime builder before release activation and expose the completed runtime beside `~/.local/bin/ao` through the rollback-safe `current` link.
- [x] 1.3 Exercise a real temporary build and verify the packaged Node executable and pinned adapter entrypoint.
- [x] 1.4 Run focused ops tests, mark the phase complete, and pass the pre-push/rebase gate.

## 2. Shared Runtime Diagnosis and Fork Contract

- [ ] 2.1 Add failing Claude ACP adapter tests for a non-Electron install-prefix layout, the shared runtime check, and actionable remediation.
- [ ] 2.2 Expose the adapter's runtime check without duplicating resolution rules.
- [ ] 2.3 Add failing doctor coverage for valid and unavailable Claude ACP runtimes, then wire the shared check into agent harness diagnostics.
- [ ] 2.4 Add the non-Electron Claude ACP runtime concern and sync anchors to `docs/fork.md` item 1.
- [ ] 2.5 Run focused Go and ops tests and validate the OpenSpec change.
- [ ] 2.6 Run the repository's full local CI gate, rebase, and push the completed phase.
