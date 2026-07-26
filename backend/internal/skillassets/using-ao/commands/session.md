# aong session

Manage agent sessions: list, inspect, rename, kill, restore, clean up, and claim PRs.

## Syntax

```
aong session <subcommand> [args] [flags]
```

## Subcommands

---

### aong session ls

List sessions.

**Syntax:**
```
aong session ls [flags]
```

**Flags:**

| Flag | Meaning | Default / Required |
|---|---|---|
| `-a, --all` | Include orchestrator sessions | - |
| `--include-terminated` | Include terminated sessions | - |
| `--json` | Output as JSON | - |
| `-p, --project string` | Filter by project ID | - |

**Examples:**

```bash
# List all active worker sessions
aong session ls
```

```bash
# List all sessions including terminated, scoped to one project
aong session ls --include-terminated -p agent-orchestrator
```

---

### aong session get

Fetch one session.

**Syntax:**
```
aong session get <id> [flags]
```

**Flags:**

| Flag | Meaning | Default / Required |
|---|---|---|
| `--json` | Output as JSON | - |
| `-p, --project string` | Project id to scope the lookup | - |

**Examples:**

```bash
# Get details for session mer-3
aong session get mer-3
```

```bash
# Get session details as JSON
aong session get mer-3 --json
```

---

### aong session kill

Terminate a session.

**Syntax:**
```
aong session kill <id> [flags]
```

**Flags:**

| Flag | Meaning | Default / Required |
|---|---|---|
| `-p, --project string` | Project id to scope the lookup | - |

**Examples:**

```bash
# Kill session mer-3
aong session kill mer-3
```

---

### aong session rename

Rename a session.

**Syntax:**
```
aong session rename <id> <name> [flags]
```

**Flags:**

| Flag | Meaning | Default / Required |
|---|---|---|
| `-p, --project string` | Project id to scope the lookup | - |

**Examples:**

```bash
# Rename session mer-3 to a new display name
aong session rename mer-3 "fix-auth-bug"
```

---

### aong session restore

Relaunch a terminated session.

**Syntax:**
```
aong session restore <id> [flags]
```

**Flags:**

| Flag | Meaning | Default / Required |
|---|---|---|
| `-p, --project string` | Project id to scope the lookup | - |

**Examples:**

```bash
# Restore a terminated session
aong session restore mer-3
```

---

### aong session cleanup

Clean up terminated sessions by reclaiming eligible workspaces. Dirty worktrees are skipped by the daemon.

**Syntax:**
```
aong session cleanup [flags]
```

**Flags:**

| Flag | Meaning | Default / Required |
|---|---|---|
| `-p, --project string` | Filter by project ID | - |
| `-y, --yes` | Skip confirmation prompt | - |

**Examples:**

```bash
# Clean up all terminated sessions (skip prompt)
aong session cleanup -y
```

```bash
# Clean up terminated sessions for one project
aong session cleanup -p agent-orchestrator
```

---

### aong session claim-pr

Attach an existing PR to a session.

**Syntax:**
```
aong session claim-pr <session-id> <pr-ref> [flags]
```

**Flags:**

| Flag | Meaning | Default / Required |
|---|---|---|
| `--json` | Output as JSON | - |
| `--no-takeover` | Refuse if another active session owns the PR | - |
| `-p, --project string` | Project id to scope the lookup | - |

**Examples:**

```bash
# Attach PR 88 to session mer-3
aong session claim-pr mer-3 88
```

```bash
# Claim PR 88 but refuse if another session already owns it
aong session claim-pr mer-3 88 --no-takeover
```
