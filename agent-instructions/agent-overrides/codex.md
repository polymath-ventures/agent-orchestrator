<!--
@sx-managed: codex-identity (polypowers-init refreshes marked copies; remove this line to own the file)
-->

## Agent Identity (Codex)

Resolve "spawn a subagent" and "run your review pool" here. This is the
Codex-specific identity.

### Skill Invocation

Skills install to `.agents/skills/<name>/SKILL.md`; invoke one explicitly with
`$<name>`. Ignore `.claude/skills/` for discovery; that is Claude's tree.

### Subagent Spawning

Use Codex subagents. Capability tier maps to the available Codex subagent
mechanics:

1. **Lightweight** — `gpt-5.4-mini`, low or medium effort.
2. **Standard** — `gpt-5.4`.
3. **Deep reasoning** — `gpt-5.4`, high effort.

Prefer a subagent for substantial phases; inline work is fine for trivial
changes.

### Many-Eyes Review Pool

Build a local reviewer CLI roster and preflight it before final review. Prefer a
different-family reviewer such as `claude -p` when installed, authenticated, and
able to read the PR diff from the worktree. Use `codex-fugu` only as a
non-implementer fallback when no different-family reviewer is available. Fire
GitHub Copilot once when available, then poll it between independent review
cycles. Copilot-only review is not enough.

If no reviewer CLI other than this Codex worker is installed, logged in, and
able to read `gh pr diff`, stop and report the missing roster.

### Review Monitor

Monitor inline by tracking findings cycle by cycle, or spawn a lightweight
Codex subagent when keeping the parent context clean matters.

### Identity Facts

GitHub login: `nhod-codex`.

OpenSpec flows: propose -> `openspec-propose`, apply ->
`openspec-apply-change`, archive -> `openspec-archive-change`.
