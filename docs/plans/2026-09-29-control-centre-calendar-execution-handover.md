# Control centre calendar page: execution handover

Commissions a fix for the control centre's Calendar page, which renders as an unusable
month grid and shows no events. Tracked in bd as the bug titled *"Control centre calendar page: month
grid collapses; cells ignore Width, rows oversized"*. Status lives there, not here.

Base: `origin/main` at `6ef3806` (the commit that added plugin events to the page).

## What the owner sees

Captured 2026-09-29 on the desktop (3440×1440, scale 1.0), with the page opened by IPC:

```bash
export NIRI_SOCKET=$(ls /run/user/1000/niri.wayland-*.sock | head -1)
export WAYLAND_DISPLAY=wayland-1 XDG_RUNTIME_DIR=/run/user/1000
~/.local/bin/sysc-shell ipc panel.open '{"panel":"control-center","section":"calendar"}'
grim /tmp/cc.png
```

- The weekday header reads `S M T W T F S` packed into ~60 px at the left of the card.
- Each week is one run of numbers, `30 31 1 2 3 4 5`, also packed left. There are no columns.
- Each week row is ~70 px tall with its numbers pinned to the top, so the card is mostly empty
  bands.
- The right ~60 % of the card is dead space.
- No events, no upcoming list, no *Open calendar* button: the page takes the no-snapshot path.

## Causes

### 1. Day and weekday cells ignore their width (layout)

`ccCalendar` (`internal/shell/controlcenter_pages.go`) builds each cell as
`{Kind: KindText, Width: ccCalendarCellW (74), CenterX: true}` inside a `KindRow`.
`ui.measureNode` (`internal/ui/layout.go`, `case KindText, KindTab`) sizes a text node from its
measured run, `MinWidthText` and `MaxWidth`. It **never reads `Node.Width`**, and `CenterX` has no
effect on a text child in a row. So every cell collapses to its text width and the row packs left.
The weekday header has the same defect. The event-count variant, a `KindColumn` with `Width`,
does honour the width, so days with events and days without would not even line up with each other.

The clock popout's grid (`internal/shell/popout_clock.go`) is built the same way, with unsized
text cells, and probably shows the same packing. Check it when you fix this one.

### 2. Rows are sized to fill the page, content is not centred in them

`rowHeight = max((ccPageH − 2·CardPadding − StandardControl − 28 − footerH) / weeks, ccCalendarRowMin)`.
With no snapshot, `footerH` is one control's worth, so five weeks get ~70 px each. D4 of the control
centre design (`2026-09-03-control-center-design.md`) does ask a shorter page's principal block to
grow to fill the body. But the day label sits at the top of that band, not centred, and the `28` is
a bare magic number for the weekday header.

### 3. No events on this machine (configuration, not code)

- The page reads the `org.sysc.calendar` plugin's persistent state, key `control_center`, via
  `loadCCCalendar` → `plugin.OpenStore(plugin.StateRoot(), …)`. That is
  `~/.local/state/sysc-shell/plugins/org.sysc.calendar/`, which **does not exist** on the desktop.
- `org.sysc.calendar` is **not in `plugins.enabled`** in `~/.config/sysc-shell/config.json`,
  so the plugin never runs and never publishes.
- The plugin (sysc-plugins, `plugins/calendar`, `cmd/sysc-plugin-calendar`) reads **Evolution Data
  Server** (`calendar.QueryEDS`, `eds.go`) and publishes the snapshot in `snapshot.go` /
  `main.go:166` (commit `a7b6c0c`). EDS is running here (`org.gnome.evolution.dataserver.Calendar8`
  and `Sources5` are on the session bus). This is the intended source.
- **The `gogcli` keyring prompt is unrelated.** A GNOME keyring dialog asking to unlock `gogcli`
  was on screen during the capture. Nothing in sysc-shell or sysc-plugins references gogcli, and no
  `gog` process was alive afterwards. Some other client raised it; do not route the calendar through
  it.

## What to do

Order matters: fix the layout first, because it is visible with or without events.

1. **Make the grid a grid.** Do not add a new UI primitive (AGENTS.md: primitives only for an
   approved component). Pick one of these:
   - Wrap each day and weekday label in a sized container that the layout already honours (a
     `KindCapsule` with `Width`, or the `KindColumn` the event variant uses), with the label centred
     in both axes. Use the same wrapper whether or not the day has events, so columns line up.
   - Or teach `measureNode`'s `KindText` case to honour `Width > 0` as a fixed cell, the way the
     capsule case does ("an explicit width is a grid cell"). This is the smaller change, but it is
     repo-wide: check that no bar or panel text node sets `Width` expecting it to be ignored
     (`grep -rn "KindText.*Width:"`), and run the whole `internal/ui` and `internal/shell` suites.

   Derive the cell width from the body: seven cells and six gaps across the card's content width,
   not a hard 74. The body is 596 px (control centre design D1/D2).
2. **Centre the day label in its row** and name the header height instead of `28`. Keep D4's
   fill-the-body rule for the grid block.
3. **Event markers.** The page currently draws a count digit under each day in the label role and
   the accent tone. Check it against the reference before keeping it. Noctalia v5 is the composition
   target named by the control centre design. Per the UI-parity rule, capture the Noctalia or DMS
   calendar and match its density; a dot or accent well may read better than a digit.
4. **Loading off the lock.** `loadCCCalendar` opens the plugin store and reads the disk every time
   the tree is built, under `Registry.mu`. Consider caching the snapshot and refreshing it on a
   plugin state change or a timer, as the other relays do. Do not block the owner on disk I/O.
5. **Enable the plugin for the live gate.** Install `org.sysc.calendar` from sysc-plugins and add it
   to `plugins.enabled` on each test machine. Confirm the state file appears and the page shows
   markers, the upcoming list and *Open calendar*. Check the laptop's config too before assuming it
   differs.

## Checks

- **Table test on geometry** (`internal/shell/controlcenter_test.go`): for a month with and without a
  snapshot, all seven cells in every week row and in the header have equal width and share column
  x-positions, and the grid spans the card's content width within one gap. The existing snapshot
  test (`6ef3806`) asserts content only, which is how this shipped.
- **Offline render**: paint the page at 1.0 and 1.25 to a PNG in `t.TempDir()`, the pattern in
  `internal/shell/tooltipviews_test.go` (`SYSC_TOOLTIP_PNG_DIR` keeps the files). Check the PNG by eye
  before deploying.
- **Seed a snapshot for tests**, not the live EDS: write a `ccCalendarSnapshot` JSON into a temp
  `XDG_STATE_HOME` store for `org.sysc.calendar`, key `control_center`.
- **Gates**: `gofmt`, `go vet ./...` (not only `./internal/...`, because `tests/integration` also
  compiles against shell APIs), and race tests **per package** (`./internal/shell`, `./internal/ui`),
  never `./...` with `-race` on this machine.
- **Live**: deploy with `scripts/deploy --host both` from a clean `origin/main` worktree. Open the
  page by the IPC command above and screenshot on both machines, with the plugin enabled and with it
  disabled.

## Known unrelated failures

On the desktop, at `e3bd065` and later: four shell tests (`TestPanelSectionValidationPrecedesMutation`
and three battery-session tests) and sixteen `tests/integration` tray tests ("no point on output 7
produced an action") fail on the base as well. Do not chase them as regressions of this work.
