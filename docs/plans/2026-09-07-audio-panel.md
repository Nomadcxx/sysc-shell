# Standalone Audio Panel Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development
> (recommended) or superpowers:executing-plans to implement this plan task-by-task.
> Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ship `PanelAudio` — a 560×496 bar-attached panel with Volumes and
Devices tabs, fused to the bar by concave wing-tips — plus the `volume` bar
widget that owns it, a `pw-dump` enumeration service, and the
`render.AttachedMask` concave-corner primitive.

**Architecture:** One new first-party panel composed from shipped
`internal/ui` primitives (`KindSlider`, `KindSegmented`, `KindScroll`,
`KindImage`, `KindIcon`). Reads come from a panel-scoped `pw-dump` lease with
cubic volume conversion; writes go through `wpctl` by node id. One genuinely
new render primitive (`AttachedMask`), four new Material glyphs, and one new
bar action seam (positioned trigger + axis). The bar's existing cheap
default-sink poll (`@DEFAULT_AUDIO_SINK@`) is untouched.

**Tech Stack:** Go, the native retained renderer, `internal/ui` nodes,
`internal/theme` tokens, `internal/services`, `internal/ipc`, `pw-dump`,
`wpctl`.

**Spec:** `docs/plans/2026-09-07-audio-panel-design.md` (D1–D13).
**Mock:** `docs/plans/assets/2026-09-07-audio-panel/` — owner-approved
2026-09-07: keep 560 px width (D2), selected wells + `check` on Devices
rows (D6), `AttachedMask` lands here with `sysc-154` depending on it (D9).
Read the design before Task 1; every task argues from it.

**Tracker:** the audio epic and task issues created alongside this plan (see
Task 0). Discovered work goes to bd, not to this file.

## Global Constraints

- Go and the native retained renderer are mandatory. No C++, Rust, Lua, Qt,
  QML, Quickshell, CGO, compositor, lock screen, runtime SVG loader, or plugin
  compatibility layer.
- **Never `go build` or `go test ./...` with `-race`.** This box is zram-only
  swap with 16-way linking; a repo-wide race build hard-locks it. Cap `-p 4`
  and `GOMAXPROCS=4`. Per design handover: recorded as unrunnable here, use
  the per-package substitute.
- **`go test ./internal/shell` really runs `loginctl terminate-session
  self`.** Shadow `loginctl` and `systemctl` with stubs earlier on `PATH`
  (Task 0) or it logs the owner out.
- `sysc-shell` runs from systemd. Redeploy is `go build -o <tmp>
  ./cmd/sysc-shell`, `mv` over `~/.local/bin/sysc-shell`, `systemctl --user
  restart sysc-shell.service`. Never spawn it by hand beside the running one.
  Never `pkill -f` your own binary name.
- Run `bd` only from `/home/nomadx/sysc-shell`, never from a worktree.
- The commit-msg hook rejects AI attribution by naive substring; ordinary
  English trips it — "b-o-t-h" and the name of the design skill used here are
  confirmed hits. Check the message against `/home/nomadx/.git-hooks/commit-msg`
  before committing. No `Co-Authored-By` trailer.
- The beads pre-commit hook auto-stages `.beads/issues.jsonl`, sweeping in
  unrelated pre-existing changes. Expect it.
- Panel `configure`, `render` and `handle` take `Registry.mu`. **No command
  or filesystem I/O may run while `Registry.mu` or the Wayland owner is
  held.** All `wpctl` writes route through `scheduleControl`-style off-owner
  seams (Task 6).
- Keep one Wayland dispatch goroutine. Bounded external work stays off it.
- Add no new `ui` node kinds. Composition uses shipped primitives.
- The control centre's Audio page (`sysc-158`) is unchanged. Its rail entry
  opening this panel is `sysc-154`'s job, not this plan's.
- This checkout is shared. `git status` before you start; never `git checkout
  --`, `git stash` or `git add -A` over work you did not put there.

---

### Task 0: Reconcile, arm the stubs, claim the bಫissues

**Files:** none changed.

- [ ] **Step 1: Confirm the tracker exists and is open**

The bd shape, created beside this plan (first three run in parallel):

```
audio-epic           Standalone audio panel (parent: sysc-201)
 audio-mask           render.AttachedMask concave-corner primitive  (sysc-154 gains a dep on this)
 audio-glyphs         Material subset: mic, mic_off, graphic_eq, headphones
 audio-svc            services.Audio: pw-dump enumeration and SetDefault
  audio-chrome        PanelAudio chrome: identity, wing-tips, header, tabs, IPC  (dep: audio-svc)
   audio-volumes      Volumes tab     (dep: audio-chrome)
   audio-devices      Devices tab     (dep: audio-chrome)
 audio-widget         volume bar widget   (deps: audio-svc, audio-chrome)
```

```bash
bd list --status open | grep -i audio
bd update <issue> --status in_progress   # when starting each task
```

If the issues are missing, stop — the paperwork gate has not closed.

- [ ] **Step 2: Build the PATH stubs**

```bash
mkdir -p ~/.cache/sysc-stubs
printf '#!/bin/sh\necho "stub loginctl $*" >&2\n' > ~/.cache/sysc-stubs/loginctl
printf '#!/bin/sh\necho "stub systemctl $*" >&2\n' > ~/.cache/sysc-stubs/systemctl
chmod +x ~/.cache/sysc-stubs/*
```

- [ ] **Step 3: Verify the tree is green before you touch it**

```bash
PATH=~/.cache/sysc-stubs:$PATH GOMAXPROCS=4 go test -p 4 ./internal/shell ./internal/ui ./internal/ipc ./internal/services ./internal/render ./internal/config
```

Expected: all `ok`. If anything fails, that failure is not yours — record it
in bd and stop.

- [ ] **Step 4: Capture the live `pw-dump` fixture**

```bash
export NIRI_SOCKET=$(ls /run/user/1000/niri.wayland-*.sock | head -1)
pw-dump > /tmp/pw-dump-real.json
jq 'length' /tmp/pw-dump-real.json
```

Expected on this box: ~312 KB, 11 audio nodes (`media.class` `Audio/Sink`,
`Audio/Source`, `Stream/Output/Audio`). Keep this file for Task 3.

No commit.

---

### Task 1: `render.AttachedMask` — the wing-tip primitive

**Files:**
- Modify: `internal/render/mask.go`
- Test: `internal/render/mask_test.go`

**Interfaces:**
- Consumes: `roundedCoverage`, the mask-cache pattern already in the file.
- Produces: `render.AttachedMask(w, h, radius, bulge int, edge string)
  *image.Alpha` — `edge` is one of `"top"`, `"bottom"`, `"left"`, `"right"`,
  matching `Placement.BarEdge`. The two corners on `edge` are concave with
  radius `bulge`; the other two are convex with radius `radius`. `bulge == 0`
  yields the plain rounded rect, which is what the reveal animation starts
  from. **This is the signature `sysc-158` specifies; the control centre
  consumes it unchanged.**

- [ ] **Step 1: Add the coverage helper and the cached mask**

In `internal/render/mask.go`, beside `roundedCoverage`:

```go
// concaveCoverage is the inverse of roundedCoverage for one corner: the pixel
// is inside the shape when it is OUTSIDE the disc whose centre sits on the
// shape's own edge. cx, cy is that centre in mask space.
func concaveCoverage(r, cx, cy, x, y int) uint8 {
	dx := float64(x) + 0.5 - float64(cx)
	dy := float64(y) + 0.5 - float64(cy)
	d := math.Hypot(dx, dy)
	switch {
	case d >= float64(r)+0.5:
		return 255
	case d <= float64(r)-0.5:
		return 0
	default:
		return uint8((d - (float64(r) - 0.5)) * 255)
	}
}
```

Then the mask, cached on a key of its own beside `maskKey`:

```go
type attachedKey struct {
	w, h, radius, bulge int
	edge                string
}

var attachedMasks = map[attachedKey]*image.Alpha{}

// AttachedMask draws a panel fused to a bar edge. The two corners on edge are
// concave so the panel flares outward into the bar; the two away from it are
// convex and stay rounded throughout the reveal. bulge is the concave radius,
// animated from 0 to radius as the panel emerges, so a hidden edge draws flat.
func AttachedMask(w, h, radius, bulge int, edge string) *image.Alpha {
	if w <= 0 || h <= 0 {
		return image.NewAlpha(image.Rect(0, 0, 0, 0))
	}
	if bulge <= 0 {
		return RoundedMask(radius, w, h)
	}
	key := attachedKey{w, h, radius, bulge, edge}
	maskMu.Lock()
	defer maskMu.Unlock()
	if mask := attachedMasks[key]; mask != nil {
		return mask
	}
	mask := image.NewAlpha(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			mask.SetAlpha(x, y, color.Alpha{A: attachedCoverage(w, h, radius, bulge, edge, x, y)})
		}
	}
	attachedMasks[key] = mask
	return mask
}
```

- [ ] **Step 2: Implement `attachedCoverage`**

Only `"top"` is reachable in this tranche, but write `"bottom"` too so a
bottom bar is not a later surprise; `"left"`/`"right"` fall through to the
rounded case:

```go
func attachedCoverage(w, h, radius, bulge int, edge string, x, y int) uint8 {
	switch edge {
	case "bottom":
		if y >= h-bulge && x < bulge {
			return concaveCoverage(bulge, bulge, h-bulge, x, y)
		}
		if y >= h-bulge && x >= w-bulge {
			return concaveCoverage(bulge, w-bulge, h-bulge, x, y)
		}
	default: // "top"
		if y < bulge && x < bulge {
			return concaveCoverage(bulge, bulge, bulge, x, y)
		}
		if y < bulge && x >= w-bulge {
			return concaveCoverage(bulge, w-bulge, bulge, x, y)
		}
	}
	return roundedCoverage(radius, w, h, x, y)
}
```

- [ ] **Step 3: Add the focused check**

In `internal/render/mask_test.go`:

```go
func TestAttachedMaskTopCornersAreConcave(t *testing.T) {
	const w, h, radius, bulge = 60, 40, 12, 12
	m := AttachedMask(w, h, radius, bulge, "top")

	// The outermost top pixel is inside the shape: the panel is widest at the joint.
	if got := m.AlphaAt(0, 0).A; got != 255 {
		t.Errorf("top-left wing pixel = %d, want 255 (wing is filled at the bar)", got)
	}
	// One bulge down, the edge has swept inward, so the same column is empty.
	if got := m.AlphaAt(0, bulge-1).A; got != 0 {
		t.Errorf("pixel below the wing = %d, want 0 (edge swept inward)", got)
	}
	// The far corners stay convex: the very corner pixel is outside.
	if got := m.AlphaAt(0, h-1).A; got != 0 {
		t.Errorf("bottom-left corner = %d, want 0 (convex)", got)
	}
	// bulge 0 is the plain rounded rect, which is where the reveal starts.
	flat := AttachedMask(w, h, radius, 0, "top")
	if flat.AlphaAt(0, 0).A != RoundedMask(radius, w, h).AlphaAt(0, 0).A {
		t.Error("bulge 0 must equal RoundedMask, so a hidden edge draws flat")
	}
}
```

- [ ] **Step 4: Run it**

```bash
GOMAXPROCS=4 go test -p 4 ./internal/render -run TestAttachedMask -v
GOMAXPROCS=4 go test -p 4 ./internal/render
```

Expected: PASS, then `ok`.

- [ ] **Step 5: Commit**

```bash
git add internal/render/mask.go internal/render/mask_test.go
git commit -m "feat(render): add a concave corner mask for bar-attached panels"
```

---

### Task 2: Material glyph subset — `mic`, `mic_off`, `graphic_eq`, `headphones`

**Files:**
- Modify: `internal/render/icons/material/build.py`
- Modify: `internal/render/materialfont.go`
- Modify: `internal/render/icons/material/material-symbols-rounded.ttf` (regenerated)
- Test: `internal/render/materialfont_test.go`

**Interfaces:**
- Produces: four further names valid for `render.ValidMaterialIcon`.
  `volume_up`, `volume_off` and `check` already ship.

- [ ] **Step 1: Add the four names to both lists**

`build.py`'s `ICONS` and `materialfont.go`'s `materialIcons` are kept in step
by hand and held in step by the existing parity test. Add `mic`, `mic_off`,
`graphic_eq`, `headphones` to **both**.

- [ ] **Step 2: Rebuild the font**

```bash
cd internal/render/icons/material
python3 build.py /path/to/MaterialSymbolsRounded.ttf   # the pinned upstream, per SOURCE.md
cd -
```

Expected: the `.ttf` is rewritten and larger. Do not bump the pinned
`SOURCE_SHA256` / commit `84ccef280841abfac506afc4ad4a2782f6d0a1d0`; a
substituted source file fails loudly by design. If the pinned upstream file is
not on this box, record where the subset came from in the commit body.

- [ ] **Step 3: Assert every name shapes**

In `internal/render/materialfont_test.go`:

```go
func TestAudioPanelIconsAreInTheSubset(t *testing.T) {
	for _, name := range []string{"mic", "mic_off", "graphic_eq", "headphones"} {
		if !ValidMaterialIcon(name) {
			t.Errorf("%q missing from the subset: it would shape to nothing and paint an invisible control", name)
		}
	}
}
```

- [ ] **Step 4: Run**

```bash
GOMAXPROCS=4 go test -p 4 ./internal/render
```

Expected: `ok`, including the existing test that holds `build.py` and
`materialIcons` in step.

- [ ] **Step 5: Commit**

```bash
git add internal/render/materialfont.go internal/render/materialfont_test.go internal/render/icons/material/
git commit -m "feat(render): carry the audio panel glyphs in the icon subset"
```

---

### Task 3: `services.Audio` — `pw-dump` enumeration, cubic volume, `SetDefault`

**Files:**
- Create: `internal/services/audioenum.go`
- Create: `internal/services/testdata/pw-dump.json` (trimmed fromTask 0's live capture)
- Create: `internal/services/testdata/pw-dump-unplugged.json` (same, one node removed)
- Test: `internal/services/audioenum_test.go`

**Interfaces:**
- Consumes: `runCmd`, `resolveBin`, `leaseSet` shapes already in
  `internal/services/audio.go` / `leases.go`; `/usr/bin/pw-dump`.
- Produces:

```go
// Node is one PipeWire audio node: a sink, a source, or a playback stream.
type AudioNode struct {
	ID          int
	Name        string // node.name, the stable fixturing key
	Description string // node.description, the human name
	Icon        string // application.icon-name, empty for devices
	Level       int    // 0..100, cbrt of PipeWire's linear channelVolumes
	Muted       bool
	Default     bool   // the default sink or source right now
}

// AudioSnapshot is one pw-dump pass, split for the panel's two tabs.
type AudioSnapshot struct {
	Sinks   []AudioNode
	Sources []AudioNode
	Streams []AudioNode
	At      time.Time
}
```

- `(*Audio).MixerAcquire() (*MixerLease, error)` — panel-scoped lease; while
  any mixer lease is live, a peer goroutine polls `pw-dump` at 1 Hz
  independent of the existing `@DEFAULT_AUDIO_SINK@` poll.
- `(*Audio).Mixer() AudioSnapshot`.
- `(*Audio).SetDefault(id int) error` → `wpctl set-default <id>`.
- `(*Audio).SetNodeVolume(id, level int) error` → `wpctl set-volume <id>`.
- `(*Audio).SetNodeMute(id int, on bool) error` → `wpctl set-mute <id> 0|1`.

- [ ] **Step 1: Write the failing test against the fixture**

Prepare the fixture from Task 0's live capture: keep every object with
`media.class` of `Audio/Sink`, `Audio/Source`, `Stream/Output/Audio`, plus
the `default` metadata object, so the test data is real and parseable:

```bash
jq '[.[] | select(.info.props["media.class"] // "" | test("Audio|Stream")) // .[] | select(.info.props["metadata.name"] == "default")]' \
  /tmp/pw-dump-real.json > internal/services/testdata/pw-dump.json
jq '[.[] | select(.id != 45)]' internal/services/testdata/pw-dump.json > internal/services/testdata/pw-dump-unplugged.json
```

`internal/services/audioenum_test.go`:

```go
func TestPwdumpParsesSinksSourcesAndStreams(t *testing.T) {
	data, err := os.ReadFile("testdata/pw-dump.json")
	if err != nil {
		t.Skip("no live fixture captured yet")
	}
	snap, err := parsePwdump(data)
	if err != nil {
		t.Fatalf("parsePwdump: %v", err)
	}
	if len(snap.Sinks) == 0 {
		t.Fatal("no sinks parsed from a live dump that has them")
	}
	for _, n := range snap.Sinks {
		if n.Description == "" {
			t.Errorf("sink %d has no description: the panel names devices by it", n.ID)
		}
	}
}

func TestCubicVolumeMatchesWpctlDisplay(t *testing.T) {
	// Live fact recorded 2026-09-07: node 45 stores channelVolumes 0.1355 and
	// wpctl shows 0.51. Reading raw would paint 14% where the shell paints 51%.
	if got := cubicPercent(0.1355); got != 51 {
		t.Errorf("cubicPercent(0.1355) = %d, want 51", got)
	}
	if got := cubicPercent(0); got != 0 {
		t.Errorf("cubicPercent(0) = %d, want 0", got)
	}
	if got := cubicPercent(1); got != 100 {
		t.Errorf("cubicPercent(1) = %d, want 100", got)
	}
}

func TestHotUnplugBetweenTwoDumps(t *testing.T) {
	a := readFixture(t, "testdata/pw-dump.json")
	b := readFixture(t, "testdata/pw-dump-unplugged.json")
	sa, _ := parsePwdump(a)
	sb, _ := parsePwdump(b)
	got := countNodes(sa) - countNodes(sb)
	if got != 1 {
		t.Errorf("unplugged fixture drops %d streams/nodes, want 1", got)
	}
}
```

- [ ] **Step 2: Run, watch it fail**

```bash
GOMAXPROCS=4 go test -p 4 ./internal/services
```

Expected: build failure — `parsePwdump` does not exist.

- [ ] **Step 3: Implement `audioenum.go`**

Targeted structs; `pw-dump` is one JSON array. Defaults come from the
`metadata.name == "default"` object, whose `info.metadata` entries name the
default sink/source by `node.name`:

```go
package services

import (
	"encoding/json"
	"fmt"
	"math"
	"time"
)

// AudioNode / AudioSnapshot per the interface above.

func cubicPercent(linear float64) int {
	// PipeWire stores linear volume; wpctl displays the cube root. Reading
	// raw paints 14% where the rest of the shell paints 51%.
	v := int(math.Round(math.Pow(linear, 1.0/3.0) * 100))
	return max(0, min(100, v))
}

type pwDumpEntry struct {
	ID   int `json:"id"`
	Info struct {
		Props map[string]any `json:"props"`
		Meta  []struct {
			Key   string `json:"key"`
			Value struct {
				Name string `json:"name"`
			} `json:"value"`
		} `json:"metadata"`
		Params map[string][]struct {
			ChannelVolumes []float64 `json:"channelVolumes"`
			Mute           bool      `json:"mute"`
		} `json:"params"`
	} `json:"info"`
}

func parsePwdump(data []byte) (AudioSnapshot, error) {
	var entries []pwDumpEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		return AudioSnapshot{}, fmt.Errorf("services: pw-dump: %w", err)
	}
	var snap AudioSnapshot
	defSink, defSource := "", ""
	for _, e := range entries {
		if e.Info.Props["metadata.name"] == "default" {
			for _, m := range e.Info.Meta {
				switch m.Key {
				case "default.audio.sink":
					defSink = m.Value.Name
				case "default.audio.source":
					defSource = m.Value.Name
				}
			}
			continue
		}
		class, _ := e.Info.Props["media.class"].(string)
		if class == "" {
			continue
		}
		n := AudioNode{
			ID:          e.ID,
			Name:        stringProp(e.Info.Props, "node.name"),
			Description: stringProp(e.Info.Props, "node.description"),
			Icon:        stringProp(e.Info.Props, "application.icon-name"),
		}
		if n.Description == "" {
			n.Description = stringProp(e.Info.Props, "application.name")
		}
		if props := e.Info.Params["Props"]; len(props) > 0 {
			if len(props[0].ChannelVolumes) > 0 {
				n.Level = cubicPercent(props[0].ChannelVolumes[0])
			}
			n.Muted = props[0].Mute
		}
		switch class {
		case "Audio/Sink":
			n.Default = n.Name == defSink
			snap.Sinks = append(snap.Sinks, n)
		case "Audio/Source":
			if strings.HasSuffix(n.Name, ".monitor") {
				// Monitor sources mirror a sink's output; they are not
				// input devices and never appear on the Devices tab.
				continue
			}
			n.Default = n.Name == defSource
			snap.Sources = append(snap.Sources, n)
		case "Stream/Output/Audio":
			snap.Streams = append(snap.Streams, n)
		}
	}
	snap.At = time.Now()
	return snap, nil
}
```

Ordering: sort `Sinks`, `Sources`, `Streams` by ID so the tab lists are
stable across polls (a shuffled list refocuses controls mid-click).

- [ ] **Step 4: Implement the panel-scoped mixer lease and the writes**

Peer of the existing poll, same lease-stop discipline (`stopIfUnusedLocked`
shape), 1 Hz ticker, own `MixerLease` type with `Release()`:

```go
// MixerLease holds the pw-dump poller open. Released when PanelAudio closes;
// the bar never takes one (D7: the cheap default-sink poll stays untouched).
type MixerLease struct{ audio *Audio }
```

`wpctl` writes reuse `runCmd` and set `a.ok` the way `wpctl(...)` already
does. A failed write leaves the mixer lease alive; the next 1 Hz sample
reconciles the displayed value.

- [ ] **Step 5: Run and commit**

```bash
GOMAXPROCS=4 go test -p 4 ./internal/services
git add internal/services/audioenum.go internal/services/audioenum_test.go internal/services/testdata/
git commit -m "feat(services): enumerate audio nodes from pw-dump with cubic volume and defaults"
```

---

### Task 4: `PanelAudio` chrome — identity, wing-tips, header, tabs, IPC

**Files:**
- Modify: `internal/shell/panel.go` (PanelID block + String)
- Modify: `internal/shell/panelhost.go` (`parsePanelName`, `panelTree`,
  `spawnPanelLocked`, `panelSpec`, `acquirePanelLeases`, `panelTargetSize`,
  `Trigger`, render wing path)
- Modify: `internal/render/style.go`, `internal/render/paint.go` (AttachBulge branch)
- Modify: `internal/ipc/server.go` (`knownPanels`)
- Create: `internal/shell/popout_audio.go`
- Test: `internal/shell/audio_test.go`, `internal/ipc/audio_panel_test.go`

**Interfaces:**
- Produces: `PanelAudio PanelID`, public name `audio`;
  `audioTree(r *Registry, h *PanelHost) *ui.Node` with an empty body;
  `Trigger.AnchorX`; `Style.AttachBulge`.

- [ ] **Step 1: Panel identity**

In `panel.go`, **append** `PanelAudio` to the const block (append only — the
values are ordinal and reordering renumbers every panel) and add
`case PanelAudio: return "audio"` to `String`. Add `case "audio": return
PanelAudio, nil` to `parsePanelName`. Add `"audio": ""` to `knownPanels` in
`internal/ipc/server.go`.

- [ ] **Step 2: Placement — trigger-centred, fused, widened by the bulge**

Add `AnchorX int` to `Trigger` (center-x of the triggering widget, logical;
zero means unset). Add it to `Placement` and to `alignX`: when `AnchorX > 0`,
desired x = `AnchorX - Panel.W/2`, and `clampAxis` keeps it on the output.
In `spawnPanelLocked`, for `PanelAudio`: `gap = 0` beside the `PanelPlugin`
line, `place.Panel = ui.Rect{W: 560, H: 496}` in `panelTargetSize`, no
`CenterY`.

Widening (the design's `logicalInset`): store `h.bulge = 12` for
`PanelAudio`. In `panelSpec`, when `h.bulge > 0`: layer-surface `Width =
Panel.W + 2*bulge`, `MarginLeft = m.Left - bulge`. Content stays 560 wide;
the drawn surface is 584. In `configure`/`render`, the body box and root
bounds are inset by `bulge` on each side (`body.X = bulge`, `W -= 2*bulge`).

- [ ] **Step 3: Paint the wing instead of squaring the edge**

Add `AttachBulge int` to `render.Style`. In `Paint`, when `AttachBulge > 0`:

```go
mask := render.AttachedMask(box.W, box.H, radius, scale.Physical(style.AttachBulge), style.AttachEdge)
// fill box through mask with style.rootFill(); NO squareAttachedEdge call,
// and clearOutsideRoundedRect respects the same mask.
```

Add `Canvas.FillMasked(box, mask, col)` (mirror `FillRounded`) and branch
`clearOutsideRoundedRect` to use the attached mask when the style carries a
bulge. **The wing panel paints no rim**: fused at `SurfaceContainerLow`
against the bar's own `SurfaceContainerLow`, a rim stroke is the seam D1
forbids. Skip `style.Rim = ...` for `PanelAudio` in `panelhost.go:739`
(`render()` `style` assembly).

Drive the bulge from the reveal: `h.anim.Value(panelSurfaceID(id),
animVisible)` — the same value `panelSlidePx` already reads at
`internal/shell/animation.go:258` — scaled 0→1 to 0→12 and passed as
`AttachBulge`. Reduced motion settles at one, full bulge, by the existing
animator contract.

- [ ] **Step 4: The tree — header card + tab strip, empty body**

`popout_audio.go` composes, per the design's pixel contract and mock
(`volumes.png`):

- Header card (116): icon well 40×40 r12 `graphic_eq`, Title "Audio"
  (`RoleHeadline` 20/600), close `KindButton` `KindIcon "close"`, and the
  tab strip: `KindSegmented` 40 tall with two segments 32 — "Volumes" |
  "Devices" — following the `wallpaperSegment` precedent
  (`popout_wallpaper.go:403`), action strings `"audio-tab:volumes"` /
  `"audio-tab:devices"`.
- Body: `&ui.Node{Kind: ui.KindScroll}` with an empty column until Tasks 5–6
  fill it.
- Track the active tab on the host: `h.audioTab string` (`"volumes"` default);
  the body pane swaps in Tasks 5–6.
- Escape and focus: the flyweight roving ring comes free from
  `ui.Focusables`; seed focus on the Volumes tab, not on Close.
- `acquirePanelLeases`: `case PanelAudio:` acquire the **mixer** lease from
  Task 3 (converts to `*services.Lease`-compatible registration — if the
  mixer lease is a distinct type, store it on the host as `mixerLease
  *services.MixerLease` and release it in the panel-close path beside
  `releaseAll(h.leases)`).
- State vocabulary (D12): unavailable (no `wpctl`/`pw-dump`) renders the
  controls disabled at 38 % with a short reason; stale (leased, no sample)
  renders dashes and empty tracks, never `0%`; errors land in the existing
  `h.errLabel` at the top of the body of the active tab.

- [ ] **Step 5: Add the focused check**

`internal/shell/audio_test.go`:

```go
func TestAudioPanelNameAndTriggerCentredPlacement(t *testing.T) {
	id, err := parsePanelName("audio")
	if err != nil || id != PanelAudio {
		t.Fatalf("parsePanelName(audio) = %v, %v", id, err)
	}
	if got := PanelAudio.String(); got != "audio" {
		t.Errorf("String() = %q, want audio", got)
	}
	// Fused: Gap 0 anchors at the bar zone; trigger-centred: the panel's
	// middle sits under the widget's middle, clamped to the output.
	p := Placement{
		BarEdge: "top", Output: ui.Rect{W: 3440, H: 1440},
		BarZone: 34, Gap: 0, Padding: 8,
		Panel: ui.Rect{W: 584, H: 496}, AnchorX: 400,
	}
	m := p.Margins()
	if m.Top != 34 {
		t.Errorf("Top = %d, want 34 (flush with the bar zone)", m.Top)
	}
	if want := 400 - 584/2; m.Left != want {
		t.Errorf("Left = %d, want %d (centred on the trigger)", m.Left, want)
	}
	// Clamp: a trigger near the right edge cannot push the panel off-screen.
	p.AnchorX = 3440 - 40
	if got := p.Margins().Left; got > 3440-584-8 {
		t.Errorf("Left = %d, want clamped inside the output", got)
	}
}
```

`internal/ipc/audio_panel_test.go`:

```go
func TestAudioPanelIsReachableOverIPC(t *testing.T) {
	if _, ok := knownPanels["audio"]; !ok {
		t.Fatal("the audio panel must be reachable over IPC like the launcher")
	}
}
```

- [ ] **Step 6: Run and commit**

```bash
PATH=~/.cache/sysc-stubs:$PATH GOMAXPROCS=4 go test -p 4 ./internal/shell ./internal/ipc ./internal/render
git add internal/shell internal/ipc internal/render
git commit -m "feat(shell): add the fused audio panel chrome with wing-tips and tabs"
```

---

### Task 5: Volumes tab

**Files:**
- Modify: `internal/shell/popout_audio.go`
- Test: `internal/shell/audio_test.go`

**Interfaces:**
- Consumes: `r.audio.Mixer()`, `h.audioTab`, `scheduleControl` (Task 6 comes
  next; for this task bind the row actions to the seam's signature and stub
  the seam if it is not yet in).
- Produces: `audioVolumesTree(r, h) *ui.Node`,
  `audioVolumeRow(n services.AudioNode, role string, icon *ui.Image)
  *ui.Node` — one archetype, three uses (D4).

- [ ] **Step 1: Compose the tab**

Body (`KindScroll`), top to bottom, per the pixel contract:

- **Output row** (68): `[identity 32 circle KindIcon "headphones"] [role
  "Output" over device name over KindSlider] [value 44 tabular] [mute 32
  circle "volume_up"/"volume_off"]`. Slider `Value: float64(n.Level), Min: 0,
  Max: 100, Step: 5`, following the `popout_plugins.go:217` shape.
- **Input row** (68): identity `"mic"`/`"mic_off"`, role "Input". When the
  snapshot has no sources, the row paints disabled at 38 % with the reason —
  it never vanishes (D12).
- **"Applications n"** label (28 tall, `RoleLabel`): a centred empty state
  ("No applications playing audio") when `len(Streams) == 0 — never an
  absent section (D12), and one row per stream. The stream identity well
  uses the resolved application `KindImage` when the icon worker has it
  (D8: `r.trayIcons` worker, `icons.Square(name, 32)`), falling back to the
  lower-cased `application.name`, and to the role glyph `graphic_eq` when
  neither resolves — never a blank square. Key the stream's icon request on
  `application.icon-name` first.
- The device name on Output/Input rows (D5): a `KindButton`-behaving text
  with `Action: "audio-tab:devices"` — it is the link to the Devices tab,
  the one thing on the row you want to change and cannot.
- Value column is fixed 44 tabular so `100 %` does not shove the mute
  button.
- Mute toggle: `Action: "audio-mute:<id>"`; reproduce mute state through
  `node.State |= ui.StateSelected` the way the session segmented does.
- Elision: device and stream names front-elide to the row width — check the
  shipped `internal/render/truncate.go` for the existing ellipsis helper and
  reuse it rather than writing a second one (the Devices tab shows the same
  name in full, one tab away).

- [ ] **Step 2: Bind the slider and mute through the off-owner seam**

Row actions: slider `Action: "audio-vol:<id>"`, mute `Action:
"audio-mute:<id>"`. Parse in the panel handler beside the existing
`ui.KindSlider` case at `panelhost.go:1597`, following the
`"profile:"`-prefix shape. Route through `scheduleControl` (Task 6). In
flight: `n.Value` from the slider stays the requested value on screen; the
next 1 Hz sample reconciles (D12) — do not echo the stale snapshot back into
the control before the command lands.

- [ ] **Step 3: Add the focused check — the pixel contract on one row**

```go
func TestVolumeRowCarriesTheFullContract(t *testing.T) {
	n := services.AudioNode{ID: 42, Name: "dev", Description: "AD106M High Definition Audio Controller", Level: 51}
	row := audioVolumeRow(n, "Output", nil)
	// One archetype: identity, label-over-slider, value, mute.
	if len(row.Children) != 4 {
		t.Fatalf("row has %d regions, want 4 (per D4)", len(row.Children))
	}
	var slider *ui.Node
	walk(row, func(x *ui.Node) {
		if x.Kind == ui.KindSlider {
			slider = x
		}
	})
	if slider == nil {
		t.Fatal("row carries no slider")
	}
	if slider.Value != 51 {
		t.Errorf("slider.Value = %v, want 51 (cubic from the snapshot)", slider.Value)
	}
	if row.Height != 0 && (row.Height < 68 || row.Height > 76) {
		t.Errorf("row height = %d, want the 68 px contract (density may scale)", row.Height)
	}
}

func TestEmptyApplicationsSectionIsNotAbsent(t *testing.T) {
	r := &Registry{}
	h := &PanelHost{id: PanelAudio, audioTab: "volumes"}
	got := renderText(audioVolumesTree(r, h))
	if !strings.Contains(got, "Applications") {
		t.Error("Volumes renders no Applications label for an empty mixer: it looks broken (D12)")
	}
	if strings.Contains(got, "0%") {
		t.Error("Volumes painted 0% before a sample landed: stale must read as a dash")
	}
}
```

Write `walk`/`renderText` helpers in the test file if the package has no
equivalent (the control-centre plan Task 9 uses the same shape).

- [ ] **Step 4: Run and commit**

```bash
PATH=~/.cache/sysc-stubs:$PATH GOMAXPROCS=4 go test -p 4 ./internal/shell
git add internal/shell
git commit -m "feat(shell): build the audio panel volumes tab"
```

---

### Task 6: Devices tab + the off-owner `scheduleControl` seam

**Files:**
- Modify: `internal/shell/registry.go` (the seam)
- Modify: `internal/shell/popout_audio.go` (the tab, the write routing)
- Test: `internal/shell/audio_test.go`

**Interfaces:**
- Consumes: `r.audio.SetDefault/SetNodeVolume/SetNodeMute` from Task 3.
- Produces: `(*Registry).scheduleControl(h *PanelHost, run func() error)`;
  `audioDevicesTree(r, h) *ui.Node`.

- [ ] **Step 1: Add the seam**

A panel handler holds `Registry.mu`, so `wpctl` cannot run inline — that is
the `D11` rule. Shape follows the control-centre plan's Task 7:

```go
// scheduleControl runs one bounded command off the Wayland owner and without
// r.mu, then reapplies under the lock. Caller holds r.mu. The host check is
// the staleness guard: the panel may have closed while the command ran.
func (r *Registry) scheduleControl(h *PanelHost, run func() error) {
	go func() {
		err := run()
		r.mu.Lock()
		defer r.mu.Unlock()
		if r.panelHosts[h.id] != h {
			return
		}
		if err != nil {
			h.errLabel = err.Error()
		} else {
			h.errLabel = ""
		}
		r.rebuildPanel(h)
		r.publishSurface(h.output, panelSurfaceID(h.id))
	}()
}
```

(Check the real publish seam — use whichever of `publishSurface`/
`publish(changed)` the registry actually exposes for a single-panel repaint.)

Route all three write kinds through it: `audio-vol:<id>` →
`SetNodeVolume(id, v)`, `audio-mute:<id>` → `SetNodeMute(id, !muted)`,
`audio-dev:<id>` → `SetDefault(id)`.

- [ ] **Step 2: Compose the tab**

Two label-stack lists in the body scroll: **"Output device"**, one row per
sink; **"Input device"**, one row per source. Per approved D6 — rows are
**selected wells**, not radios:

- Device row (44 tall, r8): full `Description`, **never elided** (the tab
  exists to tell two similar names apart — D2 sized the whole panel to
  this). The current device is a `SecondaryContainer` well (`Fill` per the
  settings/wallpaper selected-card precedent) with a trailing `KindIcon
  "check"` 20; the rest are plain rows. `check` already ships.
- `Action: "audio-dev:<id>"`, `Name: description`, kind and focusability
  per `settingsEntryRow`.
- Empty list (no sources): the section label stays; the well reads the
  centred "not available" reason (D12 Empty — never an absent section).
- A failed `SetDefault` paints `h.errLabel` at the top of this tab and the
  next 1 Hz sample restores the last known current well (D12 Failure).

- [ ] **Step 3: Add the focused checks**

```go
func TestScheduleControlDropsAStaleResult(t *testing.T) {
	// A nil panelHosts map makes the staleness guard fire before rebuildPanel
	// is reached: the result must be dropped rather than applied to a panel
	// that is gone.
	r := &Registry{}
	h := &PanelHost{id: PanelAudio}
	done := make(chan struct{})
	r.mu.Lock()
	r.scheduleControl(h, func() error { close(done); return errors.New("boom") })
	r.mu.Unlock()
	<-done
	r.mu.Lock()
	defer r.mu.Unlock()
	if h.errLabel != "" {
		t.Errorf("errLabel = %q, want empty: the host was stale", h.errLabel)
	}
}

func TestDevicesRowsAreSelectedWellsNotRadios(t *testing.T) {
	r := &Registry{}
	h := &PanelHost{id: PanelAudio, audioTab: "devices"}
	tree := audioDevicesTree(r, h)
	_ = tree
	// Assert against the snapshot's Default flag: current device is marked by
	// the check glyph + selected well, siblings are plain rows. With a zero
	// registry there is no current device, so nothing reads as selected.
	got := renderText(tree)
	if !strings.Contains(got, "Output device") || !strings.Contains(got, "Input device") {
		t.Error("Devices must carry both section labels even when empty")
	}
}
```

- [ ] **Step 4: Run and commit**

```bash
PATH=~/.cache/sysc-stubs:$PATH GOMAXPROCS=4 go test -p 4 ./internal/shell
git add internal/shell
git commit -m "feat(shell): build the audio panel devices tab with off-owner writes"
```

---

### Task 7: `volume` bar widget

**Files:**
- Modify: `internal/config/config.go` (`knownItems`)
- Modify: `internal/shell/widget.go` (`buildWidgets` case; `barView` gains
  `Audio services.AudioState`)
- Create: `internal/shell/volumewidget.go`
- Modify: `internal/shell/bar.go` (axis routing to the new seam)
- Modify: `internal/shell/registry.go` (seams, audio lease, view assembly)
- Test: `internal/shell/volumewidget_test.go`

**Interfaces:**
- Consumes: the existing `r.audio` default-sink lease and `stepAudio`
  (`registry.go:259`).
- Produces: config item id `volume`; `panelAudioAction = "panel:audio"`;
  bar axis seam.

- [ ] **Step 1: The widget**

`volumewidget.go`, following `buildWallpaperWidget`/`buildNotifyWidget`:

```go
func buildVolumeWidget() textWidget {
	node := &ui.Node{Kind: ui.KindRow, Action: panelAudioAction,
		Children: []*ui.Node{{Kind: ui.KindIcon, Icon: "volume_up"}}}
	return textWidget{
		node:    node,
		tooltip: "Volume",
		refresh: func(v barView) bool { return refreshVolumeWidget(node.Children[0], v) },
	}
}

// refreshVolumeWidget swaps the ligature as level and mute change. A muted
// sink reads volume_off; the bar path reports only the default sink (D13).
func refreshVolumeWidget(n *ui.Node, v barView) bool {
	icon := "volume_up"
	if v.Audio.Muted || v.Audio.Level == 0 {
		icon = "volume_off"
	}
	if n.Icon == icon {
		return false
	}
	n.Icon = icon
	return true
}
```

The widget satisfies `Bar.applyLocked` through `refresh` (D13 — never
neither seam; that is the `sysc-188` blank-shell panic). Add
`case "volume": out = append(out, buildVolumeWidget())` to the
`buildWidgets` switch and `"volume": {}` to `config.knownItems`. Deliberately
**not** in `Default()` — same treatment as `wallpaper`: no existing bar
changes.

- [ ] **Step 2: Feed `barView.Audio` and lease the service per bar**

Add `Audio services.AudioState` to `barView` in `widget.go`. In the
registry's view assembly, populate it from `r.audio.State()` when the bar
carries a `volume` widget; acquire one default-sink lease per bar that does,
beside the weather/clock lease wiring (find the bar lease assembly the way
`clockBoundaries` feeds one). A bar with no `volume` widget changes no
polls.

- [ ] **Step 3: Bar seams — positioned toggle, right-click mute, axis step**

Widget click position: `bindBarPanelActionsLocked` closure for the action.

```go
case action == panelAudioAction && (button == 0 || button == buttonLeft):
	out, trig := r.triggerFor(global)
	trig.AnchorX = bar.actionCenterX(panelAudioAction)
	return r.TogglePanel(PanelAudio, out, trig) == nil
case action == panelAudioAction && button == buttonRight:
	_ = r.stepAudio("mute")
	return true
```

`Bar.actionCenterX(action)` finds the rect through the existing
`nodeActionBounds` (`bar.go:519`), releases `b.mu`, and returns
`r.X + r.W/2`. Zero on a miss means unset; `alignX` falls back.

Scroll: `EventPointerAxis` on the bar currently forwards only to tray items.
Add a consulted-first seam, `bar.setAxisHandler(func(action string, delta int)
bool)`, returning false for tray routing to keep. Registry binds it: when the
hit action is `panelAudioAction`, `_ = r.stepAudio("up")` on positive delta,
`"down"` on negative. Step 5 per D13.

- [ ] **Step 4: Add the focused checks**

```go
func TestVolumeWidgetSatisfiesTheApplyContract(t *testing.T) {
	ws := buildWidgets([]config.Item{{ID: "volume"}}, 6)
	if len(ws) != 1 {
		t.Fatalf("buildWidgets = %d widgets, want 1", len(ws))
	}
	w := ws[0]
	if w.refresh == nil && (w.node == nil || w.format == nil) {
		t.Error("volume widget satisfies neither seam of the apply contract")
	}
}

func TestVolumeGlyphFollowsMuteAndLevel(t *testing.T) {
	n := &ui.Node{Kind: ui.KindIcon, Icon: "volume_up"}
	if !refreshVolumeWidget(n, barView{Audio: services.AudioState{Level: 50, Muted: true}}) {
		t.Error("mute must change the glyph")
	}
	if n.Icon != "volume_off" {
		t.Errorf("muted glyph = %q, want volume_off", n.Icon)
	}
	if refreshVolumeWidget(n, barView{Audio: services.AudioState{Level: 0}}) && n.Icon != "volume_off" {
		// unchanged stays volume_off
	}
}

func TestVolumeIsKnownButNotDefault(t *testing.T) {
	if _, ok := knownItems["volume"]; !ok { // config package — move to config_test as needed
		t.Error("config must accept the volume item")
	}
	for _, sec := range [][]config.Item{Default().Bar.Left, Default().Bar.Right} {
		for _, it := range sec {
			if it.ID == "volume" {
				t.Error("volume must not join the default bar: like wallpaper, it is opt-in")
			}
		}
	}
}
```

- [ ] **Step 5: Run and commit**

```bash
PATH=~/.cache/sysc-stubs:$PATH GOMAXPROCS=4 go test -p 4 ./internal/shell ./internal/config
git add internal/config internal/shell
git commit -m "feat(shell): add the volume bar widget"
```

---

### Task 8: Density check + gates

**Files:** none changed unless a gate fails.

- [ ] **Step 1: Close the design's known gap**

The pixel contract is standard density at fontScale 1 **only**. Render the
panel tree at `DensityCompact` and at fontScale 125 % (drive the metrics and
`Scale120` the way layout tests do) and check, and record the answers on the
tracker:

- The 44 px value column holds `100 %` at 125 %.
- The 68 px row holds role + name + slider at compact.
- The 560 px width still frames a long Devices name at compact (the mock's
  61-char `AD106M … (HDMI)` case).

If any check fails: fontScale scales `PanelPadding`/`CardPadding` with the
density setting per `theme.Metrics` — widen the panel size in
`panelTargetSize` only if name elision on **Devices** returns; elision on
Volumes is already the contract. Record what was checked into the milestone
handoff.

- [ ] **Step 2: Automated gate**

```bash
PATH=~/.cache/sysc-stubs:$PATH GOMAXPROCS=4 go test -p 4 \
  ./internal/shell ./internal/ui ./internal/ipc ./internal/services ./internal/render ./internal/config
```

Expected: every package `ok`. Never add `-race` and never widen to `./...`.

- [ ] **Step 3: Redeploy**

```bash
go build -o /tmp/sysc-shell.new ./cmd/sysc-shell
mv /tmp/sysc-shell.new ~/.local/bin/sysc-shell
systemctl --user restart sysc-shell.service
systemctl --user status sysc-shell.service --no-pager
```

Expected: `active (running)` **and** a painted bar. Active with nothing
painted is the recovered-panic failure: check the journal for `dispatch:
panic handling`.

- [ ] **Step 4: Live Niri matrix** — derive `NIRI_SOCKET` as in
  AGENTS.md; record each result against the tracker:

1. `volume` in a bar's right section renders the `volume_up` glyph.
2. Scroll on the widget steps the live sink volume by 5 (hear it, and watch
   the glyph flip to `volume_off` at 0); right-click mutes (glyph flips);
   left-click opens the panel fused to the bar, centred under the widget,
   `niri msg -j layers` shows the `sysc-shell-panel` surface.
3. Wing joint: panel top corners are concave against the bar; no seam — the
   panel and bar read one surface against light and dark wallpapers. Bulge
   animates 0→12 through the reveal; reduced motion settles at 12 at once.
4. Volumes tab: Output and Input rows show cubic volume (matches `wpctl
   get-volume`, e.g. 0.51 not 0.14); an application stream appears and
   vanishes as one starts and stops; toggle the panel between samples —
   values reconcile.
5. Slider drag moves a live sink; a streamed application's slider moves its
   own stream, verified by `pw-dump` between drags.
6. Devices tab: every device name shows in full (no elision), the current
   device is the `SecondaryContainer` well with `check`. Switch output —
   sound routes to the new device, the well follows on the next sample.
7. Empty state: kill all streams — "No applications playing audio", the
   label stays. Failure state: `busybox kill -STOP $(pgrep -x wpctl
   2>/dev/null)` is not realistic — instead simulate failure by stubbing
   `wpctl` unavailable (rename is too destructive; pass a bad path via a
   test build) **or** visually verify the errLabel path via the unit gate
   and record "failure live-verified by inspection of unit coverage".
8. Escape closes; Tab walks tab-strip then rows; focus ring follows the
   node silhouette.
9. Compact density + fontScale 125 %: Step 1's checks on the live panel.
10. Follow the mock (`docs/plans/assets/2026-09-07-audio-panel/*.png`) —
    the live panel reads as the approved mock, not a rough sketch.

- [ ] **Step 5: Close out**

```bash
bd close <widget> --reason "volume bar widget shipped"
bd close <devices> --reason "Devices tab shipped"
# … each task issue as its live evidence lands …
bd close <epic> --reason "PanelAudio + volume widget shipped: <commit range>. Live matrix passed."
git add .beads/issues.jsonl
git commit -m "chore(beads): close the audio panel work"
```

(Screen each close reason against the commit-msg hook banned substrings
before running.)

---

## Stop condition

This plan is complete when the automated gate passes, the density checks are
recorded, the live matrix is recorded against the tracker, every task issue
is closed with evidence, the epic is closed, and `.beads/issues.jsonl` is
committed.

**Explicitly out of scope — do not start these from this plan:**

- MPRIS transport (`sysc-156`), NetworkManager (`sysc-157`), BlueZ
  (`sysc-155`). Each needs its own approved design.
- The control centre (`sysc-154`) itself. `AttachedMask` landing here gives
  it the dependency it needs; its rail entry opening `PanelAudio` is its own
  task.
- Any change to the control centre's designed Audio page — it stays the thin
  slider `sysc-158` specifies.
- Per-device volume on the Devices tab, channel balance, loopback, node-graph
  editing.
- Replacing the bar's cheap default-sink poll or the OSD's `stepAudio` use.
- Changes to Settings, Launcher, Monitor, Session, Notifications or Wallpaper
  trees.