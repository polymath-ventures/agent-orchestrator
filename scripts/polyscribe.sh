#!/usr/bin/env bash
#
# polyscribe.sh
#
# @sx-managed: polyscribe (vault) — do not edit; managed by the agent-vault hook
# @sx-managed-version: 17
#
# Assemble modular markdown primitives for committed reconciliation, global
# files, and SessionStart injection:
#
#   REPO scope (committed to repo root):
#     AGENTS.md / CLAUDE.md / GEMINI.md = minimal fail-open safety stubs
#     AGENTS.shared.md    = banner + source/*.md (no identity)               ← reference artifact
#
#   SYSTEM scope (written into $HOME, applies in EVERY repo) — universal rules:
#     ~/.codex/AGENTS.md  ~/.claude/CLAUDE.md  ~/.gemini/GEMINI.md = banner + system/*.md (full)
#
#   SESSION scope (injected; never writes tracked files):
#     current hook-bundled universal rules + repo-owned local fragments + the
#     current client identity. The installed hook refreshes through sx before
#     SessionStart and returns this assembly as additionalContext.
#
# A primitive named "<name>.ref.md" instead of "<name>.md" emits a one-line
# pointer rather than inlining its body. When an earlier module names that
# .ref.md path, the pointer is anchored at that reference site; otherwise it
# emits in sorted module order. It is flagged REF in the length report — the
# convention for "this is a short pointer that tells the agent to read a bigger
# file on demand" (context-budget escape hatch). The build reports lengths so you
# can manage what to inline vs ref.
#
# HTML comments are AUTHORING-ONLY. Any <!-- ... --> block in a source primitive
# or override (multi-line included) is stripped during assembly and never reaches
# the generated AGENTS.shared.md or injected session context. Use them for provenance, refresh
# markers (e.g. "@sx-managed: <module>", which only polypowers-init reads off the SOURCE
# file), and notes to whoever edits the fragment — none of that authoring metadata
# is agent-facing, and inlining it verbatim is worse than useless to a reading LLM.
# The one HTML comment that DOES survive is the generated banner below, because it
# is printed directly, not read from a source file.
#
# Edit agent-instructions/source|agent-overrides|system, never the generated files.
#
# Usage (toolchain-free — no npm required):
#   bash "$HOME/.claude/hooks/polyscribe/polyscribe.sh"            # Claude install: build + write the REPO files
#   bash "$HOME/.gemini/hooks/polyscribe/polyscribe.sh" --check    # Gemini install: build to temp, diff, exit 1 on drift
#   bash "$HOME/.claude/hooks/polyscribe/polyscribe.sh" --system   # build + write SYSTEM files
#   bash <installed>/polyscribe.sh --session <client> <record>     # hook-internal: JSON additionalContext
#                                                                  #   honors AGENTS_SYSTEM_HOME to retarget for testing
#   (Node repos MAY alias these as `npm run agents[:check|:system]` — optional convenience,
#    added by polypowers-init only when a package.json already exists. Not required.)

set -euo pipefail

if git_root="$(git rev-parse --show-toplevel 2>/dev/null)" && [[ -n "$git_root" ]]; then
  REPO_ROOT="$(cd -- "$git_root" && pwd -P)"
else
  printf 'polyscribe: cannot resolve repo root; run inside a git repository\n' >&2
  exit 1
fi

AI_DIR="${REPO_ROOT}/agent-instructions"
SRC_DIR="${AI_DIR}/source"
OVR_DIR="${AI_DIR}/agent-overrides"
SYS_DIR="${AI_DIR}/system"
STANDARD_SET_MANIFEST="${AI_DIR}/standard-set.json"
ROLE_DIR="${AI_DIR}/roles"

SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)"
SCRIPT_PATH="${SCRIPT_DIR}/$(basename -- "${BASH_SOURCE[0]}")"
SESSION_STANDARD_DIR="${SCRIPT_DIR}/standard"
case "$SCRIPT_PATH" in
  "$HOME/.claude/hooks/polyscribe/polyscribe.sh")
    RECOVERY_COMMAND='bash "$HOME/.claude/hooks/polyscribe/polyscribe.sh"'
    ;;
  "$HOME/.gemini/hooks/polyscribe/polyscribe.sh")
    RECOVERY_COMMAND='bash "$HOME/.gemini/hooks/polyscribe/polyscribe.sh"'
    ;;
  "$REPO_ROOT/polyscribe/polyscribe.sh")
    RECOVERY_COMMAND='bash polyscribe/polyscribe.sh'
    ;;
  "$REPO_ROOT/scripts/polyscribe.sh")
    RECOVERY_COMMAND='bash scripts/polyscribe.sh'
    ;;
  *)
    RECOVERY_COMMAND="bash $(printf '%q' "$SCRIPT_PATH")"
    ;;
esac

BANNER="<!-- GENERATED — DO NOT EDIT. Edit agent-instructions/{source,agent-overrides,system}/, then rebuild with polyscribe (system scope adds --system) -->"
ROLE_BANNER="<!-- GENERATED — DO NOT EDIT. Edit agent-instructions/roles/<role>/, then rebuild with polyscribe -->"
SESSION_BANNER="<!-- SESSION-INJECTED — current vault rules plus repo-local context; when present, inspect the repo-root *-ASSEMBLED.md record -->"
CEILING=200

# --- REPO scope (NEVER glob — order is explicit) -----------------------------
# Client files are deliberately small fail-open stubs. SessionStart owns the
# current full assembly; the stubs preserve only irreversible safety constraints
# and tell an agent where to read repo-local context if injection is unavailable.
# AGENTS.shared.md remains a full identity-free authoring/reference artifact.
# Module discovery (v5): if the repo carries ordered numbered fragments
# (NN-*.md / NN-*.ref.md) under agent-instructions/source/, assemble those in
# sorted order. Otherwise fall back to the legacy fixed module list, so
# existing consumers keep building unchanged.
SOURCE_MODULES=(core coding safety project-context repo-style)   # legacy fallback
discover_numbered_modules() {
  # Echoes sorted module basenames (without .md/.ref.md) for NN-*.{md,ref.md}.
  local d="$1" f base
  ls "$d"/[0-9][0-9]-*.md 2>/dev/null | LC_ALL=C sort | while read -r f; do
    base="$(basename "$f")"
    base="${base%.ref.md}"; base="${base%.md}"
    printf '%s\n' "$base"
  done | awk '!seen[$0]++'
}
# Prefer numbered fragments when present (SRC_DIR is defined above).
if _numbered="$(discover_numbered_modules "$SRC_DIR")" && [[ -n "$_numbered" ]]; then
  SOURCE_MODULES=()
  while IFS= read -r _mod; do
    [ -n "$_mod" ] && SOURCE_MODULES+=("$_mod")
  done <<EOF
$_numbered
EOF
fi
unset _numbered _mod
REPO_CANONICAL="AGENTS.md"             # minimal fail-open stub for Codex
REPO_CANONICAL_OVERRIDE="codex"
REPO_SHARED="AGENTS.shared.md"         # shared body ONLY (no agent identity) — reference artifact
REPO_CLIENTS=(CLAUDE.md GEMINI.md)     # minimal fail-open stubs
REPO_CLIENT_OVERRIDES=(claude agy)

# Build only the client outputs whose identity overrides are configured. Polypowers-init
# intentionally scaffolds overrides for the clients listed in polypowers.json, so
# requiring every supported client would make the default Claude + Codex setup
# fail merely because Agy/Gemini was not selected.
REPO_ACTIVE_CLIENTS=()
REPO_ACTIVE_CLIENT_OVERRIDES=()
REPO_OVERRIDE_MODULES=()
REPO_ALL=()
if [[ -f "${OVR_DIR}/${REPO_CANONICAL_OVERRIDE}.md" ]]; then
  REPO_ALL+=("$REPO_CANONICAL")
  REPO_OVERRIDE_MODULES+=("$REPO_CANONICAL_OVERRIDE")
fi
REPO_ALL+=("$REPO_SHARED")
for _client_i in "${!REPO_CLIENT_OVERRIDES[@]}"; do
  if [[ -f "${OVR_DIR}/${REPO_CLIENT_OVERRIDES[$_client_i]}.md" ]]; then
    REPO_ACTIVE_CLIENTS+=("${REPO_CLIENTS[$_client_i]}")
    REPO_ACTIVE_CLIENT_OVERRIDES+=("${REPO_CLIENT_OVERRIDES[$_client_i]}")
    REPO_ALL+=("${REPO_CLIENTS[$_client_i]}")
    REPO_OVERRIDE_MODULES+=("${REPO_CLIENT_OVERRIDES[$_client_i]}")
  fi
done
unset _client_i

# --- ROLE scope -------------------------------------------------------------
# Role policies are injected by the daemon through RoleOverride.InstructionsFile.
# They must never be inlined into the ordinary AGENTS/CLAUDE/GEMINI context.
ROLE_NAMES=()
discover_role_names() {
  local role_dir="$1" d role
  [[ -d "$role_dir" ]] || return 0
  for d in "$role_dir"/*; do
    [[ -d "$d" ]] || continue
    role="$(basename "$d")"
    if discover_numbered_modules "$d" | grep -q .; then
      printf '%s\n' "$role"
    fi
  done | LC_ALL=C sort
}
if _roles="$(discover_role_names "$ROLE_DIR")" && [[ -n "$_roles" ]]; then
  while IFS= read -r _role; do
    [ -n "$_role" ] && ROLE_NAMES+=("$_role")
  done <<EOF
$_roles
EOF
fi
unset _roles _role

# --- SYSTEM scope ------------------------------------------------------------
SYSTEM_MODULES=(response-style)
# Native global path per tool. AGENTS_SYSTEM_HOME overrides $HOME (for testing).
SYS_HOME="${AGENTS_SYSTEM_HOME:-$HOME}"
SYSTEM_OUTPUTS=("${SYS_HOME}/.codex/AGENTS.md" "${SYS_HOME}/.claude/CLAUDE.md" "${SYS_HOME}/.gemini/GEMINI.md")

# --- Helpers -----------------------------------------------------------------
die() { printf 'polyscribe: %s\n' "$*" >&2; exit 1; }

# Resolve a module basename to its file: prefer <name>.md, else <name>.ref.md.
module_file() {
  local dir="$1" mod="$2"
  if [[ -f "${dir}/${mod}.md" ]]; then printf '%s' "${dir}/${mod}.md"
  elif [[ -f "${dir}/${mod}.ref.md" ]]; then printf '%s' "${dir}/${mod}.ref.md"
  else die "missing module: ${dir}/${mod}.md (or ${mod}.ref.md)"; fi
}

# Strip HTML comments (<!-- ... -->), including multi-line spans and multiple
# comments per line. Authoring-only metadata (provenance, @sx-managed refresh
# markers, notes to the fragment editor) lives in these comments and must NOT
# reach the agent-facing generated file. Takes a file arg, writes stripped stdout.
# Fails LOUDLY (exit 3) if the file ends while still inside a comment: an
# unterminated "<!--" would otherwise silently swallow the entire rest of the
# fragment — dropping real instructions with no signal. Abort the build instead,
# so the existing generated files are left untouched and the mistake is visible.
strip_html_comments() {
  local f="$1"
  awk '
    BEGIN { incomment = 0 }
    {
      line = $0; out = ""
      while (1) {
        if (incomment) {
          p = index(line, "-->")
          if (p == 0) { line = ""; break }   # comment continues past end of line
          line = substr(line, p + 3); incomment = 0
        }
        s = index(line, "<!--")
        if (s == 0) { out = out line; break }
        out = out substr(line, 1, s - 1)      # keep text before the comment
        line = substr(line, s + 4); incomment = 1
      }
      print out
    }
    END { if (incomment) exit 3 }            # unterminated comment → hard failure
  ' "$f"
}

# Emit a file with HTML comments stripped, then leading/trailing/interior-run
# blank lines trimmed (so a stripped leading comment block leaves no gap).
emit_trimmed() {
  local f="$1"
  [[ -f "$f" ]] || die "missing module: $f"
  # Capture on its own line so the assignment carries strip_html_comments' exit
  # status (a combined `local x=$(...)` would mask it behind local's status).
  local stripped
  stripped="$(strip_html_comments "$f")" \
    || die "unterminated HTML comment (<!-- with no matching -->) in $f"
  printf '%s\n' "$stripped" | awk '
    BEGIN { started = 0; pending = 0 }
    {
      if ($0 ~ /^[[:space:]]*$/) { if (started) pending++; next }
      if (started) { for (i = 0; i < pending; i++) print "" }
      pending = 0; print; started = 1
    }
  '
}

normalize_standard_module() {
  local f="$1"
  emit_trimmed "$f"
}

sha256_stdin() {
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum | awk '{print $1}'
  elif command -v shasum >/dev/null 2>&1; then
    shasum -a 256 | awk '{print $1}'
  else
    die "missing sha256sum or shasum for standard-set canon check"
  fi
}

sha256_file() {
  local f="$1"
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$f" | awk '{print $1}'
  elif command -v shasum >/dev/null 2>&1; then
    shasum -a 256 "$f" | awk '{print $1}'
  else
    die "missing sha256sum or shasum for standard-set canon check"
  fi
}

standard_opted_out() {
  local rel="$1" cfg="${REPO_ROOT}/polypowers.json"
  [[ -f "$cfg" ]] || return 1
  # The opt-out schema is intentionally scoped: only
  # subsystems.agent_instructions.opt_outs[<path>] counts.
  if command -v jq >/dev/null 2>&1; then
    jq -e --arg rel "$rel" '.subsystems.agent_instructions.opt_outs | type == "object" and has($rel)' "$cfg" >/dev/null 2>&1
  elif command -v python3 >/dev/null 2>&1; then
    python3 - "$cfg" "$rel" <<'PY' >/dev/null 2>&1
import json, sys
data = json.load(open(sys.argv[1]))
opt_outs = data.get("subsystems", {}).get("agent_instructions", {}).get("opt_outs", {})
raise SystemExit(0 if isinstance(opt_outs, dict) and sys.argv[2] in opt_outs else 1)
PY
  elif command -v node >/dev/null 2>&1; then
    node - "$cfg" "$rel" <<'JS' >/dev/null 2>&1
const fs = require("fs");
const data = JSON.parse(fs.readFileSync(process.argv[2], "utf8"));
const optOuts = data.subsystems?.agent_instructions?.opt_outs;
process.exit(optOuts && Object.prototype.hasOwnProperty.call(optOuts, process.argv[3]) ? 0 : 1);
JS
  else
    return 1
  fi
}

check_standard_set() {
  [[ -f "$STANDARD_SET_MANIFEST" ]] || return 0
  local line rel expected actual marker path checked=0 expected_count
  expected_count="$(grep -o '"source"' "$STANDARD_SET_MANIFEST" | wc -l | tr -d ' ')"
  while IFS= read -r line; do
    rel="$(printf '%s\n' "$line" | sed -n 's/.*"path"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p')"
    expected="$(printf '%s\n' "$line" | sed -n 's/.*"sha256"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p')"
    marker="$(printf '%s\n' "$line" | sed -n 's/.*"marker"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p')"
    if printf '%s\n' "$line" | grep -Eq '"(source|path|marker|sha256)"'; then
      [[ -n "$rel" && -n "$expected" ]] || die "standard-set manifest entries must keep path and sha256 on the same line: $STANDARD_SET_MANIFEST"
    else
      continue
    fi
    checked=$((checked + 1))
    path="${REPO_ROOT}/${rel}"
    if [[ ! -f "$path" ]]; then
      if standard_opted_out "$rel"; then
        printf 'polyscribe: standard-set opt-out: missing %s\n' "$rel" >&2
        continue
      fi
      die "standard-set module missing: $rel (add it via polypowers-init or record an explicit opt-out in polypowers.json)"
    fi
    if [[ -n "$marker" ]] && ! grep -F "$marker" "$path" >/dev/null 2>&1; then
      if standard_opted_out "$rel"; then
        printf 'polyscribe: standard-set repo-owned override: %s\n' "$rel" >&2
        continue
      fi
      die "standard-set repo-owned override without opt-out: $rel (missing marker $marker)"
    fi
    case "$rel" in
      *.sh)
        actual="$(sha256_file "$path")"
        ;;
      *)
        actual="$(normalize_standard_module "$path" | sha256_stdin)"
        ;;
    esac
    if [[ "$actual" != "$expected" ]]; then
      die "standard-set module drift: $rel (expected $expected, got $actual)"
    fi
  done <"$STANDARD_SET_MANIFEST"
  [[ "$checked" -gt 0 ]] || die "standard-set manifest contains no parseable module entries: $STANDARD_SET_MANIFEST"
  [[ "$checked" = "$expected_count" ]] || die "standard-set manifest parsed $checked module entries but contains $expected_count source entries: $STANDARD_SET_MANIFEST"
}

# Track .ref.md modules already emitted while rendering one output.
EMITTED_REF_MODULES="|"
REF_MODULES=()
REF_RELS=()
ref_already_emitted() {
  case "$EMITTED_REF_MODULES" in
    *"|$1|"*) return 0 ;;
    *) return 1 ;;
  esac
}
mark_ref_emitted() {
  EMITTED_REF_MODULES="${EMITTED_REF_MODULES}$1|"
}
emit_ref_pointer() {
  local mod="$1" rel="$2" prefix="${3:-}"
  printf '%sFor **%s**, read `%s` when relevant (referenced on demand, not inlined here).\n' \
    "$prefix" "$mod" "$rel"
  mark_ref_emitted "$mod"
}
prepare_ref_modules() {
  local dir="$1"; shift
  local mod f
  REF_MODULES=()
  REF_RELS=()
  for mod in "$@"; do
    f="$(module_file "$dir" "$mod")"
    [[ "$f" == *.ref.md ]] || continue
    REF_MODULES+=("$mod")
    REF_RELS+=("${f#"$REPO_ROOT"/}")
  done
}
ref_rel_for_mod() {
  local mod="$1" i
  if [[ "${#REF_MODULES[@]}" -gt 0 ]]; then
    for i in "${!REF_MODULES[@]}"; do
      [[ "${REF_MODULES[$i]}" == "$mod" ]] && { printf '%s' "${REF_RELS[$i]}"; return 0; }
    done
  fi
  return 1
}
should_skip_module() {
  local mod="$1"
  ref_rel_for_mod "$mod" >/dev/null && ref_already_emitted "$mod"
}
paragraph_ref_anchors() {
  local paragraph="$1"
  local first_line indent="" i mod rel emitted=false
  first_line="${paragraph%%$'\n'*}"
  if [[ "${#REF_MODULES[@]}" -gt 0 ]]; then
    for i in "${!REF_MODULES[@]}"; do
      mod="${REF_MODULES[$i]}"
      rel="${REF_RELS[$i]}"
      ref_already_emitted "$mod" && continue
      case "$paragraph" in
        *"$rel"*)
          indent=""
          if [[ "$first_line" =~ ^([0-9]+\.|-)[[:space:]] ]]; then
            indent="${BASH_REMATCH[1]} "
            indent="${indent//?/ }"
          fi
          printf '\n'
          emit_ref_pointer "$mod" "$rel" "$indent"
          emitted=true
          ;;
      esac
    done
  fi
  [[ "$emitted" = true ]]
}
emit_trimmed_with_ref_anchors() {
  local f="$1"
  local rendered line paragraph="" anchor_text="" in_fence=false fence_re='^[[:space:]]*(```|~~~)'
  rendered="$(emit_trimmed "$f")" || return $?
  [[ -n "$rendered" ]] || return 0
  while IFS= read -r line; do
    if [[ "$in_fence" = true ]]; then
      if [[ -n "$paragraph" ]]; then
        paragraph="${paragraph}
${line}"
      else
        paragraph="$line"
      fi
      [[ "$line" =~ $fence_re ]] && in_fence=false
    elif [[ "$line" =~ $fence_re ]]; then
      if [[ -n "$paragraph" ]]; then
        paragraph="${paragraph}
${line}"
      else
        paragraph="$line"
      fi
      in_fence=true
    elif [[ "$line" =~ ^[[:space:]]*$ ]]; then
      if [[ -n "$paragraph" ]]; then
        printf '%s\n' "$paragraph"
        paragraph_ref_anchors "$anchor_text" || true
        paragraph=""
        anchor_text=""
      fi
      printf '\n'
    elif [[ -n "$paragraph" && "$line" =~ ^([0-9]+\.|-)[[:space:]] ]]; then
      printf '%s\n' "$paragraph"
      paragraph_ref_anchors "$anchor_text" && printf '\n'
      paragraph="$line"
      anchor_text="$line"
    else
      if [[ -n "$paragraph" ]]; then
        paragraph="${paragraph}
${line}"
        anchor_text="${anchor_text}
${line}"
      else
        paragraph="$line"
        anchor_text="$line"
      fi
    fi
  done <<EOF
$rendered
EOF
  if [[ -n "$paragraph" ]]; then
    printf '%s\n' "$paragraph"
    paragraph_ref_anchors "$anchor_text" || true
  fi
}

# Emit one module. A normal <name>.md is inlined; a <name>.ref.md is the
# by-reference escape hatch — its body is NOT inlined, only a one-line pointer
# telling the agent to read the file on demand (keeps the assembled file small).
emit_module() {
  local dir="$1" mod="$2" f
  f="$(module_file "$dir" "$mod")"
  if [[ "$f" == *.ref.md ]]; then
    emit_ref_pointer "$mod" "$(ref_rel_for_mod "$mod")"
  else
    emit_trimmed_with_ref_anchors "$f"
  fi
}

# banner + each module (resolved .md/.ref.md), one blank line between, then an
# optional trailing override module with no separator after it.
emit_assembled() {
  local dir="$1"; shift
  local override_file="$1"; shift # may be empty
  local mod first=1 modules=("$@")
  EMITTED_REF_MODULES="|"
  prepare_ref_modules "$dir" "${modules[@]}"
  printf '%s\n\n' "$BANNER"
  # Separator goes BEFORE each module (except the first) and before the override,
  # so an empty-override output (AGENTS.shared.md, system files) has no trailing blank.
  for mod in "${modules[@]}"; do
    should_skip_module "$mod" && continue
    [[ $first -eq 1 ]] || printf '\n'
    emit_module "$dir" "$mod"
    first=0
  done
  if [[ -n "$override_file" ]]; then
    printf '\n'
    emit_trimmed "$override_file"
  fi
}

emit_role_assembled() {
  local role="$1" role_source mod first=1
  role_source="${ROLE_DIR}/${role}"
  local modules=()
  while IFS= read -r mod; do
    [ -n "$mod" ] && modules+=("$mod")
  done <<EOF
$(discover_numbered_modules "$role_source")
EOF
  [[ "${#modules[@]}" -gt 0 ]] || die "missing role modules: ${role_source}/NN-*.md"

  printf '%s\n\n' "$ROLE_BANNER"
  EMITTED_REF_MODULES="|"
  prepare_ref_modules "$role_source" "${modules[@]}"
  for mod in "${modules[@]}"; do
    should_skip_module "$mod" && continue
    [[ $first -eq 1 ]] || printf '\n'
    emit_module "$role_source" "$mod"
    first=0
  done
}

# --- Length report -----------------------------------------------------------
report_modules() {
  local dir="$1"; shift
  local mod f n tag
  for mod in "$@"; do
    f="$(module_file "$dir" "$mod")"
    n="$(wc -l <"$f" | tr -d ' ')"
    if [[ "$f" == *.ref.md ]]; then
      # by-reference: emits a single pointer line regardless of on-disk size.
      printf '    %-22s %4s   REF (→1 line; %s on disk)\n' "$mod" "1" "$n"
    else
      tag=""; [[ "$n" -gt "$CEILING" ]] && tag="  ⚠ OVER"
      printf '    %-22s %4s%s\n' "$mod" "$n" "$tag"
    fi
  done
}
report_output() {
  local label="$1" path="$2" n tag
  [[ -f "$path" ]] || { printf '    %-22s   (not built)\n' "$label"; return; }
  n="$(wc -l <"$path" | tr -d ' ')"
  tag=""; [[ "$n" -gt "$CEILING" ]] && tag="  ⚠ OVER"
  printf '    %-22s %4s%s\n' "$label" "$n" "$tag"
}

# Refuse to ignore a generated client file after its identity override is
# removed. Leaving it in place would let that client keep reading stale
# governance indefinitely. Repo-owned files (without our generated banner) are
# untouched and do not participate in this drift check.
check_stale_generated_output() {
  local out="$1" override="$2" path first
  path="${REPO_ROOT}/${out}"
  if [[ -f "${OVR_DIR}/${override}.md" || ! -f "$path" ]]; then
    return
  fi
  first="$(sed -n '1p' "$path")"
  if [[ "$first" == '<!-- GENERATED — DO NOT EDIT. Edit agent-instructions/'* ]]; then
    die "stale generated client output $out has no matching override ${override}.md; remove $out or restore ${OVR_DIR}/${override}.md"
  fi
}

# --- Session assembly --------------------------------------------------------
# These modules are the universal rules shipped inside the hook asset. A stale
# managed copy in a consumer repo is skipped during session assembly: sx
# refreshes this bundle before SessionStart, making it the live source of truth
# without rewriting tracked files.
SESSION_SOURCE_MODULES=(30-polypowers 35-worktree-recipe 40-operating-principles 65-agent-identity)

session_managed_source_module() {
  local mod="$1" f marker=""
  f="$(module_file "$SRC_DIR" "$mod")"
  case "$mod" in
    30-polypowers) marker='@sx-managed: polypowers-module' ;;
    35-worktree-recipe) marker='@sx-managed: polypowers-worktree-recipe' ;;
    40-operating-principles) marker='@sx-managed: operating-principles' ;;
    65-agent-identity) marker='@sx-managed: agent-identity-contract' ;;
    *) return 1 ;;
  esac
  grep -Fq "$marker" "$f" 2>/dev/null
}

session_managed_override() {
  local f="$1" marker="$2"
  [[ -f "$f" ]] && grep -Fq "$marker" "$f" 2>/dev/null
}

emit_session_local_modules() {
  local discovered mod first=1
  local modules=()
  discovered="$(discover_numbered_modules "$SRC_DIR")"
  if [[ -n "$discovered" ]]; then
    while IFS= read -r mod; do
      [[ -n "$mod" ]] || continue
      session_managed_source_module "$mod" && continue
      modules+=("$mod")
    done <<EOF
$discovered
EOF
  else
    # Legacy repos use the fixed SOURCE_MODULES list rather than NN-* files.
    # Those modules are repo-owned local context and must remain live.
    for mod in "${SOURCE_MODULES[@]}"; do modules+=("$mod"); done
  fi
  [[ "${#modules[@]}" -gt 0 ]] || return 0
  EMITTED_REF_MODULES="|"
  prepare_ref_modules "$SRC_DIR" "${modules[@]}"
  for mod in "${modules[@]}"; do
    should_skip_module "$mod" && continue
    [[ $first -eq 1 ]] || printf '\n'
    emit_module "$SRC_DIR" "$mod"
    first=0
  done
}

json_string_stream() {
  # JSON-escape UTF-8 markdown with POSIX awk so SessionStart does not depend on
  # jq, Python, Node, or a client-specific runtime.
  awk '
    BEGIN {
      printf "\""
      controls = ""
      for (i = 1; i < 32; i++) controls = controls sprintf("%c", i)
    }
    {
      for (i = 1; i <= length($0); i++) {
        c = substr($0, i, 1)
        code = index(controls, c)
        if (c == "\\") printf "\\\\"
        else if (c == "\"") printf "\\\""
        else if (code == 8) printf "\\b"
        else if (code == 9) printf "\\t"
        else if (code == 12) printf "\\f"
        else if (code == 13) printf "\\r"
        else if (code > 0) printf "\\u%04x", code
        else printf "%s", c
      }
      printf "\\n"
    }
    END { printf "\"" }
  '
}

json_string_file() {
  json_string_stream < "$1"
}

render_session() {
  local client="$1" standard_override local_override local_marker saved_banner local_context
  case "$client" in
    claude)
      standard_override="${SESSION_STANDARD_DIR}/agent-overrides/claude.md"
      local_override="${OVR_DIR}/claude.md"
      local_marker='@sx-managed: claude-identity'
      ;;
    codex)
      standard_override="${SESSION_STANDARD_DIR}/agent-overrides/codex.md"
      local_override="${OVR_DIR}/codex.md"
      local_marker='@sx-managed: codex-identity'
      ;;
    gemini)
      standard_override="${SESSION_STANDARD_DIR}/agent-overrides/agy.md"
      local_override="${OVR_DIR}/agy.md"
      local_marker='@sx-managed: agy-identity'
      ;;
    *) die "unknown session client: $client (expected claude, codex, or gemini)" ;;
  esac
  [[ -d "${SESSION_STANDARD_DIR}/source" ]] || die "missing hook-bundled universal rules: ${SESSION_STANDARD_DIR}/source"
  [[ -f "$standard_override" ]] || die "missing hook-bundled client identity: $standard_override"

  saved_banner="$BANNER"
  BANNER="$SESSION_BANNER"
  emit_assembled "${SESSION_STANDARD_DIR}/source" "$standard_override" "${SESSION_SOURCE_MODULES[@]}"
  BANNER="$saved_banner"

  local_context="$(emit_session_local_modules)" || return $?
  if [[ -n "$local_context" ]]; then
    printf '\n\n<!-- REPO-LOCAL CONTEXT -->\n\n%s\n' "$local_context"
  fi
  if [[ -f "$local_override" ]] && ! session_managed_override "$local_override" "$local_marker"; then
    printf '\n<!-- REPO-OWNED CLIENT OVERRIDE -->\n\n'
    emit_trimmed "$local_override"
  fi
}

run_session_mode() {
  local client="$1" record="$2" record_dir record_base tmp="" context_json publish_record=true
  local session_context session_sentinel=$'\034'
  [[ -d "$AI_DIR" ]] || exit 0
  [[ -n "$record" ]] || die "session mode requires an assembled-record path"
  record_dir="$(dirname -- "$record")"
  record_base="$(basename -- "$record")"
  if [[ -d "$record_dir" ]]; then
    record_dir="$(cd -- "$record_dir" && pwd -P)"
    record="${record_dir}/${record_base}"
  fi
  cleanup_session_tmp() {
    [[ -z "${tmp:-}" || ! -e "$tmp" ]] || rm -- "$tmp" 2>/dev/null || true
  }
  trap cleanup_session_tmp EXIT
  case "$record" in
    "${REPO_ROOT}/"*)
      if ! git -C "$REPO_ROOT" check-ignore -q -- "$record" || \
         ! git -C "$REPO_ROOT" check-ignore -q -- "${record}.tmp.probe"; then
        printf 'polyscribe: assembled record path is not ignored; re-run polypowers-init to enable %s\n' "$record" >&2
        publish_record=false
      fi
      ;;
  esac
  session_context="$(
    render_session "$client" || exit $?
    printf '%s' "$session_sentinel"
  )" || return $?
  session_context="${session_context%"$session_sentinel"}"
  context_json="$(printf '%s' "$session_context" | json_string_stream)"

  # The SessionStart envelope is now fully captured in memory. Record staging
  # is optional and fail-open; when enabled, the temp lives beside the final
  # record so the rename is atomic.
  if [[ "$publish_record" == true ]]; then
    if ! mkdir -p "$record_dir" 2>/dev/null || ! tmp="$(mktemp "${record}.tmp.XXXXXX" 2>/dev/null)"; then
      printf 'polyscribe: could not stage assembled record %s; continuing with injected context\n' "$record" >&2
    elif ! printf '%s' "$session_context" >"$tmp" 2>/dev/null; then
      printf 'polyscribe: could not stage assembled record %s; continuing with injected context\n' "$record" >&2
    elif mv -f "$tmp" "$record" 2>/dev/null; then
      tmp=""
    else
      printf 'polyscribe: could not publish assembled record %s; continuing with injected context\n' "$record" >&2
    fi
  fi
  cleanup_session_tmp
  trap - EXIT
  printf '{"hookSpecificOutput":{"hookEventName":"SessionStart","additionalContext":%s}}\n' "$context_json"
}

# --- Preflight ---------------------------------------------------------------
preflight_repo() {
  [[ -d "$SRC_DIR" ]] || die "missing $SRC_DIR"
  [[ -d "$OVR_DIR" ]] || die "missing $OVR_DIR"
  local m i
  for m in "${SOURCE_MODULES[@]}"; do module_file "$SRC_DIR" "$m" >/dev/null; done
  check_stale_generated_output "$REPO_CANONICAL" "$REPO_CANONICAL_OVERRIDE"
  for i in "${!REPO_CLIENTS[@]}"; do
    check_stale_generated_output "${REPO_CLIENTS[$i]}" "${REPO_CLIENT_OVERRIDES[$i]}"
  done
  [[ "${#REPO_OVERRIDE_MODULES[@]}" -gt 0 ]] || die "missing client identity overrides in $OVR_DIR"
  # Client files are stubs now, but malformed local identities must still fail
  # reconciliation before any output is moved. Session assembly consumes these
  # files later and must not discover corruption in the live hook.
  for m in "${REPO_OVERRIDE_MODULES[@]}"; do
    emit_trimmed "${OVR_DIR}/${m}.md" >/dev/null
  done
  if [[ "${#ROLE_NAMES[@]}" -gt 0 ]]; then
    for m in "${ROLE_NAMES[@]}"; do
      [[ -d "${ROLE_DIR}/${m}" ]] || die "missing role directory: ${ROLE_DIR}/${m}"
      discover_numbered_modules "${ROLE_DIR}/${m}" | grep -q . || die "missing numbered role modules: ${ROLE_DIR}/${m}"
    done
  fi
}
preflight_system() {
  [[ -d "$SYS_DIR" ]] || die "missing $SYS_DIR"
  local m
  for m in "${SYSTEM_MODULES[@]}"; do module_file "$SYS_DIR" "$m" >/dev/null; done
}

emit_failopen_stub() {
  local client="$1" override="$2"
  printf '%s\n\n' "$BANNER"
  printf '# Agent instructions — fail-open baseline (%s)\n\n' "$client"
  printf 'SessionStart normally injects the current vault rules plus this repository’s local context. If that context is absent, use this safety baseline only.\n\n'
  printf 'Before acting, read the ordered Markdown fragments under `agent-instructions/source/` and `agent-instructions/agent-overrides/%s.md`.\n\n' "$override"
  printf '1. GitHub Issues are the sole durable tracker.\n'
  printf '2. Make every mutation in an agent-owned worktree, never the shared checkout.\n'
  printf '3. For behavior changes, write a failing test first, then implement and verify the fix.\n'
  printf '4. Verify the result with the repository’s real checks before claiming success.\n'
  printf '5. Use an independent reviewer; do not self-review merge readiness.\n'
  printf '6. Never merge without explicit authorization from the user or configured autonomous mode.\n'
}

# Render one REPO output filename to stdout.
render_repo() {
  local out="$1" i
  if [[ "$out" == "$REPO_CANONICAL" ]]; then
    emit_failopen_stub "Codex" "$REPO_CANONICAL_OVERRIDE"
    return
  fi
  if [[ "$out" == "$REPO_SHARED" ]]; then
    # Shared body ONLY — no agent identity. Reference artifact (no longer imported).
    emit_assembled "$SRC_DIR" "" "${SOURCE_MODULES[@]}"
    return
  fi
  for i in "${!REPO_ACTIVE_CLIENTS[@]}"; do
    if [[ "${REPO_ACTIVE_CLIENTS[$i]}" == "$out" ]]; then
      case "$out" in
        CLAUDE.md) emit_failopen_stub "Claude" "${REPO_ACTIVE_CLIENT_OVERRIDES[$i]}" ;;
        GEMINI.md) emit_failopen_stub "Gemini" "${REPO_ACTIVE_CLIENT_OVERRIDES[$i]}" ;;
      esac
      return
    fi
  done
  die "no builder for repo output: $out"
}

# --- Modes -------------------------------------------------------------------
MODE="build"
case "${1:-}" in
  --check) MODE="check" ;;
  --system) MODE="system" ;;
  --session) MODE="session" ;;
  '') : ;;
  *) die "unknown argument: $1 (use --check, --system, --session, or no args)" ;;
esac

if [[ "$MODE" == "session" ]]; then
  run_session_mode "${2:-}" "${3:-}"
  exit 0
fi

if [[ "$MODE" == "check" ]]; then
  preflight_repo
  check_standard_set
  tmp="$(mktemp -d)"
  cleanup_check_tmp() { [[ ! -d "$tmp" ]] || rm -r "$tmp"; }
  trap cleanup_check_tmp EXIT
  drift=0
  for out in "${REPO_ALL[@]}"; do
    render_repo "$out" >"${tmp}/${out}"
    diff -u "${REPO_ROOT}/${out}" "${tmp}/${out}" --label "committed/${out}" --label "freshly-built/${out}" || drift=1
  done
  if [[ "${#ROLE_NAMES[@]}" -gt 0 ]]; then
    for role in "${ROLE_NAMES[@]}"; do
      out="agent-instructions/roles/${role}-policy.md"
      mkdir -p "${tmp}/agent-instructions/roles"
      emit_role_assembled "$role" >"${tmp}/${out}"
      diff -u "${REPO_ROOT}/${out}" "${tmp}/${out}" --label "committed/${out}" --label "freshly-built/${out}" || drift=1
    done
  fi
  [[ $drift -ne 0 ]] && die "repo agent instruction files are stale. Run: ${RECOVERY_COMMAND}"
  printf 'polyscribe: repo files (client fail-open stubs + AGENTS.shared.md reference) up to date.\n'
  exit 0
fi

if [[ "$MODE" == "system" ]]; then
  preflight_system
  # Two-phase for atomicity (same rationale as the repo build below): render all
  # system outputs to temps, then move them into place only if all succeed.
  sys_tmps=(); sys_dsts=()
  cleanup_sys_tmps() {
    local t
    for t in "${sys_tmps[@]:-}"; do
      [[ -n "$t" ]] || continue
      [[ ! -e "$t" && ! -L "$t" ]] || rm -- "$t"
    done
  }
  trap cleanup_sys_tmps EXIT
  for path in "${SYSTEM_OUTPUTS[@]}"; do
    mkdir -p "$(dirname "$path")"
    tmpf="$(mktemp "${path}.XXXXXX")"
    sys_tmps+=("$tmpf"); sys_dsts+=("$path")
    emit_assembled "$SYS_DIR" "" "${SYSTEM_MODULES[@]}" >"$tmpf"   # dies here on a malformed fragment; nothing moved yet
  done
  for i in "${!sys_tmps[@]}"; do
    mv -f "${sys_tmps[$i]}" "${sys_dsts[$i]}"
    printf 'wrote %s\n' "${sys_dsts[$i]}"
  done
  sys_tmps=(); trap - EXIT
  printf '\nagent-instructions: SYSTEM length report (ceiling %s, home=%s)\n' "$CEILING" "$SYS_HOME"
  printf '  primitives (system/):\n'; report_modules "$SYS_DIR" "${SYSTEM_MODULES[@]}"
  printf '  outputs:\n'
  for path in "${SYSTEM_OUTPUTS[@]}"; do report_output "$path" "$path"; done
  exit 0
fi

# --- build (repo) ------------------------------------------------------------
# Two-phase for atomicity: render EVERY output to a temp first, and only move
# them into place once all have rendered cleanly. If a render dies partway
# (e.g. an unterminated comment in a late-rendered override), the EXIT trap
# removes the temps and the committed files are left untouched — never a
# half-updated set split across source versions.
preflight_repo
build_tmps=(); build_dsts=()
cleanup_build_tmps() {
  local t
  for t in "${build_tmps[@]:-}"; do
    [[ -n "$t" ]] || continue
    [[ ! -e "$t" && ! -L "$t" ]] || rm -- "$t"
  done
}
trap cleanup_build_tmps EXIT
for out in "${REPO_ALL[@]}"; do
  tmpf="$(mktemp "${REPO_ROOT}/.${out}.XXXXXX")"
  build_tmps+=("$tmpf"); build_dsts+=("${REPO_ROOT}/${out}")
  render_repo "$out" >"$tmpf"        # dies here on a malformed fragment; nothing moved yet
done
if [[ "${#ROLE_NAMES[@]}" -gt 0 ]]; then
  for role in "${ROLE_NAMES[@]}"; do
    mkdir -p "${ROLE_DIR}"
    tmpf="$(mktemp "${ROLE_DIR}/.${role}-policy.md.XXXXXX")"
    build_tmps+=("$tmpf"); build_dsts+=("${ROLE_DIR}/${role}-policy.md")
    emit_role_assembled "$role" >"$tmpf"
  done
fi
for i in "${!build_tmps[@]}"; do
  mv -f "${build_tmps[$i]}" "${build_dsts[$i]}"
  printf 'wrote %s\n' "${build_dsts[$i]}"
done
build_tmps=()   # all moved; nothing left for the trap to clean
trap - EXIT
printf '\nagent-instructions: REPO length report (ceiling %s)\n' "$CEILING"
printf '  primitives (source/):\n'; report_modules "$SRC_DIR" "${SOURCE_MODULES[@]}"
printf '  overrides:\n'; report_modules "$OVR_DIR" "${REPO_OVERRIDE_MODULES[@]}"
printf '  outputs:\n'
for out in "${REPO_ALL[@]}"; do report_output "$out" "${REPO_ROOT}/${out}"; done
if [[ "${#ROLE_NAMES[@]}" -gt 0 ]]; then
  printf '  role outputs:\n'
  for role in "${ROLE_NAMES[@]}"; do report_output "${role}-policy.md" "${ROLE_DIR}/${role}-policy.md"; done
fi
