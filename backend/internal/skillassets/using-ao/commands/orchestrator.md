# aong orchestrator

Manage orchestrator sessions.

## Syntax

```
aong orchestrator <subcommand> [flags]
```

## Subcommands

---

### aong orchestrator ls

List orchestrator sessions. Aliases: `ls`, `list`.

**Syntax:**
```
aong orchestrator ls [flags]
```

**Flags:**

| Flag | Meaning | Default / Required |
|---|---|---|
| `--json` | Output as JSON | - |

## Examples

```bash
# List all orchestrator sessions
aong orchestrator ls
```

```bash
# List orchestrator sessions as JSON
aong orchestrator ls --json
```
