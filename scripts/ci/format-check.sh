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
# The changed set comes from scripts/ci/changed-files.sh, which is shared with
# the Go-stage scope predicate so the two stages can never disagree about what
# this branch touches.
set -euo pipefail

# Resolve the sibling helper relative to THIS script, before cd'ing anywhere —
# the behavioral tests run this gate against throwaway repos that contain no
# scripts/ci/ of their own.
script_dir="$(cd "$(dirname "$0")" && pwd)"

cd "$(git rev-parse --show-toplevel)"

# Collect the NUL-delimited names into an array. This stays portable to macOS's
# stock bash 3.2 / BSD userland (no GNU `sort -z`, no `xargs --no-run-if-empty`).
# Duplicates across the two diff sets are harmless — Prettier just checks a file
# twice.
#
# Write the helper's output to a temp file first (not a `< <(...)` process
# substitution): a process substitution runs asynchronously outside `pipefail`,
# so a failing `git diff` would be swallowed into a false pass. A plain
# redirection of a `set -euo pipefail` child is covered by `set -e` here, so a
# producer failure still aborts the gate.
# Track the count in a scalar `n` and expand `${files[@]}` only when it is
# non-zero, because bash 3.2 treats an empty array as unset under `set -u`.
tmp="$(mktemp)"
trap 'rm -f "$tmp"' EXIT
bash "$script_dir/changed-files.sh" >"$tmp"

# Skip paths that no longer exist in the working tree — deleted files, and a file
# the committed set (base...HEAD) lists that a LATER uncommitted change renamed
# or removed. Prettier treats a missing path as an error rather than a no-op, so
# without this the gate fails on any branch that deletes a file, or renames one
# it also added. This is the only place that filtering belongs: the shared
# changed-set helper deliberately reports deletions, because the Go-stage scope
# predicate has to compile a branch that removed a package.
files=()
n=0
while IFS= read -r -d '' f; do
	# -L as well as -e: a dangling symlink is "nonexistent" to -e, but remote
	# Prettier still rejects it, and silently skipping it would break the CI
	# parity this gate exists to provide.
	[ -e "$f" ] || [ -L "$f" ] || continue
	files+=("$f")
	n=$((n + 1))
done <"$tmp"

[ "$n" -eq 0 ] && exit 0

# The `--` terminator stops option parsing so a changed file whose name looks
# like a flag (e.g. `--write`) is treated as a path, never a Prettier option —
# without it Prettier silently ignores the name and skips the check.
npx --yes prettier@3 --check --ignore-unknown -- "${files[@]}"
