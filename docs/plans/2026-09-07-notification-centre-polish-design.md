# Notification centre polish — Design

Date: 2026-09-07. Status lives in bd (`sysc-151`, `sysc-153`).

`sysc-152` shipped the notification centre: a first-party `PanelNotifications`
with a working projection, grouping, DND, filters and toast chrome. The backend
is sound. The panel is not readable.

This slice is a visual and structural pass over the centre. It changes no
service contract in this repository and adds one command to `sysc-notify`.

## Why the panel is unreadable

Not missing features — missing stratification and missing hierarchy.

1. **Nothing separates.** `wrapNotifyCard` builds every card as
   `Fill: ui.FillNone`, and `centerTreeFor` returns a bare `KindColumn`. The
   plate, the header and every card resolve to the same fill. `theme.go:299`
   already documents the intended surface ladder; the centre never used it.
2. **Four rows of undifferentiated controls.** Title with three text buttons,
   an optional DND preset column, a `Current`/`History` tab row, then six time
   chips — all plain `KindButton` with no fill, at one weight, stacked before
   any content appears.
3. **Two filter axes for one question.** Tabs split active from closed; chips
   split closed by time. The user reads two controls to reach one list.
4. **No per-entry control.** Dismissal is a header action or a whole-group
   button. A single entry cannot be removed.

## Clone target

**Noctalia** notification centre, as captured in the owner's reference:
`https://datalabtechtv.com/posts/yotld-part-8-niri-noctalia-bazzite/noctalia-notifications.png`

`2026-09-03-notification-centre-design.md` cloned DMS 1.5.3 and said so
explicitly. That document is not amended: it records what `sysc-152` built and
why, and remains the reference for the toast chrome and the projection, which
this slice does not touch. Where the two disagree on centre layout, this
document governs.

## Scope

In:

- Three-level surface ladder across plate, header block and cards.
- Header: headline title, leading glyph, five circular icon buttons.
- One `KindSegmented` filter row replacing the tab row and the chip row.
- One merged list, live entries pinned above closed ones under section labels.
- Per-card remove control.
- Concave fillets joining the panel to the bar.
- `history.remove` in `sysc-notify`, a new tag, and the pin bump.

Out:

- Card internals. `notificationTree` keeps its layout, text roles, icon slot,
  meters, action buttons and inline reply exactly as shipped (D7).
- Circular clipping of raster app icons (D13).
- Toast chrome. Unchanged by this slice.
- The projection, grouping keys, DND policy and persistence.
- The control centre (`sysc-158`). Separate panel, separate design.

## Decisions

### D1 — Surface ladder

Three levels, all existing tokens, no theme work:

| Level | Token | Occupants |
|---|---|---|
| 0 | panel root, `Style.rootFill()` | the plate |
| 1 | `ui.FillContainerHigh` | header block, notification cards |
| 2 | `ui.FillContainerHighest` | icon buttons, filter segments, card remove |

This is the single highest-value change in the slice. `theme.go:299` already
resolves panel cards to the high container and reserves Highest for controls
sitting on them; the centre simply asked for `FillNone` everywhere.

Header block and cards share level 1 deliberately. They separate by gap, as the
reference does, not by a fourth grey.

### D2 — Header

```
╭──────────────────────────────────────────────────╮
│  🔔  Notifications        (◔)(⏰)(🗑)(⚙)(✕)      │
│  ┌────────┬─────────┬────────────┬──────────┐    │
│  │ All 7  │ Today 2 │ Yesterday 3│ Earlier 2│    │
│  └────────┴─────────┴────────────┴──────────┘    │
╰──────────────────────────────────────────────────╯
```

Leading `KindIcon` `notifications` at `ui.ToneAccent`, which needs D14.
Title at `theme.RoleHeadline` (20/600), the reference's large semibold; the
centre currently has no headline anywhere.

Five circular buttons, each a `KindButton` with `Shape: ui.ShapeCircle` and
`Fill: ui.FillContainerHighest`, wrapping one `KindIcon`:

| Glyph | Action | Note |
|---|---|---|
| `do_not_disturb_on` / `notifications` | `notify:center:dnd` | Toggles. Glyph follows state, as today. |
| `schedule` | `notify:center:schedule` | Opens the existing duration presets. |
| `delete` | `notify:center:clear` | See D6. |
| `settings` | opens `PanelSettings` | Panel already exists (`panelhost.go:369`). |
| `close` | closes the panel | Mirrors `wallpaper-close`. |

Five rather than the reference's four: `schedule` is a distinct function here
and burying the presets behind a long-press would hide them. At 416 px the row
measures roughly 310 px, so it fits without truncation.

### D3 — Filter pills

One `KindSegmented`, four equal segments, each labelled with its count.
Selection is `State |= ui.StateSelected`, the `wallpaperSegment` pattern
(`popout_wallpaper.go:490`), so the accent fill and its paired foreground come
from the theme with no new painting.

Buckets: `all`, `today`, `yesterday`, `earlier`.

### D4 — Retirements

`historyChips` drops from six buckets to four. The `1h` and `7d` filters go
(owner decision, 2026-09-07). `historyFilter` in `notifymatch.go` changes shape
with it.

The `Current`/`History` tabs go entirely: `PanelHost.notifyTab` becomes dead,
and the `notify:center:tab:0` / `notify:center:tab:1` actions are removed.

### D5 — Merged list

The filter selects a time window. Within that window the body emits:

```
── LIVE ──────────────────
  <active group cards>
── EARLIER ───────────────
  <history cards>
```

Section labels at `theme.RoleCaption` with `ui.ToneAccent`. A section with no
members emits nothing, label included. With neither populated the existing
empty state stands.

Active notifications are live by definition and always fall inside `today`.

### D6 — What the header remove button clears

Without tabs, `clearAction` has no tab to switch on. The rule is **what is in
view**: dismiss the visible active entries, remove the visible history entries.

`protocol.Command` already carries `IDs []uint32`, so this is one
`active.dismiss-all` (or an id list) and one `history.remove`. Slice 3 is gated
on slice 1 precisely so this rule is expressible when it ships; there is no
interim behaviour to specify.

### D7 — The card changes as little as possible

Owner decision, 2026-09-07: the card is close enough. Two changes only:

1. `wrapNotifyCard` fills `ui.FillContainerHigh` instead of `ui.FillNone`,
   which is what D1 requires.
2. A right-pinned remove button, `ShapeCircle` + `FillContainerHighest`,
   wrapping the `delete` glyph.

`notificationTree` is untouched: text roles, `iconSlot`, the value and timeout
meters, action buttons, inline reply and the critical left chip all stay as
`sysc-152` shipped them.

Deliberately **not** done, though the reference shows them and each is a
one-line change if the owner later wants it: app name at `ToneAccent`,
timestamp at `RoleCaption`, and `iconSlot`'s fallback capsule moving from
`ShapeMedium` to `ShapeCircle`.

Card remove maps to `notification.dismiss` for an active entry and
`history.remove` for a closed one.

### D8 — `Node.PinEnd`

`pinRowEnd` (`column.go:221`) right-pins the last child of a two-child row, but
requires the first child to be `KindText`. The card's first child is a
`KindColumn` and the header's is a `KindRow`, so neither qualifies.

It has one call site, inside `placeColumnChild`, and loosening the guard
outright would silently re-pin every two-child row in every column in the
shell. So the guard gains an explicit opt-in field, `Node.PinEnd bool`, and
the `KindText` special case stays for the callers that already rely on it.

### D9 — Wing tips

The panel's top row already abuts the bar: `Panels.Gap` defaults to `0`, so the
panel origin is `BarZone + 0`. Two pieces are missing.

1. `render.Style` gains a fillet radius and an explicit **bar** fill colour.
   The bar and the panel carry different alphas (`Style` versus `PanelStyle`),
   so painting the fillet with the panel's `rootFill()` leaves a visible seam
   wherever surface opacity is below 100.
2. The panel surface widens by the fillet radius on each side, and
   `clearOutsideRoundedRect` gains a concave branch for the attach-edge rows.
   `roundedInset` produces convex insets only; a fillet needs the inset to run
   the other way, from the body edge outward to the bar.

### D10 — Fillet clamp

The notifications panel is right-aligned at `Panels.Padding` 8, while the bar
body is inset `BarGap` 4. That leaves 4 px between the panel's right edge and
the bar's, and a fillet wider than the margin overruns the bar.

The fillet clamps to the available margin. Aligning the panel's edge to the
bar's would also work but changes shared placement for one panel's benefit.

### D11 — Icon inventory

`settings`, `close`, `notifications` and `do_not_disturb_on` are already in the
Material subset. Two are added:

- `delete` — new, and the reason for the rebuild.
- `schedule` — currently drawn from `sysc-icons.ttf`, the hand-authored SVG
  face, and reached as a raw rune through `render.IconByName`. Left there it
  would sit in a row of five buttons drawn from a different face at a different
  optical size and stroke weight, and read as a mistake.

`materialfont.go` must accept both names. Rebuilding needs the exact upstream
file `SOURCE.md` pins: sha256 `c4416e02…`, roughly 15 MB, from commit
`84ccef28`. The copy at `~/.local/share/fonts/MaterialSymbolsRounded.ttf`
hashes `4b959703…` and is a different cut — `build.py` will reject it, which is
the check working.

### D12 — `history.remove` (cross-repo, `sysc-153`)

The delta rails already exist. `DeltaHistoryRemoved` is published today by
retention pruning (`state/owner.go:376`, `state/expiry.go:95`), and this shell
already handles `KindHistoryRemoved` (`notifyclient/client.go:252`). What is
missing is a command to reach it.

| Change | File |
|---|---|
| `CommandHistoryRemove CommandKind = "history.remove"` | `protocol/types.go` |
| Validation case beside `CommandHistoryClear` | `protocol/validate.go` |
| `state.HistoryRemove` command kind and handler | `internal/state/owner.go` |
| `Store.Remove(ids)` beside `Store.Add` | `internal/history/store.go` |
| Dispatch case | `internal/presenter/connection.go:206` |

Work happens on `redesign/v0.1`, whose tip is `v0.1.0-rc.2`, in the worktree at
`~/.config/superpowers/worktrees/sysc-notify/redesign/v0.1`. `origin/main` is
docs only and is not what this module compiles. New tag `v0.1.0-rc.3`, then the
pin and the register row here move with it.

`historyRemoveSupported()` (`notifycard.go:265`) flips to true in the same
commit as the pin bump, never before: a remove button painted against rc.2 is a
control the service rejects.

### D13 — Raster icons stay square

`KindImage` paints unmasked. Circular app-icon rasters need a mask in the
painter, which is real work stacked on top of D9. Out of scope. The letter
fallback keeps its current shape per D7.

### D14 — `paintIcon` honours `Tone`

`paintIcon` (`paint.go:922`) blends every glyph with `style.Foreground`
unconditionally, so an icon cannot take the accent. `paintText` already routes
through `textColor(style, tone)` (`paint.go:1072`), and `Tone` is defined on
`Node` as "which theme colour paints this node".

`paintIcon` calls `textColor(style, n.Tone)` instead. `ToneNormal` is the zero
value and resolves to `style.Foreground`, so every icon in the shell today
paints byte for byte as it does now.

The alternative — wrapping the glyph in an accent-filled capsule — inverts the
reference: it gives an accent plate with an on-accent glyph, not an accent
glyph on the header.

This lands in slice 2 with the other shared-chrome work.

## Slices

Three, in dependency order. Each lands on `main` independently.

| # | Scope | bd | Gated by |
|---|---|---|---|
| 1 | `history.remove`, `v0.1.0-rc.3`, pin bump, `historyRemoveSupported()` | `sysc-153` | — |
| 2 | Material subset (D11), `Node.PinEnd` (D8), concave fillet (D9, D10), icon tone (D14) | new | — |
| 3 | Header, pills, merged list, ladder, card remove (D1–D7) | new | 1 for the remove button, 2 for glyphs and pinning |

Slice 3 carries the visible win and needs 1 and 2 only for the remove control
and the fillet. If a gate stalls, the readability work still ships.

## Testing

Per `AGENTS.md`: one focused runnable check per non-trivial unit, table tests
for pure layout and protocol code.

| Unit | Check |
|---|---|
| Bucket counts | Table: entries across four windows, boundary timestamps at local midnight. |
| `historyFilter` | Table over the new four-bucket mapping, including the retired ids returning false. |
| Section assembly | Empty, live only, history only, both; assert no orphan label. |
| Header remove target | Visible-set ids per filter (D6). |
| `Node.PinEnd` | Non-Text first child pins; unset row unchanged; existing `KindText` callers unchanged. |
| Icon tone | `ToneNormal` paints `style.Foreground` unchanged; `ToneAccent` paints the accent. |
| Concave inset | Geometry table over radius, height and attach edge, including the D10 clamp. |
| `Store.Remove` | Unknown id is `ErrNotFound`; known id emits one `DeltaHistoryRemoved`; image cleanup runs. |
| Painting | Golden images where `internal/render/testdata` already covers the surface. |

Then the live Niri gate: `NIRI_SOCKET` derived per `AGENTS.md`, one output
(`DP-1`, 3440×1440), `niri msg -j layers` for mapped surfaces.

Repository-wide `go test -race ./...` hard-locks this machine. Cap `-p` and
`GOMAXPROCS`, and run the shell package with a shadowed `loginctl` on `PATH`.
