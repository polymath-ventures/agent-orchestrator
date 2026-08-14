---
name: sync-upstream
description: Merge the upstream repo (AgentWrapper/agent-orchestrator) into the fork's main as a fully gated PR — auto-lands when clean, parks a draft PR with findings when judgment is needed. Safe to run daily/idempotently; no-ops when already up to date. Use when the user says "sync upstream", "pull upstream", or on a scheduled routine.
---

# Sync Upstream

Keep `polymath-ventures/agent-orchestrator` current with `AgentWrapper/agent-orchestrator` **without ever landing unreviewed code**. Every sync goes through the repo's standard merge gate (final-review status contract + independent review). GitHub's web "Sync fork" button must never be used on this fork (its only conflict option discards our commits).

## Steps

### 1. No-op check

```bash
cd "$(git rev-parse --show-toplevel)"
git remote get-url upstream 2>/dev/null || git remote add upstream git@github.com:AgentWrapper/agent-orchestrator.git
git fetch upstream --quiet && git fetch origin --quiet
BEHIND=$(git rev-list --count origin/main..upstream/main)
```

If `BEHIND` is 0: report "already up to date" and STOP. Also stop (report, don't queue) if an open PR titled `chore(sync): upstream` already exists — one sync in flight at a time.

### 2. Merge worktree

Never mutate the shared checkout. Create an isolated worktree and merge there:

```bash
git worktree add .claude/worktrees/sync-upstream-<YYYYMMDD> -b sync/upstream-<YYYYMMDD> origin/main
cd .claude/worktrees/sync-upstream-<YYYYMMDD>
git merge origin/main     # absorb any main movement after branch creation
git merge upstream/main   # preserve both histories
```

Remove the worktree in cleanup after landing or parking.

**Never rebase a sync branch — this overrides the generic rule.** The repo
constitution (`CLAUDE.md` / `AGENTS.md` Rule 3, "Test gates") tells you to rebase
against the default branch before pushing. That rule is correct for ordinary
feature branches and wrong here, and this skill deliberately overrides it for
sync branches. Do not reconcile the two by rebasing "just this once."

Rebasing replays upstream commits as fork commits, destroys the shared merge
ancestry that makes the next sync tractable, and reopens conflicts already
resolved by an earlier sync. If `origin/main` moves, merge it into the sync
branch, rerun the affected verification, and repeat final review for the new
head.

### 3. Conflict resolution defaults (document EVERY file in the PR body: ours/theirs/blend + one line why)

- `ops/**`, `docs/fork.md`, `.github/CODEOWNERS`, SDLC dotfiles (`openspec/`, `agent-instructions/`): **ours** (fork-only surfaces).
- Surfaces the fork never touched: **theirs**.
- Both-sides files: **blend** — upstream as base, fork fixes reapplied; explicitly check whether upstream's version _supersedes_ a fork fix, and say so. The authority for _which_ fork behavior must survive a blend is the **`docs/fork.md` → "Fork Features To Preserve" checklist** — reapply every named feature that lives in a both-sides file (e.g. the codex-fugu **reviewer** registration in `domain.AllReviewerHarnesses` + the `codex.NewFugu()` adapter). If a blend would drop a checklist item, it is a STOP condition (step 4), not a silent "theirs".

#### Routine migration reconciliation

Migration-version collisions are expected when both histories add migrations;
they are sync work, not by themselves a STOP condition. Preserve every applied
fork migration at its existing filename and version. Never renumber an existing
fork migration or rewrite `goose_db_version`: production has already recorded
those identities.

For incoming upstream migrations:

1. Identify already-ported upstream migrations by purpose, filename suffix,
   and SQL content even when the fork previously assigned a different version.
   Keep the fork copy and remove the duplicate incoming path.
2. Rename only genuinely new upstream migration files, assigning consecutive
   unused versions above the fork's current maximum while preserving their
   upstream relative order. This is the fork's durable upstream-to-fork mapping;
   document every mapping in the PR body.
3. Adapt each incoming migration to the schema produced by the applied fork
   chain. Exact `sqlite_master` replacements and other state-sensitive SQL must
   match the fork's current schema and preserve fork-only values; a version-only
   rename is not sufficient evidence.
4. Run the unique-version test plus migration tests from both a fresh database
   and an upgrade fixture representing the current production migration level.

Park only when a migration cannot be mapped or adapted without choosing between
different product/data outcomes. The existence of a numeric collision is not
such a decision.

### 4. Inspect merge commits and prove fork behavior

Before the general gates, read every **Behavioral guards** entry in
`docs/fork.md`'s sync checklist and inspect the upstream-absorption merge itself
for every sync-anchor path it could have changed:

```bash
git log -m --merges --name-status --oneline "origin/$DEFAULT_BRANCH..HEAD" -- <anchored-paths-from-docs/fork.md>
```

The `-m` is mandatory: ordinary `git log` hides the per-parent diffs where an
upstream merge can disconnect a fork feature without deleting its files. Sync
anchors locate the implementation; they do not prove integration. Anchor path
existence alone is never sufficient.

Run every named behavioral guard, including the browser-mode guards that are not
part of the local CI-parity gate: `frontend/e2e/browser-mode.spec.ts`,
`frontend/e2e/mobile-sidebar-toggle.spec.ts`, and
`frontend/e2e/terminal-focus.spec.ts`, plus the fork UI mount guard in
`frontend/e2e/fork-features.spec.ts`.

```bash
set -euo pipefail
npm run ci-local
npm run agents:check
(
	cd frontend
	AO_E2E_PORT="${AO_E2E_PORT:-5174}" npm run test:e2e -- \
		e2e/browser-mode.spec.ts \
		e2e/mobile-sidebar-toggle.spec.ts \
		e2e/terminal-focus.spec.ts \
		e2e/fork-features.spec.ts
)
```

`npm run ci-local` covers the named backend, frontend Vitest, and ops guards;
the targeted Playwright command covers the integration guards whose absence
allowed mounted UI features to disappear. If a checklist item names a guard
outside those commands, run it explicitly too. Any named behavioral-guard
failure is a STOP condition: do not land the sync, even when every anchor still
exists.

### 5. STOP conditions → park (step 8), never resolve unilaterally

- An upstream change that removes/renames something a fork-only surface depends on (deploy script, units, browser-mode seam) with no obvious blend.
- A named fork-feature behavioral guard fails or cannot be run.

### 6. Gates

The commands in step 4 are the blocking sync gate. All green before PR.

### 7. Land

Open PR `chore(sync): upstream <short-sha-range>` with the conflict table in the body. Run the repo's standard final-review loop (independent reviewer, different family from the executor); post the SHA-pinned `final-review`/`review-passed` statuses only on a clean verdict; merge through the status gate. Then `cleanup-merge`.

**Merge authorization**: the operator granted standing authorization (2026-07-22, recorded here and reviewed into the repo) for landing **clean, fully-gated** sync merges when this skill runs — scheduled or interactive. That grant covers ONLY the clean path: any STOP condition, unresolved finding, or gate failure parks (step 8); the standing grant never extends to merging past an ambiguous gate.

### 8. Park (instead of landing)

On any STOP condition or unresolved review finding: push the branch, open the PR as **draft** with the findings/options written up in the body, and stop. Do not file new issues; the draft PR is the park artifact. The operator (or a directed agent) resumes from it.

## Hard rules

- Never use GitHub's web Sync fork / discard flow.
- Never rebase a sync branch; merge `origin/main` and re-review the resulting head. This overrides Rule 3's generic "rebase against the default branch" instruction, which governs feature branches, not sync branches.
- Never land with failing gates or without an independent review — a clean git merge is not evidence of semantic safety.
- One sync PR in flight at a time; re-runs while one is open are no-ops.
- Deploy is out of scope — post-merge deploy is the operator's call (`deploy-verify` / `land-and-deploy`).
