## 1. Command routing and process seams

- [x] 1.1 Add tests proving non-overridden `ao` commands pass through with original argv, stdin, stdout, stderr, and exact exit code.
- [x] 1.2 Add a streaming passthrough dependency seam alongside the existing combined-output `ao` helper.
- [x] 1.3 Replace the root unknown-verb misuse path with passthrough fallback while preserving misuse handling for unknown pre-verb `aong` flags and bad override arguments.
- [x] 1.4 Add a runtime/current-command-list test that proves every current `ao` top-level command is reachable through `aong` unless explicitly overridden.

## 2. Overrides, verbose output, help, and doctor

- [x] 2.1 Move overridden verbs into an explicit table and use it for registration, help, and routing decisions.
- [x] 2.2 Add global pre-verb `--verbose` handling for passthrough, wrapping overrides, and divergent overrides.
- [x] 2.3 Implement deliberate `aong pause` guidance without aliasing it to `drain`.
- [x] 2.4 Update `aong --help`, passthrough help, and override help behavior.
- [x] 2.5 Implement `aong doctor` as `ao doctor` plus loaded `ao-web.service` and `ao-tmux.service` health checks.

## 3. Documentation and validation

- [x] 3.1 Update operator/agent-facing docs and embedded skill content from `ao` to `aong` where they instruct a human or agent what to type.
- [x] 3.2 Preserve machine entrypoints as direct `ao` calls, including service `ExecStart`, harness hooks, `pty-host`, and other hidden/internal invocations.
- [x] 3.3 Run OpenSpec validation for `complete-aong-ao-surface`.
- [x] 3.4 Run the relevant Go test package and the repo pre-push gate before pushing.
