## ADDED Requirements

### Requirement: The sidebar shows each session's harness beside its status dot

Every session row in the AO sidebar SHALL render a harness indicator adjacent to
the row's status dot. The status dot and the harness indicator SHALL occupy a
single fixed-width slot that owns the spacing between them, so the pair renders
identically regardless of where a given row variant places it. The indicator
SHALL be weighted like the status dot rather than like the name: it is a
fixed-size, non-reflowing mark, not a text label.

The indicator SHALL be derived from the harness already carried on the session
read model. Adding this indicator SHALL NOT introduce a new API field, a daemon
change, or a new session property.

#### Scenario: A session row renders its harness indicator

- **WHEN** the sidebar renders a session row for a session whose harness is `claude-code`
- **THEN** the row renders a harness indicator for Claude Code in the same marker slot as the status dot

#### Scenario: The marker pair owns its own spacing

- **WHEN** the sidebar renders any session row variant
- **THEN** the status dot and the harness indicator sit in one fixed-width slot that supplies the gap between them, rather than relying on the surrounding row's spacing

#### Scenario: Every sidebar session row carries an indicator

- **WHEN** the sidebar renders session rows at any of its row variants
- **THEN** each rendered row carries exactly one harness indicator

#### Scenario: The indicator reads from the existing session harness field

- **WHEN** the harness indicator resolves which glyph to render
- **THEN** it reads the harness already present on the session read model, and no additional session field is requested from the daemon

### Requirement: Every harness resolves to a glyph, including unknown ones

The indicator SHALL resolve a glyph for every harness AO can spawn. Harnesses
that carry a distinct published brand mark SHALL render that mark. Harnesses
with no such mark SHALL render a defined neutral fallback mark.

Any harness value the mapping does not recognise — including a harness added to
the daemon after this mapping was written — SHALL resolve to the same neutral
fallback. The indicator SHALL NOT render an empty slot, a broken image, or
nothing at all for an unrecognised harness.

#### Scenario: A harness with a published brand mark

- **WHEN** the indicator resolves a harness that has a distinct brand mark, such as `codex`
- **THEN** it renders that harness's brand mark

#### Scenario: A harness with no published brand mark

- **WHEN** the indicator resolves a harness with no distinct brand mark, such as `aider`
- **THEN** it renders the neutral fallback mark rather than an empty slot

#### Scenario: An unrecognised harness value

- **WHEN** the indicator resolves a harness value that is absent from the mapping
- **THEN** it renders the neutral fallback mark and an accessible name derived from that harness value

#### Scenario: Harness variants of the same product are distinguishable

- **WHEN** the sidebar renders one session on `codex` and another on `codex-fugu`
- **THEN** the two rows render visually distinguishable indicators

### Requirement: The harness is exposed to assistive technology and on hover

The harness's display name SHALL reach assistive technology through the row
control that contains the indicator, and SHALL be shown on hover, so that the
harness is never conveyed by colour or shape alone.

The indicator graphic itself SHALL NOT attempt to carry the accessible name.
Each sidebar row is a control with its own accessible name, and a named
control's descendants are excluded from that name — a self-labelling graphic
would therefore never be announced at all.

#### Scenario: The row announces its harness

- **WHEN** assistive technology reaches a session row
- **THEN** the row exposes the harness's display name in addition to its own accessible name, and the row's accessible name is unchanged by the indicator

#### Scenario: The indicator names the harness on hover

- **WHEN** a pointer hovers a session row's harness indicator
- **THEN** the harness's display name is shown

### Requirement: The indicator does not disturb the session name or row layout

Adding the harness indicator SHALL NOT change any session's name string.

The indicator SHALL keep the sidebar row layout stable: it occupies a fixed
inline slot that does not wrap and does not grow with the harness's name length.
The session name SHALL keep the row's only flexible slot and SHALL truncate
rather than wrap or overflow, at every sidebar width.

#### Scenario: Session names are unchanged

- **WHEN** the sidebar renders a session row with the harness indicator present
- **THEN** the session name rendered in that row is identical to the name rendered without the indicator

#### Scenario: The name still truncates at a narrow sidebar width

- **WHEN** a session whose name overflows is rendered at the sidebar's narrow width
- **THEN** the name truncates within the row and the row does not wrap or overflow its container

#### Scenario: The indicator costs the name only its own slot

- **WHEN** the marker slot is rendered in a session row
- **THEN** it consumes the surrounding row's spacing once rather than once per marker

### Requirement: The indicator renders correctly in web mode in both themes

The indicator SHALL render correctly in the browser supervisor, which is this
fork's primary product path, and SHALL remain legible in both the light and the
dark theme. Its appearance SHALL NOT depend on Electron preload APIs or desktop
packaging behaviour.

#### Scenario: Legible in both themes

- **WHEN** the sidebar renders session rows in the light theme and again in the dark theme
- **THEN** each harness indicator remains legible against the sidebar surface in both

#### Scenario: No Electron dependency

- **WHEN** the harness indicator renders in web mode with no Electron preload API present
- **THEN** it renders the same indicator it renders under the desktop shell
