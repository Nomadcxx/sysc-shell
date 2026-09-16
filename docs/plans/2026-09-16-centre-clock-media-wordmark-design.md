# Centre clock, media, and wordmark design

Date: 2026-09-16. Parent commission: `sysc-309`.

## Goal

Make the bar centre read as one time/date composition joined to the SYSC
wordmark, place media at that composition's far right, and keep the wordmark
at a fixed centre coordinate while surrounding content changes.

## Existing seams

- `config.Default` currently places two separate clock items around a
  `wordmark`. `buildWidgets` gives clocks a floor when two clocks and a
  wordmark coexist.
- `buildMediaWidget` already uses the shared `services.MediaState`, cached art
  worker, and `hideWhenAbsent`; no player currently collapses the item.
- `ui.ArrangeBar` centres the whole centre section. A variable-width section
  therefore moves the wordmark even though the existing centre test protects
  only the section's centre.
- `KindWordmark`, the gradient animator, theme roles, and existing pointer and
  keyboard paths already own the mark's rendering and activation.

## Decisions

### D1. One clock/date node, one wordmark anchor, one optional media edge

The default centre becomes three logical items:

```text
[ time/date composition ]  [ SYSC wordmark ]  [ media when available ]
```

The time/date composition is one existing group node containing the two
tabular clock values. It shares one capsule and one measured slot, with the
time before the date. The wordmark remains its own accessible button. The
media widget remains the existing shared MPRIS projection and sits after the
wordmark. A missing player gives media zero layout width and no painted empty
slot.

The media design stays bounded to current state, title, art, and transport
activation. This change does not add player discovery, seeking, volume, or a
new MPRIS protocol.

### D2. Arrange the wordmark independently

`ui.ArrangeBar` gains a narrow anchored-centre path used when the centre
section contains one `KindWordmark`. The existing path remains for other
centre sections.

The anchored path places the wordmark's measured centre at the content band's
centre. It places preceding nodes toward the left edge of the anchor and
following nodes toward the right edge. Each surrounding node receives the
space available on its side and truncates through the existing painter when
its natural width exceeds that budget. Side sections yield to the anchored
composition using the existing collision priority. The wordmark never adopts
the natural width of time/date or media.

This is a layout rule for the one approved wordmark consumer, not a general
alignment or flexbox API. The anchor uses the wordmark node's existing
`ImageW`/`ImageH`, and the existing gradient, name, role, and action remain
unchanged.

### D3. Media absence and long titles preserve the invariant

An absent media node collapses before anchored placement. A long title is
bounded by its configured max width and then truncated or marquee-clipped by
the existing bar rules. Neither case changes the wordmark centre. Clock
figures remain tabular so minute changes do not alter their natural width.

## Data flow

```text
Clock service -> two values -> one time/date group
Media service -> MediaState/art -> optional media node
Theme/render -> KindWordmark -> anchored ArrangeBar placement
```

Service relays publish immutable state. Layout and font shaping remain on the
Wayland owner through the bar's existing `needsLayout` path.

## Focused proof

The executable plan will add:

- a default-config/build test proving one constructed time/date group, one
  wordmark, and media at the composition's right edge;
- media-widget tests for present, absent, art, pause/play glyph, and long-title
  truncation without an empty slot;
- `internal/ui/bar_test.go` cases that vary left/right widths, clock/date
  widths, media presence, and title length while asserting the wordmark centre
  remains the content-band centre;
- shell hit/focus tests proving the wordmark and media keep their existing
  accessible names and actions.

## Live gate

On Niri `DP-1`, 3440×1440, scale 1.0, capture the default bar with no player,
with a player, and with a long title. Change the clock minute and surrounding
bar widths, then verify the wordmark does not drift. Record the surface state
and any collision limitation. No second-output claim belongs here.

## Rejected alternative

Keeping three independent default centre items and extending the clock floor
would preserve the current configuration shape, but it cannot establish a
fixed wordmark anchor when media or side content changes. A shell-wide layout
toolkit would move the rule farther from the only consumer. The anchored
`ArrangeBar` path plus the existing group is the smaller responsible change.

## Boundary

This design does not redesign the wordmark asset, add a generic centre layout
system, or pull media protocol work forward from the existing media owner.
