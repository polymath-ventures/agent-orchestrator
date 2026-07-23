# Design

The existing `QuotaPanel` already receives per-window `used` percentages and
dated reset instants, and `pickWindows()` already chooses the headline and
secondary window per harness. This change keeps those ownership boundaries
intact and replaces only the `WindowLine` presentation.

Each window row renders a top line with the window name and percent, a 6px
progressbar track/fill, and a final reset metadata line. The progressbar uses
`role="progressbar"` with a harness/window accessible name and clamped
`aria-valuenow` so assistive technology receives the same percent as the visual
meter. A 0% window keeps a small visual fill so the row still reads as a meter,
while exposing `aria-valuenow=0`.

Severity is derived from the clamped used percent at render time. Values below
75 use the app accent for the fill, values from 75 through 89 use warning, and
values at or above 90 use danger. The number carries the severity color for
warning and critical states, and critical reset metadata also includes an alert
icon plus remaining-percent text so color is not the only signal.

The smallest compatible change is to keep the existing component structure and
introduce small local helpers for clamping, severity, label normalization, and
percent text. No API adapter, extra state, polling, or duplicated quota model is
needed because the authoritative data already arrives in the snapshot.
