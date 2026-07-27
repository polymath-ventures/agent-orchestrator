# aong status

Show AO daemon state, fleet pause state, detected environment, and loaded
service-unit active states. Use this to verify the web-first deployment is up.

## Syntax

```
aong status
```

## Flags

No flags. `aong status` composes `ao status` and adds service-unit state. Use
raw `ao status --json` when you need machine-readable daemon state.

## Examples

```bash
# Check daemon status
aong status
```
