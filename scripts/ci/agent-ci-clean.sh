#!/usr/bin/env bash
#
# agent-ci-clean.sh - conservative cleanup for the repo-owned agent-ci workdir.
# Dry-run is the default; pass --force to delete the selected stale state.
set -euo pipefail

mode=dry-run
older_than_days=14
docker_root_helper=false

usage() {
	cat <<'EOF'
usage: npm run agent-ci:clean -- [--dry-run] [--force] [--older-than DAYS] [--docker-root-helper]

Prunes stale @redwoodjs/agent-ci state from AGENT_CI_WORKING_DIR, defaulting to
${XDG_CACHE_HOME:-$HOME/.cache}/agent-ci/<repo>. Active/recent runs and paused
retry state are preserved.
EOF
}

while [ "$#" -gt 0 ]; do
	case "$1" in
		--dry-run)
			mode=dry-run
			;;
		--force)
			mode=force
			;;
		--older-than)
			shift
			if [ "$#" -eq 0 ] || ! [[ "$1" =~ ^[0-9]+$ ]] || [ "$1" -lt 1 ]; then
				echo "error: --older-than requires a day count of at least 1" >&2
				exit 2
			fi
			older_than_days="$1"
			;;
		--docker-root-helper)
			docker_root_helper=true
			;;
		-h|--help)
			usage
			exit 0
			;;
		*)
			echo "error: unknown argument: $1" >&2
			usage >&2
			exit 2
			;;
	esac
	shift
done

need() {
	command -v "$1" >/dev/null 2>&1 || {
		echo "error: '$1' is required for agent-ci cleanup but was not found on PATH" >&2
		exit 1
	}
}

need git
need find

root="$(git rev-parse --show-toplevel)"
cd "$root"

repo_root="$(cd "$(git rev-parse --git-common-dir)/.." && pwd -P)"
repo_slug="$(basename "$repo_root")"
default_cache_home="${XDG_CACHE_HOME:-$HOME/.cache}"
raw_workdir="${AGENT_CI_WORKING_DIR:-$default_cache_home/agent-ci/$repo_slug}"

case "$raw_workdir" in
	""|"/"|"/tmp"|"/tmp/"*|"/var/tmp"|"/var/tmp/"*)
		echo "error: refusing to clean unsafe agent-ci workdir: $raw_workdir" >&2
		echo "set AGENT_CI_WORKING_DIR to the repo-approved cache location first" >&2
		exit 2
		;;
esac

if [ ! -d "$raw_workdir" ]; then
	printf 'agent-ci workdir: %s\n' "$raw_workdir"
	printf 'mode: %s; stale threshold: %s days\n' "$mode" "$older_than_days"
	printf '\nagent-ci workdir does not exist; nothing to clean\n'
	exit 0
fi

workdir="$(cd "$raw_workdir" && pwd -P)"

case "$workdir" in
	""|"/"|"/tmp"|"/tmp/"*|"/var/tmp"|"/var/tmp/"*)
		echo "error: refusing to clean unsafe agent-ci workdir: $workdir" >&2
		echo "set AGENT_CI_WORKING_DIR to the repo-approved cache location first" >&2
		exit 2
		;;
esac

cutoff_minutes=$((older_than_days * 24 * 60))
selected=()
preserved=()
failed=()

is_stale() {
	[ "$(find "$1" -mindepth 0 -maxdepth 0 -mmin "+$cutoff_minutes" -print)" = "$1" ]
}

has_pause_marker() {
	[ -e "$1/signals/paused" ]
}

remove_with_docker_root_helper() {
	local path="$1"
	local rel="${path#"$workdir"/}"
	[ "$rel" != "$path" ] || return 1
	[ -n "$rel" ] || return 1
	command -v docker >/dev/null 2>&1 || return 1
	docker run --rm --network none -v "$workdir:/workdir" alpine:3.20@sha256:c64c687cbea9300178b30c95835354e34c4e4febc4badfe27102879de0483b5e sh -c 'cd /workdir && rm -rf -- "$@"' sh "$rel"
}

consider_run() {
	local path="$1"
	if has_pause_marker "$path"; then
		preserved+=("$path (paused retry state)")
	elif find "$path" -mindepth 0 -mmin "-$cutoff_minutes" -print -quit | grep -q .; then
		preserved+=("$path (recent)")
	elif is_stale "$path"; then
		selected+=("$path")
	else
		preserved+=("$path (recent)")
	fi
}

consider_cache_dir() {
	local path="$1"
	[ -e "$path" ] || return 0
	if find "$path" -mindepth 0 -mmin "-$cutoff_minutes" -print -quit | grep -q .; then
		preserved+=("$path (recent cache)")
	elif is_stale "$path"; then
		selected+=("$path")
	else
		preserved+=("$path (recent cache)")
	fi
}

# Agent CI already prunes many stale run workspaces when a run starts. Keeping a
# manual, older-threshold pass here gives operators a dry-run-visible backstop
# for abandoned workdirs and interrupted hosts.
if [ -d "$workdir/runs" ]; then
	while IFS= read -r run_dir; do
		consider_run "$run_dir"
	done < <(find "$workdir/runs" -mindepth 1 -maxdepth 1 -type d | sort)
fi

for rel in \
	cache/toolcache \
	cache/npm-cache \
	cache/pnpm-store \
	cache/yarn-cache \
	cache/bun-cache \
	cache/playwright \
	cache/remote-workflows \
	cache/dtu \
	cache/node-modules-v2 \
	runner; do
	consider_cache_dir "$workdir/$rel"
done

printf 'agent-ci workdir: %s\n' "$workdir"
printf 'mode: %s; stale threshold: %s days\n' "$mode" "$older_than_days"

if [ "${#preserved[@]}" -gt 0 ]; then
	printf '\npreserved:\n'
	printf '  %s\n' "${preserved[@]}"
fi

if [ "${#selected[@]}" -eq 0 ]; then
	printf '\nno stale agent-ci state selected\n'
	exit 0
fi

printf '\nselected for cleanup:\n'
printf '  %s\n' "${selected[@]}"

if [ "$mode" = "dry-run" ]; then
	printf '\ndry-run only; pass --force to delete the selected paths\n'
	exit 0
fi

for path in "${selected[@]}"; do
	if rm -rf -- "$path" 2>/dev/null; then
		printf 'deleted: %s\n' "$path"
		continue
	fi

	if [ "$docker_root_helper" = true ] && remove_with_docker_root_helper "$path"; then
		printf 'deleted with docker root helper: %s\n' "$path"
		continue
	fi

	echo "warning: normal removal failed for $path" >&2
	echo "hint: if files are UID/GID-mapped from runner containers (for example numeric 1001), re-run with --docker-root-helper to remove only the selected relative path mounted under the agent-ci workdir." >&2
	failed+=("$path")
done

if [ "${#failed[@]}" -gt 0 ]; then
	printf '\nfailed to delete:\n' >&2
	printf '  %s\n' "${failed[@]}" >&2
	exit 1
fi
