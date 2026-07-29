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
#
# `--require-base` makes a missing base ref one of those undecidable cases. The
# two consumers genuinely want different behaviour from the same condition, so
# the policy is the caller's and the derivation stays here:
#
#   - Prettier (format-check.sh) degrades gracefully. With no base ref it checks
#     the working tree alone; missing a few committed files is a small, visible
#     miss, and the gate still works offline.
#   - The Go predicate must NOT degrade. A branch whose backend changes are
#     already committed has a clean working tree, so without the committed half
#     the changed set looks empty and the expensive stages get skipped on a
#     branch that may not even compile. It passes --require-base so that case is
#     reported as undecidable and the stages run.
set -euo pipefail

require_base=0
if [ "${1:-}" = "--require-base" ]; then
	require_base=1
fi

cd "$(git rev-parse --show-toplevel)"

# Derive the default branch from the remote HEAD — never assume "main".
default_ref="$(git symbolic-ref --quiet refs/remotes/origin/HEAD 2>/dev/null || echo refs/remotes/origin/main)"
base="${default_ref#refs/remotes/}" # e.g. origin/main

# Committed vs the merge-base with the default branch — the exact set CI checks.
# Absent locally (not fetched, a single-branch or shallow clone, a fresh
# throwaway repo) it is either skipped or fatal, per --require-base above.
if git rev-parse --verify --quiet "$base" >/dev/null; then
	git diff --name-only -z "${base}...HEAD"
elif [ "$require_base" -eq 1 ]; then
	echo "base ref '$base' is not present locally; the committed changed set is undecidable" >&2
	exit 3
fi

# Tracked working-tree + staged edits not yet committed.
git diff --name-only -z HEAD
