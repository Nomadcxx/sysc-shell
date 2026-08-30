# Stable multi-output bar live gate

Milestone 2 requires live Niri evidence for configure ordering, exclusive zones, transforms, input
coordinates and compositor shutdown.

Record observations in the milestone handoff. Keep machine-specific results out of Git.

## Prerequisites

- a running Niri session with `NIRI_SOCKET` and `WAYLAND_DISPLAY` set;
- at least two connected outputs, one of which can be unplugged;
- one output that can be rotated;
- `niri msg` on `PATH`.

## Automated gate

```bash
go mod tidy -diff
go test -race -count=1 ./...
go vet ./...
go build ./cmd/sysc-shell
```

All four commands must exit zero.

## The matrix

Run `go run ./cmd/sysc-shell` for each check. The shell creates a bar on each connected output.

| # | Check | Passing observation |
|---|---|---|
| 1 | one output | exactly one bar, correct reserved band |
| 2 | two or more outputs | one bar each, independent workspace text |
| 3 | physical hotplug | a bar appears without restarting the shell |
| 4 | physical unplug | that bar disappears, the others keep running |
| 5 | reconnect | a new registry global, one bar, no duplicate |
| 6 | one transformed output | width follows the transformed logical output; height stays 44; the bar is upright and hit-tests correctly |
| 7 | mixed scales, one non-1 | per-output buffer sizes correct |
| 8 | scale or mode change while mapped | no stale buffer, no wrong hit region |
| 9 | exclusive zone with Niri windows | windows begin at the configured distance |
| 10 | physical pointer on at least two bars | clicks route to the bar under the pointer |
| 11 | validated reload with all bars present | every bar adopts the new policy together |
| 12 | restart | one bar per output restored |
| 13 | 60-minute idle run | no continuous frame loop |

Checks 6 and 10 close the two proof checks deferred on 2026-08-28.

## Geometry to record

The default tokens are height 48 and gap 4. The painted body is `height - 2*gap = 40`. The surface
and exclusive zone are `gap + body = 44`.

- The layer-surface configure height must be **44**.
- Niri windows must begin **44** logical pixels from the anchored edge.
- The configure width equals the output width minus other clients' exclusive zones. It does not equal
  the output mode width.
- The 4px top and side gap and the 12px body corners must remain transparent.

## Check 6: transform

The design expects the configure size in the output's post-transform logical space and makes no
`wl_surface.set_buffer_transform` request. Capture the current transform for exact restoration:

```bash
OUT=<connector>
BEFORE=$(niri msg -j outputs | python3 -c "import json,sys;print(json.load(sys.stdin)['$OUT']['logical']['transform'])")
niri msg output "$OUT" transform 90

go run ./cmd/sysc-shell

niri msg output "$OUT" transform "$BEFORE"
```

Record the configure width and height before and after rotation. The height must stay 44. The width must
follow the output's post-transform logical width, normally the old logical height minus any competing
exclusive zones. If the compositor rotates the bar pixels, add `set_buffer_transform` and rerun this
check.

## Check 7: mixed scales

Prefer an output running at a non-1 scale. If each output uses scale 1, change a non-focused output and
capture its current value:

```bash
OUT=<connector>
BEFORE=$(niri msg -j outputs | python3 -c "import json,sys;print(json.load(sys.stdin)['$OUT']['logical']['scale'])")
niri msg output "$OUT" scale 1.5

go run ./cmd/sysc-shell

niri msg output "$OUT" scale "$BEFORE"
```

Record each output's logical configure size and derived buffer size:

```text
buffer = (logical * scale120 + 60) / 120
```

## Check 11: reload

```bash
mkdir -p ~/.config/sysc-shell
cat > ~/.config/sysc-shell/config.json <<'JSON'
{"bar": {"height": 56, "gap": 6},
 "theme": {"accent": "#ff8800"},
 "outputs": [{"connector": "<connector>", "bar": {"height": 48}}]}
JSON
pkill -HUP sysc-shell
```

Each bar must adopt the candidate together. Then write `{"bar": {"height": 4, "gap": 4}}`, send
`SIGHUP`, and confirm that each bar retains its prior state and the error names `bar.height`.

## Tranche 3A: built-in widget foundation

Run only after Milestone 2 passes its own live matrix. Record connector names,
window titles and measurements outside this repository.

Build and start:

    go build -o /tmp/sysc-shell-milestone3 ./cmd/sysc-shell
    /tmp/sysc-shell-milestone3

Matrix:

1. One output, then at least two. Every configured output receives exactly one
   bar, with clock, date, workspace and title.
2. One clock snapshot on every bar: the minute changes on all bars together.
3. Independent per-output text: switch workspaces on one monitor and confirm
   the other monitor's workspace and title do not change.
4. Focus a different window in the same workspace and confirm the title
   updates. This is the one behavior the design could not verify offline: it
   assumes Niri emits WorkspaceActiveWindowChanged for an in-workspace focus
   move. If the title does not update, consume WindowFocusChanged as a second
   trigger and re-run.
5. Retitle a window (change a browser tab) and confirm only that output's bar
   repaints.
6. Unplug and replug an output. No duplicate bar, no missing widget, no leaked
   instance.
7. Edit the configuration to add and remove clock and Niri widgets, then
   SIGHUP. Confirm the new set renders and the clock does not visibly stall.
8. Write an invalid configuration and SIGHUP. Confirm the previous widgets stay
   live and the error names its field path on stderr.
9. Kill the Niri socket. Confirm the shell exits cleanly with a named error and
   leaves no process or socket behind.
10. Suspend and resume. Confirm the clock catches up within one boundary
    (at most 60 seconds).

Baselines to record before setting any budget:

- idle CPU and wakeups over 60 minutes, with a minute-boundary clock;
- CPU during clock ticks and during a burst of window title changes;
- RSS after one hour;
- submitted and skipped frame counts;
- layout and paint duration per update;
- allocations per update;
- binary size.

## Tranche 3B: core metrics

Run after Tranche 3A's matrix. Record connector, device, interface and mount
names and all measurements outside this repository.

Build and start:

    go build -o /tmp/sysc-shell-tranche3b ./cmd/sysc-shell
    /tmp/sysc-shell-tranche3b

Matrix:

1. One output, then at least two. Each output renders the same metric
   independently and updates together.
2. A text metric, a meter and a graph on one bar simultaneously, all updating.
3. Load the machine and confirm the CPU value and graph track the load, and
   settle when it stops.
4. Fill and empty a filesystem and confirm the meter follows.
5. Down an interface, or unplug a device, and confirm its widget renders "-"
   while every other widget keeps updating and the shell keeps running.
6. Bring it back and confirm the widget recovers without a restart.
7. Confirm the first sample after start renders "-" for rate widgets for one
   interval only.
8. Reload adding and removing metric widgets, and changing an interval, and
   confirm the service does not restart and no widget stalls.
9. Write an invalid metric configuration and SIGHUP; confirm the previous
   widgets stay live and the error names its field path on stderr.
10. Confirm stderr carries exactly one line when a source starts failing and
    one when it recovers, not one per sample.

Baselines to record before setting any budget:

- idle CPU and wakeups over 60 minutes with a 2-second interval, against the
  Tranche 3A clock-only baseline;
- CPU during sampling and during a graph repaint;
- RSS after one hour with a graph running;
- submitted and skipped frame counts;
- allocations per sampling pass;
- binary size against the Tranche 3A binary.

## Tranche 3D: weather and visual vocabulary

Run after Tranche 3B's matrix. Coordinates, place names and measurements stay
outside this repository.

Build and start:

    go build -o /tmp/sysc-shell-tranche3d ./cmd/sysc-shell
    /tmp/sysc-shell-tranche3d

Matrix:

1. One output, then at least two, each rendering the reading independently.
2. The icon and the temperature render, and the icon matches the condition.
3. Disconnect the network: the reading goes stale with its age and the shell
   keeps painting.
4. Reconnect: the reading recovers without a restart, within one backoff step.
5. Start with an unreachable host: the widget renders the error tone, not an
   empty space.
6. Confirm stderr carries one line when fetching starts failing and one when
   it recovers, not one per attempt.
7. Reload changing coordinates, unit and interval; the service must not
   restart and no widget may stall.
8. Hover a widget: the tooltip appears after the dwell and is placed fully
   inside the output.
9. Hover a widget at the extreme left and right of an output: the tooltip stays
   on screen.
10. Reload with a tooltip open: it closes and no surface leaks.
11. Idle CPU and wakeups over 60 minutes against the Tranche 3B baseline.

## Tranche 3C: power

Run on a laptop; item 4 additionally needs a machine with no battery. Record
machine values outside this repository.

Build and start:

    go build -o /tmp/sysc-shell-tranche3c ./cmd/sysc-shell
    /tmp/sysc-shell-tranche3c

Matrix:

1. On battery: the level glyph tracks discharge and the label matches the
   system reading.
2. Plug in: the glyph switches to a charging one within one interval.
3. Discharge across the threshold: the warning tone appears; plugging in
   clears it without waiting for the charge to rise.
4. A machine with no battery, same configuration file: the widget renders
   nothing and reserves no space.
5. Remove a battery at runtime if the hardware allows, or stop the source:
   the widget hides with no reload, and returns when it comes back.
6. Each label mode renders its own field, and "none" renders the glyph alone.
7. Just after plugging in, the time mode renders no label rather than a
   placeholder, and recovers once the estimate settles.
8. Reload changing the label mode and the threshold without restarting the
   sampling service.
9. Idle wakeups over 60 minutes against the Tranche 3B baseline: a battery at
   a steady charge must cost no frames.

## Tranche 4A: panels

Live Niri checklist. Record observations in the milestone handoff. Unrun
hardware checks stay open; do not convert a missing session into a pass.

1. **Focus fall-through:** open the session panel from a foot window, press
   Escape; keyboard focus returns to foot without any `focus-window` call.
2. **Shield pointer delivery:** with a panel open, click outside it; the panel
   closes and the click does not reach the window beneath.
3. **Exclusive beats windows:** with a panel open, click a window, press
   Escape — the panel closes.
4. **Compositor keybinds survive:** with a panel open, a niri keybind such as
   Super+Return still fires.
5. **Fullscreen does not hide panels:** fullscreen a window; open the clock
   panel — it stays visible on the Overlay layer.
6. **Hotkeys:** add the binds in `docs/niri-hotkeys.md`; Super+P/M/X toggle
   clock, system-monitor, and session.
7. **High contrast:** set `accessibility.high-contrast: true`, reload; tokens
   differ from the default palette.
8. **Multi-output:** focus a window on each output and trigger the same panel
   through IPC; it closes and reopens on the newly focused output.

