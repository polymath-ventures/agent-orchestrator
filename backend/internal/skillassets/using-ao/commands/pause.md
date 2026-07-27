# aong pause

Explain the honest fleet work-control verbs, or pause one project. Bare
`aong pause` intentionally does not alias `drain`: use `aong drain` to gate and
drain at idle, or `aong stop-work` to terminate live work now. `aong pause
<project>` and `aong pause <project> --hard` preserve AO's project-scoped pause.

## Syntax

```
aong pause [project]
```

## Examples

```bash
aong pause
```

```bash
# Pause one project
aong pause agent-orchestrator
```

```bash
# Hard-pause one project
aong pause agent-orchestrator --hard
```
