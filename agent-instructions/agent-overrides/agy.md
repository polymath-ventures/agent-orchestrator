<!--
@sx-managed: agy-identity (nickify refreshes marked copies; remove this line to own the file)
-->

## Agent Identity (Agy)

Resolve "spawn a subagent" and "run your review pool" here. Agy is the
Gemini-family agent identity.

### Skill Invocation

Skills install to the Gemini skills directory; invoke one per Gemini's
conventions. Ignore `.claude/skills/` and `.agents/skills/` for discovery.

### Subagent Spawning

Use Gemini subagents. Capability tier maps to the available Gemini mechanics:

1. **Lightweight** — `gemini-2.5-flash`.
2. **Standard** — `gemini-2.5-pro`.
3. **Deep reasoning** — `gemini-2.5-pro`, high effort.

Prefer a subagent for substantial phases; inline work is fine for trivial
changes.

### Many-Eyes Review Pool

Build a local reviewer CLI roster and preflight it before final review. Prefer
Claude, Codex, or Codex Fugu when installed, authenticated, and able to read the
PR diff from the worktree. Fire GitHub Copilot once when available, then poll it
between independent review cycles. Copilot-only review is not enough.

If no non-Gemini reviewer CLI is installed, logged in, and able to read
`gh pr diff`, stop and report the missing roster. If `gh` is unavailable in the
runtime, hand the remote-CI watch, reviewer roster setup, and Copilot request to
the operator.

### Review Monitor

Monitor inline by tracking findings cycle by cycle and escalating ping-pong.

### Identity Facts

GitHub login: `nhod-agy`.

OpenSpec flows: propose -> `openspec-propose`, apply ->
`openspec-apply-change`, archive -> `openspec-archive-change`.
