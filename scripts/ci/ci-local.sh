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

# golangci-lint is fetched and built on demand via `go run ...@v2.12.2` (see the
# `lint` npm script), so the toolchain the gate actually needs is Go plus Node —
# not a separately-installed golangci binary. Check every tool the gate shells
# out to up front, BEFORE the first `git` call below, so a missing tool fails
# with a clear message instead of a confusing mid-run error.
need() {
	command -v "$1" >/dev/null 2>&1 || {
		echo "error: '$1' is required for the local CI-parity gate but was not found on PATH" >&2
		exit 1
	}
}
need git
need go
need node
need npm
need npx

cd "$(git rev-parse --show-toplevel)"

run() {
	printf '\n== %s ==\n' "$1"
	shift
	"$@"
}

# 1. Prettier format parity (.github/workflows/prettier.yml) — changed files only.
run "format (prettier, changed files)" bash scripts/ci/format-check.sh

# 2-5. Backend build-test job (.github/workflows/go.yml): gofmt, build, vet, and
# `go test -race ./...`. Use -race to match CI exactly — a data race that CI's
# race detector would catch must not pass this gate and waste the round-trip.
run "gofmt" bash -c 'cd backend && unformatted=$(gofmt -l .); if [ -n "$unformatted" ]; then echo "these files need gofmt:"; echo "$unformatted"; exit 1; fi'
run "go build" bash -c 'cd backend && go build ./...'
run "go vet" bash -c 'cd backend && go vet ./...'
run "go test -race" bash -c 'cd backend && go test -race ./...'

# 6. Lint job (.github/workflows/go.yml). scripts/ci/golangci.sh is the single
# source of the golangci-lint pin (v2.12.2, matching the CI action) and the
# per-worktree cache; `npm run lint` uses the same script, so they never drift.
run "golangci-lint" bash scripts/ci/golangci.sh

# 7. Frontend typecheck (.github/workflows/frontend.yml).
run "frontend typecheck" npm run frontend:typecheck

printf '\n\xe2\x9c\x93 local CI-parity gate passed\n'
