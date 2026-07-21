#!/usr/bin/env bash
# Deploy Agent Orchestrator to this host (fork-only, headless topology).
#
# What it does, in order: build an immutable release directory from a git ref,
# flip the ~/.ao/deploy/current symlink, install the daemon binary, sync
# systemd units (drop-ins in *.service.d/ are never touched), restart
# services, and verify the live system. One flag rolls back to the previous
# release.
#
# Deliberately small. The predecessor repo grew a 2,250-line deploy script;
# every line here should stay justifiable to an operator reading it once.
#
# Usage:
#   ops/deploy.sh [ref]      deploy ref (default: origin/main)
#   ops/deploy.sh --rollback re-flip to the previous release and restart
#
# Not handled (on purpose): release pruning (rm -rf old dirs by hand),
# multi-host, database backup/migration control (the daemon migrates on boot).
set -euo pipefail

DEPLOY_ROOT="${AO_DEPLOY_ROOT:-$HOME/.ao/deploy}"
RELEASES="$DEPLOY_ROOT/releases"
CURRENT="$DEPLOY_ROOT/current"
PREVIOUS_FILE="$DEPLOY_ROOT/previous-release"
LOG_FILE="$DEPLOY_ROOT/agent-orchestrator.deploy.log"
LAST_FILE="$DEPLOY_ROOT/agent-orchestrator.last-deployed"
BIN_TARGET="$HOME/.local/bin/ao"
UNIT_DIR="${AO_UNIT_DIR:-$HOME/.config/systemd/user}"
SERVICES=(ao-web.service ao.service)

log() { printf '%s %s\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)" "$*" | tee -a "$LOG_FILE"; }
die() { log "FATAL: $*"; exit 1; }

repo_root() { cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd; }

restart_and_verify() {
  systemctl --user daemon-reload
  systemctl --user restart "${SERVICES[@]}"
  log "Services restarted: ${SERVICES[*]}"

  # The daemon writes running.json (pid/port) once it has bound its port.
  local run_file="${AO_RUN_FILE:-$HOME/.ao/running.json}" port=""
  for _ in $(seq 1 30); do
    port="$(python3 -c 'import json,sys;print(json.load(open(sys.argv[1]))["port"])' "$run_file" 2>/dev/null || true)"
    if [ -n "$port" ] && curl -fsS -m 3 "http://127.0.0.1:$port/healthz" >/dev/null 2>&1; then
      break
    fi
    port=""
    sleep 2
  done
  [ -n "$port" ] || die "daemon healthz never came up (run file: $run_file)"
  log "Daemon healthy on 127.0.0.1:$port."

  # ao-web serves the shell and proxies the daemon on the same origin.
  curl -fsS -m 5 "http://127.0.0.1:5173/healthz" >/dev/null || die "ao-web proxy is not serving /healthz"
  local public_url
  public_url="$(systemctl --user show ao-web.service -p Environment | tr ' ' '\n' | sed -n 's/^AO_WEB_PUBLIC_URL=//p' | head -1)"
  if [ -n "$public_url" ]; then
    local code
    code="$(curl -s -m 10 -o /dev/null -w '%{http_code}' "$public_url/" || true)"
    [ "$code" = "200" ] || die "public URL $public_url returned HTTP ${code:-unreachable}"
    log "Public URL $public_url returned HTTP 200."
  else
    log "WARN: AO_WEB_PUBLIC_URL not set on ao-web.service; skipping public check."
  fi

  # Boot-log gate: surface WARN/ERROR/FATAL from the fresh boot; never bury it.
  sleep 5
  local noise
  noise="$(journalctl --user -u ao.service --since '30 seconds ago' --no-pager 2>/dev/null \
    | grep -iE 'level=(WARN|ERROR|FATAL)' | grep -v 'DeprecationWarning' || true)"
  if [ -n "$noise" ]; then
    log "Boot-log findings (review before walking away):"
    printf '%s\n' "$noise" | tee -a "$LOG_FILE"
  else
    log "Boot log clean."
  fi
}

sync_units() {
  local rel="$1" unit
  mkdir -p "$UNIT_DIR"
  for unit in "$rel"/systemd/*.service "$rel"/systemd/*.timer; do
    [ -e "$unit" ] || continue
    if ! cmp -s "$unit" "$UNIT_DIR/$(basename "$unit")"; then
      cp "$unit" "$UNIT_DIR/$(basename "$unit")"
      log "Unit updated: $(basename "$unit")"
    fi
  done
}

rollback() {
  local prev
  prev="$(cat "$PREVIOUS_FILE" 2>/dev/null || true)"
  if [ -z "$prev" ] || [ ! -d "$prev" ]; then
    die "no previous release recorded at $PREVIOUS_FILE"
  fi
  log "Rolling back to $prev"
  ln -sfn "$prev" "$CURRENT"
  install -m 755 "$prev/bin/ao" "$BIN_TARGET"
  cat "$prev/REVISION" > "$LAST_FILE"
  restart_and_verify
  log "Rollback complete: $(cat "$prev/REVISION")"
}

deploy() {
  local ref="${1:-origin/main}" root sha ts rel prev_target
  root="$(repo_root)"
  git -C "$root" fetch origin --quiet
  sha="$(git -C "$root" rev-parse --verify "$ref^{commit}")" || die "cannot resolve ref: $ref"
  if [ "$(cat "$LAST_FILE" 2>/dev/null || true)" = "$sha" ]; then
    log "NOTE: $sha is already the deployed revision; redeploying anyway."
  fi
  ts="$(date -u +%Y%m%d%H%M%S)"
  rel="$RELEASES/$sha-$ts-$$"
  mkdir -p "$rel/bin" "$rel/systemd"
  log "Deploying $ref ($sha) from $root"

  git clone -q --no-hardlinks "$root" "$rel/source"
  git -C "$rel/source" checkout -q "$sha"
  echo "$sha" > "$rel/REVISION"
  git -C "$root" remote get-url origin > "$rel/SOURCE_REPO"
  git -C "$root" rev-parse "$sha:frontend" > "$rel/FRONTEND_TREE"
  cp "$rel"/source/ops/ao*.service "$rel"/source/ops/ao*.timer "$rel/systemd/" 2>/dev/null || true

  log "Building backend."
  (cd "$rel/source/backend" && go build -trimpath -o "$rel/bin/ao" ./cmd/ao)
  log "Building web bundle."
  (cd "$rel/source" && npm --prefix frontend ci --silent && npm --prefix frontend run build:web --silent) >/dev/null
  [ -f "$rel/source/frontend/dist/index.html" ] || die "web bundle missing after build"

  # Record the outgoing release for --rollback, then flip atomically-enough.
  prev_target="$(readlink -f "$CURRENT" 2>/dev/null || true)"
  if [ -n "$prev_target" ]; then
    echo "$prev_target" > "$PREVIOUS_FILE"
  fi
  ln -sfn "$rel" "$CURRENT"
  echo "$sha" > "$LAST_FILE"
  install -m 755 "$rel/bin/ao" "$BIN_TARGET"
  sync_units "$rel"
  log "Flipped current -> $rel; installed $BIN_TARGET."

  restart_and_verify
  log "Deploy complete: $sha"
}

case "${1:-}" in
  --rollback) rollback ;;
  --help|-h) sed -n '2,18p' "${BASH_SOURCE[0]}"; exit 0 ;;
  *) deploy "${1:-origin/main}" ;;
esac
