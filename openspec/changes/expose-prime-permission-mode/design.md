## Context

`PrimeSettings.AgentConfig.Permissions` is already part of the daemon settings contract. The backend validates it through the shared agent config validation path, persists it in daemon settings, returns it through the Prime settings API, and uses it when spawning Prime. The missing piece is operator access: `ao prime enable`, `ao prime set`, and global Settings omit the field.

## Goals / Non-Goals

**Goals:**

- Expose Prime permission mode in the same terms already used by project settings.
- Write the existing persisted field rather than adding a duplicate setting.
- Preserve Prime's unattended default when the operator does not set a mode.

**Non-Goals:**

- Changing the default permission mode.
- Changing the permission vocabulary.
- Adding a Prime-specific API field or storage migration.

## Decisions

### Reuse the existing permission vocabulary

The CLI flag accepts the same values as `ao project set --permission`, then relies on the existing backend validation to reject invalid modes. This avoids a Prime-specific parser and keeps help text consistent with the rest of AO.

Alternative considered: define a narrower Prime-only vocabulary. That would make Prime drift from session launch semantics even though it writes the same `AgentConfig.Permissions` field.

### Add the UI control at the Prime settings owner

`PrimeSection` already owns the form state for fleet Prime settings. Add the permission select there and bind it to `settings.agentConfig.permissions`, matching `ProjectSettingsForm` vocabulary and labels.

Alternative considered: hide Prime permissions behind advanced raw JSON or leave API-only. That keeps the current broken surface: the supported operator controls cannot express a persisted setting they already read.

### Preserve defaulting by omitting empty values

The CLI and UI should send an explicit permission only when the operator chooses one. Empty settings keep flowing through `DefaultPrimeSettings()` / `WithDefaults()`, so the existing unattended default remains the single defaulting layer.

Alternative considered: have the UI or CLI write `bypass-permissions` when no value is selected. That duplicates defaulting and would make it harder to change defaults later.

## Risks / Trade-offs

- UI option labels can drift from CLI vocabulary. Mitigation: use the same string values and add focused UI tests around save payloads.
- Invalid CLI modes could produce inconsistent errors. Mitigation: reuse the existing permission mode constants and validation path.
