# Architectural-proof live gate

The architectural proof cannot be qualified by unit tests alone. Its purpose is to prove the Wayland,
text, layout, input, Niri IPC, scheduling and shutdown architecture against a real compositor, so this
gate runs on a live Niri session and records what the compositor actually reported.

Run every step on the machine under test. Record the observations in the milestone handoff, not in this
repository: committed results would be a benchmark claim from one machine.

## Prerequisites

- a running Niri session, with `NIRI_SOCKET` and `WAYLAND_DISPLAY` set;
- at least one connected output;
- `niri msg` on `PATH`.

## Step 1: automated checks

```bash
go test -race ./...
go vet ./...
go build ./cmd/sysc-shell
```

All three must exit zero.

## Step 2: one output

```bash
niri --version
niri msg outputs
go run ./cmd/sysc-shell --output <connector>
```

Record:

- the Niri version;
- the output name, mode, transform, and scale;
- the layer-surface configure width and height, **and** the output's logical width for comparison;
- the `wp_fractional_scale_v1.preferred_scale` numerator, and whether it arrived before or after the
  first configure;
- the buffer width, height, and stride;
- the advertised `wl_shm` format list and the selected format;
- the workspace label after a workspace change, and the meter and button after a click;
- the shutdown result.

The configure size is **logical** and is not the output size. It is what remains after other layer
surfaces' exclusive zones, so it may be smaller than the output's logical width in either dimension.
Never derive the surface size from `wl_output` mode or from Niri IPC.

To prove that reserved space is honoured rather than assumed, run the proof twice: once with the
session's other shell running, and once with it stopped. The configure size must differ by exactly the
space that shell reserves, and must equal the output's logical size when nothing else reserves any.

## Step 3: a non-1 scale

Prefer an output already running at a non-1 scale and select it with `--output`; that path mutates
nothing. When every output is at scale 1, change a **non-focused** output temporarily. `niri msg output`
changes configuration temporarily and never writes the configuration file, but capture the current value
so the restore is exact:

```bash
OUT=<connector>
BEFORE=$(niri msg -j outputs | python3 -c "import json,sys;print(json.load(sys.stdin)['$OUT']['logical']['scale'])")
niri msg output "$OUT" scale 1.5

go run ./cmd/sysc-shell --output "$OUT"

niri msg output "$OUT" scale "$BEFORE"
niri msg -j outputs | python3 -c "import json,sys;print(json.load(sys.stdin)['$OUT']['logical']['scale'])"
```

Confirm the restored scale matches `BEFORE`.

Record the logical configure size and the derived buffer size together. They are different numbers and
both must be right:

```text
buffer = (logical * scale120 + 60) / 120
```

Expect the buffer **not** to be a multiple of the output's pixel width. The viewport scales the buffer to
the logical destination, so a surface narrowed by another client's exclusive zone yields a buffer width
that matches neither the output mode nor the logical width.

## Step 4: idle rendering

Run the proof for 60 seconds with no input and no workspace change. Instrument the submitted frame count
in debug output for this gate and remove the instrumentation afterwards; the proof carries no permanent
diagnostic flag. The count must not change after the frames that the initial configure and the first
workspace snapshot produce.

## Step 5: restart cleanup

Start and stop the proof ten times. Every run must map its surface, exit zero, and leave no process
behind.

## Step 6: shutdown

Confirm a clean exit on both `SIGINT` and `SIGTERM`, with no protocol error reported.

## Input note

Verifying the click requires real pointer input. Synthetic input through a `uinput` virtual device is not
sufficient on its own: the compositor only delivers events from devices its seat has taken, so a virtual
device the seat has not adopted produces no events at all in any client. Confirm that synthetic input
reaches an ordinary Wayland client before relying on it, and otherwise perform the click by hand.

---

# Stable multi-output bar live gate

Milestone 2 adds a bar on every connected output. Nothing below can be proved by
unit tests: configure ordering, exclusive zones, transform handling, input
coordinates and compositor shutdown only exist against a real Niri.

Record observations in the milestone handoff, not here. Committed results would
be a benchmark claim from one machine.

## Prerequisites

- a running Niri session, with `NIRI_SOCKET` and `WAYLAND_DISPLAY` set;
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

All four must exit zero.

## The matrix

Run `go run ./cmd/sysc-shell` for each check. There is no `--output` flag: every
connected output receives a bar.

| # | Check | Passing observation |
|---|---|---|
| 1 | one output | exactly one bar, correct reserved band |
| 2 | two or more outputs | one bar each, independent workspace text |
| 3 | physical hotplug | a bar appears without restarting the shell |
| 4 | physical unplug | that bar disappears, the others keep running |
| 5 | reconnect | a new registry global, one bar, no duplicate |
| 6 | one transformed output | configure dimensions swap; the bar is upright and hit-tests correctly |
| 7 | mixed scales, one non-1 | per-output buffer sizes correct |
| 8 | scale or mode change while mapped | no stale buffer, no wrong hit region |
| 9 | exclusive zone with Niri windows | windows begin at the configured distance |
| 10 | physical pointer on at least two bars | clicks route to the bar under the pointer |
| 11 | validated reload with all bars present | every bar adopts the new policy together |
| 12 | restart | one bar per output restored |
| 13 | 60-minute idle run | no continuous frame loop |

Checks 6 and 10 also close the two proof checks deferred on 2026-08-28.

## Geometry to record

The default tokens are height 48, gap 4. Those are **not** Wayland dimensions:
the painted body is `height - 2*gap` = 40 and the surface is `gap + body` = 44,
which is also the exclusive zone. So:

- the layer-surface configure height must be **44**, not 48;
- Niri windows must begin **44** logical pixels from the anchored edge;
- the configure width is the output width minus other clients' exclusive zones,
  and is never the output mode width.

## Check 6: transform

The design states, but has never verified, that the configure size is already in
the output's post-transform logical space and that the client must not call
`wl_surface.set_buffer_transform`. Capture the current value so the restore is
exact:

```bash
OUT=<connector>
BEFORE=$(niri msg -j outputs | python3 -c "import json,sys;print(json.load(sys.stdin)['$OUT']['transform'])")
niri msg output "$OUT" transform 90

go run ./cmd/sysc-shell

niri msg output "$OUT" transform "$BEFORE"
```

Record the configure width and height before and after rotating. They must swap.
If the bar renders rotated, the correction is to call `set_buffer_transform` with
the output transform and re-run; nothing else in the design changes.

## Check 7: mixed scales

Prefer an output already at a non-1 scale. When every output is at scale 1,
change a **non-focused** one temporarily, capturing the current value first:

```bash
OUT=<connector>
BEFORE=$(niri msg -j outputs | python3 -c "import json,sys;print(json.load(sys.stdin)['$OUT']['logical']['scale'])")
niri msg output "$OUT" scale 1.5

go run ./cmd/sysc-shell

niri msg output "$OUT" scale "$BEFORE"
```

Record each output's logical configure size and derived buffer size together:

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

Every bar must adopt the new policy together. Then prove a rejected candidate
changes nothing: write `{"bar": {"height": 4, "gap": 4}}`, send `SIGHUP`, and
confirm the bars are unchanged and the error names `bar.height`.
