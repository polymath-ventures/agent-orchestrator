# Polypowers Init

`/polypowers-init` is the canonical initializer for the Polymath agent standard.
It provisions either the current user account or the current repository by
orchestrating `sx`, agent CLIs, OpenSpec, GitHub, trusted tool sidecars, and the
Polyscribe instruction assembler.

## Canonical state

Repository scope records durable desired state in `polypowers.json` and appends
idempotent run receipts to `.polypowers-init.json`. User scope writes its receipt
to `~/.config/sx/.polypowers-init.json`.

The initializer preserves explicit opt-outs, clients, profiles, subsystem state,
collection scopes, repo-owned source fragments, extensions, and unknown keys.
It adopts only recognized managed markers, refuses unmarked collisions, and
produces no semantic or generated-file change on a second equivalent run.

## User scope

User scope ensures the selected agent CLIs (`claude`, `codex`, `gemini`, `opencode`), a supported `sx`, GitHub CLI, org
assets, and session hooks are installed and repaired for the current OS account.
When the configured sx source is the recognized legacy repository URL, the
initializer backs up the config, changes the source through supported `sx init`,
restores unrelated profile and extension state structurally, and runs
`sx install --repair` for every preserved client. Ambiguous ownership fails
before mutation.

## Repository scope

Repository scope reconciles the selected clients, repo-scoped sx assets,
OpenSpec, the versioned standard instruction set, Polyscribe stubs, trusted tool
requests, Git hygiene, and the managed GitHub pull-request contract. Existing
repo-owned files and disabled subsystems remain untouched.

## Desired-state example

```json
{
	"schema": 1,
	"repository": "owner/name",
	"clients": ["claude-code", "codex", "gemini"],
	"subsystems": {
		"agent_instructions": { "enabled": true, "standard_version": 3 }
	}
}
```

## Nickify compatibility

The deprecated `/nickify` entry point remains available during fleet migration
and delegates to `/polypowers-init`. Canonical runs read legacy `nickify.json`
and `.nickified.json` without modifying them, preserve their unknown fields,
and write the canonical state and receipts above. Exact legacy GitHub markers
and the old sx source URL are recognized only for bounded adoption. See the polypowers-init asset's `MIGRATION.md` for the compatibility and removal contract.

## Standard set

`standard-set.json` records the normalized SHA-256 hashes for the generic
Polypowers governing module, operating principles, worktree reference, identity
contract, and client identity overrides. Repository-local product fragments are
owned by the repository and are never replaced by the standard set.
