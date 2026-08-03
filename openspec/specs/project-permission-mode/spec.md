# project-permission-mode Specification

## Purpose

TBD - created by archiving change add-permission-mode-to-project-setup. Update Purpose after archive.

## Requirements

### Requirement: Project permission mode is deliberate at creation

The system SHALL expose the project-level permission mode during first-run project setup using the same supported modes and labels as project settings, and SHALL persist an explicit setup selection in `ProjectConfig.agentConfig.permissions` before starting the project's initial orchestrator.

#### Scenario: Operator selects a permission mode during setup

- **WHEN** an operator selects `bypass-permissions` in first-run project setup and creates the project
- **THEN** the create-project request stores `bypass-permissions` in `ProjectConfig.agentConfig.permissions`
- **AND** the initial orchestrator resolves that project-level permission mode

#### Scenario: Operator keeps the project default

- **WHEN** an operator creates a project without selecting an explicit permission mode
- **THEN** the create-project request omits `ProjectConfig.agentConfig.permissions`
- **AND** the daemon and selected harness retain their existing default permission behavior

#### Scenario: Setup and settings use one permission vocabulary

- **WHEN** the setup dialog and project settings render their permission-mode controls
- **THEN** both surfaces offer `default`, `accept-edits`, `auto`, and `bypass-permissions`
- **AND** both surfaces present the same user-facing labels for those explicit modes
