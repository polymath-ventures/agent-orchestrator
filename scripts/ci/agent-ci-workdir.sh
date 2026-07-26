#!/usr/bin/env bash
#
# Shared workdir resolution for the repo-owned @redwoodjs/agent-ci wrapper and
# cleanup command. Source this file from scripts that already run at the repo
# root or have a valid git checkout as cwd.

agent_ci_repo_root() {
	cd "$(git rev-parse --git-common-dir)/.." && pwd -P
}

agent_ci_repo_slug() {
	basename "$(agent_ci_repo_root)"
}

agent_ci_default_workdir() {
	if [ -e /.dockerenv ]; then
		printf '%s/.agent-ci\n' "$(git rev-parse --show-toplevel)"
		return
	fi

	local default_cache_home="${XDG_CACHE_HOME:-$HOME/.cache}"
	printf '%s/agent-ci/%s\n' "$default_cache_home" "$(agent_ci_repo_slug)"
}

agent_ci_should_export_default_workdir() {
	[ ! -e /.dockerenv ]
}
