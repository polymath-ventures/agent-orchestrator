## 1. Pin the namespace-label contract with failing tests

- [x] 1.1 Add domain tests for safe readable-label normalization, fallback behavior, separators, and length boundaries
- [x] 1.2 Add storage migration and round-trip tests for an immutable session namespace key, including empty legacy rows
- [x] 1.3 Add spawn-order tests proving the key is persisted before workspace, branch, or runtime creation

## 2. Persist one immutable namespace key

- [x] 2.1 Add a backward-compatible SQLite migration and sqlc fields for the session namespace key without rewriting existing rows
- [x] 2.2 Compute one namespace key from the creation-time daemon display/work context plus the complete session ID
- [x] 2.3 Persist the key on the seed row and fail spawn before external resource creation when persistence fails

## 3. Adopt the key across external namespaces

- [x] 3.1 Wire AO-generated worker root branches to the namespace key while preserving explicit branches and sibling-PR attribution
- [x] 3.2 Wire managed worker workspace paths to the namespace key while preserving legacy restore paths and Orc/Prime singleton rules
- [x] 3.3 Wire tmux create/lookup/attach/restart/destroy to the same stored namespace key and strengthen canonicalization boundary tests
- [x] 3.4 Verify Claude project-directory separation transitively through the readable workspace path without adapter-specific slug duplication

## 4. Compatibility and end-to-end verification

- [x] 4.1 Prove legacy sessions restore their existing workspace, runtime handle, and branch without synthesizing or migrating a new label
- [x] 4.2 Prove display-name renames do not mutate the namespace key or move live resources
- [x] 4.3 Exercise new worker creation against real git worktrees and tmux, confirming readable labels and collision-safe distinctness
- [x] 4.4 Update `docs/session-identity.md` and the `session-identity` delta that archives into the canonical spec to match final derivations
- [x] 4.5 Run sqlc/API generation only if their source contracts change, then run focused race tests and `npm run ci-local`

## 5. Review and landing

- [ ] 5.1 Rebase onto current `main`, push, open the implementation PR, and complete independent final review
- [ ] 5.2 After merge, archive `readable-session-namespace-labels`, close GH #255 / bead `ao-wmh`, and deploy/verify if production code changed
