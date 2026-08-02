# agent-task-prompts Specification

## Purpose

Defines how operators replace issue-driven worker task prompts globally or per project while preserving legacy behavior when no override is configured.

## Requirements

### Requirement: Operators can replace issue-driven worker task prompts

AO SHALL accept an optional worker task-prompt template in typed per-project configuration and an optional daemon-wide operator default. The per-project template SHALL take precedence over the daemon-wide template, and the daemon-wide template SHALL apply to every project that does not override it, including projects registered after daemon startup. Existing append-only role/system instruction fields SHALL remain the system-prompt customization boundary; wholesale system-prompt replacement is not part of this capability.

#### Scenario: Global template applies to existing and new projects

- **WHEN** the daemon-wide worker task template is configured and projects without per-project templates spawn issue workers
- **THEN** each project uses the daemon-wide template whether it was registered before or after the setting became active

#### Scenario: Project template wins

- **WHEN** both a daemon-wide worker task template and a per-project worker task template are configured
- **THEN** issue-driven workers for that project use only the per-project template

#### Scenario: System prompt customization remains append-only

- **WHEN** an operator configures a worker task template
- **THEN** AO does not replace its system-prompt scaffold and continues applying role rules through the existing append-only assembly

### Requirement: Issue templates render one portable issue placeholder

AO SHALL replace every literal `{issue}` token in a configured worker task template with the issue's native task identifier. Canonical tracker references ending in `#<identifier>` and manual native references for the same issue SHALL resolve to the same placeholder value. AO SHALL otherwise preserve the template bytes exactly.

#### Scenario: Address-issue template renders a native GitHub number

- **WHEN** the template is `/address-issue {issue}` and the issue reference is `github:acme/demo#242`
- **THEN** the rendered task message is exactly `/address-issue 242`

#### Scenario: Repeated placeholders are all rendered

- **WHEN** a configured template contains `{issue}` more than once
- **THEN** every occurrence is replaced with the same native task identifier

#### Scenario: Fixed template is allowed

- **WHEN** a configured template contains no `{issue}` placeholder
- **THEN** AO uses the template unchanged rather than rejecting it

### Requirement: Configured and explicit task prompts are authoritative

A rendered configured template SHALL be the worker's complete initial task message. AO SHALL NOT inline issue title, URL, labels, assignees, body, trust-boundary prose, or an `## Issue Context` section into that message. An explicit caller-supplied prompt SHALL likewise remain unchanged when issue context was prefetched.

#### Scenario: Configured template suppresses fetched issue content

- **WHEN** an issue spawn resolves a configured template and tracker prefetch has returned a full issue body
- **THEN** the initial task message is exactly the rendered template with no appended or inlined context

#### Scenario: Explicit prompt suppresses fetched issue content

- **WHEN** a caller supplies `/address-issue 242` explicitly together with an issue reference whose context is prefetched
- **THEN** the initial task message remains exactly `/address-issue 242`

### Requirement: Intake and manual issue spawns share configured rendering

Tracker auto-intake and `ao spawn --issue` SHALL use the same template resolution and rendering contract. Given the same effective template and issue, both paths SHALL produce byte-identical initial task messages.

#### Scenario: Intake and manual spawn match

- **WHEN** tracker intake sees `github:acme/demo#242` and a manual spawn names issue `242` under the same effective template
- **THEN** both workers receive byte-identical initial task messages

### Requirement: Invalid active templates fail closed

AO SHALL reject an active configured template whose rendered task message is empty or whitespace-only. The error SHALL identify the worker task-template configuration and SHALL occur before a session or workspace is created. AO SHALL NOT silently select a lower-precedence template or built-in prompt after such a failure.

#### Scenario: Whitespace project override does not fall back globally

- **WHEN** a project configures a whitespace-only worker task template while a valid daemon-wide template exists
- **THEN** the spawn fails with a project worker-task-template configuration error and does not use the daemon-wide template

### Requirement: Unconfigured prompts remain compatible

When neither a per-project nor daemon-wide worker task template is configured, AO SHALL preserve the current initial task messages byte-for-byte. Tracker intake SHALL retain its current issue-detail formatting, truncation, and footer; promptless manual issue spawns SHALL retain their current prefetched-context and no-context variants.

#### Scenario: Unconfigured tracker intake is unchanged

- **WHEN** tracker intake spawns an issue worker with no effective template
- **THEN** its initial task message is byte-identical to the legacy `BuildIssuePrompt` output

#### Scenario: Unconfigured manual issue spawn with context is unchanged

- **WHEN** a promptless manual issue spawn has prefetched context and no effective template
- **THEN** its initial task message is byte-identical to the legacy context-bearing task prompt

#### Scenario: Unconfigured manual issue spawn without context is unchanged

- **WHEN** a promptless manual issue spawn has no prefetched context and no effective template
- **THEN** its initial task message is byte-identical to the legacy fallback task prompt

### Requirement: Effective task-template overrides are inspectable

The effective worker role-prompt inspection surface SHALL report a configured task template and whether it came from the project or daemon-wide default without representing that task message as part of the system prompt. Existing inspection output SHALL remain unchanged for projects with no effective task-template override.

#### Scenario: Role inspection shows project template precedence

- **WHEN** a project task template overrides a daemon-wide template and the operator inspects the worker role prompt
- **THEN** the inspection reports the project template and identifies the project as its source

#### Scenario: Unconfigured inspection remains unchanged

- **WHEN** no effective worker task template exists
- **THEN** role-prompt inspection returns the same system-prompt output as before this capability
