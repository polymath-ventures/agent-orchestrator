#!/usr/bin/env bash
#
# agent-ci.sh - repo-owned entrypoint for @redwoodjs/agent-ci. The upstream
# Linux default stores durable runner state under /tmp; this wrapper pins this
# repo's default under the invoking account's cache directory instead.
set -euo pipefail

need() {
	command -v "$1" >/dev/null 2>&1 || {
		echo "error: '$1' is required for agent-ci but was not found on PATH" >&2
		exit 1
	}
}

need git
need npx

root="$(git rev-parse --show-toplevel)"
cd "$root"

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)"
source "$script_dir/agent-ci-workdir.sh"

if [ -n "${AGENT_CI_WORKING_DIR:-}" ]; then
	mkdir -p "$AGENT_CI_WORKING_DIR"
	resolved_workdir="$(cd "$AGENT_CI_WORKING_DIR" && pwd -P)"
elif agent_ci_should_export_default_workdir; then
	export AGENT_CI_WORKING_DIR="$(agent_ci_default_workdir)"
	mkdir -p "$AGENT_CI_WORKING_DIR"
	resolved_workdir="$(cd "$AGENT_CI_WORKING_DIR" && pwd -P)"
else
	resolved_workdir=""
fi

if [ -n "$resolved_workdir" ]; then
	case "$resolved_workdir" in
		"/tmp"|"/tmp/"*|"/var/tmp"|"/var/tmp/"*)
			echo "warning: AGENT_CI_WORKING_DIR resolves under temporary storage: $resolved_workdir" >&2
			echo "warning: npm run agent-ci:clean will refuse to manage that location" >&2
			;;
	esac
fi

if [ "$#" -eq 0 ]; then
	set -- run --all
fi

exec npx @redwoodjs/agent-ci "$@"
