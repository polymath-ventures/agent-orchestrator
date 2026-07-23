<!-- GENERATED — DO NOT EDIT. Edit agent-instructions/{source,agent-overrides,system}/, then rebuild: bash scripts/polyscribe.sh (system scope adds --system) -->

# Agent Orchestrator

Agent Orchestrator is a Go daemon and web-first React supervisor for
coordinating coding-agent sessions. The backend owns durable lifecycle state,
storage, runtime adapters, and the HTTP API. The frontend is a thin supervisor
surface over the generated API client.

This fork tracks upstream closely while carrying Polymath SDLC wiring and a
curated set of ported features. Keep product changes small, rebase-friendly,
and suitable for later upstream submission unless a ticket is explicitly
fork-only.

## Tracking: GitHub Issues + Beads, always paired

Durable work lives in **two places on purpose**:

- The **GitHub issue** is the canonical record and the collaboration surface —
  what humans, other agents, and CI see and link to.
- The **Bead** (`bd`) mirrors it and adds what GitHub lacks: dependency edges,
  claims, ready/blocked queries, cross-agent state.

The pairing rules:

1. **New bug/feature/task → `/capture`**, which files the GitHub issue _and_
   the linked bead (`Tracks GH #N`) together. Never one without the other.
2. Issues filed outside `/capture` (bulk filings, web UI) get beads backfilled
   via `/sync-issues-to-beads`. Audit before ending a filing or queue session:
   any open bead without a `Tracks GH #…` link either gets linked or gets a
   written reason it's internal-only.
3. Raw `bd create` without a GitHub issue is reserved for explicit
   internal-only records and tool-managed follow-ups.
4. **TodoWrite/TaskCreate are in-task scratch only** — sub-steps of the bead
   you've claimed. If losing it at session end would lose information someone
   else needs, it's an issue + bead, not a todo.
5. **No beads? Degrade, don't stop.** On a repo without `.beads/`, the GitHub
   issue is the sole tracker and every skill runs in GitHub-only mode
   (claim = GH assignee, close = `Closes #N`).

## Claim vs author contract

Trackers carry identity in two different ways, and skills must not mix them:

- **Author/creator fields are informational.** GitHub `author`, Beads `owner`,
  Beads `created_by`, and similar fields say who filed or created the record.
  They MUST NEVER block dispatch, routing, reservation, cleanup, or review.
- **Only assignee/claim fields gate ownership.** GitHub `assignees` and the
  Beads `assignee` set by `bd update <id> --claim` are the active claim. Every
  `EXPECTED_ASSIGNEE` check and cross-agent ownership gate keys only on those
  fields.
- **Unassigned work is claimable.** A linked issue or bead with no assignee is
  available to any agent identity, regardless of who authored or created it.
- **Starting work claims both trackers.** When an agent begins work, it claims
  the bead with `bd update <id> --claim` when Beads are present and mirrors the
  claim to GitHub with `gh issue edit <n> --add-assignee <gh-login>`.
- **Foreign assignee means park, not steal.** If another agent family is the
  current assignee, park or skip the item unless the user explicitly reassigns
  it. A different author/creator is never a foreign claim.

## Beads backend — shared host is configuration, not code

A repo's `bd` may attach to a **shared beads host** so every agent — across
machines and accounts — sees the same live state. This is configured per repo,
never hardcoded in skills:

- The attachment is established at repo setup (`/nickify`) or by a
  session-start hook: `BEADS_DIR`, a shared Dolt server
  (`bd init --server …` / `--database …`), or an orchestrator-provisioned DB.
- When `.beads/metadata.json` records canonical shared-server metadata —
  `dolt_mode = "server"` plus non-empty `dolt_server_host` and `dolt_database` — durable `bd` writes
  MUST reach that shared backend. A session that can't reach it does not fake
  durability: file the GitHub half (the issue) now, and materialize the bead
  later via `/sync-issues-to-beads` from a connected host.
- Skills assume `bd` is attached to whatever the repo configured and never
  select or name a host themselves. Put the host specifics in a repo fragment,
  not in a skill.
- Similarly, skills derive the target GitHub repo from the git remote; an
  orchestrator may pin it instead via `POLYPOWERS_REPO=owner/repo`
  (`AO_PROJECT_REPO` honored as a legacy alias).

## Development Rules

Non-negotiable. Violating any of these is a bug in your behavior.

1. **TDD.** Failing test → implement → pass. Every module, endpoint, behavior
   change. Write the failing test for behavior that should exist. Do not add
   tests to make machinery that should not exist look rigorous; deciding
   whether the code should exist happens first, under Rule 9.
2. **Worktree per task — ALWAYS, for ALL mutating work.** Every change you
   make — bead-tracked or ad-hoc, code or docs or config — happens in a
   worktree YOU created under the repo-local agent worktree directory:

   ```bash
   DEFAULT_BRANCH_REF="$(git symbolic-ref --quiet refs/remotes/origin/HEAD 2>/dev/null || echo refs/remotes/origin/main)"
   DEFAULT_BRANCH="${DEFAULT_BRANCH_REF#refs/remotes/origin/}"
   git fetch origin "$DEFAULT_BRANCH"
   git worktree add .claude/worktrees/<slug> -b <branch> "origin/$DEFAULT_BRANCH"
   ```

   Run this from the main repo root, never inside another worktree, then install
   deps. Fetch and branch from the remote ref even when the local default branch
   appears clean; a clean local branch can still be stale.
   `.claude/worktrees/` is the shared convention for Claude, Codex,
   Gemini, and other agents; the `.claude` path name is historical, not a
   Claude-only boundary. Do not place working copies under `.git/worktrees/` —
   that is Git's private metadata directory for linked worktrees. Derive the
   default branch — don't assume `main`. **The shared main checkout root is
   read-only ground truth**:
   never commit or switch branches there, and treat its files as read-only
   during ordinary task work — other agents (and the user) rely on its state.
   The `cleanup-merge` lifecycle is the one narrow exception: it may
   fast-forward the worktree that already owns the default branch only after
   confirming that checkout is clean, and it must never switch that checkout's
   branch. Fetch-only sync of refs is always fine. A Codex-supplied detached
   worktree may itself have been created
   from a stale local branch before session-start logic ran. Never reset or move
   a supplied worktree that may contain active work; use it only as launch
   context and create the required task worktree from the freshly fetched
   remote ref as above.

3. **Test gates.** Fast loop per commit. Before push: full CI (build, format, and tests), then rebase against the default branch — clean → push
   (`--force-with-lease` if rewritten); conflicted → park. Never push a stale
   stack.
4. **Explicit git adds.** `git add <file>` — never `git add .` / `-A`. Never
   disable commit signing to dodge a failure.
5. **Verify before claiming.** Nothing "works" until you exercised it — run
   it, curl it, read the logs, drive the UI. Reviewer and subagent claims are
   leads, not facts: the primary agent verifies them and reports the exact
   command and exact error; "not installed" does not mean "unavailable." You
   have access to browsers, screenshots, web search, and other tools; use them.
6. **Don't self-review; merge only with authorization.** Independent review
   belongs to a different model family (see the identity contract below) —
   never to the implementer. Merging requires **explicit authorization**, which
   comes in exactly two forms: the user says so in the session, or the session
   runs in **autonomous mode** (`POLYPOWERS_AUTOMERGE=1` set by the
   orchestrator, or a queue invoked with `--merge`).

   In autonomous mode, the agent merges **only after the full gate**:
   final-review verdict clean, CI green, all current-head inline review threads
   resolved — then immediately runs `/cleanup-merge` and `/deploy-verify`.

   A repo fragment may forbid autonomous merge outright, or mark **sensitive
   paths** — when the PR diff touches a marked path, autonomous mode parks the
   merge-ready PR for a human instead of merging, stating which path triggered
   it. Fragments may never grant autonomy implicitly.

7. **Specs go through the OpenSpec tooling.** Canonical `openspec/specs/` is
   read-only outside checkbox/date/gap-note edits. Most new features can
   benefit from using `/opsx:explore` to explore and plan out the feature
   before moving on to requirements. Every requirement change is
   `/opsx:propose` → `/opsx:apply` → `/opsx:archive`, validated. No
   `--skip-specs`, no hand-made or hand-archived change dirs.
8. **Bugs found while building ship in the same PR** when the fix belongs to
   the same outcome and does not significantly enlarge the change or reach into
   a separate part of the system. When the fix does not fit that test, file a
   separate issue and note that you deferred it. Document with an issue and
   bead when it helps. (By-design exceptions: `/bug-hunt` files-only;
   `/deploy-verify` post-merge findings.)
9. **Merit.** Build the smallest change that removes the problem.
   Over-engineering is a defect, found and returned in review the same as any
   other. Prefer a prevention, a reusable piece, or a property the code keeps
   true by construction over a detector, a workaround, or a new gate, and
   enforce a rule at the layer that owns the data (the full reasoning is in the
   Operating Principles). In the PR, name the simpler alternative you
   considered and rejected, and say why it did not work.

## The workflow — one skill per phase

Features go through OpenSpec; bugs go to the tracker; keep spec-implementation
and bug-fix sessions separate.

**Start here (routing entry points):**

- `/capture <description>` — untracked idea/bug/task → GH issue + bead +
  (features) `/opsx:propose`, then hands off to `/address-issue`. Flags:
  `--type`, `--priority`, `--quick`, `--no-ship`, `--openspec=<change>`.
- `/address-issue <id>` — existing issue/bead → dispatches by type: bug →
  `/fix-bug`; feature with spec → `/ship-feature`; feature without →
  `/opsx:propose` then `/ship-feature`; task → `/ship-quick` or `/ship-task`;
  prose-only → `/ship-hotfix`.

**Work skills (invoke directly when the shape is known):**

- `/ship-feature <id>` — phased feature work against an OpenSpec change:
  claim, worktree, `/plan-work`, per-phase TDD, opt-in `/phase-review`,
  `/final-review` loop, merge-ready report. `--no-spec` for phased non-spec
  work.
- `/ship-task <id>` — thin wrapper: `/ship-feature --no-spec`.
- `/fix-bug <id>` — reproduce-first bug flow with bounded
  investigate-fix-verify cycles, regression coverage, `/final-review`.
- `/ship-quick <id|desc>` — tiny changes; one cross-family adversarial review
  cycle. `/ship-hotfix` — prose-only; skips tests, single review pass.

**Quality and lifecycle:**

- `/bug-hunt` — parallel multi-reviewer hunt (`--high|--medium|--security`,
  `--scope`); dedupes, files survivors; fixes go through `/fix-bug`.
- `/final-review` — the pre-merge gate: independent cross-family review loop +
  optional PR-integrated reviewer, monitored to a verdict.
- `/address-issue-queue` — unattended batch runner; parks blockers, continues.
  (`/ship-feature-queue`, `/ship-task-queue`, `/fix-bug-queue` forward here.)
- `/cleanup-merge` — post-merge: close beads, archive OpenSpec, remove
  worktree, delete branch. `/deploy-verify` — deploy + verify live.
- `/sync-issues-to-beads` — GH → beads backfill (see Tracking above).

## Session habits

**Start ("what's next"):** check `bd list --status=in_progress --assignee=@me`,
`bd ready`, `bd blocked` (or open GH issues on beads-less repos). Finish
in-progress work first; recommend 1–3 unclaimed items, not the full list.

**End:** close/update beads and issues, run CI, `git pull --rebase && git
push`, report. Merge only under rule 6's authorization (user's word, or
autonomous mode with the gate satisfied) — never on your own initiative.

## Final-review status contract

The clean status is the only machine-readable final-review verdict the merge
gate may consume.

`/final-review` emits its verdict as a GitHub commit status on the reviewed head
SHA, using context `final-review`. A clean review writes `state=success`; a
non-clean, inconclusive, or timed-out review writes `state=failure`. The status
description is the parseable contract: `verdict=<clean|parked>
reviewer_family=<family> head=<full-head-sha>`. A clean review that is parked
only because repo policy requires a human merge still writes
`final-review=success`; the human gate is recorded separately as a current-head
`merge-park` status with `reason=human-required`.

Human merge gates check the `final-review` status on the **current** PR head
SHA. Autonomous-merge paths check the same clean review status and additionally
refuse to merge when a current-head `merge-park` signal exists or when a linked
issue carries the manual non-AO-worker hold marker `no-ao`. If the PR receives
a new push, the old statuses are tied to the old SHA and no longer count. This
replaces any PR-comment protocol; do not use comments or free-form summaries as
the gate.

AO's native review API (`GET /sessions/{id}/reviews`, with states such as
`ineligible` or `needs_review`) is a separate AO reviewer system. It is useful
for AO's own review UI, but it is **not** `/final-review` and must never be read
as the final-review merge verdict.

Repos that carry `ops/final-review-status.mjs` use it as the status helper:
`node ops/final-review-status.mjs set --repo <owner/repo> --sha
<full-head-sha> --verdict <clean|parked> --reviewer-family <family>
--author-family <implementer-family>` after the review loop; add
`--human-merge-required` when a clean review must park for human merge authority.
A clean `set` **requires** one or more `--author-family` values and is
**refused** when `--reviewer-family` matches any of them. Reviewer independence
is enforced here, at write time, so a clean status is independent by
construction. Pass several `--author-family` flags when more than one family
authored the head. Use `node ops/final-review-status.mjs check --repo
<owner/repo> --sha <current-head-sha>` for a human-authorized merge gate, and
add `--mode autonomous --pr <PR-number>` for autonomous merge eligibility. The
`check` command is deliberately family-agnostic because independence was already
enforced at `set` time, so the required `review-passed` merge-queue gate, which
cannot see per-session harness provenance, is never bricked.

## Agent reviewers run in the foreground

Operator standing rule: agent and harness invocations that a worker starts for
implementation, review, final-review, diagnosis, or rescue work run in the
foreground/attached. Do not background reviewer or diagnostic agents.

- A foreground invocation is attached, observable, and fails loudly.
- A long review uses the maximum foreground timeout; if it still does not fit,
  split it into smaller foreground passes and re-run. Do not detach to dodge a
  shell's time cap.
- If a reviewer hangs at startup, use the active workflow's or harness's
  narrower startup fallback for that run, still attached. Optional integrations
  that fail at startup may be disabled for that foreground run when the harness
  supports it.
- This binds every agent invocation a worker or orchestrator drives for review
  passes, `/final-review`, diagnosis, and rescue runs. AO's own daemon launch of
  worker sessions into a TTY is already blocking/attached and stays that way.

## The identity contract — what skills defer to your agent identity

Shared skills describe _process_ and resolve the _who/how_ from this contract:

- **Subagents**, by capability tier: lightweight for triage and monitoring;
  standard for reproduction, implementation, and verification; deep reasoning
  for root-cause analysis and architecture; planner for design-only work. Each
  agent identity maps these tiers to its available mechanics. Prefer a subagent
  for any substantial phase; you orchestrate.
- **Many-eyes review pool** — reviews exist for diversity of failure modes. The
  primary independent reviewer is a **different local reviewer agent**,
  preferably a different model family and independent of the implementer. The
  agent identity defines the available reviewer roster and invocation mechanics.
  One reviewer is never a review, and a single integrated reviewer is never
  enough.
- **Review monitor** — a lightweight subagent watches cross-cycle patterns
  (ping-pong, convergence) and calls the verdict; the orchestrator fixes.
- Repo fragments may extend this contract (name a roster, add gates); they may
  not weaken rules 6–9 above.

## Operating Principles

Quality and speed are both non-negotiable. The Polymath agent system produces high-quality work quickly by using the full workflow: tracked work, fresh worktrees, focused planning, subagents where they help, real verification, and independent review. Do not trade quality for speed. The most reliable way to have both is to build less, because the smallest correct change is the fastest to write, the fastest to review, and the least likely to break something later.

Build the smallest change that makes the problem stop existing. This is required, and over-engineering is a defect that a reviewer sends back the same as any other bug. Before writing anything, understand the whole area the change touches, then look for the fix that is smallest and closest to the cause. Work at the layer that owns the data, and at the moment the state is first set. When you find yourself writing a check, a background loop, or a second piece of code that cleans up after the first, you are usually working one layer too far out and one step too late. The better fix is closer to where the data is authoritative, and earlier, at the point where the state is created.

Prefer a prevention, a small reusable piece, or a property the code keeps true by construction, over a detector, a workaround, or a new gate. Fix the cause rather than the case: a change that removes the need for the behavior is better than one that handles a single occurrence of it, and writing a second handler for the same kind of failure is a sign the cause is still present. Keep each fact in one place and derive the rest, because two copies that have to agree will eventually disagree. Put a rule in the component that owns the data, not in a caller that holds only a copy and has to re-derive it. Set state at the moment it changes, rather than building a loop that looks for the change later. Ask whether an existing piece already does the work, and whether the new piece needs to exist at all, before adding it. Weigh the cost honestly: a detector, a gate, or a polling loop runs on every future execution, adds one more place the system can fail, and tends to fail when its result matters most, while a prevention is paid for once and then removes work instead of adding it. Reach for a detector, a fallback, or a new gate only after you can show that the property cannot be kept where the state is created, and record that reasoning in the PR.

This fleet ran well under manual operation for months. Automating it means carrying the operator's judgment to more agents at once, so the work is to reproduce that judgment rather than to build defenses against agents that are assumed to fail. Prefer a clear instruction over an enforcement gate, and prefer showing the operator a problem over blocking the work automatically. Require a person only where a mistake would be severe and hard to reverse. If the automated behavior is more cautious than the operator would be working by hand, the automation is wrong, not the operator.

Every ticket should deliver real incremental value. Each ticket pays the full cost of CI, review, and merge coordination, so do not split adjacent fixable work into separate tickets without reason. When a bug, cleanup, or small missing piece turns up while building the current ticket, include it in the current PR when it belongs to the same outcome and does not significantly enlarge the change or reach into a separate part of the system. When it does not fit that test, file a separate issue and note that you deferred it. Finding that the ticket as scoped is materially larger than the need, or duplicates something that already exists, is itself a reason to stop and re-scope.

Tickets sail once started. Resolve ambiguity from the ticket, repo conventions, existing specs, these principles and the development rules, memory subsystems, and the decision defaults recorded on the issue. After work starts, do not stop for preference questions. Take a reasonable defensible option, note the assumption in the PR, and keep moving, unless the decision is a product call, a destructive action, an authorization gate, or a finding that the ticket should be smaller or should not be built at all.

Use the resources available to you. An agent is one node in a larger system with specialized skills, capability-tiered subagents, independent reviewers, and multiple model families. Substantial phases should use that system: planning and architect passes for design, focused subagents for bounded work, and independent review for merge readiness. Context is one of our most precious resources and is easy to exhaust, and subagents can significantly help prevent that exhaustion. Acting alone on hard work while the roster exists is a process failure, and so is wrapping light work in heavy process. Match the weight of the workflow to the size of the work.

# Repo-Specific Guidance

## Repo layout

- `backend/` contains the Go daemon, Cobra CLI, services, storage, runtime
  adapters, lifecycle/reaper, terminal mux, and tests.
- `frontend/` contains the React web supervisor wired to the generated daemon
  client, plus upstream Electron shell code that this fork preserves for
  compatibility.
- `docs/` contains current architecture and status notes. Start here before
  changing lifecycle, CLI, agents, storage, or daemon behavior.
- `test/` contains external smoke/e2e assets, including the CLI fresh-install
  container check.
- `.github/workflows/` contains CI definitions. Mirror these commands locally
  when possible.

## Fork posture

This fork is web-first. Treat the browser-based supervisor talking to the daemon
over HTTP as the primary product path. Electron behavior is upstream
compatibility surface, not the default assumption for Polymath work. Do not make
frontend correctness depend on `window.ao`, Electron preload APIs, Electron-only
daemon status fields such as a discovered port, or desktop packaging behavior
unless the ticket explicitly targets Electron. For frontend changes, include or
preserve web-mode coverage when the behavior can run in a browser.

## Commands

From the repo root unless noted:

```bash
npm run lint
npm run frontend:typecheck
npm run sqlc
npm run api
npx @redwoodjs/agent-ci run --all
```

### Pre-push gate (required before every push)

Run the local CI-parity gate before pushing — it mirrors the remote `format`
and `lint` CI jobs (plus build/vet/test/typecheck) so those violations are
caught locally instead of on a wasted remote CI round-trip:

```bash
npm run ci-local
```

It runs, fail-fast and cheapest-first: `format:check` (prettier `--check
--ignore-unknown` on changed files, matching `.github/workflows/prettier.yml`),
`gofmt`, `go build`, `go vet`, `go test -race ./...`, golangci-lint (pinned to
the CI version v2.12.2, run via `go run` — no separate golangci install needed),
and `npm run frontend:typecheck`. `npm run format:check` is the fast
changed-files-only subset if you just need the format check.

Optionally install it as a git `pre-push` hook (per-clone, opt-in) so it runs
automatically on `git push`; bypass a single push with `git push --no-verify`:

```bash
npm run hooks:install
```

Backend-specific checks:

```bash
cd backend
go build ./...
go test ./...
go test -race ./...
go vet ./...
go run ./cmd/ao start
```

Frontend-specific checks:

```bash
cd frontend
npm run typecheck
npm run build
```

When showing or demoing frontend changes, run `ao preview [url]` from inside
the session so the change renders in the desktop browser panel.

## Distribution

For this fork, the web supervisor is the canonical product direction. Upstream
Electron packaging may remain in the tree for compatibility, but new product
flows should not assume the desktop app is the user's runtime unless the ticket
explicitly says so. The `@aoagents/ao` npm package remains a frozen legacy
on-ramp at `0.10.0`; do not add new features, docs, or flows that treat npm as
the intended install path.

## Coding conventions

Keep changes surgical and tied to the task. Follow existing Go package
boundaries. CLI code should call daemon HTTP routes through shared CLI client
helpers; it should not open SQLite, spawn runtimes, or call adapters directly.

Return usage errors as `usageError` so CLI misuse exits 2; runtime or daemon
failures should exit 1. Preserve API error envelopes and request IDs when
surfacing daemon errors. Use `context.Context` as the first argument for
functions that do I/O or blocking work.

Do not modify already-merged SQLite migrations. Add a new migration instead.
Do not hand-edit generated sqlc code; change queries or migrations and run
`npm run sqlc`. For daemon API contract changes, edit controller DTOs and the
spec generator, then run `npm run api` and commit the generated OpenAPI and
TypeScript schema updates together.

The daemon primary listener stays bound to `127.0.0.1` and unauthenticated. A
second opt-in LAN listener may bind `0.0.0.0` only while explicitly enabled and
only behind bearer-password auth, as documented in
`docs/adr/0001-lan-listener-for-mobile.md`.

All app state belongs under `~/.ao` unless explicitly overridden by
`AO_DATA_DIR` or `AO_RUN_FILE`. Do not rely on Electron default app-data paths.

## Agent Identity Contract

Shared skills describe process; each agent identity supplies the concrete
mechanics. The client-specific identity body appended after this module defines
how that client invokes skills, spawns subagents, runs independent review,
monitors review cycles, maps Beads assignees to GitHub logins, and invokes
OpenSpec flows.

Subagents are selected by capability tier, not by a hardcoded model in shared
skill text:

1. **Lightweight** — classification, triage, monitoring, and narrow checks.
2. **Standard** — reproduction, implementation, and verification work.
3. **Deep reasoning** — root-cause analysis, architecture, and design-only
   planning.

Use subagents for substantial phases when they can advance work without
duplicating the main thread. Keep the immediate critical path local when waiting
would slow the work down, and delegate bounded sidecar tasks with clear file or
responsibility ownership.

Independent review is required before merge readiness. The primary reviewer
must be independent of the implementer and preferably from a different model
family. A PR-integrated bot can supplement the review, but it is never the only
reviewer. If no independent local reviewer is available and authenticated, stop
at the review gate and report the missing roster instead of self-reviewing.

The review monitor watches cycle history for convergence, persistent findings,
and ping-pong. It can be a lightweight subagent or an explicit inline pass, but
the result must be recorded before a PR is declared merge-ready.

Nested agent CLI launches must scrub parent AO session credentials from the
child environment. Use the explicit form below for reviewer, fixer, diagnostic,
or other peer-agent subprocesses:

```bash
env -u AO_SESSION_ID -u AO_RUNTIME_TOKEN -u AO_RUN_FILE <agent-cli> ...
```

The scrub applies only to the child process. Do not unset those variables in the
parent pane; parent hooks still need them to authenticate activity for the
current session.

## Agent Identity (Claude)

Resolve "spawn a subagent" and "run your review pool" here. This is the
Claude-specific identity.

### Skill Invocation

Skills install to `.claude/skills/<name>/SKILL.md`; invoke one explicitly with
`/<name>`. Ignore `.agents/skills/` for discovery; that is Codex's tree.

### Subagent Spawning

Use the `Agent` tool. Capability tier maps to the available Claude subagent
mechanics:

1. **Lightweight** — `haiku` or `sonnet`.
2. **Standard** — `general-purpose` with the session model.
3. **Deep reasoning** — `opus` or `sonnet`.

Prefer a subagent for substantial phases; inline work is fine for trivial
changes.

### Many-Eyes Review Pool

Build a local reviewer CLI roster and preflight it before final review. Prefer
Codex or Codex Fugu when installed, authenticated, and able to read the PR diff
from the worktree. Fire GitHub Copilot once when available, then poll it between
independent review cycles. Copilot-only review is not enough.

If no non-Claude reviewer CLI is installed, logged in, and able to read
`gh pr diff`, stop and report the missing roster.

### Review Monitor

Use a lightweight Claude subagent for cross-cycle pattern matching when
available, or monitor inline by tracking findings cycle by cycle.

### Identity Facts

Beads assignee family: `polymath-claude`.

Default Beads-to-GitHub map: `nhod-claude|polymath-claude -> nhod-claude`,
`nhod-codex|polymath-codex -> nhod-codex`.

OpenSpec flows: propose -> `openspec-propose`, apply ->
`openspec-apply-change`, archive -> `openspec-archive-change`.
