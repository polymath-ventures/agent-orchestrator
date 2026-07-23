#!/usr/bin/env bash
#
# format-check.sh — local mirror of the remote Prettier "format" CI job
# (.github/workflows/prettier.yml). Runs `prettier@3 --check --ignore-unknown`
# over the files this branch changes, using the same command shape as CI so a
# format violation is caught locally instead of on a wasted remote round-trip.
#
# Prettier honors .prettierignore (generated files, and backend/ which uses
# gofmt), so this stays quiet on exactly the files CI also skips.
#
# Changed set = files committed on this branch relative to the merge-base with
# the default branch (the exact set CI checks) UNION tracked working-tree/staged
# edits (so the gate also catches unformatted files before they are committed).
# Untracked scratch files are excluded to keep the gate quiet.
set -euo pipefail

cd "$(git rev-parse --show-toplevel)"

# Derive the default branch from the remote HEAD — never assume "main".
default_ref="$(git symbolic-ref --quiet refs/remotes/origin/HEAD 2>/dev/null || echo refs/remotes/origin/main)"
base="${default_ref#refs/remotes/}" # e.g. origin/main

changed_files() {
	# Committed vs the merge-base with the default branch — the exact set CI
	# checks. Skipped when the base ref is not present locally (e.g. not fetched,
	# or a fresh throwaway repo) so the gate still works offline.
	if git rev-parse --verify --quiet "$base" >/dev/null; then
		git diff --name-only -z --diff-filter=d "${base}...HEAD"
	fi
	# Tracked working-tree + staged edits not yet committed.
	git diff --name-only -z --diff-filter=d HEAD
}

# sort -zu de-duplicates the union; --no-run-if-empty means "no changed files"
# is a clean pass. pipefail propagates a non-zero prettier exit as our exit.
changed_files | sort -zu | xargs -0 --no-run-if-empty npx --yes prettier@3 --check --ignore-unknown
