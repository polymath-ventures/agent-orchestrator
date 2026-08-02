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

# 2. The ops-units job (.github/workflows/go.yml, and again in frontend.yml).
# Node-only and seconds long, so it runs early and unconditionally: its inputs
# are the workflow and script files that the Go scope predicate below ignores,
# and the coverage for that predicate itself lives here.
run "ops tests" npm run test:ops

# 3-7. Backend build-test and lint jobs (.github/workflows/go.yml): gofmt, build,
# vet, `go test -race ./...`, and golangci-lint. Use -race to match CI exactly —
# a data race that CI's race detector would catch must not pass this gate and
# waste the round-trip.
#
# Scoped to the diff, the same way the Prettier stage above already is. These are
# the expensive stages (the race suite is minutes, not seconds), and a branch
# that changed no Go input cannot learn anything from re-running them. The
# predicate reports "skippable" only when it walked the whole changed set and
# found nothing under backend/ or scripts/ci/; every other outcome, including any
# internal failure, runs the stages. See scripts/ci/go-stages-skippable.sh.
#
# Only the LOCAL gate is scoped. Remote CI stays unconditional — it is the real
# gate, it runs on fresh machines, and it is not what churns a developer's build
# cache.
#
# `if var="$(cmd)"` is a condition context, so `set -e` does not abort on the
# non-zero (run) branch, and the variable is assigned either way, which keeps it
# safe under `set -u`.
# CI_LOCAL_FORCE_GO is set by .githooks/pre-push when the push targets a ref
# other than the checked-out HEAD, because the predicate below reasons about HEAD
# and would otherwise scope by the wrong commit's diff.
if [ -n "${CI_LOCAL_FORCE_GO:-}" ]; then
	go_scope_reason="forced: pushing a ref other than the checked-out HEAD"
	skip_go=0
elif go_scope_reason="$(bash scripts/ci/go-stages-skippable.sh)"; then
	skip_go=1
else
	skip_go=0
fi

if [ "$skip_go" = 1 ]; then
	printf '\n== go stages skipped (%s) ==\n' "${go_scope_reason:-no Go-relevant changes}"
else
	printf '\n== go stages (%s) ==\n' "${go_scope_reason:-changed set undetermined; running}"

	# -trimpath keeps absolute source paths out of compiled output. Without it
	# every worktree gets its own cache entries for all of this module's own
	# packages, which is what grows ~/.cache/go-build without bound across the
	# several worktrees that are routinely live. Dependencies already come from
	# the module cache at a fixed path and never duplicated.
	#
	# Passed per-command rather than as an exported GOFLAGS: an export would
	# reach scripts/ci/golangci.sh, which `npm run lint` shares and whose
	# per-worktree cache exists precisely to fix a stale absolute-path bug.
	run "gofmt" bash -c 'cd backend && unformatted=$(gofmt -l .); if [ -n "$unformatted" ]; then echo "these files need gofmt:"; echo "$unformatted"; exit 1; fi'
	run "go build" bash -c 'cd backend && go build -trimpath ./...'
	run "go vet" bash -c 'cd backend && go vet -trimpath ./...'
	run "go test -race" bash -c 'cd backend && go test -trimpath -race ./...'

	# scripts/ci/golangci.sh is the single source of the golangci-lint pin
	# (v2.12.2, matching the CI action) and the per-worktree cache; `npm run lint`
	# uses the same script, so they never drift.
	run "golangci-lint" bash scripts/ci/golangci.sh
fi

# 8. Frontend typecheck (.github/workflows/frontend.yml).
run "frontend typecheck" npm run frontend:typecheck

# 9. Frontend vitest unit suite (.github/workflows/frontend.yml "Run vitest
# suite"). Mirrored here because the gate typechecked the frontend but never ran
# its unit tests, so a frontend unit regression (the #220 SessionView.test.tsx
# failures) passed locally and only broke on remote CI — the wasted round-trip
# this gate exists to prevent. Vitest is fast and self-contained, so like the
# typecheck it runs unconditionally rather than behind fragile frontend-touched
# scoping, and sits after the typecheck so a cheaper type error fails first.
#
# The slower browser-mode Playwright e2e (frontend/e2e) is deliberately NOT in
# this gate — it needs a built web bundle, a Playwright browser download, and a
# free port (5173 is hardcoded in frontend/playwright.config.ts). It stays a
# manual opt-in step; see docs/local-ci.md. Remote CI (frontend.yml) runs it on
# every frontend-touching PR.
#
# The vitest suite collects frontend/src/landing/scripts/*.test.mjs, whose script
# under test imports from the landing app's own dependency tree. Remote CI has an
# explicit "Install landing dependencies" step before it runs vitest; without the
# same install the local gate fails on a fresh clone while CI is green — exactly
# the parity break this script exists to close. Installed only when absent,
# because CI restores it from cache and a fresh `npm ci` here is not cheap.
if [ ! -d frontend/src/landing/node_modules ]; then
	run "landing deps (for vitest)" npm ci --prefix frontend/src/landing
fi
run "frontend vitest" npm run frontend:test

printf '\n\xe2\x9c\x93 local CI-parity gate passed\n'
