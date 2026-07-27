# aong start

Start the local AO user services on this fork's web-first deployment. On a
systemd host, `aong start` starts whichever AO user units are loaded, in
dependency order. On a plain host with no AO services, it fails with a message
explaining that there is no service manager path for `aong` to compose.

## Syntax

```
aong start
```

## Flags

No flags.

## Examples

```bash
# Start loaded AO user services
aong start
```
