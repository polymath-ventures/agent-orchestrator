#!/usr/bin/env bash
#
# go-stages-skippable.sh — may the local pre-push gate skip its Go stages?
#
#     exit 0        = yes, skippable   (reason on stdout)
#     exit non-zero = no, run them     (reason on stdout)
#
# The inversion is load-bearing. `exit 0` is reached only after positively
# establishing the branch changes nothing Go-relevant; every other outcome,
# including any internal failure, runs the stages. That makes failing safe
# structural rather than something each caller remembers — which matters because
# a skipped race suite and a passing one look identical in the output.
#
# Only the LOCAL gate is scoped; remote CI is unconditional and remains the real
# gate. See docs/local-ci.md for the rationale, the limits of "touches", and the
# defects this shape exists to avoid. Behaviour is pinned by
# ops/ci-go-scope.test.mjs — change it there first.
set -euo pipefail

# Resolve before cd: a substitution that fails inside a command's arguments does
# not trip `set -e`, and `cd ""` succeeds.
repo_root="$(git rev-parse --show-toplevel)"
cd "$repo_root"

# Git pathspecs, so git decides what "under backend/" means — deletions, renames,
# and awkward filenames need no handling here. Every Go input lives under
# backend/; go.work* are the only ones that could sit outside it, since the
# toolchain searches parent directories for a workspace file.
go_paths=(backend scripts/ci go.work go.work.sum)

# Fully qualified, and never guessed. The short form `origin/main` is ambiguous
# (refs/heads/ is searched first, so a local branch of that name shadows the
# remote-tracking ref, with only a warning). An unset origin/HEAD means the
# default branch is unknown, which is undecidable, not "main".
if ! base="$(git symbolic-ref --quiet refs/remotes/origin/HEAD 2>/dev/null)"; then
	echo "refs/remotes/origin/HEAD is not set, so the default branch is unknown;" \
		"run 'git remote set-head origin -a' to let this gate scope itself"
	exit 4
fi

if ! git rev-parse --verify --quiet "$base" >/dev/null; then
	echo "base ref '$base' is not present locally; cannot tell what this branch changed"
	exit 2
fi

# Two separate assignments, deliberately. Grouped into one substitution, `set -e`
# would not abort on the first command's failure and the result would take the
# last command's status — so a failed diff would read as "no changes" and skip.
committed="$(git diff --name-only "${base}...HEAD" -- "${go_paths[@]}")"
worktree="$(git diff --name-only HEAD -- "${go_paths[@]}")"

# First line via parameter expansion; `head` in a pipeline under `pipefail` can
# SIGPIPE the producing git.
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
