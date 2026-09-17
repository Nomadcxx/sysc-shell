# Settings foundation design

Date: 2026-09-15. Owner-approved in brainstorming on 2026-09-15.

This design covers sub-project A of Milestone 9: the schema spine, control
vocabulary, surface chrome, and apply contract for a settings system that
exposes everything the shell already models.

It satisfies `sysc-204` and supersedes D1 to D4 of
[the Tranche 4B design](2026-08-30-settings-osd-theme-catalog-design.md). D5 to
D10 of that document — the OSD, the audio and brightness services, stock themes,
and the template catalog — are untouched.

It also settles the settings-composition question that
[the component parity design](2026-09-11-component-parity-design.md) deliberately
left open, and is the first document of the Milestone 9 programme.

Sources read as behaviour and architecture references only:

- `internal/settings/registry.go`, `internal/shell/popout_settings.go`
- `internal/config/config.go`, `load.go`, `write.go`
- `internal/shell/panelhost.go`, `registry.go`, `popout_controlcenter.go`,
  `controlcenter_pages.go`, `menu.go`
- `internal/ui/tree.go`, `column.go`, `scroll.go`, `controls.go`, `drag.go`
- `internal/theme/profile.go`, `internal/render/materialfont.go`, `fontmap.go`
- `/home/nomadx/noctalia/src/shell/settings/` — `settings_registry.{h,cpp}`,
  `settings_control_factory.h`, `bar_widget_editor.h`, `settings_bar_management.h`
- `/home/nomadx/Documents/GitHub/DankMaterialShell/Modules/Settings/`

## Milestone 9 context

Milestone 9 is user control of the shell. It decomposes into five sub-projects,
each with its own design, plan, and implementation cycle:

| | Sub-project | Note |
|---|---|---|
| A | Settings foundation | This document. The spine B, C and D consume. |
| B | Bar composition | Widget add, remove, reorder, per-widget options. |
| C | Bar geometry and placement | Edge, auto-hide, exclusive zone, per-output overrides. |
| D | Surfaces and behaviour | Notifications, OSD, launcher, panels settings. |
| E | New subsystems | Night light, idle, screenshot, hooks, keybinds, dock. |

Clipboard history is tracked separately as `sysc-205` and is not part of E.

## Goal and scope

In scope:

- A schema in which one setting is defined in exactly one place.
- Descriptions, groups, and per-entry reset.
- The control vocabulary listed in D8.
- The standalone settings panel's information architecture and chrome.
- A control-centre shortcut page that deep-links into that panel.
- The live-apply, debounce, and reload contract.
- Closing the config coverage gap recorded below.

Out of scope, and not to be grown on the way past:

- Bar widget arrangement, which is B.
- Bar edge, auto-hide, and geometry, which is C.
- Notification, OSD, and launcher settings sections, which are D.
- Any new subsystem, which is E.
- Multiple named bars, keybind editing, hooks, and a colour picker.

## The problem this solves

The shipped pane is 180 lines: a rail of seven hardcoded names, a search field,
and a flat `KindVirtualList` of label-and-control rows. There are no
descriptions, no groups, no reset, and no per-output anything.

Two measured facts drive the design.

**Coverage.** `config.Config` has twelve domains. The registry's 37 entries
reach five of them — `Bar`, `Theme`/`ThemeGen`, `Panels`, `Session`, and
`Accessibility`, plus sixteen template toggles and two widget entries.
`Weather`, `Wallpaper`, `Tray`, `Outputs`, and `Plugins.Enabled` are modelled in
configuration and unreachable from the user interface.

**Cost per setting.** Each entry is declared in three places: the entry list,
a `Get` switch case, and a `Set` switch case. Nothing makes the three agree. A
missing `Get` case returns an empty string rather than failing to compile. At 37
entries that is survivable. At the 200-plus this milestone implies, with B
needing entries synthesised per widget instance, it is not.

## Approaches considered

The approved approach gives each entry its own typed accessors.

Extending the flat registry was rejected. It keeps the three-places tax, grows
the two switches past 600 lines, and cannot express a per-widget-instance
setting at all, because a switch cannot enumerate an arbitrary instance's
fields. B would need a second, parallel mechanism.

Reflection over dotted config paths was rejected twice over: the built-in widget
foundation handover forbids reflection-driven config decoding, and it trades
compile-time safety for exactly the silent-empty-string failure being removed.

## Decisions

### D1: Typed accessor entries

Each entry declares `get(config.Config) string` and
`set(*config.Config, string) error` beside its label, bounds, and options. The
two switch statements in `internal/settings/registry.go` are deleted.

One setting is defined in one place, and the compiler enforces that a new entry
carries its accessors. This is what makes B's widget inspector possible: an
entry can be constructed at runtime over a specific `config.Item`.

### D2: Entries carry a description and a group

`Entry` gains `Description` and `Group`. Sections partition into named groups;
each row may carry a caption line beneath its label.

This is the largest legibility gap against both references, and it is the reason
D3 is forced.

### D3: Plain grouped column; the virtual list goes

`KindVirtualList` is strictly uniform-stride. `internal/ui/column.go` computes
`ContentH = ItemCount * ItemHeight`, boxes every item at exactly `ItemHeight`,
and advances by that stride; `VisibleRange` divides the scroll offset by it.
Descriptions and group headings cannot exist under that contract.

Settings therefore renders one `KindScroll` column per section. Section
partitioning bounds the row count — the largest Noctalia section is roughly 50
rows, not 300 — so laying out the whole section stays cheap.

`KindVirtualList` is not extended. Its six other consumers — the tray drawer,
tray menu, launcher, wallpaper picker, and process list — are genuinely uniform,
and per-item measurement would tax all of them to serve one pane.

This reverses an earlier deliberate fix rather than drifting from it, and the
lineage belongs on the record. The settings pane originally set no virtual list
at all; `sysc-40` added one as Milestone 4 shortfall remediation, and
[the M4 code-quality sweep](2026-08-31-m4-code-quality-sweep.md) records that
the keyboard acceptance test flipped the scroll node's kind in memory rather
than proving one in the product tree. What makes the list wrong for this pane
now is the uniform-stride contract meeting descriptions and group headings, not
a judgement that the earlier remediation was mistaken. The primitive stays
correct for the consumers that are genuinely uniform.

Settings composes **no cards**. This matches v4, whose settings panes are plain
row columns, and settles the question the component parity design left open. It
makes settings the one surface in the shell that is not card-composed, which is
accepted deliberately rather than by oversight.

### D4: Live apply, debounced

Toggles, dropdowns, and segmented controls commit on change. Sliders and text
fields debounce and commit through the existing `scheduleControl` seam, which
runs the write off the Wayland owner, re-takes `Registry.mu`, discards the
result if the host has been replaced, and rebuilds.

Today every keystroke rewrites the entire configuration file. Both reference
shells apply live; neither writes per keystroke.

### D5: Per-entry reset, resolved by the writer's own rule

"Default" is not one thing, and this is the easiest part of the design to get
wrong.

`config.Write`'s `themeDiff` bases every appearance axis against
`theme.PresetComposition(got.Preset)`. Every other diff — `barDiff`,
`panelsDiff`, `accessibilityDiff`, `wallpaperDiff` — bases against `Default()`.

Reset resolves an entry's default through the same rule the writer uses, so
resetting a theme axis returns it to the selected preset's value, not to the
built-in default. An entry knows which rule applies to it.

A row whose value deviates from its default is marked, and only such a row shows
its reset control.

### D6: The draft re-syncs on reload

`PanelHost.draft` is assigned once, when the panel opens
(`internal/shell/panelhost.go`), and is never refreshed.
`Registry.PrepareConfig`'s `Commit` replaces `r.cfg`, rethemes open surfaces,
and swaps every bar without touching it.

So a configuration change arriving from outside the panel — SIGHUP, another
tool, a second surface — is silently reverted by the next control change, which
writes the stale draft back whole. This is a live defect on `main`, not a
hypothesis; `TestReloadKeepsOpenPanels` asserts only that the panel stays mapped.

D4 widens the window from rare to the entire time the panel is open, so this
design closes it: `Commit` re-seeds the draft of any open settings host while it
already holds `Registry.mu`.

### D7: One full surface, one shortcut

The standalone panel is the complete settings interface. The control-centre
Settings page is a curated shortcut: the most-changed controls, plus category
links that open the panel at the requested section.

This needs no new addressing. `panelSection` already validates a requested
section against `settingsSections` and `HandlePanelByName` already routes it
through `selectPanelSectionLocked`, so IPC section addressing for settings works
today and the page rides it.

The control-centre body is roughly 480 logical pixels tall once the rail takes
its 56, against the panel's far greater room. Rendering one settings tree into
both would size-constrain every future section for no benefit.

### D8: Control vocabulary, and two exclusions

Ships: toggle, slider, stepper, segmented, dropdown, text field, validated hex
text, searchable picker, and path browse. Stepper and path browse compose from
existing node kinds; path browse lists directories with `os.ReadDir` and needs
no portal.

**No colour picker.** `internal/ui` carries no colour type at all: `ui.Fill` is a
closed eighteen-member semantic enum resolved against the theme in
`internal/render`. A real picker would give the node tree its first non-semantic
colour plus a paint path for it, which cuts directly against the token
conformance work in `sysc-265`. Colour settings are validated hex fields. A
preview swatch, if wanted later, is one explicitly named affordance and its own
decision.

**No list, string-map, or keybind editors.** They have no consumer in A. They
belong to B and E and should enter with one. D9 does not create one either: see
the exposure rule below.

### D9: Close the coverage gap

A exposes `Weather`, `Wallpaper`, `Tray`, `Outputs`, and `Plugins.Enabled`.

This is most of "the same options or more" reached before a single new subsystem
exists, and it is why A precedes E.

**Exposure rule, which keeps D8 honest.** `Tray.Hidden`, `Tray.Pinned`,
`Tray.Order`, and `Plugins.Enabled` are string lists in the configuration
structure, and exposing them as free-form list editors would require exactly the
control D8 excludes. They are not exposed that way. Every one of them is keyed by
a token that a live service already enumerates — tray items from the tray host,
plugins from the plugin host — so each is rendered as a row per discovered item
carrying toggles, which compose from the vocabulary D8 does ship. A token whose
item is absent is preserved and shown as such, never silently dropped; that
invariant already governs plugin settings and is inherited rather than invented.

Tray ordering is the one part of this that a toggle cannot express. A is
responsible for visibility and pinning only; reordering shares its interaction
model with bar widget reordering and belongs with it in B.

**The bar item lists are untouched.** `bar.items.left`, `bar.items.center`, and
`bar.items.right` keep their existing comma-separated string entries through A.
Replacing them is the whole substance of B, and A neither improves nor removes
them, so that B inherits one unambiguous starting point rather than a
half-migrated one.

### D10: The font picker is real

`fontscan.SystemFonts(logger, cacheDir) ([]Footprint, error)` enumerates every
scanned system font from the already-pinned `go-text/typesetting`. No new
dependency, no cgo, no fontconfig subprocess. The picker dedupes and sorts on
`Footprint.Family`.

`Footprint.Family` is stored **normalized** — `dejavusans`, not `DejaVu Sans`.
Display names are therefore not free. The implementation either derives a
display form or presents normalized names; it must not assume pretty names
appear on their own.

### D11: The surface is 900 x 760

Width is unchanged from the shipped 900 on purpose:
`TestEveryPanelLaysOutAtItsNarrowestWidth` lays `PanelSettings` out at its
target width, and holding width leaves that test's premise intact while the
plain column gains the vertical room it needs. `FittedSize` clamps on short
outputs. For reference, v4's settings surface is 840 x 910.

## Information architecture

Twelve sections, each grounded in a domain `config.Config` models today:

| Section | Source |
|---|---|
| Appearance | `Theme`, `ThemeGen` |
| Templates | `Templates` |
| Bar | `Bar` |
| Widgets | per-item options discovered from configured items |
| Panels | `Panels` |
| Wallpaper | `Wallpaper` |
| Weather | `Weather` |
| Displays | `Outputs` per-connector overrides |
| Tray | `TrayPreferences` |
| Plugins | `Plugins` |
| Session | `Session` |
| Accessibility | `Accessibility` |

Templates leaves Appearance and becomes its own section; sixteen toggles beneath
the theme axes is the crowding both references suffer from.

Notifications and OSD are deliberately absent. `config.Config` models neither, so
those sections would be empty. They arrive with D, and the rail is built to take
them.

## Composition

The surface mirrors the control centre so settings reads as the same product: a
56-pixel icon rail, a `MarginXL` gutter, then a body column of header over
scroll. The rail reuses the shipped shape — 40-pixel `ShapeMedium` buttons,
`FillAccent` when selected, `Role: "tab"`.

One row:

```
KindRow   height <- Metrics.StandardControl   PinEnd
|- KindColumn  gap MarginXXS
|  |- label        RoleBody
|  '- description  RoleCaption, muted
'- KindRow  gap MarginS
   |- reset   (only when the value deviates)
   '- control
```

Row height derives from `Metrics.StandardControl` and never from a literal, so
rows breathe across all five density steps. A new pane full of hardcoded heights
would add to the very census `sysc-265` is shrinking.

Spacing uses existing ladder rungs only:

| Relationship | Rung |
|---|---|
| Section body inset | `PanelPadding` |
| Between groups | `MarginXL` |
| Group heading to first row | `MarginS` |
| Between rows within a group | `MarginM` |
| Label to description | `MarginXXS` |

Group headings are `RoleLabel`, rendered inline. They are **not** sticky:
`KindScroll` has no sticky-child support and this design does not add one. The
section name is always visible in the header.

## Three properties that exceed the references

**Deviation is ambient.** D5 makes per-entry deviation available, so a changed
row is marked continuously and a filter for changed rows only is nearly free.
Noctalia hides this behind a toggle the user must discover. At 200-plus entries,
"what have I actually changed" is the most common question, and neither
reference answers it at rest.

**Search keeps context.** The shipped pane and Noctalia both replace the sidebar
with a flat label list, so a match gives no clue which section owns it. Matches
here are grouped under their section heading, and descriptions are searched as
well as labels, which makes search double as discovery.

**Settings is density-aware.** Rows derive from the density ladder, so the pane
honours Mini through Spacious like every other surface. Neither reference ties
its settings surface to a density system.

## Material glyphs

The embedded subset holds roughly sixty ligature names, kept in step with
`internal/render/icons/material/build.py` by hand and asserted by
`ValidMaterialIcon`. The twelve sections need approximately nine additions,
including glyphs for appearance, widgets, panels, displays, templates, plugins,
accessibility, path browse, and group expansion.

Each candidate name must be confirmed to exist in the pinned Material Symbols
source before it lands. That confirmation is a plan task. A name the font does
not carry shapes to nothing and paints an invisible control.

## Testing

The domain-coverage invariant is the most valuable single check: **every
`config.Config` domain has at least one entry**. The five-of-twelve gap opened
silently because nothing ever asserted otherwise, and without this guard C and D
will reopen it.

`internal/settings`: accessor round-trip replacing the switch-era tests, bounds
rejection, search matching descriptions as well as labels, the coverage
invariant, and reset resolving under both rules of D5 — preset-relative for a
theme axis and `Default()`-relative elsewhere.

`internal/shell`: the rail renders twelve sections with focus; search groups
matches under their section; a row renders label, description, and control; a
deviating row reveals its reset; a debounced field commits once rather than per
keystroke; and the D6 regression — a `PrepareConfig` reload while settings is
open re-seeds the draft, proven by showing an external change survives a
subsequent control change.

Retired: `TestSettingsContentIsVirtualList`, which D3 makes false by design.

Updated rather than deleted: the settings, keyboard-only, and accessible-name
acceptance checks in `gate4b_test.go`, and `chromefit_test.go` at the new height.

Gate: not AGENTS.md's repository-wide `go test -race -count=1 ./...`. A
repository-wide race build hard-locks this machine — zram-only swap against
16-way linking. The per-package substitute established by the notification
centre plan applies, with `-p` and `GOMAXPROCS` capped. The implementation plan
states the exact commands rather than inheriting a gate that cannot run here.

## Cost

This rewrites `internal/settings/registry.go` and
`internal/shell/popout_settings.go` outright and rewrites their tests with them:
eight tests in `popout_settings_test.go`, nine in `settings/registry_test.go`,
plus the `gate4b_test.go` acceptance checks and `chromefit_test.go`. Shell tests
carry 31 `PanelSettings` references. That is the price of D1 and it was weighed
before the decision, not discovered after it.

## Not verified

- The nine candidate Material ligature names are not yet confirmed against the
  pinned Material Symbols source.
- Whether `sysc-107`, stock themes not selectable from settings, is fixed by this
  rewrite or is a separate defect. Verify before closing it; do not assume.
- Per-connector `Outputs` overrides have never had a user interface. The Displays
  section's editing model is specified no further than "expose the override" and
  may need its own decision during planning.
- Entry counts for the new sections are derived from the config structures, not
  from a built registry.

## File and anchor note

Every file path and behaviour above was read against `main` on 2026-09-15.
Anchors drift; re-verify before relying on any of them, per the project's own
experience with stale plan anchors.
