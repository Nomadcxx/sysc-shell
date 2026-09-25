# System Monitor Panel: Execution Handover

This commissions the rest of the system monitor panel redesign:

- the live Niri gate that has not run;
- the two tasks gated on upstream releases;
- the review minors and open questions left over;
- one final polish pass against the reference.

bd holds status. This document says what each item is, where it lives, and how to prove it done.

## Read first

- `AGENTS.md`: project rules, the live Niri environment, the commit hook.
- `docs/plans/2026-09-25-system-monitor-panel-design.md` (D1–D10): the binding design.
- `docs/plans/2026-09-25-system-monitor-panel.md`: the plan. Tasks 1–11 were executed; this handover picks up
  Tasks 12–14.
- The reference:
  - `https://github.com/noctalia-dev/community-plugins/tree/main/processes`: read `panel.luau` and
    `scripts/stats.py`.
  - The screenshot, `screenshots/panel.png`. It was captured at about 1.53× scale: the 110 px logo spans
    168 px.

## Where the code is

- Branch: `feature/system-monitor-panel`.
- Worktree: `/home/nomadx/sysc-shell/.worktrees/system-monitor-panel`.
- The branch starts at `db0d837` and has 12 commits, ending with `abf7b9d fix(shell): monitor review fixes`.
  It is not merged or deployed.
- `main` has moved on (`9fc87ba feat(plugin): add calendar host APIs`). That commit touches three files the
  branch changes: `internal/render/paint.go`, `internal/shell/panelhost.go` and `internal/ui/tree.go`.
  `git merge-tree` reported a clean merge on 2026-09-26. Re-check before merging.
- The executor's ledger is at `.superpowers/sdd/2026-09-25-system-monitor-panel/progress.md` in that
  worktree. It is gitignored. Every ruling in it is copied below, so the file is a convenience only.

| File | What it holds |
|---|---|
| `internal/shell/processlines.go` | Pure projection: application trees, executable groups, filters, sorting, expansion |
| `internal/shell/monitorframe.go` | Shared header, info card, options column, gauges, `monitorView` |
| `internal/shell/popout_process.go` | Processes page, `activateMonitor`, `parseProcessOrder`, signal scheduling |
| `internal/shell/processdetail.go` | Detail view (SIGINT / SIGKILL / Close) |
| `internal/shell/popout_system.go` | System page |
| `internal/shell/usernames.go` | Per-uid username cache with its own lock |
| `internal/theme/colorroles.go`, `internal/ui/paintrole.go` | Colour-role vocabulary and paint roles |
| `internal/render/paint.go` | `FillRole` and `rowFill` (role hover) |
| `internal/config`, `internal/settings` | `monitor` block and the Monitor settings section |

## Known pre-existing failures

These fail identically on `main` at `db0d837`. Do not chase them as part of this work.

- `internal/shell`:
  - `TestPanelSectionValidationPrecedesMutation`;
  - `TestRightClickingTheBarBatteryOpensSession`;
  - `TestRightClickingBatteryCapsulePaddingOpensSession`;
  - `TestABatteryWidgetOpensTheSessionPanel`.
- `tests/integration`: 16 tray tests (`TestTray*`). The set was compared name for name against `main`.

## Items

Do them in this order:

1. Items 1 and 2 need nothing upstream.
2. Item 3 is the polish pass, done after you have seen the panel live.
3. Items 4 and 5 wait on releases.
4. Item 6 closes the work.

### 1. Tracker entries

bd has no issues for this work yet. The main checkout's `.beads/issues.jsonl` carried another session's
uncommitted rows when this handover was written, and writing to bd in that state has minted duplicate IDs
before. Check `git status .beads/issues.jsonl` in `/home/nomadx/sysc-shell` first. Once it is clean, create:

- an epic for the system monitor panel redesign;
- a gate issue: "sysc-metrics: release per-process executable, swap, I/O rates and private/shared memory";
- a gate issue: "sysc-clipboard: release a copy message".

Record each deferred minor in item 2 as an issue under the epic, discovered from it. After that, bd is the
status record.

### 2. Deferred review minors

A fresh whole-branch review graded these Minor. Each needs a failing test first.

1. **Missing values do not sort last for `user` and `pid`.**
   - `compareLines` in `processlines.go` treats `user` and `pid` as always valid.
   - A group row with mixed owners (`User == ""`) or with no PID (`PID == 0`) sorts first when ascending.
   - D4 says rows with an invalid value go last in either direction.
   - Also extend `TestSortKeysAndInvalidLast` to cover every key in each direction: swap, io and user are
     missing, as are cpu ascending, name descending and pid ascending.
2. **A search that matches one member of an executable group shows a plain row, not the group.**
   - `TestSearchKeepsAGroupWithOnlyTheMatchingMembersCounted` asserts the plain row, which contradicts its
     name.
   - Decide which behaviour is right. The Review Focus in the plan says the group is kept. Then make the
     code and the test's name agree.
3. **The `pid` sort direction depends on how you get there.**
   - `parseProcessOrder("pid")` sorts descending, but clicking the PID header sorts ascending (`activateMonitor`).
   - Pick one rule for numeric keys and apply it in both places.
4. **A changed `monitor.refresh` does not reach an open panel.**
   - Leases are taken only when the panel spawns.
   - On a config reload with the monitor open, re-acquire its leases at `monitorLeaseInterval`.
5. **The System page scrolls at 650 px tall.**
   - Its body is about 476 px inside a 410 px scroll, so the Network row sits below the fold.
   - Either make the rows fit (`ccMonRowH`, gaps) or accept the scroll. Decide during the live gate.
6. **Usernames are cached for the shell's lifetime.**
   - D10 says the panel's lifetime.
   - Reset `r.usernames` when `PanelMonitor` closes.
7. **Every icon that finishes loading rebuilds the whole monitor.**
   - `applyTrayIcon` rebuilds it once per arriving icon.
   - Coalesce the rebuilds: one per batch, or one on the next metrics tick.

The reviewer declined to judge the following. Settle each and record the decision in the handover you
write at the end (item 6).

- **Apps listed twice.** Processes under Applications also appear under Processes. Check the reference's
  `stats.py`; `group_processes` receives the whole process table, which suggests that is intended.
- **Windows that share one PID across app ids.** xwayland-satellite and PWAs do this. `windowOwner` keeps
  whichever window it saw last, so the other applications come out empty and are hidden.
- **Widths below 800 logical.** A surface configured narrower than 800 logical px fails layout, as
  Settings does. The ruling in Task 7 accepted this.
- **Hover and sort role colours on a translucent panel.** Check them with the expressive preset.

### 3. One last polish pass

This happens after the live gate in item 6, step 2 has shown the panel on screen.

Put a grim capture of each page next to `screenshots/panel.png`, cropping and scaling to match. Then close
the visible gaps. Things the executor could not check without a screen:

- **Header.** The title uses the title text role, not headline: the style guard requires a title-role
  heading. Check its weight and size against the reference's bold "Processes". Check the pills (the accent
  fill when selected, rounded 8 px corners), the search well's accent outline when focused, and the square
  clear button.
- **Info card.**
  - The rings shrank from 118 to 84 px so their label and caption fit inside the card. The reference puts
    the label inside the ring, which would let the ring grow back. If you change this, keep
    `TestInfoCardGaugesStayInsideTheCard` green.
  - The logo is 110 px. Check that it loads (`LOGO=archlinux-logo` here) and doesn't fall back to the letter
    tile.
  - Check that the facts rows are aligned.
- **Table.**
  - The rows are on a 32 px pitch.
  - The sorted column's pill, and the hover fill with its rounded corners.
  - The pink section headers with their chevrons.
  - Icon sizes, and the letter tiles shown when an icon is missing.
  - Numbers right-aligned in the CPU, MEM, SWAP and DISK cells.
  - The Name column ellipsising long names.
- **Detail view.** The column widths (`detailWideValueW`, `detailNarrowValueW`), with a long command line.
- **System page.** Section rhythm. Decide the scroll question from item 2.5 here.
- **Motion and density.** Check compact and comfortable density, and both theme modes (light and dark).

Keep every change within the style guards in `surfacerole_test.go`:

- no numeric literals for `Width`, `Height`, `Gap`, `Padding` and similar fields; use named constants or
  the theme metrics;
- no synthetic bold;
- cards pad by `CardPadding`.

Rows inside a virtual list are not reachable through `findAction` on the root; walk `list.Item(i)` instead.

### 4. Gated: copy on click (plan Task 12, D9)

- **Gate:** sysc-clipboard releases a `copy` protocol message and a client method `Copy(text string) error`.
  That change needs its own short plan in the sysc-clipboard repository. None is written yet.
- **Shell side:** follow plan Task 12 as written:
  1. Bump the pin in `go.mod`, `go.sum` and the README pin table.
  2. Add `Copy` to `clipboardCommandSender`.
  3. Make the detail values and fact rows into `monitor:copy:` buttons.
  4. Send the copy off the lock.
- **Live proof:** click a PID; `wl-paste` prints it.

### 5. Gated: executable, swap, I/O and the memory breakdown (plan Task 13, D10)

- **Gate:** sysc-metrics releases a version, stacked on v0.6.1, that carries the field names fixed in plan
  Task 13:
  - `Executable` / `ExecutableValid`;
  - `SwapBytes` / `SwapValid`;
  - `IO ProcessIO{ReadBytesPerSecond, WriteBytesPerSecond, Valid}`;
  - `ReadProcessMemory(ProcessIdentity) (ProcessMemory, error)`, with `PrivateBytes` and `SharedBytes`.

  That change needs its own plan in the sysc-metrics repository. None is written yet, and the in-flight
  sysc-529/530 work there does not add these fields.
- **Shell side:** follow plan Task 13:
  1. Implement the three accessors in `processlines.go`: `processExecutable`, `processSwap` and `processIO`.
     They are stubs today, which is why SWAP and DISK read `—` and groups key on the process name.
  2. Fill the detail view's I/O rows.
  3. Read `smaps_rollup` once per selection, off the lock.
- **Live proof:** the SWAP and DISK columns show values, and Firefox content processes group under
  `/usr/lib/firefox/firefox`.

### 6. Live Niri gate, merge and completion (plan Task 14)

1. **Deploy.** Before deploying, look at `~/.local/bin/sysc-shell` and the `before-*` copies beside it. On
   2026-09-25 another session deployed a monitor follow-ups build at 23:08. Ask the owner before replacing a
   build that isn't yours.
   - Build with `GOMAXPROCS=4 go build -p 2`.
   - Back up the running binary as `sysc-shell.before-system-monitor-panel`.
   - Install with `mv`, then `systemctl --user restart sysc-shell`.
2. **Open and capture.** Open the panel over IPC:
   `{"method":"panel.open","params":{"panel":"system-monitor","section":"mem"}}` to
   `/run/user/1000/sysc-shell/ipc.v1.sock`. Assert `sysc-shell-panel` in `niri msg -j layers`, then capture
   each page with grim. Do not drive the panel with `wtype`: the virtual keyboard steals focus and unmaps
   it.
3. **Observations.**
   - An application row's MEM equals the sum of `VmRSS` over its window PID and descendants, from `/proc`.
   - A `sleep 600` found by search, then killed from the detail view, is gone.
   - Proofs 4 and 5 above, once those items land.
4. **Gates.**
   - Run the gates in the plan's Task 11.
   - Never run the race detector over the whole repository at once; it hard-locks this machine. Race-check
     one package at a time.
   - Screen every commit message with `bash ~/.git-hooks/commit-msg <file>`. The word "both" is rejected.
5. **Finish.**
   - Merge through superpowers:finishing-a-development-branch.
   - Write `docs/plans/<date>-system-monitor-panel-completion-handover.md` with its register row. It records
     the gate output, the side-by-side captures, the live observations, and anything that still differs
     from the reference.
   - Then delete this handover and its register row, per `docs/plans/README.md`.

## Rulings already made during execution

These are settled unless the owner says otherwise. The cost of each being wrong is noted.

- **Render tests.** Two tests set `Track`, because `on_surface_variant` resolves to it through the existing
  switch. Cost: a test tweak.
- **Settings section count.** The settings rail's section-count guard goes from 12 to 13 for Monitor.
  Cost: none.
- **Existing helpers.** Reused `walkNodes` and `toggleValue`; tests build CPU snapshots with
  `metrics.CPUSnapshot`. Cost: none.
- **Uptime.** `machineFacts.Uptime` keeps the short form for the Control Centre's Home identity;
  `UptimeLong` feeds the monitor. Cost: one extra field.
- **View options.** `writeConfig` only signals a reload, so view-option toggles also set `r.cfg.Monitor`
  immediately. Cost: none; the reload writes the same value.
- **Kept process tests.**
  - The tests were adapted: fixtures use distinct names, and pointer and keyboard tests target the row's
    `monitor:select:` action.
  - The signal test drives the action directly.
  - Cost: test rewrites.
- **Header layout.**
  - The search field is 220 px wide; 300 does not fit at 800 wide.
  - The table header row's height includes the insets that align its cells with the rows below.
  - Cost: layout constants.
- **Old surface sizes.** Tests that configured at 640 × 720 and 640 × 480 (the old size) now use
  `panelTargetSize`. Cost: a blank monitor on a surface narrower than 800 logical px.
- **Deleted tests.** The old monitor-card tests were deleted, and the tall-tree height test was rewritten.
  Cost: GPU-ambiguity coverage now rests on `selectGPU`'s tests.
- **Style guards.**
  - Cards pad by `CardPadding`.
  - The header title and section headers are title-role headings.
  - System rows are not indented.
  - Cost: the title reads one role smaller than the reference, which item 3 should check.
- **Refresh interval.** The refresh is pinned through a pure `monitorLeaseInterval`, not a test-only
  metrics API. Cost: a call site that bypasses it goes untested.
- **Control Centre GPU temperature.** The reviewer graded the doubled temperature Minor; it was re-graded to
  Important and fixed: only the System page's GPU caption adds it. Cost: none.
