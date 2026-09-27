# Settings Redesign Completion Handover

Date: 2026-09-27
Design: `2026-09-27-settings-redesign-design.md`
Plan: `2026-09-27-settings-redesign.md` (Tasks 1–9)
Epic: `sysc-597` (closed). Follow-ups: `sysc-617` to `sysc-621`. Deploy guard: `sysc-622`.

The branch `feature/settings-redesign` fast-forwarded `origin/main` from
`a44d25b` to `6dd19fd`. The branch and its worktree have been removed.

## Commits

| Commit | Contents |
|---|---|
| `4a55ee2` | Task 1: `Entry.Page`, `Entry.Present`, `SectionClusters`, `SectionPages`, `PageEntries`; Displays becomes Bar › Displays |
| `ba57644` | Task 2: plugin widgets named by their plugin (`WidgetName(it, resolver)`) |
| `c5d9a7e` | Task 3: segmented control for enums of 2–4 options; `pick:<path>=<value>` |
| `a37e04c` | Task 4: group cards; rows take their column's width; search captioned `Section › Page` |
| `a7a7baf` | Task 5: labelled clustered rail; page tabs; `Section/Page` addressing and the `Displays` alias |
| `ae25c08` | Task 6: `settingsPanelSize` (72% × 88%, capped 1120×820); 0xf0 opacity floor |
| `a7e3690` | Task 7: offscreen bar preview and picture cards |
| `a68a272` | Task 8: Bar › Appearance, Layout and Displays pages; dimming |
| `ea47ccb` | Task 9: layout matrix over every section and page × 3 outputs × 3 scales |
| `0067ec4` | Live gate (desktop): body cut at the 240 scroll fallback; picture cards a control tall |
| `2e6023e` | Live gate (laptop): preview laid out at the output's width; one-option enum as text; slider values (D4) |
| `4103057` | Whole-branch review fixes (below) |
| `6dd19fd` | Tracking: close `sysc-597` |

## Automated gate at `4103057`

```text
gofmt -l .                                   no output
GOMAXPROCS=4 go vet -p 2 ./...               exit 0
git diff --exit-code origin/main -- go.mod go.sum   exit 0
GOMAXPROCS=4 go test -p 2 -count=1 ./...     only the known failures:
  internal/shell: TestPanelSectionValidationPrecedesMutation and three *Battery* tests
  tests/integration: 16 TestTray* (sysc-586)
go test -race, one package at a time: render, shell, settings   no data race
```

## Live gate

Both machines ran Niri 26.04. Pages were opened with `panel.open settings`
and a section such as `Bar/Appearance`, and captured with `grim`.

- **Desktop** (`DP-1`, 3440×1440, scale 1.0), build `ea47ccb`. It found
  two bugs the tests missed. The scrolling body sat in a column and took the
  layout's 240 fallback, cutting Appearance off after Shape. The picture
  cards were a control tall, so their pictures spilled out. Both were fixed in
  `0067ec4`, and the matrix now asserts the body height.
- **Laptop** (`eDP-1`, 1536×864, scale 1.25), builds `0067ec4`, `2e6023e`
  and `4103057`. All 15 section and page addresses map a panel, and the
  journal shows no `closing surface`. It found three more. The preview was
  laid out at the card's width, which crowded the centre into the window
  title. The one-option Edge enum drew as a clipped menu. Sliders showed no
  value (a D4 requirement the plan had no task for). All three were fixed in
  `2e6023e`.
- **Dimming.** With `bar.style` set to solid in the config and reloaded, the
  Frost sliders render faded, their labels are muted, and "Applies to the
  Frosted style." replaces the description.
- **Not confirmed.** Clicking the Style and Shape cards needs the owner at
  the machine; `wtype` closes panels.

## Whole-branch review

A fresh reviewer read `a44d25b..ea47ccb`. Its two critical findings were the
live gate's layout bugs, already fixed in `0067ec4`. The Important findings
were fixed in `4103057`. Each has a test, and seven of the tests failed
against `2e6023e` before the fix:

- the preview ignored the draft's Style and Shape on an output with an
  override
- each bar picture rescanned system fonts, costing 38 ms a rebuild on the
  Wayland owner; pictures are now cached
- turning the bar off did not rebuild the page, so nothing dimmed
- disabled toggles, sliders and menus painted as live; rows now also say why
- switching a section or page kept the old scroll offset
- the rail ran off the pane at comfortable and spacious density
- the first preview was a 1.0 raster upscaled at 1.25
- Settings opened from a shortcut ignored the output's size

## Rulings made during execution

1. The plugin-name resolver is stored on `PanelHost` by `barLaneStripFor`
   rather than threaded through five builders.
2. The segmented and pick tests use `weather.unit`. `bar.edge` has one
   option, and a one-option enum renders as text.
3. Page tabs are radios in a segmented control. `tab` stays the rail's
   role, one per section.
4. The rail runs the full pane height. The title and page tabs sit over the
   content only.
5. The lane editor's Alt+arrow commands require the Layout page.
6. Picture cards share the row width, capped at 240.
7. `TestAcceptKeyboardOnlyAllControls` turns the bar back on before looking
   for a slider, because an off bar correctly dims them.
8. Rail tabs use `MarginS` padding, which lets twelve fit at spacious
   density.
9. Cached bar pictures keep the widget data they were painted with.

## Open

- `sysc-617`: search skips the dim rules and loses the page's scroll.
- `sysc-618`: picture cards have no last-good fallback, and Islands is
  shorter.
- `sysc-619`: comments, a stale icon entry, plugin names in the Widgets
  section, Enabled's position, and weak tests.
- `sysc-620`: dropdowns clip their value at 1.25.
- `sysc-621`: the toggle knob disappears when the palette's error colour
  matches its accent.
- `sysc-622`: stale deploys. Other sessions redeployed shells without this
  work on both machines within minutes of each deploy. Details are in the
  issue.
