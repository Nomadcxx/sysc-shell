# System Monitor Panel: Completion Handover

A snapshot, taken 2026-09-26, of the system monitor panel redesign as merged and deployed. The design
is `2026-09-25-system-monitor-panel-design.md` and the plan is `2026-09-25-system-monitor-panel.md`.
The work that remains is open in bd under epic `sysc-533`.

## Commits

- `feature/system-monitor-panel` merged into `main` at `e63d564`.
- Commits on the branch after the execution handover:
  - `73baffe` fix(shell): monitor sort and search minors (`sysc-539`, `sysc-540`, `sysc-541`)
  - `2ef3b78` fix(shell): monitor lease, username and icon lifetimes (`sysc-542`, `sysc-544`, `sysc-545`)
  - `2e19378` merge of `main`, which by then carried `feature/monitor-followups-20260925` (merged at
    `d9401d9`)
  - `db99ec6` fix(shell): lay out monitor group rows at real width
  - `40fce26` fix(shell): monitor table cells match the reference
  - `69c2c9d` fix(shell): fit the System page without scrolling (`sysc-543`)
- Tracker rows: `3de0809` on `main`.

## Gates

Run at `69c2c9d` on the branch, and again on `e63d564` for the shell package.

| Check | Result |
|---|---|
| `test -z "$(gofmt -l .)"` | clean |
| `go vet ./...` | clean |
| `go test ./...` | 20 failures, the same set as `main` before the merge: 4 in `internal/shell`, 16 `TestTray*` in `tests/integration` |
| `go test -race` on `./internal/shell`, `./internal/render` and `./internal/ui`, one package at a time | no races |
| `git diff --exit-code main -- go.mod go.sum` | no difference |

The four `internal/shell` failures are `TestPanelSectionValidationPrecedesMutation`,
`TestRightClickingTheBarBatteryOpensSession`, `TestRightClickingBatteryCapsulePaddingOpensSession` and
`TestABatteryWidgetOpensTheSessionPanel`.

## Deployment

- `~/.local/bin/sysc-shell` is built from `e63d564` and runs under `systemctl --user`.
- The build this replaced, the monitor follow-ups build from 2026-09-25 23:08, is kept as
  `sysc-shell.before-system-monitor-panel`.
- `panel.open` with `{"panel":"system-monitor","section":"mem"}` maps `sysc-shell-panel` on `DP-1`,
  layer `Overlay`, according to `niri msg -j layers`.

## Live observations

1. **The first deploy showed an empty table.**
   - The Processes table drew its first section header and nothing below it.
   - Cause: a group row's chevron slot was a nested `PinEnd` row. The row layout gives such a row all the
     remaining width, so the icon beside it failed the fit check and the whole surface refused to lay out.
   - No fixture had a group row, and no test laid out a row at panel width, so the suite never saw it.
   - Fixed in `db99ec6`. The same commit fixes value cells that drifted with the length of each row's
     name: a nested row is measured by its content, not its `Width`.
2. **Application memory matches `/proc`.** The kitty row read `336.4 M`. The sum of `VmRSS` over kitty's
   window PID and its nine descendants, read from `/proc` at the same moment, was 336.4 MiB. Capture:
   `assets/2026-09-26-system-monitor-panel/live-applications-vmrss.png`.
3. **The kill from the detail view did not run live.**
   - `ydotool mousemove --absolute` did not land on the panel. The first click fell outside it and closed
     it, and the second click focused a terminal.
   - Nothing was typed. Pointer injection was stopped there instead of being calibrated on a shared desktop.
   - The kill path is covered by the signal tests only.
4. **The System page and the detail view were rendered headless.** There is no IPC to switch pages, and
   `wtype` unmaps the panel. Both were painted through `PanelHost.render` with a real metrics sample
   (about 530 processes) and the real fonts, but without the generated theme, which is why the accent is
   orange.
   - The System page shows all seven rows with no scrollbar: 406 px of content in a 410 px view.
   - The detail view ellipsises a long command line, and it disables Kill and Force kill for a
     root-owned process.

## Captures

These sit beside the reference, `processes/screenshots/panel.png` in
`https://github.com/noctalia-dev/community-plugins`.

| File | What it is |
|---|---|
| `assets/2026-09-26-system-monitor-panel/live-processes.png` | Processes page, live on DP-1, from `e63d564`, cropped to the panel |
| `assets/2026-09-26-system-monitor-panel/system.png` | System page, rendered headless with a real sample |
| `assets/2026-09-26-system-monitor-panel/detail.png` | Detail view for a root-owned process, rendered headless |
| `assets/2026-09-26-system-monitor-panel/live-applications-vmrss.png` | Applications rows used for observation 2 |

## Decisions on the execution handover's open questions

- **Apps listed twice: kept.**
  - A process under an application also appears under Processes.
  - The reference's `group_processes` receives the whole process table, and the live panel reads the
    same way.
- **Windows that share one PID across app ids: left as is.**
  - `windowOwner` keeps the window it saw last, so the other app ids come out empty and are hidden.
  - Nothing on this machine produces the case: there are two windows with two PIDs. It stays a known
    limit, with no bd issue until a real case shows up.
- **Widths below 800 logical: accepted.** The Task 7 ruling stands. The panel is fixed at 800 × 650.
- **Hover and sort role colours on a translucent panel: not checked.** The expressive preset was not
  switched on the live machine.

## Rulings made in this pass

Each ruling lists what it costs if it is wrong.

- **User and name sort keys.** A row with no user, name or PID sorts last in either direction, which is
  D4's rule for every key. Coverage is a table test on `compareLines`: SWAP and DISK cannot carry values
  through the projection until Task 13. Cost: one test moved.
- **Search in a group.** A search that matches one member of a larger executable group keeps the group,
  as D5 and Review Focus 2 require. Whether a process is a group of one is decided on the owner-filtered
  set, before the search narrows it. Cost: one condition.
- **First sort direction.** PID ascends first, like name and user, and measurements descend. This holds
  for a header click and for `panel.open` alike, through `processSortDescends`. Cost: one predicate.
- **Refresh on reload.** A reload that changes `monitor.refresh` takes the new leases before releasing
  the old ones, and re-resolves the rate subjects at the new interval. Cost: one re-lease per reload
  while the monitor is open.
- **Icon rebuilds.** Arriving icons rebuild the monitor once per 1/30 s batch, the same pattern as the
  plugin text flush. The launcher keeps its rebuild per icon. Cost: the same helper applied to the
  launcher.
- **Root device.** Root device resolution now serves both rate-subject hosts. After the merge, the
  monitor had gone back to resolving under `Registry.mu`, which is what `sysc-528` fixed for the Control
  Centre. Cost: one loop.
- **Table cells.**
  - Only the sorted column is a pill.
  - Numbers are pinned right. The `PinEnd` flag set on the text node did nothing, because it is a row
    flag.
  - PID widens from 72 to 80 so that a seven-digit PID fits.
  - Cost: a column width.
- **System page height.** The System page fits instead of scrolling: its rows are 40 px, not the Control
  Centre's 50. Cost: one constant.

## Still different from the reference

- **Title weight.** The title "Processes" uses the title role at regular weight. The reference is bold
  and larger. The style guard requires a title-role heading and forbids synthetic bold.
- **Header hover.** Unsorted header cells are plain text with an action. They take pointer state but
  paint no hover fill.
- **GPU sparkline width.** On the System page the GPU sparkline is half as wide as the CPU sparkline.
  `a374942` narrowed it so the Control Centre could fit a VRAM caption, and the System page shares that
  row.
- **Missing values.** SWAP and DISK read `—`, and processes group by name rather than executable, until
  Task 13 (`sysc-537`).
- **Copy on click.** There is no copy on click until Task 12 (`sysc-536`).

## Open in bd

- `sysc-533`: epic.
- `sysc-534`: gate. sysc-metrics must release the per-process fields.
- `sysc-535`: gate. sysc-clipboard must release a copy message.
- `sysc-536`: Task 12, blocked by `sysc-535`.
- `sysc-537`: Task 13, blocked by `sysc-534`.
