#!/usr/bin/env bash
#
# ci-local.sh — one local pre-push gate that mirrors the remote CI `format` and
# `lint` jobs (plus build / vet / test / typecheck), so violations are caught
# locally instead of on a wasted remote CI round-trip. Run it before every push:
#
#     npm run ci-local
#
# Enable it automatically as a git pre-push hook with `npm run hooks:install`.
#
# Checks run cheapest-first and fail fast: the first failing check aborts the
# gate, and that tool's own output explains what to fix.
set -euo pipefail

cd "$(git rev-parse --show-toplevel)"

# golangci-lint is fetched and built on demand via `go run ...@v2.12.2` (see the
# `lint` npm script), so the toolchain the gate actually needs is Go plus Node —
# not a separately-installed golangci binary. Fail with a clear message rather
# than a confusing mid-run error if either is missing.
need() {
	command -v "$1" >/dev/null 2>&1 || {
		echo "error: '$1' is required for the local CI-parity gate but was not found on PATH" >&2
		exit 1
	}
}
need go
need node
need npx

run() {
	printf '\n== %s ==\n' "$1"
	shift
	"$@"
}

# 1. Prettier format parity (.github/workflows/prettier.yml) — changed files only.
run "format (prettier, changed files)" bash scripts/ci/format-check.sh

# 2-4. Backend build-test job (.github/workflows/go.yml): gofmt, build, vet.
run "gofmt" bash -c 'cd backend && unformatted=$(gofmt -l .); if [ -n "$unformatted" ]; then echo "these files need gofmt:"; echo "$unformatted"; exit 1; fi'
run "go build" bash -c 'cd backend && go build ./...'
run "go vet" bash -c 'cd backend && go vet ./...'

# 5. Lint job (.github/workflows/go.yml) + go test. `npm run lint` is the single
# source of the golangci-lint pin (v2.12.2, matching the CI action) and points
# golangci at a per-worktree cache so absolute paths cached by a sibling worktree
# (later removed) can't leak stale gen/*.sql.go issues — the noise CI never sees
# because it starts from an empty cache. Kept single-sourced so it never drifts.
run "go test + golangci-lint (npm run lint)" npm run lint

# 6. Frontend typecheck (.github/workflows/frontend.yml).
run "frontend typecheck" npm run frontend:typecheck

printf '\n\xe2\x9c\x93 local CI-parity gate passed\n'
