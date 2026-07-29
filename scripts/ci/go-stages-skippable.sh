#!/usr/bin/env bash
#
# go-stages-skippable.sh — decides whether the local pre-push gate's Go stages
# (gofmt, build, vet, `go test -race ./...`, golangci-lint) can be skipped for
# this branch. `go test -race ./...` alone is the better part of five minutes on
# this module, so running it for a branch that changed only markdown buys nothing
# and has, in practice, killed reviewer sessions mid-suite.
#
# Only the LOCAL gate is scoped. Remote CI stays unconditional: it is the real
# gate, it runs on fresh machines, and it is not what churns a developer's build
# cache.
#
#     exit 0        = the Go stages may be skipped   (reason on stdout)
#     exit non-zero = run them                       (reason on stdout)
#
# The inversion is deliberate and load-bearing. The only path that reaches
# `exit 0` is the one that positively established the branch changes nothing
# Go-relevant. Everything else — a missing base ref, an unusable revision, a
# `set -e` abort, this script being absent or unreadable — exits non-zero and
# therefore RUNS the stages. Failing safe is a structural property of the
# convention rather than something each caller has to remember. The obvious
# spelling (`exit 0` = run) would invert exactly that: any crash would silently
# skip the gate's most expensive stage, and a skipped race suite looks identical
# to a passing one in the output.
set -euo pipefail

# Resolve then cd, rather than `cd "$(git rev-parse --show-toplevel)"`. A command
# substitution that fails inside a command's ARGUMENTS does not trip `set -e`,
# and `cd ""` is a no-op that returns 0 — so the one-liner would quietly carry on
# in whatever directory it started in. As a top-level assignment the failure is
# caught. (Today the downstream git calls would fail anyway and land on "run",
# but that is luck, and this file's whole contract is that fail-safe is
# structural.)
repo_root="$(git rev-parse --show-toplevel)"
cd "$repo_root"

# Everything the Go build reads lives under backend/ — sources, go.mod/go.sum,
# sqlc.yaml, the .sql and testdata fixtures, and every //go:embed root — so
# naming the directory is a strict superset of enumerating those inputs and stays
# correct when another embed root is added. scripts/ci is included so a change to
# the gate itself is exercised by the gate. go.work/go.work.sum are the only Go
# inputs that would sit outside backend/: the toolchain searches parent
# directories for a workspace file, so one added at the root changes module
# selection for a build run from backend/. None exists today.
#
# These are git pathspecs, not string matching, which is what keeps this honest:
# git decides what "under backend/" means, so deletions, renames, and paths
# containing spaces, quotes, or newlines are all handled by the tool that owns
# the question. A constant, non-empty array is safe under `set -u` on bash 3.2.
go_paths=(backend scripts/ci go.work go.work.sum)

# Derive the default branch from the remote HEAD, and if that is unknown treat
# the base as undecidable rather than guessing. Falling back to a hardcoded
# refs/remotes/origin/main is the same mistake as treating a missing base ref as
# an empty diff: on a repo whose default is `master`, a stale origin/main would
# silently become the comparison point and skip committed backend changes.
# origin/HEAD is genuinely often unset (it is a local convenience ref that fetch
# does not maintain), so this is reachable — and the cost of being wrong is a
# skipped build, while the cost of bailing out is one extra honest gate run.
#
# Keep the ref FULLY QUALIFIED: the short form `origin/main` is ambiguous,
# because refs/heads/ is searched before refs/remotes/, so a local branch
# literally named `origin/main` shadows the remote-tracking ref. Git resolves it
# to the local one with only a warning on stderr and exit 0 — which, if that
# branch points at HEAD, makes both diffs empty and skips the build on committed
# backend changes. refs/remotes/origin/... resolves to exactly one ref.
if ! base="$(git symbolic-ref --quiet refs/remotes/origin/HEAD 2>/dev/null)"; then
	echo "refs/remotes/origin/HEAD is not set, so the default branch is unknown;" \
		"run 'git remote set-head origin -a' to let this gate scope itself"
	exit 4
fi

# A missing base ref is undecidable, not "nothing changed". Without it the
# committed half of the branch is invisible, and a branch whose Go changes are
# already committed has a clean working tree — so treating this as an empty diff
# would skip the build on code that may not compile. Prettier's own gate
# deliberately degrades to the working tree here, because missing a few committed
# files is a small and visible miss; a skip-the-build decision cannot borrow that
# tolerance.
if ! git rev-parse --verify --quiet "$base" >/dev/null; then
	echo "base ref '$base' is not present locally; cannot tell what this branch changed"
	exit 2
fi

# Committed on this branch, then tracked working-tree and staged edits.
#
# These MUST be two separate top-level assignments. Grouping them into one
# `changed="$( git diff …; git diff … )"` silently breaks the fail-safe: `set -e`
# does not abort on a non-final failure inside a command substitution, and the
# substitution takes the status of its LAST command — so a first diff that died
# (`fatal: … no merge base`, on a base ref that exists but shares no history)
# would be swallowed, the result would read as empty, and the Go stages would be
# skipped. A top-level assignment whose substitution fails IS governed by
# `set -e`, so each git failure aborts the script and the caller sees non-zero.
committed="$(git diff --name-only "${base}...HEAD" -- "${go_paths[@]}")"
worktree="$(git diff --name-only HEAD -- "${go_paths[@]}")"

# First line via parameter expansion rather than a pipeline: `head` in a pipeline
# under `pipefail` can SIGPIPE the producing `git` and turn a successful check
# into a failure. Checked separately so neither has to be concatenated, which
# would reintroduce the question of which half a leading blank line came from.
if [ -n "$committed" ]; then
	echo "changed: ${committed%%$'\n'*}"
	exit 1
fi
if [ -n "$worktree" ]; then
	echo "changed: ${worktree%%$'\n'*}"
	exit 1
fi

echo "no changed paths under backend/, scripts/ci/, go.work, or go.work.sum"
exit 0
