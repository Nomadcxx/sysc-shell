# Toast and notification card redesign — design (2026-09-28)

Closes GitHub #28 (blur behind toast cards) and #30 (toast density and typography).
Tracker: sysc-635, sysc-636. Visual decision record:
https://claude.ai/artifact/KHwHjJA81L3uRk6ncGwQSr (rendered through the shell's own painter).

## Decisions

- **D1 Layout C.** A 32 px lead icon beside a text column. The column's first line is the summary with
  the time pinned right; under it one muted body line. No app-name line, no timeout line.
- **D2 See-through.** A toast card is the surface ground itself, painted at the panel style's opacity,
  which follows the global `panel-opacity` setting. There is no inner capsule: `FillNone` on a capsule
  inherits the theme's opaque card colour, which is why every toast painted solid until now.
- **D3 Blur through the compositor.** The toast surface supplies `BlurShape`: each visible card's rounded
  silhouette, cut with `ui.BlurStrips`, placed where the card draws this frame. No screencopy capture, so
  D13's objection (capturing a whole output for a corner card) no longer applies.
- **D4 Fallback.** Glass applies only when `cfg.Theme.BlurBehind && caps.Blur`, the condition panels use.
  Otherwise toasts keep today's overlay opacity, which was chosen for a surface with no backdrop.
- **D5 Edge.** The card strokes `Style.Rim` = the theme outline, as a floating panel does. A critical card's
  rim is the error colour.
- **D6 Height.** A card's height is its content height. `toastCardHeight` loses its `+2*radius`, which is
  the empty band under every card today.
- **D7 Notification centre.** History and Current-tab cards adopt the same text block (D1). They keep their
  filled container cards, remove/dismiss controls and group actions, since they sit inside a panel.

## Card anatomy

```
┌─────────────────────────────────────────────┐  rim: outline (error if critical)
│ [icon 32]  Summary (RoleFigure)        now  │  time: RoleCaption, subtle, pinned end
│            Body, one line, subtle…          │  collapsed: one line; drag-expand wraps
│            ▬▬▬▬▬▬▬▬▬▬▬▬▬▬▬▬░░░░░░░░         │  only when the notification has a value
│ (Reply) (Mute)                              │  action pills, FillContainerHighest
│ [ reply field ]                             │  only when inline reply is offered
└─────────────────────────────────────────────┘  padding 12, gaps 10 / 8
```

- Summary tone: normal; critical uses the error tone. Low urgency keeps the subtle tone on every line.
- Lead: the notification's image when it decodes. Otherwise a `notifications` glyph in a
  `FillContainerHighest` tile (`FillErrorContainer` and error tone when critical). This replaces the
  letter tile, which painted blank for critical cards.
- The time line uses `formatNotifyTime`. Pinning it to the row end means it can no longer be truncated
  to "n…" by a long app name, which today shares a row with it.
- A default action still makes the text block clickable; without one a click dismisses. Links in the body
  keep their `notify:<id>:link:` actions.

## Toast surface

- `rebuild` picks the style once per rebuild: `PanelStyle()` with `Rim = Outline` when glass applies (D4),
  otherwise `OverlayStyle()`. It already re-reads the theme per rebuild (GitHub #29), so a capability or
  opacity change reaches open toasts.
- `paintCard` sets the per-card rim (error for critical) before painting.
- `spec` adds `BlurShape`: under `r.mu`, for each card on each output, `BlurStrips(SurfaceShape{Body:
  displayRect, Radius: style radius})` offset to the card origin; empty when glass does not apply or no
  card is visible. The platform already sends a region only when it changes and clears it when empty.
- The input region is unchanged: the union of card rects.

## Notification centre

`notificationTree` becomes the D1 text block, shared by `NotificationCard`, `HistoryCard` and
`ActiveGroupCard`. The centre's `wrapNotifyCard` keeps its container fill; its critical chip is replaced
by the same error stroke the toast uses, so critical reads the same in both places.

## Tests

- A toast card's height equals its content height (regression for D6).
- The blur shape covers exactly the visible cards, follows a sliding card, and is empty with no cards or
  without compositor blur.
- A critical toast paints an error-coloured rim; a normal one the outline.
- With glass, a pixel inside a card is translucent over a transparent surface; without, it matches the
  overlay alpha.
- The time node survives a summary long enough to truncate.
- Existing toast, notification card and centre tests pass, updated where they assert the old tree shape.
- Proof: before/after renders through the painter, then a live check with real notifications.

## Out of scope

Toast width (380), stack placement and slide animation, expiry and hover-pause behaviour, the sysc-notify
protocol, and new glyphs in the Material subset.
