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
git merge upstream/main   # merge, not rebase — preserve both histories
```

Remove the worktree in cleanup after landing or parking.

### 3. Conflict resolution defaults (document EVERY file in the PR body: ours/theirs/blend + one line why)

- `ops/**`, `docs/fork.md`, `.github/CODEOWNERS`, SDLC dotfiles (`openspec/`, `agent-instructions/`): **ours** (fork-only surfaces).
- Surfaces the fork never touched: **theirs**.
- Both-sides files: **blend** — upstream as base, fork fixes reapplied; explicitly check whether upstream's version _supersedes_ a fork fix, and say so. The authority for _which_ fork behavior must survive a blend is the **`docs/fork.md` → "Fork Features To Preserve" checklist** — reapply every named feature that lives in a both-sides file (e.g. the codex-fugu **reviewer** registration in `domain.AllReviewerHarnesses` + the `codex.NewFugu()` adapter). If a blend would drop a checklist item, it is a STOP condition (step 4), not a silent "theirs".

### 4. STOP conditions → park (step 7), never resolve unilaterally

- **Migration-version collision** between upstream's new migrations and the fork's applied chain (fork migrations are applied to the production DB — renumbering either side is an operator decision).
- An upstream change that removes/renames something a fork-only surface depends on (deploy script, units, browser-mode seam) with no obvious blend.

### 5. Gates

Full backend suite + `go vet` + `gofmt -l`, frontend typecheck + tests, `npx prettier --check` on changed files, `npm run agents:check`, `node --test ops/*.test.mjs`. All green before PR.

### 6. Land

Open PR `chore(sync): upstream <short-sha-range>` with the conflict table in the body. Run the repo's standard final-review loop (independent reviewer, different family from the executor); post the SHA-pinned `final-review`/`review-passed` statuses only on a clean verdict; merge through the status gate. Then `cleanup-merge`.

**Merge authorization**: the operator granted standing authorization (2026-07-22, recorded here and reviewed into the repo) for landing **clean, fully-gated** sync merges when this skill runs — scheduled or interactive. That grant covers ONLY the clean path: any STOP condition, unresolved finding, or gate failure parks (step 7); the standing grant never extends to merging past an ambiguous gate.

### 7. Park (instead of landing)

On any STOP condition or unresolved review finding: push the branch, open the PR as **draft** with the findings/options written up in the body, and stop. Do not file new issues; the draft PR is the park artifact. The operator (or a directed agent) resumes from it.

## Hard rules

- Never use GitHub's web Sync fork / discard flow.
- Never land with failing gates or without an independent review — a clean git merge is not evidence of semantic safety.
- One sync PR in flight at a time; re-runs while one is open are no-ops.
- Deploy is out of scope — post-merge deploy is the operator's call (`deploy-verify` / `land-and-deploy`).
