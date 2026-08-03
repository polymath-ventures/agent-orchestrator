## Context

`ProjectConfig.agentConfig.permissions` already owns project-level permission mode. The daemon validates that field and uses it when resolving worker and orchestrator launches. `ProjectSettingsForm` exposes the field after creation, but `CreateProjectAgentSheet` and `createProjectConfig` do not carry it, so the first orchestrator always starts with the harness default.

The web-first React supervisor is the primary product path. The setup flow is shared by browser and Electron entry points, so the change belongs in the shared setup sheet and route-level config assembly rather than an Electron bridge.

## Goals / Non-Goals

**Goals:**

- Let the operator deliberately choose project permission mode before the first orchestrator starts.
- Preserve the existing default when no explicit mode is selected.
- Keep setup, project settings, and Prime on one permission-mode vocabulary.
- Reuse the existing project config and daemon launch contract without backend changes.

**Non-Goals:**

- Change the daemon or harness default permission mode.
- Add role-specific permission modes.
- Add a new API field, migration, or Electron-only setup path.

## Decisions

### Carry one optional project-level selection through the existing setup payload

Add a permission value to `CreateProjectAgentSelection`, initialize it to the project default sentinel in `CreateProjectAgentSheet`, and pass the value to `createProjectConfig`. Config assembly adds `permissions` to the existing `agentConfig` only when the value is non-empty; model defaults and permissions therefore compose without either overwriting the other.

Alternative considered: set `bypass-permissions` automatically for every new project. Rejected because the ticket asks for a deliberate operator choice and changing the default would silently broaden permissions.

### Share the permission vocabulary, not a second backend contract

Move the four explicit permission options to a small shared frontend module and use them from setup, project settings, and Prime. Each surface may retain its own select wrapper because their default labels and form styling differ.

Alternative considered: duplicate the four values in the setup component, matching the current duplication between project settings and Prime. Rejected because a third copy increases drift risk for a security-relevant choice while a shared constant is smaller than a generalized form component.

### Test both UI submission and request config assembly

The setup component test selects bypass permissions and verifies the emitted setup selection. The route helper test verifies that an explicit selection persists beside any model defaults and that the untouched project default still omits `agentConfig` when it has no other values.

Alternative considered: test only the rendered select. Rejected because the defect is not display-only; the mode must cross two payload boundaries before project creation.

## Risks / Trade-offs

- [An empty default accidentally becomes an explicit `default` value] → Keep the UI sentinel mapped to an empty string and assert omission in the route helper test.
- [Adding permissions drops model defaults or vice versa] → Build one `agentConfig` object from both optional inputs and cover their composition in the route helper test.
- [The setup sheet becomes longer on small screens] → Place the compact control with existing project-wide setup fields inside the already scrollable dialog.

## Migration Plan

No data migration is required. Existing projects and create requests without the new optional selection keep their current behavior. Rollback removes the setup field while stored project permission values remain valid and editable in project settings.

## Open Questions

None.
