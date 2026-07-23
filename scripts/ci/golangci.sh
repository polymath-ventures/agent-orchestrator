#!/usr/bin/env bash
#
# golangci.sh — the single source of the local golangci-lint invocation: the
# version pin (v2.12.2, matching the CI action in .github/workflows/go.yml) and
# the per-worktree cache. Used by both `npm run lint` and the ci-local gate so
# neither can drift from the other.
#
# The cache is pinned under the worktree (backend/.cache, already gitignored via
# the repo's `.cache/` rule) because golangci's shared user-level cache stores
# results keyed by content hash with ABSOLUTE paths: a sibling worktree with
# identical-content generated files caches its own abs paths, and after that
# worktree is removed this run gets those cache hits, can't re-read the missing
# files to apply the generated-file exclusion, and leaks stale gen/*.sql.go
# findings. CI never sees this (fresh runner = empty cache). A per-worktree cache
# makes that cross-worktree collision impossible by construction.
set -euo pipefail

root="$(git rev-parse --show-toplevel)"
export GOLANGCI_LINT_CACHE="$root/backend/.cache/golangci-lint"
cd "$root/backend"
exec go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.12.2 run --path-mode=abs "$@"
