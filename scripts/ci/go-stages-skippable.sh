#!/usr/bin/env bash
#
# go-stages-skippable.sh — decides whether the local pre-push gate's Go stages
# (gofmt, build, vet, `go test -race`, golangci-lint) can be skipped for this
# branch. `go test -race ./...` alone is the better part of five minutes on this
# module, so running it for a branch that changed only markdown buys nothing and
# has, in practice, killed reviewer sessions mid-suite.
#
# Only the LOCAL gate is scoped. Remote CI stays unconditional: it is the real
# gate, it runs on fresh machines, and it is not what churns a developer's build
# cache.
#
#     exit 0        = the Go stages may be skipped   (reason on stdout)
#     exit non-zero = run them                       (reason on stdout)
#
# The inversion is deliberate and load-bearing. The only path that reaches
# `exit 0` is the one that walked the entire changed set and positively found
# nothing Go-relevant. Everything else — an underivable changed set, a mktemp
# failure, a `set -e` abort, this script being absent or unreadable — exits
# non-zero and therefore RUNS the stages. Failing safe is a structural property
# of the convention rather than something each caller has to remember. The
# obvious spelling (`exit 0` = run) would invert exactly that: any crash would
# silently skip the gate's most expensive and most load-bearing stage, and a
# skipped race suite looks identical to a passing one in the output.
#
# Trigger set: `backend/` and `scripts/ci/`. Everything the Go build reads lives
# under backend/ — sources, go.mod/go.sum, sqlc.yaml, the .sql and testdata
# fixtures, and all six //go:embed roots — so a leading-segment match on backend/
# is a strict superset of enumerating those inputs, and stays correct when a
# seventh embed root is added. scripts/ci/ is included so a change to the gate
# itself is exercised by the gate.
set -euo pipefail

# Resolve the sibling helper relative to THIS script, before cd'ing anywhere: the
# repo being inspected is not necessarily the repo this script lives in (the
# behavioral tests run it against throwaway repos that have no scripts/ci/).
script_dir="$(cd "$(dirname "$0")" && pwd)"

cd "$(git rev-parse --show-toplevel)"

# Write to a temp file rather than reading from a process substitution: a process
# substitution runs asynchronously outside `pipefail`, so a failing `git diff`
# would be swallowed and read as an empty changed set — i.e. "nothing changed,
# skip the suite", the precise failure this script must never produce.
tmp="$(mktemp)"
trap 'rm -f "$tmp"' EXIT

if ! bash "$script_dir/changed-files.sh" >"$tmp"; then
	echo "could not determine the changed set"
	exit 2
fi

# `case` with a `backend/*` pattern anchors to the leading path segment. A
# substring match would drag docs/backend-notes.md and frontend/backend-client.ts
# into the trigger set and re-introduce the waste this script removes.
#
# Redirect the loop's input (`done <"$tmp"`) instead of piping into it, so the
# loop body runs in the current shell and `exit` exits the script.
while IFS= read -r -d '' f; do
	case "$f" in
	backend/* | scripts/ci/*)
		echo "changed: $f"
		exit 1
		;;
	esac
done <"$tmp"

echo "no changed paths under backend/ or scripts/ci/"
exit 0
