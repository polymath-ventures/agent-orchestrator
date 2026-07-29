#!/usr/bin/env bash
#
# changed-files.sh — the single source of "what does this branch change?", used
# by every scoped stage of the local pre-push gate: format-check.sh (which files
# does Prettier check) and go-stages-skippable.sh (do the Go stages need to run
# at all). Both need the same set, so it lives in one place; two copies that have
# to agree would eventually disagree.
#
# Changed set = files committed on this branch relative to the merge-base with
# the default branch (the exact set CI checks) UNION tracked working-tree/staged
# edits (so the gate also reflects changes before they are committed). Untracked
# scratch files are excluded to keep the gate quiet.
#
# Output: NUL-delimited repo-relative paths on stdout. Duplicates across the two
# diff sets are possible and harmless — every consumer is idempotent per path.
#
# Deletions ARE included. An earlier `--diff-filter=d` lived here when Prettier
# was the only consumer, but a branch that only deletes a backend package still
# has to be compiled, so filtering deletions out would let the Go stages be
# skipped on a change that breaks the build. Prettier's need is narrower — it
# cannot check a path that no longer exists — so that filtering belongs in
# format-check.sh, which already drops nonexistent paths for the rename case.
#
# Exit status is load-bearing: a non-zero exit means the changed set could not be
# derived (no commits yet, unreachable base ref on a shallow clone). Consumers
# must treat that as "unknown", never as "nothing changed".
set -euo pipefail

cd "$(git rev-parse --show-toplevel)"

# Derive the default branch from the remote HEAD — never assume "main".
default_ref="$(git symbolic-ref --quiet refs/remotes/origin/HEAD 2>/dev/null || echo refs/remotes/origin/main)"
base="${default_ref#refs/remotes/}" # e.g. origin/main

# Committed vs the merge-base with the default branch — the exact set CI checks.
# Skipped when the base ref is not present locally (e.g. not fetched, or a fresh
# throwaway repo) so the gate still works offline.
if git rev-parse --verify --quiet "$base" >/dev/null; then
	git diff --name-only -z "${base}...HEAD"
fi

# Tracked working-tree + staged edits not yet committed.
git diff --name-only -z HEAD
