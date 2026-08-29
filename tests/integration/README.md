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
