<!--
@sx-managed: polypowers-module (nickify refreshes marked copies; remove this line to own the file)
polypowers governing module.

Assembled by polyscribe into a repo's CLAUDE.md / AGENTS.md / GEMINI.md. This is
the generic SDLC constitution: how work is tracked, the rules, and the identity
contract the shared skills defer to. Skills are not listed here — every harness
injects its own listing of what is actually installed. It is
repo-agnostic — no product, repo, or host names. Repo- or product-specific
rules (sensitive paths, deploy targets, and reviewer rosters)
belong in sibling fragments assembled alongside this one.

Response formatting rules are NOT here — they ship as their own vault rule
asset (nhod-response-structure). Don't duplicate them in repo fragments.
-->

## Tracking: GitHub Issues are the sole tracker

The GitHub issue is the durable record and collaboration surface for humans,
agents, automation, dependency relationships, assignment, and status.

1. **New bug/feature/task → `/capture`**, which files the GitHub issue and
   records native blocker or parent relationships when supplied.
2. Durable coordination, design decisions, progress, park reasons, and cold
   handoffs live in issue or pull-request comments. Do not create a parallel
   tracker or mirror GitHub state elsewhere.
3. **TodoWrite/TaskCreate are in-task scratch only.** If losing an item at
   session end would lose information someone else needs, it belongs in the
   GitHub issue, not a local todo.
4. Claim = GitHub assignee. Completion = the closing PR body says `Closes #N`.

## Issue size and PR closure

An epic is simply an issue that has sub-issues. Routers and work skills never
split an issue themselves, and never infer from its prose or scope that it
should be split — that is the user's call. A router handed an epic reports it,
says whether the epic carries work of its own, and asks whether to run the
sub-issues as a queue or to stop there.

Every PR closes exactly one issue and says so in its body (`Closes #N`). The
sole exception is a ministerial post-merge OpenSpec archive PR: it inherits the
parent work item's closure and never gets its own issue. Otherwise,
work that does not close an issue does not get a PR: it either belongs on the
existing open PR where it was found, or it needs an issue first.

## Claim vs author contract

GitHub carries identity in two different ways, and skills must not mix them:

- **Author/creator fields are informational.** GitHub `author` and similar
  creator fields say who filed or created the record.
  They MUST NEVER block dispatch, routing, reservation, cleanup, or review.
- **Only assignee fields gate ownership.** GitHub `assignees` are the active
  claim. Every cross-agent ownership gate keys only on those fields.
- **Unassigned work is claimable.** An issue with no assignee is
  available to any agent identity, regardless of who authored or created it.
- **Starting work claims GitHub.** When an agent begins work, it runs
  `gh issue edit <n> --add-assignee <gh-login>` using the login defined by its
  agent identity.
- **Foreign assignee means park, not steal.** If another agent family is the
  current assignee, park or skip the item unless the user explicitly reassigns
  it. A different author/creator is never a foreign claim.

Skills derive the target GitHub repository from the git remote. An orchestrator
may pin it instead via `POLYPOWERS_REPO=owner/repo` (`AO_PROJECT_REPO` is
honored as a legacy alias).

## Development Rules

Non-negotiable. Violating any of these is a bug in your behavior.

1. **TDD.** Failing test → implement → pass. Every module, endpoint, behavior
   change. Write the failing test for behavior that should exist. Do not add
   tests to make machinery that should not exist look rigorous; deciding
   whether the code should exist happens first, under Rule 9.
2. **Worktree per task — ALWAYS, for ALL mutating work.** Every change you
   make — issue-tracked or ad-hoc, code or docs or config — happens in a
   worktree YOU created under the repo-local agent worktree directory, never in
   the shared main checkout and never in the launcher-supplied session anchor.
   Create it with the ownership helper, which records the
   `polypowers-worktree-owner` marker at creation; nothing backfills it later,
   so a worktree made any other way carries no proof of who owns it. The
   helper, the branch/detach forms, and the session-anchor rules are in
   `agent-instructions/source/35-worktree-recipe.ref.md` — read it when you
   create a worktree.
3. **Test gates.** Fast loop per commit. Before push: full CI (build, format, and tests), then rebase against the default branch — clean → push
   (`--force-with-lease` if rewritten); conflicted → park. Never push a stale
   stack.
4. **Explicit git adds.** `git add <file>` — never `git add .` / `-A`. Never
   disable commit signing to dodge a failure.
5. **Verify before claiming.** Nothing "works" until you exercised it — run
   it, curl it, read the logs, drive the UI. Reviewer and subagent claims are
   leads, not facts — and so is your own premise: before filing, or guarding
   against something, establish that its condition is present in THIS repo,
   because a mechanism that is real in general is not yet a defect here. Where
   that cannot be executed — a latent path, a race, a destructive trigger —
   say what you checked and what you could not, never claiming borrowed
   certainty. (Whether something becomes a ticket at all is rule 8's question,
   not this one; this rule governs the honesty of the claim either way.) The primary agent verifies and reports the exact
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
8. **Fix what you find, in the PR you found it in.** A bug, gap, or cleanup you
   hit while building is part of the work in front of you. If it truly does not
   belong here — a different outcome, or big enough to swamp this change — that
   is a decision to raise with the human, not a ticket to file on your way
   past. (`/bug-hunt` files rather than fixes, by design. `/deploy-verify` owns
   post-merge remediation — fixing forward or rolling back — but anything it
   surfaces arrives after the merge, so there is no longer a PR to fold it
   into.)
9. **Merit.** Build the smallest change that removes the problem.
   Over-engineering is a defect, found and returned in review the same as any
   other. Prefer a prevention, a reusable piece, or a property the code keeps
   true by construction over a detector, a workaround, or a new gate, and
   enforce a rule at the layer that owns the data (the full reasoning is in the
   Operating Principles). In the PR, name the simpler alternative you
   considered and rejected, and say why it did not work.

## The workflow — one skill per phase

Tracked work is routed by type: features go through OpenSpec, bugs go through
the tracker, and spec implementation and bug-fix sessions stay separate. Bugs,
gaps, or cleanup found while already building follow Rule 8 instead.

Start from `/capture` for untracked work and `/address-issue` for anything
already tracked; both route to the right work skill by type. Your harness lists
the installed skills and what they do — read that rather than a copy kept here,
which can only be staler.

## Session habits

**Start ("what's next"):** inspect open GitHub issues assigned to your login,
then unassigned issues with no open blockers. Finish in-progress work first; recommend
1–3 unclaimed items, not the full list.

**End:** update the GitHub issue and PR, run CI, `git pull --rebase && git push`,
and report. Merge only under rule 6's authorization (user's word, or
autonomous mode with the gate satisfied) — never on your own initiative.

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
  passes, `/final-review`, diagnosis, and rescue runs.

## The identity contract — what skills defer to your agent identity

Shared skills describe *process* and resolve the *who/how* from this contract:

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
