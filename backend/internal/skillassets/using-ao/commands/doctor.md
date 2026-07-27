# aong doctor

Run local AO health checks. `aong doctor` runs `ao doctor` and adds fork-owned
service-unit health for loaded `ao-web.service` and `ao-tmux.service`.

## Syntax

```
aong doctor [flags]
```

## Flags

| Flag | Meaning | Default / Required |
|---|---|---|
| `--json` | Output health checks as JSON | - |

## Examples

```bash
# Run health checks
aong doctor
```

```bash
# Get health check results as JSON
aong doctor --json
```
