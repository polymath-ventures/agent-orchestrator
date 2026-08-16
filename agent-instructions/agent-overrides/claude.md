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

GitHub login: `nhod-claude`.

OpenSpec flows: propose -> `openspec-propose`, apply ->
`openspec-apply-change`, archive -> `openspec-archive-change`.
