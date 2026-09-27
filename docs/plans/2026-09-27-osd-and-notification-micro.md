# OSD Breadth and Notification Micro-interactions Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** The OSD shows real icons and text for seven kinds — volume, brightness, media, Caps Lock, Num Lock, keyboard layout, do not disturb — with a value-reactive handle; toast cards expand on a vertical drag, the stack slides closed after a dismiss, group badges pop, and empty notification lists read as deliberate.

**Architecture:** The OSD moves from hand-placed fills onto the shared tree renderer (`render.Paint` over a pure per-kind `osdTree`), fed by relays in `Registry`: the existing audio and brightness relays, the media relay, a do-not-disturb change hook, niri keyboard-layout events, and a new sysfs lock-key service. Toast gestures stay in `notifyResolver.release` as a pure classifier; the toast host gains an animator for the collapse slide.

**Tech Stack:** Go 1.26; existing `internal/shell` animator, `internal/render`, `internal/ui`, `internal/platform/niri`, `internal/services`.

**Spec:** `docs/plans/2026-09-27-osd-and-notification-micro-design.md` (`sysc-592`), including its **Amendment 2026-09-27** on D1 trigger sources. Read both before Task 1.

## Global Constraints

- Build and test only from your own worktree off `origin/main`; never from `/home/nomadx/sysc-shell` itself.
- Cap every repo-wide Go command: `GOMAXPROCS=4 go test -p 2 ...`.
- Commit messages must not contain these substrings (a global hook rejects them): `bot` (so not "both", "bottom"), `agent`, `cursor`, `llm`, `codex`, `claude`. No attribution trailers.
- `OSDManager.Show` takes `Registry.mu` itself. Never call it while holding `r.mu`: call it from a relay goroutine, after unlock, or in a new goroutine.
- `Registry.relayMedia`'s startup ordering is `sysc-566`'s (in progress, another session). This slice edits only the loop body inside `relayMedia`; do not touch how or when it starts.
- No protocol changes, no new node kinds, no new animator channels (spec D2, D5).
- Reduced motion: every animation here settles instantly when `cfg.Accessibility.ReducedMotion` is set; the animator already does this when constructed with `reduced == true`.
- Deploys follow the deploy rule (ancestor checks, rollback copy, report `vcs.revision`/`vcs.modified`, redeploy clean `origin/main` after testing a branch).

## Review Focus

1. **The shell starts while Caps Lock is already on.** Expect no OSD at startup; only a change shows one. Test: Task 4 `TestLockKeysBaselineIsSilent`.
2. **A laptop with no LED files (some VMs, some keyboards).** Expect the service to stay idle — no polling goroutine, no errors in the log. Test: Task 4 `TestLockKeysWithoutLEDsNeverPolls`.
3. **niri's first snapshot after the shell connects.** It carries the current layout; expect no layout OSD for it. Test: Task 5 `TestFirstLayoutSnapshotIsSilent`.
4. **A diagonal swipe that is mostly vertical but crosses 35% horizontally.** Vertical dominance wins: expand, not dismiss; a mostly horizontal drag is byte-identical to today. Test: Task 7 `TestClassifyToastDrag`.
5. **A dismiss while an earlier slide is still running.** Expect the new slide to start from where the card is drawn now, not from its old target (no jump). Test: Task 8 `TestSlideRetargetsFromTheDrawnRect`.

## Fidelity notes — where this plan departs from the design's wording

1. **The OSD paint path is rebuilt first (Task 2).** D1 says "the paint path grows one switch on Kind"; the existing `OSDManager.render` paints no text at all (a coloured square for the icon, one 4×8 bar per label character, a plain level bar), so there is nothing to switch. The OSD now builds a tree and paints it with `render.Paint` and a system-font `TextRenderer`.
2. **D1 trigger sources** follow the design's amendment: niri events for `layout`, sysfs LEDs for `caps lock`/`num lock`.
3. **Media seeks need a new signal.** `services.MediaState` carries no seek marker, and the real bus turns `Seeked` into a generic refresh (`media_bus.go`, `nameChange{Acquired: true}`). Task 6 adds a `Seeked` flag on that event and a `Seeks` counter on `MediaState`.
4. **An expanded toast is word-wrapped, not the control centre's group layout.** D3 says "same card layout the control centre shows"; the control centre's expanded view lists a group's members, which a single toast does not have, and `ui` text nodes do not wrap. Expanded toasts wrap their body into at most eight measured lines (a pure `wrapLines`).
5. **An expanded toast collapses on a second vertical drag, not on "release-outside".** The toast surface's input region covers only its cards, so a release outside a card never reaches it.
6. **Toggle kinds show on/off in text.** `caps lock`, `num lock` and `do not disturb` carry `On bool` and label as "Caps Lock on"; no dedicated caps-lock glyph exists in the Material subset, so they use `keyboard` (lock keys) and `do_not_disturb_on`/`notifications` (DND).
7. **The empty state exists in one of two places already.** The control centre's notification page has icon + "Nothing to see here"; the notification centre panel has bare text. Task 9 gives both the design's muted icon + "No notifications".
8. **The badge pop uses the channel's catalogue timing**, not a hand-set 150 ms: `animator.Target` takes its duration from the theme's motion set, and D5 forbids a new channel.

## File Map

| File | Responsibility |
|---|---|
| `internal/shell/osdview.go` (new) | OSD kinds, `OSDView`, icon/label choice, pure `osdTree`, `osdHandle` |
| `internal/shell/osd.go` | render through the shared renderer; text renderer ownership |
| `internal/services/lockkeys.go` (new) | sysfs lock-key polling service |
| `internal/shell/osdrelay.go` (new) | lock-key, layout, media and DND → OSD decisions and relays |
| `internal/platform/niri/events.go` | keyboard layouts in `Snapshot` |
| `internal/services/media.go`, `media_bus.go` | `Seeks` counter from real `Seeked` signals |
| `internal/shell/popout_notifications.go` | DND change hook; centre empty state |
| `internal/shell/notifyactions.go` | `classifyToastDrag`; expand toggle action |
| `internal/shell/notifywrap.go` (new) | pure `wrapLines` |
| `internal/shell/notifycard.go` | expanded toast card; badge key |
| `internal/shell/toasthost.go` | expanded set; collapse-slide animator |
| `internal/shell/badgepop.go` (new) | badge count tracking and paint-copy scale |
| `internal/shell/controlcenter_pages.go` | control-centre empty state copy |

---

### Task 1: OSD kinds and the per-kind tree

**Files:**
- Create: `internal/shell/osdview.go`, `internal/shell/osdview_test.go`
- Modify: `internal/shell/osd.go` (move `OSDView` and `osdLabel` out; nothing else yet)

**Interfaces:**
- Produces:

```go
const (
	osdAudio      = "audio"
	osdBrightness = "brightness"
	osdMedia      = "media"
	osdCapsLock   = "caps lock"
	osdNumLock    = "num lock"
	osdLayout     = "layout"
	osdDND        = "do not disturb"
)
type OSDView struct {
	Kind  string
	Level int    // 0..100 for metered kinds
	Muted bool   // audio muted; media stopped
	Text  string // media title; layout name
	On    bool   // toggle kinds; media playing
}
func osdMetered(kind string) bool // audio, brightness, media
func osdIcon(v OSDView) string    // "" for layout
func osdLabel(v OSDView) string
func osdTree(v OSDView) *ui.Node  // KindColumn root, laid out inside the OSD body
const osdInnerW = osdWidth - 16 - 2*osdPad // 180
const osdPad = 12
```

- [ ] **Step 1: Write the failing tests**

```go
package shell

import (
	"testing"

	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

func TestOSDIconAndLabelPerKind(t *testing.T) {
	cases := []struct {
		v           OSDView
		icon, label string
	}{
		{OSDView{Kind: osdAudio, Level: 40}, "volume_up", "Volume"},
		{OSDView{Kind: osdAudio, Muted: true}, "volume_off", "Muted"},
		{OSDView{Kind: osdBrightness, Level: 70}, "brightness_high", "Brightness"},
		{OSDView{Kind: osdMedia, Text: "Song", On: true}, "play_arrow", "Song"},
		{OSDView{Kind: osdMedia, Text: "Song"}, "pause", "Song"},
		{OSDView{Kind: osdMedia, Muted: true}, "music_note", "Stopped"},
		{OSDView{Kind: osdMedia, On: true}, "play_arrow", "Media"},
		{OSDView{Kind: osdCapsLock, On: true}, "keyboard", "Caps Lock on"},
		{OSDView{Kind: osdNumLock}, "keyboard", "Num Lock off"},
		{OSDView{Kind: osdLayout, Text: "German"}, "", "German"},
		{OSDView{Kind: osdDND, On: true}, "do_not_disturb_on", "Do not disturb on"},
		{OSDView{Kind: osdDND}, "notifications", "Do not disturb off"},
	}
	for _, tc := range cases {
		if got := osdIcon(tc.v); got != tc.icon {
			t.Errorf("%+v icon = %q, want %q", tc.v, got, tc.icon)
		}
		if got := osdLabel(tc.v); got != tc.label {
			t.Errorf("%+v label = %q, want %q", tc.v, got, tc.label)
		}
	}
}

func TestOSDTreeShapePerKind(t *testing.T) {
	count := func(n *ui.Node, k ui.Kind) int {
		c := 0
		var walk func(*ui.Node)
		walk = func(n *ui.Node) {
			if n == nil {
				return
			}
			if n.Kind == k {
				c++
			}
			for _, ch := range n.Children {
				walk(ch)
			}
		}
		walk(n)
		return c
	}
	for _, kind := range []string{osdAudio, osdBrightness, osdMedia} {
		tree := osdTree(OSDView{Kind: kind, Level: 50})
		if tree.Kind != ui.KindColumn || count(tree, ui.KindMeter) != 1 {
			t.Errorf("%s: want a column root with one meter", kind)
		}
	}
	for _, kind := range []string{osdCapsLock, osdNumLock, osdDND, osdLayout} {
		if count(osdTree(OSDView{Kind: kind}), ui.KindMeter) != 0 {
			t.Errorf("%s: toggle and text kinds carry no meter", kind)
		}
	}
	if count(osdTree(OSDView{Kind: osdLayout, Text: "German"}), ui.KindIcon) != 0 {
		t.Error("layout is text-only")
	}
}

// Every kind lays out inside the 220x64 OSD body with real layout rules.
func TestOSDTreesFitTheBody(t *testing.T) {
	measure := func(s string, _ ui.TextAttrs) (int, int) { return len(s) * 7, 16 }
	body := ui.Rect{W: osdWidth - 16, H: osdHeight - 16}
	for _, v := range []OSDView{
		{Kind: osdAudio, Level: 100}, {Kind: osdBrightness}, {Kind: osdCapsLock, On: true},
		{Kind: osdMedia, Text: "A very long track title that must be clipped not overflow", Level: 30, On: true},
		{Kind: osdLayout, Text: "English (US, intl., with dead keys)"}, {Kind: osdDND, On: true},
	} {
		if err := ui.LayoutColumn(osdTree(v), body, measure); err != nil {
			t.Errorf("%+v: %v", v, err)
		}
	}
}
```

- [ ] **Step 2: Run to verify they fail**

Run: `GOWORK=off go test ./internal/shell -run 'OSDIconAndLabel|OSDTreeShape|OSDTreesFit' -count=1`
Expected: FAIL — `osdIcon`, `osdTree`, kind constants undefined.

- [ ] **Step 3: Implement `internal/shell/osdview.go`**

Move `OSDView` and `osdLabel` from `osd.go` into this file and extend them:

```go
package shell

import "github.com/Nomadcxx/sysc-shell/internal/ui"

const (
	osdAudio      = "audio"
	osdBrightness = "brightness"
	osdMedia      = "media"
	osdCapsLock   = "caps lock"
	osdNumLock    = "num lock"
	osdLayout     = "layout"
	osdDND        = "do not disturb"

	osdPad    = 12
	osdInnerW = osdWidth - 16 - 2*osdPad
	osdIconSz = 20
	osdGap    = 10
)

// OSDView is one OSD payload. Level is 0..100 for metered kinds; Muted is
// audio mute or a stopped player; Text is a media title or a layout name;
// On is a toggle's state or a playing player.
type OSDView struct {
	Kind  string
	Level int
	Muted bool
	Text  string
	On    bool
}

func osdMetered(kind string) bool {
	return kind == osdAudio || kind == osdBrightness || kind == osdMedia
}

func osdIcon(v OSDView) string {
	switch v.Kind {
	case osdAudio:
		if v.Muted {
			return "volume_off"
		}
		return "volume_up"
	case osdBrightness:
		return "brightness_high"
	case osdMedia:
		switch {
		case v.Muted:
			return "music_note"
		case v.On:
			return "play_arrow"
		default:
			return "pause"
		}
	case osdCapsLock, osdNumLock:
		return "keyboard"
	case osdDND:
		if v.On {
			return "do_not_disturb_on"
		}
		return "notifications"
	}
	return ""
}

func onOff(on bool) string {
	if on {
		return "on"
	}
	return "off"
}

func osdLabel(v OSDView) string {
	switch v.Kind {
	case osdAudio:
		if v.Muted {
			return "Muted"
		}
		return "Volume"
	case osdBrightness:
		return "Brightness"
	case osdMedia:
		if v.Muted {
			return "Stopped"
		}
		if v.Text != "" {
			return v.Text
		}
		return "Media"
	case osdCapsLock:
		return "Caps Lock " + onOff(v.On)
	case osdNumLock:
		return "Num Lock " + onOff(v.On)
	case osdDND:
		return "Do not disturb " + onOff(v.On)
	case osdLayout:
		return v.Text
	}
	return v.Kind
}

// osdTree is the OSD's content for one view, laid out inside the body the
// manager paints. Metered kinds put the icon beside a label over a meter;
// media leads with its title; toggles are icon and label; layout is a
// centred name.
func osdTree(v OSDView) *ui.Node {
	label := &ui.Node{Kind: ui.KindText, Text: osdLabel(v), MaxWidth: osdInnerW - osdIconSz - osdGap}
	icon := &ui.Node{Kind: ui.KindIcon, Icon: osdIcon(v), IconSize: osdIconSz}
	meter := func(w int) *ui.Node {
		tone := ui.ToneNormal
		if v.Muted {
			tone = ui.ToneSubtle
		}
		return &ui.Node{Kind: ui.KindMeter, Key: "osd-meter", Value: float64(v.Level) / 100, Width: w, Height: 6, Tone: tone}
	}
	root := &ui.Node{Kind: ui.KindColumn, Padding: osdPad, Gap: 6}
	switch {
	case v.Kind == osdLayout:
		label.MaxWidth = osdInnerW
		label.CenterX = true
		label.Size = "title"
		root.Children = []*ui.Node{label}
	case v.Kind == osdMedia:
		root.Children = []*ui.Node{
			{Kind: ui.KindRow, Gap: osdGap, Children: []*ui.Node{icon, label}},
			meter(osdInnerW),
		}
	case osdMetered(v.Kind):
		root.Children = []*ui.Node{{Kind: ui.KindRow, Gap: osdGap, Children: []*ui.Node{
			icon,
			{Kind: ui.KindColumn, Gap: 6, Children: []*ui.Node{label, meter(osdInnerW - osdIconSz - osdGap)}},
		}}}
	default:
		root.Children = []*ui.Node{{Kind: ui.KindRow, Gap: osdGap, Children: []*ui.Node{icon, label}}}
	}
	return root
}
```

If a node field used above has a different name in `internal/ui/tree.go` (`Size`, `MaxWidth`, `CenterX`, `Tone` on a meter), use the actual field; `grep -n 'MaxWidth\|CenterX\|Size  *string' internal/ui/tree.go` shows them.

Update the four existing `Show` call sites in `registry.go` to use the constants (`osdAudio`, `osdBrightness`). Update any existing test asserting the old `osdLabel` strings ("audio", "audio muted") to the new ones.

- [ ] **Step 4: Run**

Run: `GOWORK=off go test ./internal/shell -run 'OSD' -count=1`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/shell/osdview.go internal/shell/osdview_test.go internal/shell/osd.go internal/shell/registry.go
git commit -m "feat(osd): per-kind view model, icons, labels and tree"
```

### Task 2: The OSD paints through the shared renderer

**Files:**
- Modify: `internal/shell/osd.go` (`OSDManager.render`; `text` field and `ensureText`)
- Test: `internal/shell/osdpaint_test.go` (new)

**Interfaces:**
- Consumes: `osdTree` (Task 1).
- Produces: `func (m *OSDManager) ensureText() error`; `func (m *OSDManager) layoutView(scale ui.Scale120) (*ui.Node, render.Style, error)` — laid-out tree plus the style it paints with (Task 3 extends the tree after layout).

- [ ] **Step 1: Write the failing test**

```go
package shell

import (
	"testing"
)

// The OSD draws real glyph ink for its label: more than the placeholder's
// row of 4x8 bars, and different pixels for different labels.
func TestOSDPaintsRealText(t *testing.T) {
	r := newPanelRegistry(t)
	m := newOSDManager(r, 0)
	paint := func(v OSDView) []byte {
		r.mu.Lock()
		m.view, m.theme = v, r.panelTheme()
		r.mu.Unlock()
		pix := make([]byte, osdWidth*osdHeight*4)
		if err := m.render(pix, osdWidth, osdHeight, osdWidth*4); err != nil {
			t.Fatal(err)
		}
		return pix
	}
	a := paint(OSDView{Kind: osdCapsLock, On: true})
	b := paint(OSDView{Kind: osdNumLock, On: true})
	same := true
	for i := range a {
		if a[i] != b[i] {
			same = false
			break
		}
	}
	if same {
		t.Fatal("Caps Lock and Num Lock painted identical pixels: the label is not text")
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `GOWORK=off go test ./internal/shell -run TestOSDPaintsRealText -count=1`
Expected: FAIL — the placeholder paints the same bars for two 12-character labels.

- [ ] **Step 3: Implement**

Add `text *render.TextRenderer` to `OSDManager`. Add:

```go
// ensureText builds the OSD's text renderer once, from the same system font
// resolution the panels use. Registry.mu is held.
func (m *OSDManager) ensureText() error {
	if m.text != nil {
		return nil
	}
	fonts, err := render.NewSystemFontMap(m.r.cfg.ForConnector("").FontFamily, render.DefaultFontCacheDir())
	if err != nil {
		return err
	}
	m.text = render.NewTextRendererWithFontMap(fonts)
	return nil
}

// layoutView lays the current view out inside the OSD body at the given
// scale and returns it with the style it paints with.
func (m *OSDManager) layoutView(scale ui.Scale120, body ui.Rect) (*ui.Node, render.Style, error) {
	style := m.theme.PanelStyle()
	style.Scale120 = scale
	style.Body = body
	measure := func(s string, attrs ui.TextAttrs) (int, int) {
		spec := render.SpecFor(style, attrs)
		if w, h, err := m.text.Measure(s, spec, attrs.Tabular); err == nil {
			return scale.Logical(w), scale.Logical(h)
		}
		return len(s) * 8, 16
	}
	root := osdTree(m.view)
	if err := ui.LayoutColumn(root, body, measure); err != nil {
		return nil, style, err
	}
	return root, style, nil
}
```

Replace `render`:

```go
func (m *OSDManager) render(pixels []byte, width, height, stride int) error {
	c, err := render.NewCanvas(pixels, width, height, stride)
	if err != nil {
		return err
	}
	if err := m.ensureText(); err != nil {
		return err
	}
	// The buffer is physical pixels; the OSD is laid out in logical ones.
	scale := ui.Scale120(120 * width / osdWidth)
	if !scale.Valid() {
		scale = ui.ScaleUnit
	}
	slide := m.slidePx()
	body := ui.Rect{X: 8, Y: 8 + slide, W: osdWidth - 16, H: osdHeight - 16}
	c.FillRounded(scale.PhysicalRect(body), scale.Physical(8), m.theme.Background)
	root, style, err := m.layoutView(scale, body)
	if err != nil {
		return err
	}
	return render.Paint(c, root, m.text, style)
}
```

`render` runs from `publishSurface` with `Registry.mu` held, like the panel host's render; if it does not, take `m.r.mu` around `ensureText` and `layoutView` (check the call path in `renderLocking`-style wrappers for the OSD's aux callbacks in `osd.go`'s `spec`). If `render.Paint` repaints the whole canvas background, set `style.Body` as above and confirm the rounded body survives by eye in Task 10's live check; the pixel test here only proves text is drawn.

- [ ] **Step 4: Run**

Run: `GOMAXPROCS=4 GOWORK=off go test -p 2 -count=1 ./internal/shell -run 'OSD'`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/shell/osd.go internal/shell/osdpaint_test.go
git commit -m "feat(osd): paint icons, labels and meters with the shared renderer"
```

### Task 3: The value-reactive handle (D2)

**Files:**
- Modify: `internal/shell/osdview.go` (`osdHandle`), `internal/shell/osd.go` (`render` adds the handle after layout)
- Test: `internal/shell/osdview_test.go`

**Interfaces:**
- Produces:

```go
// x is the handle's centre on the track; iconOpacity and textOpacity are
// percent, 0 meaning "do not paint".
func osdHandle(level, trackX, trackW int) (x int, iconOpacity, textOpacity uint8)
func osdHandleNodes(v OSDView, meter *ui.Node) []*ui.Node // positioned after layout
```

- [ ] **Step 1: Write the failing test**

```go
func TestOSDHandleRidesTheFillAndCrossfadesNearFull(t *testing.T) {
	cases := []struct {
		level          int
		x              int
		icon, text     uint8
	}{
		{0, 10, 100, 0},
		{50, 60, 100, 0},
		{85, 95, 100, 0},
		{90, 100, 50, 50},
		{95, 105, 0, 100},
		{100, 110, 0, 100},
		{140, 110, 0, 100},
		{-5, 10, 100, 0},
	}
	for _, tc := range cases {
		x, icon, text := osdHandle(tc.level, 10, 100)
		if x != tc.x || icon != tc.icon || text != tc.text {
			t.Errorf("level %d: x=%d icon=%d text=%d, want %d %d %d", tc.level, x, icon, text, tc.x, tc.icon, tc.text)
		}
	}
}

func TestOSDHandleNodesOnlyForMeteredKinds(t *testing.T) {
	meter := &ui.Node{Kind: ui.KindMeter, Bounds: ui.Rect{X: 40, Y: 30, W: 150, H: 6}}
	if n := osdHandleNodes(OSDView{Kind: osdCapsLock}, meter); len(n) != 0 {
		t.Fatal("a toggle kind got a handle")
	}
	nodes := osdHandleNodes(OSDView{Kind: osdAudio, Level: 90}, meter)
	if len(nodes) != 2 || nodes[0].Opacity != 50 || nodes[1].Text != "90%" {
		t.Fatalf("handle nodes at 90%%: %+v", nodes)
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `GOWORK=off go test ./internal/shell -run 'OSDHandle' -count=1`
Expected: FAIL — undefined.

- [ ] **Step 3: Implement**

```go
const (
	osdFadeFrom = 85 // below: the icon rides the handle
	osdFadeTo   = 95 // above: the percentage replaces it
	osdHandleSz = 14
)

func osdHandle(level, trackX, trackW int) (x int, iconOpacity, textOpacity uint8) {
	level = min(max(level, 0), 100)
	x = trackX + trackW*level/100
	switch {
	case level <= osdFadeFrom:
		return x, 100, 0
	case level >= osdFadeTo:
		return x, 0, 100
	}
	text := uint8((level - osdFadeFrom) * 100 / (osdFadeTo - osdFadeFrom))
	return x, 100 - text, text
}

// osdHandleNodes are painted over a laid-out meter: the kind's icon at the
// fill edge, crossfading to the percentage near full. Opacity zero means
// "unset" to the painter, so a fully faded node is left out instead.
func osdHandleNodes(v OSDView, meter *ui.Node) []*ui.Node {
	if !osdMetered(v.Kind) || meter == nil {
		return nil
	}
	x, iconOp, textOp := osdHandle(v.Level, meter.Bounds.X, meter.Bounds.W)
	cy := meter.Bounds.Y + meter.Bounds.H/2
	var out []*ui.Node
	if iconOp > 0 {
		out = append(out, &ui.Node{Kind: ui.KindIcon, Icon: osdIcon(v), IconSize: osdHandleSz, Opacity: iconOp,
			Bounds: ui.Rect{X: x - osdHandleSz/2, Y: cy - osdHandleSz/2, W: osdHandleSz, H: osdHandleSz}})
	}
	if textOp > 0 {
		out = append(out, &ui.Node{Kind: ui.KindText, Text: fmt.Sprintf("%d%%", min(max(v.Level, 0), 100)),
			Size: "caption", Tabular: true, Opacity: textOp,
			Bounds: ui.Rect{X: x - 16, Y: cy - 8, W: 32, H: 16}})
	}
	return out
}
```

Add `"fmt"` to the imports, and in `osdview.go`:

```go
func osdFindKey(n *ui.Node, key string) *ui.Node {
	if n == nil {
		return nil
	}
	if n.Key == key {
		return n
	}
	for _, c := range n.Children {
		if m := osdFindKey(c, key); m != nil {
			return m
		}
	}
	return nil
}
```

In `OSDManager.render`, between `layoutView` and `render.Paint`:

```go
	root.Children = append(root.Children, osdHandleNodes(m.view, osdFindKey(root, "osd-meter"))...)
```

The appended nodes carry their own bounds and are painted over the meter.

- [ ] **Step 4: Run and commit**

Run: `GOWORK=off go test ./internal/shell -run 'OSD' -count=1` — PASS.

```bash
git add internal/shell/osdview.go internal/shell/osdview_test.go internal/shell/osd.go
git commit -m "feat(osd): the kind's icon rides the fill and crossfades to the percentage"
```

### Task 4: Caps Lock and Num Lock from sysfs

**Files:**
- Create: `internal/services/lockkeys.go`, `internal/services/lockkeys_test.go`
- Create: `internal/shell/osdrelay.go`, `internal/shell/osdrelay_test.go`
- Modify: `internal/shell/registry.go` (construct and relay; field `lockKeys *services.LockKeys`)

**Interfaces:**
- Produces:

```go
// internal/services
type LockState struct{ Caps, Num bool }
func NewLockKeys(root string, interval time.Duration) *LockKeys // root "" = /sys/class/leds, interval 0 = 250ms
func (l *LockKeys) Available() bool
func (l *LockKeys) Changes() <-chan LockState
func (l *LockKeys) Baseline() LockState
func (l *LockKeys) Start()
func (l *LockKeys) Close()
func readLockState(root string) (LockState, bool) // false: no LED files at all

// internal/shell
func lockOSD(prev, next services.LockState) []OSDView
func (r *Registry) relayLockKeysOSD(l *services.LockKeys)
```

- [ ] **Step 1: Write the failing service tests**

```go
package services

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func writeLED(t *testing.T, root, name, val string) {
	t.Helper()
	dir := filepath.Join(root, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "brightness"), []byte(val+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestReadLockStateAnyKeyboardLitCountsAsOn(t *testing.T) {
	root := t.TempDir()
	writeLED(t, root, "input13::capslock", "0")
	writeLED(t, root, "input17::capslock", "1")
	writeLED(t, root, "input13::numlock", "0")
	st, ok := readLockState(root)
	if !ok || !st.Caps || st.Num {
		t.Fatalf("state %+v ok %v", st, ok)
	}
}

// Review focus 2.
func TestLockKeysWithoutLEDsNeverPolls(t *testing.T) {
	l := NewLockKeys(t.TempDir(), time.Millisecond)
	l.Start()
	defer l.Close()
	if l.Available() {
		t.Fatal("no LED files but Available")
	}
	l.mu.Lock()
	running := l.stop != nil
	l.mu.Unlock()
	if running {
		t.Fatal("a machine without LEDs started a poll loop")
	}
	select {
	case st := <-l.Changes():
		t.Fatalf("unexpected change %+v", st)
	case <-time.After(20 * time.Millisecond):
	}
}

// Review focus 1: the first read is a baseline, not a change.
func TestLockKeysBaselineIsSilent(t *testing.T) {
	root := t.TempDir()
	writeLED(t, root, "input1::capslock", "1")
	writeLED(t, root, "input1::numlock", "0")
	l := NewLockKeys(root, 2*time.Millisecond)
	l.Start()
	defer l.Close()
	if got := l.Baseline(); !got.Caps || got.Num {
		t.Fatalf("baseline %+v", got)
	}
	select {
	case st := <-l.Changes():
		t.Fatalf("baseline published %+v", st)
	case <-time.After(20 * time.Millisecond):
	}
	writeLED(t, root, "input1::capslock", "0")
	select {
	case st := <-l.Changes():
		if st.Caps {
			t.Fatalf("change reported %+v", st)
		}
	case <-time.After(time.Second):
		t.Fatal("no change published after Caps Lock went off")
	}
}
```

- [ ] **Step 2: Run to verify they fail**

Run: `GOWORK=off go test ./internal/services -run 'LockState|LockKeys' -count=1`
Expected: FAIL — undefined.

- [ ] **Step 3: Implement `internal/services/lockkeys.go`**

```go
package services

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const lockKeysPoll = 250 * time.Millisecond

// LockState is the session's Caps Lock and Num Lock state. Any keyboard with
// its LED lit counts: the kernel mirrors the lock state to every keyboard.
type LockState struct{ Caps, Num bool }

// LockKeys polls the keyboard LED class for lock-key changes, the way the
// brightness service polls the backlight. The shell cannot learn lock state
// from wl_keyboard: it only receives keyboard events while it has focus.
type LockKeys struct {
	root     string
	interval time.Duration
	changes  chan LockState

	mu         sync.Mutex
	ok         bool
	base       LockState
	last       LockState
	hasLast    bool
	stop, done chan struct{}
}

func NewLockKeys(root string, interval time.Duration) *LockKeys {
	if root == "" {
		root = "/sys/class/leds"
	}
	if interval <= 0 {
		interval = lockKeysPoll
	}
	return &LockKeys{root: root, interval: interval, changes: make(chan LockState, 1)}
}

func (l *LockKeys) Available() bool           { l.mu.Lock(); defer l.mu.Unlock(); return l.ok }
func (l *LockKeys) Changes() <-chan LockState { return l.changes }

// Baseline is the state read by Start, before any change was published.
// The relay compares its first change against it.
func (l *LockKeys) Baseline() LockState { l.mu.Lock(); defer l.mu.Unlock(); return l.base }

// Start reads the baseline synchronously, then polls. A machine with no
// lock-key LEDs never starts polling.
func (l *LockKeys) Start() {
	l.mu.Lock()
	running := l.stop != nil
	l.mu.Unlock()
	if running {
		return
	}
	l.poll(true)
	l.mu.Lock()
	defer l.mu.Unlock()
	if !l.ok || l.stop != nil {
		return
	}
	l.stop, l.done = make(chan struct{}), make(chan struct{})
	go l.run(l.stop, l.done)
}

func (l *LockKeys) Close() {
	l.mu.Lock()
	stop, done := l.stop, l.done
	l.stop, l.done = nil, nil
	l.mu.Unlock()
	if stop != nil {
		close(stop)
		<-done
	}
}

func (l *LockKeys) run(stop, done chan struct{}) {
	defer close(done)
	tick := time.NewTicker(l.interval)
	defer tick.Stop()
	for {
		select {
		case <-stop:
			return
		case <-tick.C:
			l.poll(false)
		}
	}
}

func (l *LockKeys) poll(baseline bool) {
	st, ok := readLockState(l.root)
	l.mu.Lock()
	defer l.mu.Unlock()
	l.ok = ok
	if !ok {
		return
	}
	if baseline || !l.hasLast {
		l.last, l.base, l.hasLast = st, st, true
		return
	}
	if st == l.last {
		return
	}
	l.last = st
	select {
	case l.changes <- st:
	default:
		select {
		case <-l.changes:
		default:
		}
		l.changes <- st
	}
}

func readLockState(root string) (LockState, bool) {
	caps, capsOK := anyLit(filepath.Join(root, "*::capslock", "brightness"))
	num, numOK := anyLit(filepath.Join(root, "*::numlock", "brightness"))
	return LockState{Caps: caps, Num: num}, capsOK || numOK
}

func anyLit(pattern string) (lit, found bool) {
	paths, _ := filepath.Glob(pattern)
	for _, p := range paths {
		b, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		found = true
		if v := strings.TrimSpace(string(b)); v != "" && v != "0" {
			lit = true
		}
	}
	return lit, found
}
```

- [ ] **Step 4: Run the service tests**

Run: `GOWORK=off go test ./internal/services -run 'LockState|LockKeys' -count=1 -race`
Expected: PASS.

- [ ] **Step 5: Write the failing relay test**

```go
package shell

import (
	"testing"

	"github.com/Nomadcxx/sysc-shell/internal/services"
)

func TestLockOSDShowsOnlyWhatChanged(t *testing.T) {
	cases := []struct {
		prev, next services.LockState
		want       []OSDView
	}{
		{services.LockState{}, services.LockState{Caps: true}, []OSDView{{Kind: osdCapsLock, On: true}}},
		{services.LockState{Caps: true}, services.LockState{Caps: true, Num: true}, []OSDView{{Kind: osdNumLock, On: true}}},
		{services.LockState{}, services.LockState{Caps: true, Num: true}, []OSDView{{Kind: osdCapsLock, On: true}, {Kind: osdNumLock, On: true}}},
		{services.LockState{Num: true}, services.LockState{Num: true}, nil},
	}
	for _, tc := range cases {
		got := lockOSD(tc.prev, tc.next)
		if len(got) != len(tc.want) {
			t.Fatalf("%+v -> %+v: %+v", tc.prev, tc.next, got)
		}
		for i := range got {
			if got[i] != tc.want[i] {
				t.Fatalf("%+v -> %+v: %+v", tc.prev, tc.next, got)
			}
		}
	}
}
```

- [ ] **Step 6: Implement the relay in `internal/shell/osdrelay.go`**

```go
package shell

import "github.com/Nomadcxx/sysc-shell/internal/services"

// lockOSD names the lock keys that changed. Both changing in one poll shows
// both, the later one replacing the first on screen.
func lockOSD(prev, next services.LockState) []OSDView {
	var out []OSDView
	if next.Caps != prev.Caps {
		out = append(out, OSDView{Kind: osdCapsLock, On: next.Caps})
	}
	if next.Num != prev.Num {
		out = append(out, OSDView{Kind: osdNumLock, On: next.Num})
	}
	return out
}

// relayLockKeysOSD runs on its own goroutine, like the audio relay, so Show
// can take Registry.mu. l.Start has already read the baseline.
func (r *Registry) relayLockKeysOSD(l *services.LockKeys) {
	if l == nil {
		return
	}
	prev := l.Baseline()
	for {
		select {
		case <-r.closed:
			return
		case st, ok := <-l.Changes():
			if !ok {
				return
			}
			for _, v := range lockOSD(prev, st) {
				r.OSD().Show(v)
			}
			prev = st
		}
	}
}
```

In `NewRegistry` (next to `r.setBrightness(...)`), unless `runningAsTest()`:

```go
	r.lockKeys = services.NewLockKeys("", 0)
	r.lockKeys.Start()
	go r.relayLockKeysOSD(r.lockKeys)
```

and close it where the registry closes its other services (search `r.brightness.Close()` and add `r.lockKeys.Close()` beside it, nil-guarded).

- [ ] **Step 7: Run and commit**

Run: `GOMAXPROCS=4 GOWORK=off go test -p 2 -count=1 ./internal/services ./internal/shell -run 'Lock'` — PASS.

```bash
git add internal/services/lockkeys.go internal/services/lockkeys_test.go internal/shell/osdrelay.go internal/shell/osdrelay_test.go internal/shell/registry.go
git commit -m "feat(osd): Caps Lock and Num Lock from the keyboard LED class"
```

### Task 5: Keyboard layout from niri

**Files:**
- Modify: `internal/platform/niri/events.go` (`KeyboardLayouts`, `Snapshot.Layouts`, `state.layouts`, two event cases, `publishIfChanged`)
- Modify: `internal/platform/niri/client_test.go` (its "unknown event" fixture uses `KeyboardLayoutsChanged`; switch it to an event the client still ignores, e.g. `{"OverviewOpenedOrClosed":{"is_open":true}}`)
- Modify: `internal/shell/registry.go` (`UpdateNiri`), `internal/shell/osdrelay.go`
- Test: `internal/platform/niri/events_test.go`, `internal/shell/osdrelay_test.go`

**Interfaces:**
- Produces:

```go
// niri
type KeyboardLayouts struct {
	Names   []string
	Current int
}
// Snapshot gains: Layouts KeyboardLayouts
// shell
func layoutOSD(prev, next niri.KeyboardLayouts, seen bool) (OSDView, bool)
// Registry gains: layouts niri.KeyboardLayouts; layoutsSeen bool
```

- [ ] **Step 1: Write the failing niri test**

```go
func TestKeyboardLayoutEventsReachTheSnapshot(t *testing.T) {
	var s state
	changed, err := s.apply([]byte(`{"KeyboardLayoutsChanged":{"keyboard_layouts":{"names":["English (US)","German"],"current_idx":0}}}`))
	if err != nil || !changed {
		t.Fatalf("layouts changed: %v %v", changed, err)
	}
	if got := s.last.Layouts; len(got.Names) != 2 || got.Current != 0 {
		t.Fatalf("layouts %+v", got)
	}
	changed, err = s.apply([]byte(`{"KeyboardLayoutSwitched":{"idx":1}}`))
	if err != nil || !changed || s.last.Layouts.Current != 1 {
		t.Fatalf("switch: %v %v %+v", changed, err, s.last.Layouts)
	}
	if changed, _ := s.apply([]byte(`{"KeyboardLayoutSwitched":{"idx":1}}`)); changed {
		t.Fatal("a switch to the current layout published")
	}
	if _, err := s.apply([]byte(`{"KeyboardLayoutSwitched":{}}`)); err == nil {
		t.Fatal("a switch without idx was accepted")
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `GOWORK=off go test ./internal/platform/niri -run KeyboardLayoutEvents -count=1`
Expected: FAIL — `Layouts` undefined.

- [ ] **Step 3: Implement in `events.go`**

```go
// KeyboardLayouts is niri's configured layout list and the active index.
type KeyboardLayouts struct {
	Names   []string
	Current int
}

type wireKeyboardLayoutsChanged struct {
	KeyboardLayouts struct {
		Names      []string `json:"names"`
		CurrentIdx *int     `json:"current_idx"`
	} `json:"keyboard_layouts"`
}

type wireKeyboardLayoutSwitched struct {
	Idx *int `json:"idx"`
}
```

Add `Layouts KeyboardLayouts` to `Snapshot`, `layouts KeyboardLayouts` to `state`, copy it in `snapshot()` (`Layouts: KeyboardLayouts{Names: slices.Clone(s.layouts.Names), Current: s.layouts.Current}`), and extend `publishIfChanged`'s equality with `slices.Equal(next.Layouts.Names, s.last.Layouts.Names) && next.Layouts.Current == s.last.Layouts.Current`. In `apply`, before the fallthrough that ignores unknown events:

```go
	if payload, ok := envelope["KeyboardLayoutsChanged"]; ok {
		var changed wireKeyboardLayoutsChanged
		if err := json.Unmarshal(payload, &changed); err != nil {
			return false, fmt.Errorf("niri: decode KeyboardLayoutsChanged: %w", err)
		}
		if changed.KeyboardLayouts.CurrentIdx == nil {
			return false, fmt.Errorf("niri: KeyboardLayoutsChanged is missing current_idx")
		}
		s.layouts = KeyboardLayouts{Names: changed.KeyboardLayouts.Names, Current: *changed.KeyboardLayouts.CurrentIdx}
		return s.publishIfChanged(), nil
	}

	if payload, ok := envelope["KeyboardLayoutSwitched"]; ok {
		var switched wireKeyboardLayoutSwitched
		if err := json.Unmarshal(payload, &switched); err != nil {
			return false, fmt.Errorf("niri: decode KeyboardLayoutSwitched: %w", err)
		}
		if switched.Idx == nil {
			return false, fmt.Errorf("niri: KeyboardLayoutSwitched is missing idx")
		}
		s.layouts.Current = *switched.Idx
		return s.publishIfChanged(), nil
	}
```

Update `client_test.go`'s unknown-event fixture as noted in **Files**.

- [ ] **Step 4: Run the niri package**

Run: `GOWORK=off go test ./internal/platform/niri -count=1`
Expected: PASS.

- [ ] **Step 5: Write the failing shell test**

```go
// Review focus 3.
func TestFirstLayoutSnapshotIsSilent(t *testing.T) {
	two := niri.KeyboardLayouts{Names: []string{"English (US)", "German"}, Current: 0}
	if _, show := layoutOSD(niri.KeyboardLayouts{}, two, false); show {
		t.Fatal("first snapshot showed a layout OSD")
	}
	switched := niri.KeyboardLayouts{Names: two.Names, Current: 1}
	v, show := layoutOSD(two, switched, true)
	if !show || v.Kind != osdLayout || v.Text != "German" {
		t.Fatalf("switch: %+v %v", v, show)
	}
	if _, show := layoutOSD(switched, switched, true); show {
		t.Fatal("no change showed an OSD")
	}
	if _, show := layoutOSD(two, niri.KeyboardLayouts{Names: two.Names, Current: 5}, true); show {
		t.Fatal("an out-of-range index showed an OSD")
	}
}
```

(Import `"github.com/Nomadcxx/sysc-shell/internal/platform/niri"`.)

- [ ] **Step 6: Implement**

In `osdrelay.go`:

```go
// layoutOSD announces a layout switch. The first snapshot after connecting
// only reports the current layout, so it is never announced.
func layoutOSD(prev, next niri.KeyboardLayouts, seen bool) (OSDView, bool) {
	if !seen || next.Current == prev.Current && slices.Equal(next.Names, prev.Names) {
		return OSDView{}, false
	}
	if next.Current < 0 || next.Current >= len(next.Names) {
		return OSDView{}, false
	}
	return OSDView{Kind: osdLayout, Text: next.Names[next.Current]}, true
}
```

In `Registry.UpdateNiri`, inside the locked section before `r.mu.Unlock()`:

```go
	layoutView, showLayout := layoutOSD(r.layouts, s.Layouts, r.layoutsSeen)
	r.layouts = s.Layouts
	if len(s.Layouts.Names) > 0 {
		r.layoutsSeen = true
	}
```

and after `r.publish(changed)`:

```go
	if showLayout {
		r.OSD().Show(layoutView)
	}
```

- [ ] **Step 7: Run and commit**

Run: `GOMAXPROCS=4 GOWORK=off go test -p 2 -count=1 ./internal/platform/niri ./internal/shell -run 'Layout|Niri'` — PASS.

```bash
git add internal/platform/niri internal/shell/osdrelay.go internal/shell/osdrelay_test.go internal/shell/registry.go
git commit -m "feat(osd): announce keyboard layout switches from niri events"
```

### Task 6: Media and do-not-disturb OSDs

**Files:**
- Modify: `internal/services/media_bus.go` (`nameChange.Seeked`), `internal/services/media.go` (`seeks` counter, `MediaState.Seeks`)
- Modify: `internal/shell/registry.go` (`relayMedia` loop body only), `internal/shell/osdrelay.go`
- Modify: `internal/shell/popout_notifications.go` (`notifyState.onDND` hook), `internal/shell/registry.go` (install the hook)
- Test: `internal/services/media_test.go`, `internal/shell/osdrelay_test.go`

**Interfaces:**
- Produces:

```go
// services
// MediaState gains: Seeks uint64 — increments once per MPRIS Seeked on the active player
// nameChange gains: Seeked bool
// shell
func mediaOSD(prev, next services.MediaState) (OSDView, bool)
// notifyState gains: onDND func(on bool) — called after the state changes, outside s.mu
```

- [ ] **Step 1: Write the failing tests**

```go
// internal/shell/osdrelay_test.go
func TestMediaOSDFiresOnStatusTrackAndSeek(t *testing.T) {
	base := services.MediaState{Available: true, Title: "One", Status: services.PlaybackPlaying, PositionUS: 30e6, LengthUS: 120e6}
	cases := []struct {
		name string
		next services.MediaState
		show bool
		want OSDView
	}{
		{"pause", func() services.MediaState { s := base; s.Status = services.PlaybackPaused; return s }(), true,
			OSDView{Kind: osdMedia, Text: "One", Level: 25}},
		{"track change", func() services.MediaState { s := base; s.Title = "Two"; s.PositionUS = 0; return s }(), true,
			OSDView{Kind: osdMedia, Text: "Two", Level: 0, On: true}},
		{"seek", func() services.MediaState { s := base; s.Seeks = 1; s.PositionUS = 60e6; return s }(), true,
			OSDView{Kind: osdMedia, Text: "One", Level: 50, On: true}},
		{"position tick only", func() services.MediaState { s := base; s.PositionUS = 31e6; return s }(), false, OSDView{}},
		{"stopped", func() services.MediaState { s := base; s.Status = services.PlaybackStopped; return s }(), true,
			OSDView{Kind: osdMedia, Text: "One", Level: 25, Muted: true}},
		{"player gone", services.MediaState{}, false, OSDView{}},
	}
	for _, tc := range cases {
		got, show := mediaOSD(base, tc.next)
		if show != tc.show || (show && got != tc.want) {
			t.Errorf("%s: %+v %v, want %+v %v", tc.name, got, show, tc.want, tc.show)
		}
	}
}

func TestDNDChangesCallTheHookOnceAfterUnlock(t *testing.T) {
	s := &notifyState{}
	var calls []bool
	s.onDND = func(on bool) {
		s.mu.Lock() // must not deadlock: the hook runs outside s.mu
		s.mu.Unlock()
		calls = append(calls, on)
	}
	s.setDND(true)
	s.setDND(true) // no change: no call
	s.setDNDPreset(time.Now(), time.Hour)
	s.setDND(false)
	if len(calls) != 2 || !calls[0] || calls[1] {
		t.Fatalf("calls %v, want [true false]", calls)
	}
}
```

(`setDNDPreset` while already on is not a change of the on/off state, so it does not call the hook.) Add a media service test on the existing fake-bus harness (`internal/services/media_test.go`):

```go
func TestSeekedSignalIncrementsSeeks(t *testing.T) {
	t.Parallel()
	const vlc = "org.mpris.MediaPlayer2.vlc"
	b := newFakeBus(vlc)
	b.setProps(vlc, map[string]any{"PlaybackStatus": "Playing"})
	m := NewMedia(b)
	t.Cleanup(m.Close)
	lease, err := m.Acquire()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(lease.Release)
	waitFor(t, func() bool { return m.State().Available })

	b.nameCh <- nameChange{Name: vlc, Acquired: true}
	b.nameCh <- nameChange{Name: vlc, Acquired: true, Seeked: true}
	waitFor(t, func() bool { return m.State().Seeks == 1 })
	b.nameCh <- nameChange{Name: vlc, Acquired: true}
	time.Sleep(20 * time.Millisecond)
	if got := m.State().Seeks; got != 1 {
		t.Fatalf("a plain refresh changed Seeks to %d", got)
	}
}
```

If `setProps` keys differ from MPRIS property names in this harness (see `TestMediaDecodesMetadata` for the form it expects), match that test's setup.

- [ ] **Step 2: Run to verify they fail**

Run: `GOWORK=off go test ./internal/shell ./internal/services -run 'MediaOSD|DNDChanges|SeekedSignal' -count=1`
Expected: FAIL.

- [ ] **Step 3: Implement the service half**

In `media_bus.go`: add `Seeked bool` to `nameChange`; in the `Seeked` signal case emit `nameChange{Name: name, Acquired: true, Seeked: true}`. In `media.go`: add `seeks uint64` to `Media` and `Seeks uint64` to `MediaState`; in `handleNameChange` (`media.go`), in the acquired branch after `probePlayer` merges the refreshed player under `m.mu`, add `if ch.Seeked && ch.Name == m.active { m.seeks++ }` before its `publishLocked()`; `snapshotLocked` copies `Seeks: m.seeks`. Also increment in `onSeeked` so the direct path agrees.

- [ ] **Step 4: Implement the shell half**

In `osdrelay.go`:

```go
// mediaOSD announces transport changes: play/pause/stop, a new track, or a
// seek. Position ticks are not announcements.
func mediaOSD(prev, next services.MediaState) (OSDView, bool) {
	if !next.Available {
		return OSDView{}, false
	}
	if next.Status == prev.Status && next.Title == prev.Title && next.Seeks == prev.Seeks {
		return OSDView{}, false
	}
	level := 0
	if next.LengthUS > 0 {
		level = int(next.PositionUS * 100 / next.LengthUS)
	}
	return OSDView{Kind: osdMedia, Text: next.Title, Level: level,
		On: next.Status == services.PlaybackPlaying, Muted: next.Status == services.PlaybackStopped}, true
}
```

In `relayMedia`, keep the startup lines exactly as they are (`sysc-566`). Change only the loop:

```go
	prev := media.CachedState()
	for {
		select {
		case <-r.closed:
			return
		case <-cancel:
			return
		case state := <-media.Changes():
			r.publishMediaSnapshot(media, state)
			if v, show := mediaOSD(prev, state); show {
				r.OSD().Show(v)
			}
			prev = state
		}
	}
```

`relayMedia` runs on its own goroutine and `publishMediaSnapshot` returns before `Show`, so `Show` takes `r.mu` safely; confirm `publishMediaSnapshot` does not hold `r.mu` on return.

In `popout_notifications.go`, give `notifyState` `onDND func(on bool)` and make both setters report changes outside the lock:

```go
func (s *notifyState) setDND(on bool) {
	s.mu.Lock()
	changed := s.dnd != on
	s.dnd = on
	s.dndUntil = time.Time{}
	hook := s.onDND
	s.mu.Unlock()
	if changed && hook != nil {
		hook(on)
	}
}

func (s *notifyState) setDNDPreset(now time.Time, d time.Duration) {
	s.mu.Lock()
	changed := !s.dnd
	s.dnd = true
	s.dndUntil = now.Add(d)
	hook := s.onDND
	s.mu.Unlock()
	if changed && hook != nil {
		hook(true)
	}
}
```

In `NewRegistry`, after `r.notify` exists and `r.osd` is set:

```go
	// Every DND toggle runs under Registry.mu; Show takes it, so the OSD is
	// shown from its own goroutine once the toggler unlocks.
	r.notify.onDND = func(on bool) { go r.OSD().Show(OSDView{Kind: osdDND, On: on}) }
```

A preset expiring on its own is not a toggle and is not announced (no code path observes it); note this in the commit body.

- [ ] **Step 5: Run and commit**

Run: `GOMAXPROCS=4 GOWORK=off go test -p 2 -count=1 -race ./internal/services ./internal/shell -run 'Media|DND|Seek'` — PASS (the `-race` run confirms no new race beyond `sysc-566`'s known setup race; if that one fires, confirm it also fires on a clean `origin/main`).

```bash
git add internal/services internal/shell
git commit -m "feat(osd): announce media transport changes and do-not-disturb toggles"
```

### Task 7: Drag-to-expand toast cards

**Files:**
- Modify: `internal/shell/notifyactions.go` (`classifyToastDrag`, `toggleExpand` in `notifyActions`, `release`)
- Create: `internal/shell/notifywrap.go`, `internal/shell/notifywrap_test.go`
- Modify: `internal/shell/notifycard.go` (`notificationTree` takes a `wrap` func; `ExpandedNotificationCard`)
- Modify: `internal/shell/toasthost.go` (`expanded map[uint32]bool`; `toggleExpand`; `cardFor`)
- Modify: `internal/shell/notifyactions_test.go` (`resolverHarness.toggleExpand`)
- Test: `internal/shell/notifyactions_test.go`, `internal/shell/toasthost_test.go`

**Interfaces:**
- Produces:

```go
type toastGesture uint8
const (
	gestureNone toastGesture = iota
	gestureDismiss
	gestureExpand
)
const expandThreshold = 12
// dx is leftward travel (pressX - x); dy is downward travel (y - pressY).
func classifyToastDrag(dx, dy, width int) toastGesture
func wrapLines(s string, width int, measure func(string) int, maxLines int) []string
func ExpandedNotificationCard(n protocol.Notification, lt *protocol.Lifetime, raster *ui.Image, allowLinks bool, wrap func(string) []string) *ui.Node
// notifyActions gains: toggleExpand(id uint32)
```

- [ ] **Step 1: Write the failing tests**

```go
// Review focus 4.
func TestClassifyToastDrag(t *testing.T) {
	const w = 360
	cases := []struct {
		name   string
		dx, dy int
		want   toastGesture
	}{
		{"click", 2, 3, gestureNone},
		{"short left swipe", 60, 0, gestureNone},
		{"committed left swipe", 130, 10, gestureDismiss},
		{"right swipe never dismisses", -200, 0, gestureNone},
		{"down drag expands", 3, 20, gestureExpand},
		{"up drag toggles too", 0, -20, gestureExpand},
		{"under the expand threshold", 0, 11, gestureNone},
		{"mostly vertical beats a 35% horizontal", 130, 140, gestureExpand},
		{"mostly horizontal is today's dismiss", 140, 130, gestureDismiss},
	}
	for _, tc := range cases {
		if got := classifyToastDrag(tc.dx, tc.dy, w); got != tc.want {
			t.Errorf("%s: %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestWrapLines(t *testing.T) {
	m := func(s string) int { return len(s) * 10 }
	got := wrapLines("the quick brown fox jumps over the lazy dog", 100, m, 8)
	want := []string{"the quick", "brown fox", "jumps over", "the lazy", "dog"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("got %q", got)
	}
	long := wrapLines("abcdefghijklmnopqrstuvwxyz", 100, m, 8)
	if long[0] != "abcdefghij" || long[1] != "klmnopqrst" {
		t.Fatalf("hard break: %q", long)
	}
	capped := wrapLines(strings.Repeat("word ", 40), 100, m, 3)
	if len(capped) != 3 || !strings.HasSuffix(capped[2], "…") {
		t.Fatalf("cap: %q", capped)
	}
}
```

(Add `"strings"` to the test imports.)

Add to `resolverHarness` in `notifyactions_test.go`: `expanded []uint32` and `func (h *resolverHarness) toggleExpand(id uint32) { h.expanded = append(h.expanded, id) }`, and a resolver test:

```go
func TestVerticalDragOnACardTogglesExpand(t *testing.T) {
	h := &resolverHarness{}
	r := newNotifyResolver(h)
	root := NotificationCard(protocol.Notification{ID: 7, Summary: "Hello", Body: "World"}, nil, nil, false)
	if err := ui.LayoutColumn(root, ui.Rect{W: 360, H: 120}, func(s string, _ ui.TextAttrs) (int, int) { return len(s) * 8, 16 }); err != nil {
		t.Fatal(err)
	}
	r.press(root, 100, 20)
	r.release(root, 102, 50)
	if len(h.expanded) != 1 || h.expanded[0] != 7 || len(h.dismissed) != 0 {
		t.Fatalf("expanded %v dismissed %v", h.expanded, h.dismissed)
	}
}
```

(Use the harness's actual dismissed-slice field name; `grep -n 'func (h \*resolverHarness) dismiss' internal/shell/notifyactions_test.go`.)

- [ ] **Step 2: Run to verify they fail**

Run: `GOWORK=off go test ./internal/shell -run 'ClassifyToastDrag|WrapLines|VerticalDragOnACard' -count=1`
Expected: FAIL.

- [ ] **Step 3: Implement the gesture**

In `notifyactions.go`:

```go
const expandThreshold = 12

type toastGesture uint8

const (
	gestureNone toastGesture = iota
	gestureDismiss
	gestureExpand
)

// classifyToastDrag decides what a press-to-release drag on a card means.
// Horizontal dominance keeps today's rule exactly: a leftward drag past
// swipeCommitFraction of the card dismisses. A vertical drag past
// expandThreshold toggles the card's expanded state.
func classifyToastDrag(dx, dy, width int) toastGesture {
	adx, ady := dx, dy
	if adx < 0 {
		adx = -adx
	}
	if ady < 0 {
		ady = -ady
	}
	if ady > adx {
		if ady > expandThreshold {
			return gestureExpand
		}
		return gestureNone
	}
	if width > 0 && float64(dx) > swipeCommitFraction*float64(width) {
		return gestureDismiss
	}
	return gestureNone
}
```

Replace the first block of `release` (the `if dx := r.pressX - x; ... dismiss` block) with:

```go
	switch classifyToastDrag(r.pressX-x, y-r.pressY, r.cardWidth) {
	case gestureDismiss:
		if id, ok := cardID(root); ok {
			r.actions.dismiss(id)
		}
		return
	case gestureExpand:
		if id, ok := cardID(root); ok {
			r.actions.toggleExpand(id)
		}
		return
	}
```

Keep the swipe-slop return that follows. Add `toggleExpand(id uint32)` to the `notifyActions` interface.

- [ ] **Step 4: Implement wrapping and the expanded card**

`internal/shell/notifywrap.go`:

```go
package shell

import (
	"strings"
	"unicode/utf8"
)

// wrapLines breaks s into lines no wider than width, at spaces where it can
// and between runes where a single word is wider than the line. More than
// maxLines ends the last line with an ellipsis. ui text nodes do not wrap,
// so an expanded toast is built from these lines.
func wrapLines(s string, width int, measure func(string) int, maxLines int) []string {
	var lines []string
	line := ""
	flush := func() { lines = append(lines, line); line = "" }
	for _, word := range strings.Fields(s) {
		candidate := word
		if line != "" {
			candidate = line + " " + word
		}
		if measure(candidate) <= width {
			line = candidate
			continue
		}
		if line != "" {
			flush()
		}
		for measure(word) > width {
			cut := len(word)
			for cut > 0 && measure(word[:cut]) > width {
				_, size := utf8.DecodeLastRuneInString(word[:cut])
				cut -= size
			}
			if cut == 0 {
				_, cut = utf8.DecodeRuneInString(word)
			}
			line = word[:cut]
			flush()
			word = word[cut:]
		}
		line = word
	}
	if line != "" {
		flush()
	}
	if maxLines > 0 && len(lines) > maxLines {
		lines = lines[:maxLines]
		lines[maxLines-1] += "…"
	}
	return lines
}
```

In `notifycard.go`, add a final parameter `wrap func(string) []string` to `notificationTree`; inside the body-run loop, when `wrap != nil`, append one `KindText` per line of `wrap(run.Text)` (same styling fields as the run, link action on each line of a link run) instead of one node. Pass `nil` from every existing caller (`NotificationCard`, `HistoryCard`, `ActiveGroupCard`). Add:

```go
// ExpandedNotificationCard is a toast after a vertical drag: the same card
// with its body wrapped over several lines instead of one truncated line.
func ExpandedNotificationCard(n protocol.Notification, lt *protocol.Lifetime, raster *ui.Image, allowLinks bool, wrap func(string) []string) *ui.Node
```

built exactly like `NotificationCard` but passing `wrap` to `notificationTree`. (Factor the shared body into an unexported `notificationCard(n, lt, raster, allowLinks, wrap)` that both exported functions call.)

In `toasthost.go`: add `expanded map[uint32]bool` (initialised in `newToastHost`); implement

```go
func (h *toastHost) toggleExpand(id uint32) {
	if h.expanded[id] {
		delete(h.expanded, id)
	} else {
		h.expanded[id] = true
	}
	h.recompute()
}
```

`recompute` is how card heights and the stack are rebuilt today; confirm it re-measures `cardHeight` and republishes. In `cardFor`, when `h.expanded[id]`, return `ExpandedNotificationCard(..., h.wrapBody)` where

```go
func (h *toastHost) wrapBody(s string) []string {
	measure := h.measureText()
	width := toastCardWidth - 2*cardPadding - notifyIconSlot - cardGap
	return wrapLines(s, width, func(t string) int { w, _ := measure(t, ui.TextAttrs{}); return w }, 8)
}
```

(Use the real constants for the card's text column width; `grep -n 'toastCardWidth\|iconSlot\|cardPadding' internal/shell/*.go` shows them. If the icon slot has no named width, add `const notifyIconSlot = <its width>` beside `iconSlot` and use it in both places.) When a notification leaves the active set, delete it from `expanded` in `recompute`.

- [ ] **Step 5: Run and commit**

Run: `GOMAXPROCS=4 GOWORK=off go test -p 2 -count=1 ./internal/shell -run 'Toast|Notify|Notification|Wrap|Classify'` — PASS, including every pre-existing swipe test unchanged.

```bash
git add internal/shell
git commit -m "feat(notify): a vertical drag expands a toast; horizontal swipe is unchanged"
```

### Task 8: The stack slides closed after a dismiss

**Files:**
- Modify: `internal/shell/toasthost.go` (`anim`, `shown`, `from` maps; `rebuild`; `render`; publish loop)
- Test: `internal/shell/toastslide_test.go` (new)

**Interfaces:**
- Consumes: `ui.LerpRect(from, to ui.Rect, progress float64) ui.Rect` (`internal/ui/transition.go`), `newAnimator`, `animVisible`, `animateSurface`.
- Produces:

```go
func toastSlideKey(id uint32) string // "toast-slide:<id>"
func (h *toastHost) displayRect(id uint32, target ui.Rect) ui.Rect
func (h *toastHost) noteTargets(ids []uint32, rects []ui.Rect) (moved bool)
```

- [ ] **Step 1: Write the failing tests**

```go
package shell

import (
	"testing"
	"time"

	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

func slideHost(t *testing.T, now *time.Time) *toastHost {
	t.Helper()
	h := &toastHost{shown: map[uint32]ui.Rect{}, from: map[uint32]ui.Rect{}}
	h.anim = newAnimator(func() time.Time { return *now }, false, testMotion())
	return h
}

func TestNewCardsDoNotSlide(t *testing.T) {
	now := time.Unix(100, 0)
	h := slideHost(t, &now)
	if h.noteTargets([]uint32{1}, []ui.Rect{{Y: 10, W: 300, H: 80}}) {
		t.Fatal("a new card reported movement")
	}
	if got := h.displayRect(1, ui.Rect{Y: 10, W: 300, H: 80}); got.Y != 10 {
		t.Fatalf("new card drawn at %d", got.Y)
	}
}

func TestDismissSlidesTheRestIntoPlace(t *testing.T) {
	now := time.Unix(100, 0)
	h := slideHost(t, &now)
	h.noteTargets([]uint32{1, 2}, []ui.Rect{{Y: 10, W: 300, H: 80}, {Y: 100, W: 300, H: 80}})
	if !h.noteTargets([]uint32{2}, []ui.Rect{{Y: 10, W: 300, H: 80}}) {
		t.Fatal("card 2 moved up but no movement reported")
	}
	if got := h.displayRect(2, ui.Rect{Y: 10, W: 300, H: 80}); got.Y != 100 {
		t.Fatalf("at the start card 2 is drawn at %d, want its old 100", got.Y)
	}
	now = now.Add(time.Second)
	if got := h.displayRect(2, ui.Rect{Y: 10, W: 300, H: 80}); got.Y != 10 {
		t.Fatalf("settled card 2 at %d, want 10", got.Y)
	}
}

// Review focus 5.
func TestSlideRetargetsFromTheDrawnRect(t *testing.T) {
	now := time.Unix(100, 0)
	h := slideHost(t, &now)
	h.noteTargets([]uint32{3}, []ui.Rect{{Y: 200, W: 300, H: 80}})
	h.noteTargets([]uint32{3}, []ui.Rect{{Y: 100, W: 300, H: 80}})
	now = now.Add(50 * time.Millisecond)
	mid := h.displayRect(3, ui.Rect{Y: 100, W: 300, H: 80})
	h.noteTargets([]uint32{3}, []ui.Rect{{Y: 10, W: 300, H: 80}})
	if got := h.displayRect(3, ui.Rect{Y: 10, W: 300, H: 80}); got.Y != mid.Y {
		t.Fatalf("retarget jumped from %d to %d", mid.Y, got.Y)
	}
}

func TestReducedMotionNeverSlides(t *testing.T) {
	now := time.Unix(100, 0)
	h := &toastHost{shown: map[uint32]ui.Rect{}, from: map[uint32]ui.Rect{}}
	h.anim = newAnimator(func() time.Time { return now }, true, testMotion())
	h.noteTargets([]uint32{1}, []ui.Rect{{Y: 100}})
	h.noteTargets([]uint32{1}, []ui.Rect{{Y: 10}})
	if got := h.displayRect(1, ui.Rect{Y: 10}); got.Y != 10 {
		t.Fatalf("reduced motion drew %d", got.Y)
	}
}
```

`testMotion()` is whatever `internal/shell` tests already use to build a `render.MotionSet` (search `newAnimator(` in `*_test.go`); if none exists, use `DefaultTheme().Motion`.

- [ ] **Step 2: Run to verify they fail**

Run: `GOWORK=off go test ./internal/shell -run 'NewCardsDoNotSlide|DismissSlides|SlideRetargets|ReducedMotionNeverSlides' -count=1`
Expected: FAIL — `noteTargets` undefined.

- [ ] **Step 3: Implement**

Add to `toastHost`: `anim *animator`, `shown map[uint32]ui.Rect` (last target), `from map[uint32]ui.Rect` (where the current slide started), `stopSlide chan struct{}`, `sliding bool`. In `newToastHost`, initialise the maps and `h.anim = newAnimator(nil, r.cfg.Accessibility.ReducedMotion, r.panelTheme().Motion)` (read the theme under the same lock the constructor runs under).

```go
func toastSlideKey(id uint32) string { return fmt.Sprintf("toast-slide:%d", id) }

// displayRect is where a card is drawn this frame: on its way from where it
// was to its slot, or in its slot once settled.
func (h *toastHost) displayRect(id uint32, target ui.Rect) ui.Rect {
	from, ok := h.from[id]
	if !ok || h.anim == nil {
		return target
	}
	p := h.anim.Value(toastSlideKey(id), animVisible)
	if p >= 1 {
		delete(h.from, id)
		return target
	}
	return ui.LerpRect(from, target, p)
}

// noteTargets records each visible card's new slot. A card whose slot moved
// starts a slide from where it is drawn now, so a second dismiss mid-slide
// continues smoothly. New cards take their slot directly.
func (h *toastHost) noteTargets(ids []uint32, rects []ui.Rect) (moved bool) {
	seen := make(map[uint32]bool, len(ids))
	for i, id := range ids {
		if i >= len(rects) {
			break
		}
		seen[id] = true
		old, ok := h.shown[id]
		if ok && old != rects[i] {
			h.from[id] = h.displayRect(id, old)
			key := toastSlideKey(id)
			h.anim.Reset(key, animVisible)
			h.anim.Target(key, animVisible, 1)
			moved = true
		}
		h.shown[id] = rects[i]
	}
	for id := range h.shown {
		if !seen[id] {
			delete(h.shown, id)
			delete(h.from, id)
			h.anim.Forget(toastSlideKey(id))
		}
	}
	return moved
}
```

Check `animator.Reset`'s semantics before relying on it (`internal/shell/animation.go:443`): the slide needs the channel at 0 immediately and then easing to 1. If `Reset` removes the key so `Value` reads 0, the code above is right; if it sets the value to its target, write the channel to 0 with the animator's own API instead and note which in the commit body. With `reduced == true`, `Target` settles at once and `displayRect` returns the target — that is what `TestReducedMotionNeverSlides` pins.

In `rebuild`, after computing `rects`, call `moved := h.noteTargets(ids[:len(rects)], rects)`; if `moved && !h.sliding`, start a publish loop modelled on `OSDManager.revealLoop`:

```go
	if moved && !h.sliding {
		h.sliding = true
		h.stopSlide = make(chan struct{})
		frameCap := h.anim.frameCap()
		go animateSurface(h.stopSlide, func() bool {
			h.r.mu.Lock()
			defer h.r.mu.Unlock()
			if h.anim.Settled() {
				h.sliding = false
				return true
			}
			return false
		}, func() {
			for _, connector := range h.outputOrder() {
				h.r.publishSurface(h.outputs[connector], toastSurfaceID(connector))
			}
		}, func() time.Duration { return frameCap })
	}
```

(`rebuild` runs with `r.mu` held; the loop takes the lock itself. If `outputOrder`/`outputs` need the lock, take it inside the publish func and release it before publishing, as `revealLoop` does.)

In `render`, paint each card at `h.displayRect(id, card.rect)` where `id, _ := cardID(card.root)`: pass a copy of `card` with `rect` replaced to `paintCard`.

- [ ] **Step 4: Run and commit**

Run: `GOMAXPROCS=4 GOWORK=off go test -p 2 -count=1 ./internal/shell -run 'Toast|Slide|Notify'` — PASS.

```bash
git add internal/shell/toasthost.go internal/shell/toastslide_test.go
git commit -m "feat(notify): the toast stack slides into a dismissed card's slot"
```

### Task 9: Group badge pop and empty states

**Files:**
- Create: `internal/shell/badgepop.go`, `internal/shell/badgepop_test.go`
- Modify: `internal/shell/notifycard.go` (badge `Key`), `internal/shell/panelhost.go` (`rebuildPanel` hook; `render` paint-copy scale), `internal/shell/popout_notifications.go` (empty state), `internal/shell/controlcenter_pages.go` (empty state copy and tone)
- Modify: `internal/ui/transition.go` (`ScaleRectAbout`)

**Interfaces:**
- Produces:

```go
// internal/ui
func ScaleRectAbout(r Rect, s float64) Rect
// internal/shell
const badgeKeyPrefix = "notify-badge:"
func badgePopScale(v float64) float64 // 1 + 0.15*sin(pi*v)
func (h *PanelHost) noteBadges(root *ui.Node)   // after rebuild: start a pop where a count rose
func (h *PanelHost) applyBadgePop(root *ui.Node) // on the paint copy: scale popping badges
// PanelHost gains: badgeCounts map[string]int
```

- [ ] **Step 1: Write the failing tests**

```go
package shell

import (
	"math"
	"testing"

	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

func TestScaleRectAboutKeepsTheCentre(t *testing.T) {
	got := ui.ScaleRectAbout(ui.Rect{X: 10, Y: 20, W: 40, H: 20}, 1.5)
	if got != (ui.Rect{X: 0, Y: 15, W: 60, H: 30}) {
		t.Fatalf("got %+v", got)
	}
}

func TestBadgePopScalePeaksMidway(t *testing.T) {
	if badgePopScale(0) != 1 || math.Abs(badgePopScale(1)-1) > 1e-9 || math.Abs(badgePopScale(0.5)-1.15) > 1e-9 {
		t.Fatalf("scale 0/0.5/1 = %v %v %v", badgePopScale(0), badgePopScale(0.5), badgePopScale(1))
	}
}

func TestBadgePopsOnlyWhenTheCountRises(t *testing.T) {
	h := &PanelHost{anim: newAnimator(nil, false, testMotion())}
	badge := func(n string) *ui.Node {
		return &ui.Node{Kind: ui.KindColumn, Children: []*ui.Node{{
			Kind: ui.KindCapsule, Key: badgeKeyPrefix + "app", Children: []*ui.Node{{Kind: ui.KindText, Text: n}},
		}}}
	}
	h.noteBadges(badge("2"))
	if !h.anim.Settled() {
		t.Fatal("first sighting popped")
	}
	h.noteBadges(badge("3"))
	if h.anim.Settled() {
		t.Fatal("rise from 2 to 3 did not pop")
	}
	h.anim = newAnimator(nil, false, testMotion())
	h.noteBadges(badge("1"))
	if !h.anim.Settled() {
		t.Fatal("a fall popped")
	}
}

func TestEmptyNotificationCentreShowsIconAndMutedCopy(t *testing.T) {
	r := newPanelRegistry(t)
	r.mu.Lock()
	tree := r.centerTree()
	r.mu.Unlock()
	if !treeHasText(tree, "No notifications") || treeHasText(tree, "Nothing to see here") {
		t.Fatal("centre empty state copy")
	}
}
```

- [ ] **Step 2: Run to verify they fail**

Run: `GOWORK=off go test ./internal/shell -run 'ScaleRectAbout|BadgePop|EmptyNotificationCentre' -count=1`
Expected: FAIL.

- [ ] **Step 3: Implement**

`internal/ui/transition.go`:

```go
// ScaleRectAbout grows or shrinks r by s about its centre.
func ScaleRectAbout(r Rect, s float64) Rect {
	w := int(math.Round(float64(r.W) * s))
	h := int(math.Round(float64(r.H) * s))
	return Rect{X: r.X + (r.W-w)/2, Y: r.Y + (r.H-h)/2, W: w, H: h}
}
```

(Add `"math"` if the file lacks it.)

`internal/shell/badgepop.go`:

```go
package shell

import (
	"math"
	"strconv"
	"strings"

	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

const badgeKeyPrefix = "notify-badge:"

func badgePopScale(v float64) float64 { return 1 + 0.15*math.Sin(math.Pi*v) }

func badgeCount(n *ui.Node) (int, bool) {
	for _, c := range n.Children {
		if c.Kind == ui.KindText {
			v, err := strconv.Atoi(strings.TrimPrefix(c.Text, "+"))
			return v, err == nil
		}
	}
	return 0, false
}

func walkBadges(n *ui.Node, fn func(*ui.Node)) {
	if n == nil {
		return
	}
	if strings.HasPrefix(n.Key, badgeKeyPrefix) {
		fn(n)
	}
	for _, c := range n.Children {
		walkBadges(c, fn)
	}
}

// noteBadges runs after a rebuild. A badge whose count rose since the last
// rebuild starts one 0-to-1 run on its progress channel; the first sighting
// and a fall do not.
func (h *PanelHost) noteBadges(root *ui.Node) {
	if h.anim == nil {
		return
	}
	if h.badgeCounts == nil {
		h.badgeCounts = map[string]int{}
	}
	seen := map[string]bool{}
	walkBadges(root, func(n *ui.Node) {
		count, ok := badgeCount(n)
		if !ok {
			return
		}
		seen[n.Key] = true
		if prev, had := h.badgeCounts[n.Key]; had && count > prev {
			h.anim.Reset(n.Key, animProgress)
			h.anim.Target(n.Key, animProgress, 1)
		}
		h.badgeCounts[n.Key] = count
	})
	for k := range h.badgeCounts {
		if !seen[k] {
			delete(h.badgeCounts, k)
		}
	}
}

// applyBadgePop swells a popping badge on the paint copy. Layout is
// untouched: only the drawn rectangles grow about their centre.
func (h *PanelHost) applyBadgePop(root *ui.Node) {
	if h.anim == nil {
		return
	}
	walkBadges(root, func(n *ui.Node) {
		v := h.anim.Value(n.Key, animProgress)
		if v <= 0 || v >= 1 {
			return
		}
		s := badgePopScale(v)
		n.Bounds = ui.ScaleRectAbout(n.Bounds, s)
		for _, c := range n.Children {
			c.Bounds = ui.ScaleRectAbout(c.Bounds, s)
		}
	})
}
```

Apply the same `Reset` caveat as Task 8: confirm the channel restarts from 0.

Add `badgeCounts map[string]int` to `PanelHost`. In `ActiveGroupCard` give the count capsule `Key: badgeKeyPrefix + g.key`. In `Registry.rebuildPanel`, next to `resolveProgressMotion(h.anim, probe)`, call `h.noteBadges(h.root)`. In `PanelHost.render`, after `root := copyNode(h.root)` (and after Task-free code already there), call `h.applyBadgePop(root)`.

Empty states. In `popout_notifications.go`, replace `body = append(body, &ui.Node{Kind: ui.KindText, Text: "Nothing to see here"})` with:

```go
		body = append(body, &ui.Node{Kind: ui.KindColumn, Gap: cardGap, Padding: theme.MarginL, Children: []*ui.Node{
			{Kind: ui.KindIcon, Icon: "notifications", IconSize: centreIconSize, Tone: ui.ToneSubtle, CenterX: true},
			{Kind: ui.KindText, Text: "No notifications", Tone: ui.ToneSubtle, CenterX: true},
		}})
```

In `controlcenter_pages.go`, change the empty card's text to `"No notifications"` and add `Tone: ui.ToneSubtle` to both its icon and text. Update any test asserting "Nothing to see here".

- [ ] **Step 4: Run and commit**

Run: `GOMAXPROCS=4 GOWORK=off go test -p 2 -count=1 ./internal/ui ./internal/shell` — PASS apart from failures that also fail on a clean `origin/main` (on 2026-09-27 these were `TestPanelSectionValidationPrecedesMutation` and three battery-widget tests).

```bash
git add internal/ui internal/shell
git commit -m "feat(notify): group badges pop on a new member; quiet empty states"
```

### Task 10: Gates, live check, and tracking

- [ ] **Step 1: Full capped gate**

Run: `GOMAXPROCS=4 GOWORK=off go vet ./... && GOMAXPROCS=4 GOWORK=off go test -p 2 -count=1 ./...`
Expected: PASS apart from failures proven pre-existing on a clean `origin/main` worktree; list each with the command that proved it.

- [ ] **Step 2: Live gate on Niri (laptop, per the deploy rule)**

Deploy per the rule and report `vcs.revision`/`vcs.modified`. Then, with the owner present:

1. Volume and brightness keys: icon, label, meter; at ≥ 95 % the handle shows the percentage; muted shows `volume_off` and a subtle meter.
2. Caps Lock and Num Lock from another application's window (not a shell panel): OSD appears within ~250 ms with the right on/off state. Restart the shell with Caps Lock on: no OSD at startup.
3. `niri msg action switch-layout next` with two layouts configured: the layout OSD names the new layout. No OSD when the shell starts.
4. Media: play/pause, next track, and seeking in a player each show the media OSD; a playing track's position ticking does not.
5. Toggle do-not-disturb from the control centre and from the bell: the OSD announces each toggle.
6. Toasts: a vertical drag expands a card (body wraps) and a second one collapses it; a left swipe still dismisses; dismiss the middle of three toasts and watch the lower one slide up; dismiss twice quickly — no jump.
7. Group badge: with the control centre open, send two notifications from one app — the count capsule pops.
8. Empty notification centre and empty control-centre page show the icon and "No notifications".
9. **D0 record:** hover a toast for more than six seconds — it stays; move away — it expires. Note the observation in the completion handover.
10. Reduced motion on: no slides, no pops, OSD appears without motion.

Redeploy a clean `origin/main` build afterwards per the rule.

- [ ] **Step 3: Tracking (from the primary checkout)**

```bash
cd /home/nomadx/sysc-shell
bd close sysc-592 --reason "OSD breadth and notification micro-interactions landed; live gate passed on the laptop."
```

If the live gate shows the linear collapse slide reads badly against enter/exit timings, file the spring follow-up the design names (`sysc-590` D6) with `--deps discovered-from:sysc-592` instead of closing that question here.
