# System Monitor Panel Design

Owner-approved 2026-09-25. Redesigns both pages of `PanelMonitor` after the Noctalia community
`processes` plugin by tordex.

## Why

The owner judged the reference better than our panel in almost every respect and asked for it to be
emulated extremely closely, if not identically. Our panel today:

- The Processes page is a flat table of every PID ordered as it arrives: kernel threads first, a red
  `Kill` pill on every row, only Name / CPU / Memory / PID, and no notion of an application.
- The System Monitor page is a stack of titled cards, two per row, plus a separate System facts card
  and a Resources card, in a 640 × 720 panel.

## Reference

- Source: `https://github.com/noctalia-dev/community-plugins/tree/main/processes` (`panel.luau`,
  `scripts/stats.py`), screenshot `screenshots/panel.png`, listing
  `https://noctalia.dev/plugins/community/processes`.
- It is a behaviour and layout reference only. Nothing is ported: no Luau, no Python, no psutil.
- In the reference, `User | System` filters processes by owner. It is not a page switch. This design
  uses that pill pair's position for our page switch and moves the owner filter into the options menu
  (D2).

## Decisions

### D1. Panel size and frame

`panelTargetSize(PanelMonitor)` becomes 800 × 650 logical, the reference's size. `FittedSize` still
clamps on a short output. Both pages share one frame: a header row, an info card, and a content card
that fills the remaining height.

### D2. Header

One component on both pages, left to right:

1. A monitor icon and the page title: **Processes** or **System**.
2. A two-pill page switch, **Processes | System**, replacing the "System Processes / System Monitor"
   segmented control. The selected pill takes the accent fill, as the reference's selected `User` pill
   does. The chosen page persists for the session in `h.monitorPage`, as today.
3. Processes page only: a search glyph, the search field ("type to search", accent outline while
   focused), and a square `×` clear button.
4. Processes page only: an options button opening a menu with *Show applications*, *Show processes*,
   and the owner filter *All / User / System*.
5. A settings button, which opens the Settings panel at the Monitor section, and a close button.

### D3. Info card

One component on both pages, in a rounded `ContainerHigh` card:

- **Distro logo**, about 110 px square, resolved from `LOGO=` in `/etc/os-release` (`archlinux-logo`
  here) through `internal/icons`. With no `LOGO=` or no icon, show the first letter of `NAME` in a
  rounded tile, as `runningAppLetter` does for apps.
- **Five facts**, each with a leading glyph: distro (`PRETTY_NAME`), kernel release, CPU model, board
  vendor and name (`/sys/class/dmi/id/board_vendor`, `board_name`), and uptime written out as
  `0 days 9 hours 25 minutes`. The current WM row is removed: Niri is the only supported compositor.
- **Two `KindRadialGauge` rings** on the right. **CPU** shows usage % in the ring, the label `CPU`,
  and the CPU temperature beneath. **Memory** shows used bytes (`8.3G`), the label `Memory`, and the
  available bytes beneath (`+22.9G`).
- On the Processes page, selecting a process replaces this card's contents with the detail view (D6).
  The card keeps its size, so the table does not move.

### D4. Processes page table

A rounded card with a header row and a `KindVirtualList`.

**Columns:** Name, CPU, MEM, SWAP, DISK, PID, USER.

- Name carries a disclosure chevron for groups, then an icon, then the name.
- CPU is `0.2%`. MEM and SWAP use the reference's `2.4 G` / `651.1 M` / `41.2 K` form. DISK is
  read + write bytes per second in the same form with `/s`. PID is blank on a group row. USER is the
  username resolved from the UID.
- A zero or unavailable value is `—`. This is the reference's behaviour and the monitor design's
  absent rule (D4 there).

**Sorting:** clicking a header cell sorts by that column; clicking it again reverses the order. The
active header shows `▼` or `▲`. The sorted column gets a filled pill behind its header cell and behind
every value in that column, as in the reference screenshot. Rows with an invalid value in the sort
column go last in either direction.

**Sections:** two collapsible headers coloured with the `Tertiary` role, the pink in the reference.

- **Applications:** one row per Niri `app_id`, named and iconed from its desktop entry through the
  existing `runningapps.go` lookup (`loadRunningAppEntries`, `lookupRunningApp`), falling back to the
  raw `app_id`. Its members are each window's PID plus all of its descendants via `ParentPID`, with each
  PID counted once. Its values are the members' sums. Expanding it lists the members.
- **Processes:** processes grouped by `Executable`, falling back to `Name` when the executable is
  unreadable (kernel threads, other users' processes). A group of one is a plain row showing its PID. A
  larger group is named after the executable's base name, sums its members, and expands to them.

Without a Niri connection, the Applications section is hidden rather than shown empty. The
*Show applications* and *Show processes* options hide their section.

**Rows** are on a 32 px pitch. That is the reference's row measured at its screenshot scale: the logo, declared 110 px in the plugin source, spans 168 px in the capture, a scale of 1.53, so its 50 px physical rows are about 33 logical. Hovering fills a row with the configured hover colours (D7).
Clicking a leaf row opens the detail view (D6). Clicking a group row toggles it. Expansion is keyed by
stable IDs, `app:<app_id>` and `exe:<path>`, so it survives refreshes.

**Footer:** `Total processes: N (user: U  system: S)`, and the existing "N processes could not be read"
line when the snapshot carries issues.

### D5. The projection

`projectProcessLines(in processLineInput) []processLine` in `internal/shell/processlines.go` is a pure
function and holds every rule in D4.

- The input is the process snapshot, the Niri windows, the current UID, the query, the owner filter,
  the sort key and direction, the expanded set, and the two section flags.
- It returns flat lines, each with a kind (section header, group, leaf), a depth, a stable key, and
  display values.
- The search matches name, executable, arguments and username, case-insensitively. A group is kept
  when any member matches.
- It replaces `projectProcesses`.

### D6. Detail view and actions

The detail view has three parts, left to right:

1. A button column: **Kill** (SIGINT), **Force kill** (SIGKILL), and **Close**. Kill and Force kill are
   disabled for a process the user does not own.
2. comm, PID, PPID, exe, cmdline and CPU.
3. private, shared, mem, swap, io read/s and io write/s.

Clicking any value copies it to the clipboard (D9).

The signals go through the existing identity-checked path: `parseProcessIdentityAction` carries PID
and start ticks, `ValidateProcessIdentity` runs before `Signal`, and a recycled PID is never signalled.
`parseProcessAction` generalises from SIGTERM to the two actions `process:int:` and `process:kill:`. The
per-row `Kill` pills are removed.

If the selected process leaves the snapshot, the view reads "Process exited" and its actions disable.
Close and Escape return to the info card.

### D7. Configuration

A new `[monitor]` section, with entries in the Settings panel through `internal/settings`. Values
mirror the reference's settings.

| Key | Type | Default | Use |
|---|---|---|---|
| `refresh` | int seconds, 1–10 | 1 | Interval of the panel's process and metric leases |
| `sort_column_background` | theme role | `surface_variant` | Sorted column pill |
| `sort_column_color` | theme role | `on_surface_variant` | Sorted column text |
| `hover_background` | theme role | `surface_variant` | Hovered row fill |
| `hover_color` | theme role | `on_surface_variant` | Hovered row text |
| `show_apps` | bool | true | Applications section |
| `show_processes` | bool | true | Processes section |

- Colours are theme role names, not hex, so they follow wallpaper and matugen retheming, as in the
  reference. An unknown role name is refused by the loader with a path error, as every other invalid value in this configuration is, and the running configuration is kept.
- The options menu's section toggles write the same keys, so they persist.
- The owner filter and sort order are session state, not configuration.
- The metric thresholds stay fixed in code, as the Control Centre monitor design (D2 there) decided.

### D8. System page

The same header (D2) and info card (D3). Below them is one content card in the process table's style:
the same container and the same collapsible pink section headers, so the two pages read as one panel.
Its rows are the Control Centre's two-line 50 px rows, because each carries a value, a caption and a mark.

| Section | Row | Value | Caption | Mark |
|---|---|---|---|---|
| Compute | CPU | usage % | °C · mean GHz · load 1 / 5 / 15 | sparkline |
| Compute | GPU | usage % | name · VRAM used / total · °C | sparkline |
| Memory | RAM | used % | used / total | meter |
| Memory | Swap | used % | used / total | meter |
| Storage & network | `/` | used % | used / total | meter |
| Storage & network | Disk I/O | R · W rates | device · peak | two-series sparkline |
| Storage & network | Network | ↓ · ↑ rates | interface · peak | two-series sparkline |

- Each row has an icon, a label, a tabular value in a fixed-width column, a caption, and its mark on
  the right.
- The value builders, captions, threshold tones, scales and subject pickers come from the Control
  Centre monitor page (`controlcenter_monitor.go`, `monitorscale.go`, `monitorsubjects.go`) and are
  shared, not copied. Threshold tones colour the value and its line.
- An absent source keeps its row and shows `—`.
- The current two-per-row cards, the System facts card and the Resources card are removed. The panel
  leases the same sources the Control Centre leases, at the `refresh` interval.

### D9. Clipboard

The shell has no path to put arbitrary text on the clipboard. `clipboardCommandSender` only restores
history entries, and `sysc-clipboard` v0.1.0's protocol has no copy message.

`sysc-clipboard`, which already owns the data-control selection, gains a `copy` message carrying
text/plain. `clipboardCommandSender` gains `Copy(string) error`. The alternative of spawning `wl-copy`
is rejected: it adds an external runtime dependency the shell does not otherwise need, and a second
selection owner.

### D10. sysc-metrics process fields

This stacks on sysc-metrics v0.6.1 (`sysc-529`, `sysc-530`), which the design assumes has landed.

A new sysc-metrics release adds these fields to `Process`, each with a validity flag:

| Field | Source |
|---|---|
| `Executable` | `readlink /proc/<pid>/exe` |
| `SwapBytes` | `VmSwap` in `/proc/<pid>/status` |
| `ReadBytesPerSecond`, `WriteBytesPerSecond` | `read_bytes` / `write_bytes` deltas in `/proc/<pid>/io`, keyed by `ProcessIdentity` as the CPU deltas already are |
| `PrivateBytes`, `SharedBytes` | `Private_*` and `Shared_*` sums in `/proc/<pid>/smaps_rollup` |

- An unreadable file (permissions, a process that exited) leaves that field invalid. It is not an
  issue, because it is the normal case for other users' processes.
- `smaps_rollup` is the expensive read. It is sampled only for the selected process, through a new
  per-identity detail call, not for every PID on every tick.
- Usernames are resolved in the shell through `os/user`, cached per UID for the panel's lifetime.

## Units

| Unit | Responsibility |
|---|---|
| `internal/shell/processlines.go` | D5 projection, pure |
| `internal/shell/popout_process.go` | Processes page tree, D4 |
| `internal/shell/monitorframe.go` | Shared header and info card, D2 and D3; `readMachineFacts` gains board and logo, drops WM |
| `internal/shell/processdetail.go` | D6 view and actions |
| `internal/shell/popout_monitor.go` | System page, D8 |
| `internal/config`, `internal/settings` | `[monitor]`, D7 |
| sysc-clipboard | `copy` message, D9 |
| sysc-metrics | Process fields, D10 |

## Dependencies and gates

- The SWAP and DISK columns, grouping by executable, and the detail view's exe, memory breakdown and
  I/O rows depend on the D10 sysc-metrics release and a shell pin bump.
- Copy-on-click depends on the D9 sysc-clipboard release and a shell pin bump.
- Each is a gate issue in this repository. Everything else in this design depends on neither.

## Out of scope

- Other compositors' application grouping. Niri is the only supported compositor.
- Configurable metric thresholds, history windows or chart colours on the System page.
- Per-core CPU and network session totals.
- A tree view of the process hierarchy other than the application and executable grouping.

## Verification

- `processlines_test.go` table tests:
  - An application summing a window PID and its descendants, each counted once.
  - Executable grouping with the `Name` fallback.
  - Single-member groups rendered as plain rows.
  - Owner filter, and search matching a group through one member.
  - Every sort key in each direction, with invalid values last.
  - Expansion keys surviving a snapshot where PIDs change.
  - Section flags, and no Niri windows hiding Applications.
- Parser tests for `process:int:` / `process:kill:` and the identity check.
- A config round-trip and validation test for `[monitor]`, including an unknown role name.
- Tree tests:
  - The sorted column and hovered row use the configured roles.
  - A group row carries no PID.
  - The detail view's actions disable for a foreign UID and after exit.
  - The System page shows `—` with no mark for an absent source.
- Live Niri gate:
  - Open the panel by IPC on each page and assert the `sysc-shell-panel` layer in `niri msg -j layers`.
  - Capture each page with grim and set it beside `screenshots/panel.png`.
  - The Firefox application row's memory equals the sum of the RSS of its PIDs, read from `/proc`.
  - Kill a throwaway `sleep` through the detail view.
  - A clicked PID reads back from `wl-paste`.
