# Settings Redesign and Bar Settings Design

Date: 2026-09-27
Builds on: `2026-09-15-settings-foundation-design.md` (reverses its D3 and D11)
Follows: `2026-09-25-bar-surface-and-attach-design.md` (its bar style settings)
Reference: `reports/sysc-shell UI design gap analysis.md`

## Why

The owner could not find the bar style controls after the bar surface work
merged. They exist, as `bar.style`, `bar.shape`, `bar.frost-opacity` and
`bar.pill-opacity` in Settings › Bar, but a headless layout of that page on
the laptop (1536×864 at scale 1.25) puts Style at y=1201 in a scroll viewport
628 tall. The widget lane editor sits above every Bar entry and is longer than
the viewport. The owner's verdict: Settings needs dramatic improvement.

Problems seen on the live laptop capture (2026-09-27):

1. The rail is 13 icons with no labels.
2. A section is one long column. Bar opens on the lane editor, and appearance
   settings are more than a screen below it.
3. Every enum is a dropdown (`settingsMenuControl`), even two- and
   three-option choices like Style and Shape. D8's segmented control never
   reached Settings.
4. Visual choices have no picture. Nothing shows what "islands" or
   "attached" means before the user commits to it.
5. A plugin's lane row shows its entry point (`it.Entry`, e.g. "bar") rather
   than the plugin's name (`settings.WidgetName`).
6. The pane follows the panel opacity. At the frosted default it sits over
   busy windows and body text loses contrast.

## Scope

The owner chose every part of the scope: navigation and layout, visual
pickers with a preview, legibility and polish, and more settings. They are
split in three:

1. **Foundation**, for every section: surface, labelled rail, pages, card
   groups, and the control vocabulary. This design.
2. **Bar settings**, the first consumer of (1): Appearance, Layout and
   Displays pages, the live preview, and the plugin label fix. This design.
3. **Bar breadth**: auto-hide, capsule border, corner styles, rim and shadow,
   scroll actions (`sysc-587`). Each needs renderer work, not only a settings
   row. It gets its own design. It is out of scope here.

The owner asked for a blend of the references: DMS's labelled sidebar and
search, Noctalia's sub-tabs for large sections, and Caelestia-style picture
cards with previews for appearance choices.

## Approaches considered

- **A. Extend the registry (chosen).** Entries gain a page and a presentation
  hint. A section may add a custom block, such as the bar preview. Search,
  per-entry reset and the coverage test keep working, because pages are still
  built from entries.
- **B. Hand-built pages per section.** Most freedom, but it gives up search,
  reset and coverage, and every section becomes bespoke code.
- **C. A declarative layout file.** An extra layer of indirection with no
  second consumer.

## Decisions

### D1. Entries carry a page

`settings.Entry` gains `Page string`. A section's pages are a fixed list,
`settings.SectionPages(section)`, because a page can hold no entries at all:
Bar › Layout is the lane editor. A section with no pages is unchanged, and
its entries leave `Page` empty. A test holds every entry to a page its
section lists. Search hits stay editable in place, grouped under
"Section › Page" captions, so a hit says where it lives.

### D2. Entries carry a presentation hint

`settings.Entry` gains `Present Presentation`, a closed enum:

| Value | Renders | Used for |
|---|---|---|
| `PresentAuto` (zero) | today's control, except that an enum of two to four options renders segmented | everything by default |
| `PresentMenu` | dropdown, whatever the option count | enums whose labels are long |
| `PresentCards` | picture cards, one per option | Bar Style, Bar Shape |

`PresentCards` needs a picture per option. The pictures are drawn by the
bar's own painter (D6), so they cannot drift from what the option does.

### D3. Groups render as cards (reverses foundation D3)

Foundation D3 chose plain row columns and "no cards", to match Noctalia v4.
The owner has now asked for a blend that includes picture cards and grouped
cards. A plain column is what made the Bar page read as one undivided list.
Each group becomes a titled card (`ui.KindCapsule`, `FillContainerHigh`,
`ShapeCard`). Its rows sit inside, and the gap between rows gives way to the
gap between cards. This makes Settings card-composed like every other surface
in the shell.

The virtual-list reasoning in foundation D3 still holds. The page stays one
`KindScroll` column.

### D4. The row: label over description, control on the right

A row is the label (`RoleBody`) with its description under it
(`RoleCaption`, `ToneSubtle`), and the control right-aligned in the control
column. A row is at least `StandardControl` tall. A slider shows its value in
a tabular text cell sized by measurement, never by a fixed width. The audio
panel's 1 px overflow at scale 1.25 (`sysc-589`) is the lesson.
Reset stays per row, as today.

### D5. Rail: labelled, clustered, with search

The rail is icon plus label, 208 wide, with search at its top in place of the
header field. Sections are clustered under small captions:

| Cluster | Sections |
|---|---|
| Look | Appearance, Templates, Wallpaper |
| Bar | Bar, Widgets, Tray |
| Panels | Panels, Monitor, Weather, Plugins |
| System | Session, Accessibility |

Displays leaves the rail: it becomes Bar › Displays (D8). `SectionNames`
keeps its role as the rail order; a new `SectionClusters` supplies the
captions. The coverage test changes from "every section is in the rail" to
"every entry's section and page is reachable".

### D6. The bar preview

Bar › Appearance opens with a preview: the real bar, built from the settings
draft (`h.draft`) with `NewWithTheme`, painted by `Bar.Render` into an
offscreen buffer, and shown as a `ui.KindImage` in a card, at the page's
content width and the bar's own height. The preview is rebuilt when the
draft changes, not per frame. Its widgets show the same data as the live bar,
because they are fed `Registry.viewLocked` for the panel's output.

Style and Shape picture cards reuse this path. Each card renders a 240 px
bar segment in that option's style or shape, from the draft with that one
value substituted.

Compositor blur cannot be reproduced offscreen. The bar is resolved as if
blur were present, so a frosted or islands preview shows its translucent
ground, composited over the card fill. A wallpaper strip behind the preview
would read more truthfully. The registry has no per-output wallpaper
thumbnail to draw one from, so it is left for later.

### D7. Legibility: an opaque floor

Settings paints its root at `max(panel opacity, 94%)`. Other panels keep the
configured opacity. A settings pane is read for minutes at a time; the
references' settings windows are opaque. The bar preview is where
translucency gets shown.

### D8. Bar pages

| Page | Contents |
|---|---|
| Appearance (first) | Preview; Style cards; Shape cards; Edge (segmented); Frost group (frost and pill opacity), dimmed with a one-line reason when the style is solid; Geometry group; Typography group |
| Layout | The lane editor (`barLaneStripFor`), unchanged apart from the plugin label |
| Displays | Today's Displays section: per-output overrides, with its empty-state note |

Enabled stays at the top of Appearance. With the bar disabled, the other
Appearance rows dim and say why.

### D9. Plugin rows show the plugin's name

`settings.WidgetName` takes a resolver for plugin names. The shell passes one
backed by the plugin catalogue, so a row reads "Mini Docker", not "bar". An
unknown plugin falls back to its ID, never to the entry name.

### D10. The surface is responsive (reverses foundation D11)

The target is `min(1120, 72% of output width) × min(820, 88% of output
height)`, fitted as today. That comes to 1105×760 on the laptop and 1120×820
on the desktop. Foundation D11 held 900 wide for the narrowest-width test's
sake; that test now lays Settings out at its fitted size on a 1280×720
output, the smallest this design supports.

## Composition

```text
┌──────────────────────────────────────────────────────────────┐
│ [search………]      │ Bar                                  [x] │
│ LOOK              │ ( Appearance | Layout | Displays )        │
│  ◐ Appearance     │ ┌───────────── preview ─────────────────┐ │
│  ▤ Templates      │ │ ▣ ●●●  ▭ SYSC 19:04  ▭ ▭ ▭ ▭ ▭        │ │
│  ▨ Wallpaper      │ └───────────────────────────────────────┘ │
│ BAR               │ Style                                     │
│  ▭ Bar       ◀    │ [ frosted ] [  solid  ] [ islands ]       │
│  ▦ Widgets        │ Shape                                     │
│  ▢ Tray           │ [attached ] [floating ]                   │
│ PANELS …          │ ┌ Frost ─────────────────────────────┐    │
│ SYSTEM …          │ │ Frost opacity  ───────●──  65%  ↺  │    │
│                   │ └────────────────────────────────────┘    │
└──────────────────────────────────────────────────────────────┘
```

## Testing

- Table test: every entry resolves to a rail section and a page that
  `settingsTree` renders (replaces the rail coverage test).
- `Pages` order, the segmented threshold, and `PresentCards` produce one card
  per option, with the selected one marked.
- Every section and page lays out without error at 1.0, 1.25 and 1.5 scale,
  at the fitted size on 1280×720, 1536×864 and 3440×1440. The layout errors
  that closed the audio panel are exactly what this catches.
- The preview renders non-empty pixels for each style, and follows a draft
  change.
- Search: a Bar Style hit sits under a "Bar › Appearance" caption, and
  clearing the query returns to the section and page the user was on.
- `WidgetName` resolves a plugin to its catalogue name.
- Live: open Settings over IPC on both machines, capture every page with
  `grim`, and change Style and Shape from the cards. Style changes must reach
  the live bar. Changes need clicks, which this setup cannot inject
  (`wtype-closes-the-panel`), so the owner confirms interaction live; the
  tests prove the routing.

## Not in this design

- Bar breadth (`sysc-587`): its own design.
- Colour pickers: foundation D8's exclusion stands.
- A detachable or toplevel settings window: it stays a layer-shell panel.
- Redesigning other sections' contents: they gain the rail, cards, segmented
  enums and the opaque floor, and nothing else.
