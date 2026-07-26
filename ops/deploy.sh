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
# aong must land beside ao: it resolves its sibling before PATH so the pair that
# was built together stays together.
AONG_BIN_TARGET="$HOME/.local/bin/aong"
UNIT_DIR="${AO_UNIT_DIR:-$HOME/.config/systemd/user}"
SERVICES=(ao-web.service ao.service)

log() { printf '%s %s\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)" "$*" | tee -a "$LOG_FILE"; }
die() { log "FATAL: $*"; exit 1; }

# Every dependency is checked BEFORE any mutation: discovering a missing tool
# mid-deploy would leave the system half-flipped with a misleading error.
preflight() {
  local dep
  for dep in git go npm curl python3 systemctl journalctl cmp install; do
    command -v "$dep" >/dev/null 2>&1 || die "missing dependency: $dep"
  done
}

repo_root() { cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd; }

restart_and_verify() {
  # provenance_required=0 only on rollback: a release built before the daemon
  # reported its own revision cannot answer, and blocking recovery to a
  # known-good older release is worse than the gap. Everything the artifact CAN
  # prove is still enforced there. Self-healing: once every release in the
  # rollback window postdates the field, rollback is strict again.
  local expected_sha="$1" provenance_required="${2:-1}"
  systemctl --user daemon-reload
  systemctl --user restart "${SERVICES[@]}"
  log "Services restarted: ${SERVICES[*]}"

  # The daemon writes running.json (pid/port) once it has bound its port.
  # Identity, not just liveness: the healthz responder must be the daemon this
  # deploy installed (executablePath) and the process the run file names (pid)
  # — a stale or foreign daemon on the same port must not satisfy the gate.
  #
  # executablePath proves WHICH FILE is answering; buildRevision proves WHAT
  # THAT FILE IS. Without the second, a stale binary left at the expected path
  # satisfies every other check, which is the failure this gate exists to catch.
  #
  # Checks raise instead of assert: python -O strips assert statements, which
  # would silently void the whole gate if PYTHONOPTIMIZE ever reached this
  # environment. stdout carries only the port, so any non-numeric output is a
  # failure explanation worth surfacing rather than discarding.
  local run_file="${AO_RUN_FILE:-$HOME/.ao/running.json}" port="" probe="" last_err=""
  for _ in $(seq 1 30); do
    probe="$(python3 - "$run_file" "$BIN_TARGET" "$expected_sha" "$provenance_required" <<'PY' 2>&1 || true
import json, sys, urllib.request

run_file, bin_target, expected = sys.argv[1], sys.argv[2], sys.argv[3]
provenance_required = sys.argv[4] == "1"

def die(msg):
    raise SystemExit(msg)

run = json.load(open(run_file))
health = json.load(urllib.request.urlopen(f"http://127.0.0.1:{run['port']}/healthz", timeout=3))

if health.get("status") != "ok":
    die(f"healthz reports status={health.get('status')!r}, want ok")
if health.get("pid") != run["pid"]:
    die(f"healthz pid {health.get('pid')} != run-file pid {run['pid']}")
if health.get("executablePath") != bin_target:
    die(f"responder is {health.get('executablePath')}, not {bin_target}")

# Kept distinct on purpose: a daemon too old to report at all, a build whose
# provenance was never recorded, and the wrong commit send an operator to three
# different places.
revision = health.get("buildRevision")
if revision is None:
    if provenance_required:
        die("responder predates buildRevision; it cannot prove which commit it is")
    sys.stderr.write(f"WARN: responder predates buildRevision; provenance unverified against {expected}\n")
elif revision == "unknown":
    die("responder reports an unstamped build (no VCS metadata at build time, or -buildvcs=false); it cannot prove which commit it is")
elif revision != expected:
    die(f"responder was built from {revision}, not the expected {expected}")
elif health.get("buildModified") is not False:
    die(f"responder does not report a clean build of {revision} (buildModified={health.get('buildModified')!r})")

print(run["port"])
PY
)"
    # Only a bare port means every check passed; anything else is diagnostic.
    case "$probe" in
      ''|*[!0-9]*) last_err="$probe" ;;
      *) port="$probe"; break ;;
    esac
    sleep 2
  done
  [ -n "$port" ] || die "daemon healthz never verified as the installed binary at $expected_sha (run file: $run_file): ${last_err:-no response}"
  log "Daemon healthy and identity-verified on 127.0.0.1:$port (build $expected_sha)."

  # ao-web serves the shell and proxies the daemon on the same origin.
  curl -fsS -m 5 "http://127.0.0.1:5173/healthz" >/dev/null || die "ao-web proxy is not serving /healthz"
  local public_url
  public_url="$(systemctl --user show ao-web.service -p Environment | tr ' ' '\n' | sed -n 's/^AO_WEB_PUBLIC_URL=//p' | head -1)"
  if [ -n "$public_url" ]; then
    local code api_code
    code="$(curl -s -m 10 -o /dev/null -w '%{http_code}' "$public_url/" || true)"
    [ "$code" = "200" ] || die "public URL $public_url returned HTTP ${code:-unreachable}"
    # The shell loading is not enough: a browser at the public origin must be
    # able to reach the proxied daemon, which requires the daemon's CORS
    # allowlist to include this origin (AO_ALLOWED_ORIGINS).
    api_code="$(curl -s -m 10 -o /dev/null -w '%{http_code}' -H "Origin: $public_url" "$public_url/healthz" || true)"
    [ "$api_code" = "200" ] || die "browser-mode API check failed: $public_url/healthz with Origin header returned HTTP ${api_code:-unreachable} (check AO_ALLOWED_ORIGINS on the daemon)"
    log "Public URL $public_url returned HTTP 200 (shell + browser-mode API)."
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
  # aong resolves ao as its sibling, so the pair rolls back together when the
  # outgoing release has one. A release predating aong has nothing to restore;
  # say so and leave the installed binary alone. Deleting it would be a
  # destructive surprise on a path this script does not own exclusively, and
  # aong only composes ao verbs that predate it, so the skew is inert.
  if [ -f "$prev/bin/aong" ]; then
    install -m 755 "$prev/bin/aong" "$AONG_BIN_TARGET"
  else
    log "NOTE: $prev predates aong; left $AONG_BIN_TARGET untouched."
  fi
  sync_units "$prev"
  cat "$prev/REVISION" > "$LAST_FILE"
  # The rollback target's own recorded revision — the gate proves we landed on
  # the release we rolled back TO, not merely that something healthy is up.
  restart_and_verify "$(cat "$prev/REVISION")" 0
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
  local staged
  for staged in "$rel"/source/ops/ao*.service "$rel"/source/ops/ao*.timer; do
    [ -e "$staged" ] || continue
    cp "$staged" "$rel/systemd/"
  done

  log "Building backend."
  (cd "$rel/source/backend" && go build -trimpath -o "$rel/bin/ao" ./cmd/ao)
  (cd "$rel/source/backend" && go build -trimpath -o "$rel/bin/aong" ./cmd/aong)
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
  install -m 755 "$rel/bin/aong" "$AONG_BIN_TARGET"
  sync_units "$rel"
  log "Flipped current -> $rel; installed $BIN_TARGET."

  restart_and_verify "$sha" 1
  log "Deploy complete: $sha"
}

case "${1:-}" in
  --rollback) preflight; rollback ;;
  --help|-h) sed -n '2,18p' "${BASH_SOURCE[0]}"; exit 0 ;;
  *) preflight; deploy "${1:-origin/main}" ;;
esac
