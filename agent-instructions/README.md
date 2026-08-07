<!--
@sx-managed: agent-instructions-readme (nickify refreshes marked copies; remove this line to own the file)
-->

# nickify

One skill, one entrypoint (`/nickify`), that brings the **Polymath agent
standard** to either a **user account** or a **repo**. It detects where you run
it and asks which you want. It's an *orchestrator* — it drives your existing
tools (`sx`, `openspec`, `git`, GitHub, the agent-CLI installers, and the
agent-instructions assembler) rather than reimplementing them.

**How it behaves everywhere:**
- **Idempotent** — re-running does nothing if you're already set up.
- **Durable repo intent** — repo scope writes `nickify.json`, a committed
  desired-state file that records provisioning choices such as target clients
  and subsystem opt-outs.
- **Leaves a receipt** — each run records itself in `.nickified.json` (scope,
  date, skill version, per-step outcome). It's a log, never a gate: the run
  always re-detects, so drift introduced since the last run still gets caught.
- **One upfront choice, then unattended** — you pick the scope once; it doesn't
  nag per tool. Built to run in an auto-approve (yolo) session.
- **Self-provision** — user scope configures the account you're running in; it
  never touches other OS accounts.
- **Never clobbers** — it only creates what's missing; your own files are safe.

---

## `/nickify` → **user** — provision the account

Ensures the account you're in has the whole toolkit:

1. **Agent CLIs** — installs any missing: `claude`, `codex`, `opencode`.
2. **`sx`** — installs or upgrades to at least 2.2.7, configures it against the vault if needed, then runs
   `sx install` so **polypowers** and the other org assets land in `~/.claude/`.
3. **Hooks + wiring** — ensures the global hooks and `settings.json` entries:
   `tool-installer`, the instructions assembler (`polyscribe`), and the
   SessionStart `sx install`. Per-tool-use usage reporting is intentionally not
   installed or ensured; nickify records `bootstrapOptions.analytics_hook=false`
   and removes only exact sx-owned reporters (including exact legacy
   `skills`-binary equivalents) left by older configuration.
4. **Baseline tools** — ensures `gh`.

Result: a bare account becomes one where every agent CLI, sx, the org
skills, and the hooks are present and wired.

---

## `/nickify` → **repo** — configure the current repo

Brings the current repo to the standard:

1. **Detect** — repo root, project type (`package.json`/`go.mod`/`pyproject`/
   `Cargo`/none), `nickify.json` desired state, and what's already set up;
   prints a plan of what it'll do.
2. **Repo assets** — `sx install --target <repo>` for each client listed in
   `nickify.json.clients` (generic **polypowers** bundle + repo-scoped skills).
   The org-scoped skills are live.
3. **OpenSpec** — `openspec init` for the configured clients (if absent) and
   seeds `project.md`.
4. **agent-instructions + assembler** — scaffolds the modular
   `agent-instructions/` source (tailored to the stack), **including the
   versioned generic standard set** (polypowers, operating principles, identity
   contract, and per-client identity bodies), then runs the assembler to emit
   `CLAUDE.md`/`AGENTS.md`/`GEMINI.md`. So the repo actually tells agents *how
   and when they must use polypowers* — not just makes the skills available.
5. **tool request drift** — reports repo-level tool files and points new recipes to trusted vault sidecars.
6. **GitHub contract** — installs trusted closing-issue and final-review status
   checks, requires pull requests without bypass actors on the default branch,
   and binds both status contexts after their workflows reach the default branch.
7. **Finalize** — `.gitignore` hygiene for repo-local agent worktrees,
   generated local asset/runtime output (`.claude/skills/`,
   `.claude/commands/`, `.agents/skills/`, `.agents/commands/`,
   `.playwright-cli/`); the `.nickified.json` receipt;
   optional initial commit; and a summary with next steps.

### `nickify.json`

Repo scope records the intended shape of the repo in a committed
`nickify.json`. This file is the source of truth for provisioning choices on
rerun; filesystem detection measures drift against it.

The schema is nested by subsystem:

```json
{
  "schema": 1,
  "repo": "owner/name",
  "clients": ["claude-code", "codex"],
  "subsystems": {
    "openspec": { "enabled": true },
    "sx": { "enabled": true },
    "agent_instructions": { "enabled": true, "standard_version": 3 },
    "tools": { "enabled": true },
    "hooks": { "enabled": true },
    "github_contract": { "enabled": true }
  }
}
```

New files default `clients` to `["claude-code", "codex"]`; reruns preserve an
existing array byte-for-byte unless the user edits `nickify.json`.
Repo scope needs a structured JSON parser (`jq`, `python3`, or `node`) to read
the desired-state file and stops with a clear message if none is available.

Unknown retired subsystem keys in an older `nickify.json` are reported as
legacy configuration and ignored without deleting their files. If the file says
an active subsystem is disabled but files already exist, nickify reports the
drift and leaves those files untouched. The `repo` field is derived once from
the `origin` remote, omitted when no remote exists, and then preserved on rerun
for byte-identical idempotency.

`.nickified.json` remains the receipt of completed runs. It is not a desired
state file and does not replace `nickify.json`.

---

## The seed (prerequisite, not part of the skill)

A skill can't install the runtime that runs it. So a bare account is first
brought to "can run a skill" by a tiny standalone `curl | bash` **seed** that
installs **Claude Code + `sx` + `sx install`** — which is what lands the nickify
skill in `~/.claude/skills/`. Everything past that is done by `/nickify` in user
scope.

---

## What the standard set is

The standard set is a versioned collection of generic repo instruction modules
published by this skill and recorded in `standard-set.json` with normalized
SHA-256 hashes. It includes:

1. `30-polypowers.md` — the shared workflow constitution. Skills are not
   listed here; every harness injects its own installed-skill listing.
1. `35-worktree-recipe.ref.md` — the worktree mechanics rule 2 points at.
   Referenced on demand rather than inlined, so it costs one line of context.
2. `40-operating-principles.md` — generic operator principles: quality and
   speed, engineering merit, smallest correct changes, operator judgment over
   enforcement gates, ticket scope, and workflow proportionality.
3. `65-agent-identity.md` — the client-neutral identity contract.
4. `agent-overrides/{claude,codex,agy}.md` — client-specific identity bodies.
5. `README.md` — authoring notes for the installed source tree.

Role policy and Orc escalation text are intentionally absent. Those belong in
the product that launches role-specific sessions, not in shared repo context.

## What `polypowers` is

`polypowers` is the portable **SDLC skill bundle** — the shipping pipeline, plus
the prose that governs it. It consists of:

- **Work orchestration** — `ship-feature`, `ship-task`, `ship-quick`,
  `ship-hotfix`, and their `*-queue` batch variants. The end-to-end "claim →
  worktree → plan → implement in phases → review → merge-ready" loop.
- **Planning** — `plan-work` (break a task into phases), `phase-review`
  (per-phase gate).
- **Review** — `final-review` (independent + cross-family review loop before
  merge), `bug-hunt` (parallel multi-reviewer bug sweep), `fix-bug` (drive a bug
  from repro to fix).
- **Spec-driven change** — `openspec-explore` / `-propose` / `-apply-change` /
  `-archive-change` and the `opsx/*` commands (the OpenSpec workflow).
- **Entry points & issues** — `address-issue` (router: reads a GitHub issue and
  dispatches to the right skill), `capture` (turn notes/recordings into tracked
  work), and `issue-status`.
- **Lifecycle** — `cleanup-merge`, `deploy-verify`, `stabilize`.
- **The governing module** — the "constitution": the
  MUST-do rules (e.g. *don't self-review, don't self-merge*), the reviewer-roster
  contract, and the subagent-tier/model model. This is the piece that tells an
  agent *how and when* it must reach for the skills above. It ships as an
  `agent-instructions/` fragment so `polyscribe` can weave it into every repo's
  identity files.

Product-specific skills are **not** in `polypowers`; they stay in their own
product bundles.

## What `polyscribe` does

`polyscribe` is the **agent-instructions assembler**. Instead of hand-editing a
giant `CLAUDE.md`, a repo keeps a modular `agent-instructions/` directory of
small, ordered markdown fragments (identity, workflow, the polypowers governing
module, tools, project overview, …). polyscribe:

- concatenates those fragments in order and **emits the configured per-client
  identity files** — for example `CLAUDE.md`, `AGENTS.md`, or `GEMINI.md` — so
  each configured agent gets the same governance in its own expected file;
- runs automatically at **session start** (as a global hook) in **check-only**
  mode — it reports drift without rewriting tracked files;
- is a **no-op** in any repo that doesn't opt in (no `agent-instructions/` →
  nothing happens), and never clobbers a repo-owned assembler script.

Net: one modular source of truth → consistent, multi-client agent instructions,
checked automatically and rebuilt through tracked reconcile commits. (Shipped as
the org-scoped `polyscribe` hook asset.)

## Depends on

- **`polypowers`** — the bundle described above. nickify degrades gracefully
  until it's published org-wide.
- **`polyscribe`** — the assembler described above.
