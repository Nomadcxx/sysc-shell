package shell

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	launcher "github.com/Nomadcxx/sysc-launch"
	"github.com/Nomadcxx/sysc-shell/internal/icons"
	"github.com/Nomadcxx/sysc-shell/internal/platform/wayland"
	"github.com/Nomadcxx/sysc-shell/internal/render"
	"github.com/Nomadcxx/sysc-shell/internal/theme"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

func launcherTestEntries() []launcher.Entry {
	return []launcher.Entry{
		{
			ID: "firefox.desktop", Name: "Firefox", Comment: "Browse the Web", Argv: []string{"firefox"},
			Actions: []launcher.Action{{ID: "new-window", Name: "New Window", Argv: []string{"firefox", "--new-window"}}},
		},
		{ID: "foot.desktop", Name: "Foot", Comment: "Terminal emulator", Argv: []string{"foot"}},
		{ID: "nautilus.desktop", Name: "Files", Comment: "Access and organize files", Argv: []string{"nautilus"}},
	}
}

type recordedSpawn struct {
	mu   sync.Mutex
	argv []string
	err  error
}

func (r *recordedSpawn) run(_ context.Context, argv []string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.argv = append([]string(nil), argv...)
	return r.err
}

func (r *recordedSpawn) waitArgv(t *testing.T) []string {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		r.mu.Lock()
		argv := r.argv
		r.mu.Unlock()
		if argv != nil {
			return argv
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("no spawn recorded")
	return nil
}

// openLauncherPanel injects a stub-backed launcher service, opens the panel,
// lays it out at its target size, and waits for the first result publish.
func openLauncherPanel(t *testing.T, entries []launcher.Entry) (*Registry, *recordedSpawn, []wayland.AuxRequest) {
	t.Helper()
	reg := newPanelRegistry(t)
	run := &recordedSpawn{}
	svc := launcher.NewService(launcher.ServiceConfig{
		Scan: func() []launcher.Entry { return entries },
		Rank: launcherRank,
		Run:  run.run,
	})
	reg.mu.Lock()
	reg.launcherSvc = svc
	reg.mu.Unlock()
	go reg.relayLauncher(svc)

	if err := reg.OpenPanel(PanelLauncher, 7, Trigger{BarEdge: "top", BarZone: 40, OutW: 1920, OutH: 1080}); err != nil {
		t.Fatal(err)
	}
	reqs := drainAux(t, reg, 2)
	if err := reqs[1].Open.Callbacks.Configure(560, 500, 120); err != nil {
		t.Fatal(err)
	}
	waitForLauncherResults(t, reg, len(entries))
	return reg, run, reqs
}

func TestLauncherHistoryPath(t *testing.T) {
	got := launcherHistoryPath(func(key string) string {
		switch key {
		case "XDG_STATE_HOME":
			return "/tmp/xdg-state"
		case "HOME":
			return "/home/test"
		default:
			return ""
		}
	})
	want := "/tmp/xdg-state/sysc-shell/launcher/history.gob"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
	homeOnly := launcherHistoryPath(func(key string) string {
		if key == "HOME" {
			return "/home/test"
		}
		return ""
	})
	wantHome := "/home/test/.local/state/sysc-shell/launcher/history.gob"
	if homeOnly != wantHome {
		t.Fatalf("home fallback: got %q, want %q", homeOnly, wantHome)
	}
}

// launcherListNode finds the virtual list by kind. The tree gained a rail
// above it and a footer below, so its index is not a stable handle.
func launcherListNode(t *testing.T, h *PanelHost) *ui.Node {
	t.Helper()
	for _, c := range h.root.Children {
		if c.Kind == ui.KindVirtualList {
			return c
		}
	}
	t.Fatal("no virtual list in the launcher tree")
	return nil
}

func launcherHost(t *testing.T, reg *Registry) *PanelHost {
	t.Helper()
	reg.mu.Lock()
	defer reg.mu.Unlock()
	h := reg.panelHosts[PanelLauncher]
	if h == nil {
		t.Fatal("launcher panel is not hosted")
	}
	return h
}

func waitForLauncherResults(t *testing.T, reg *Registry, want int) {
	t.Helper()
	waitForLauncherState(t, reg, func(h *PanelHost) bool {
		return h != nil && len(h.launcherResults) == want
	})
}

func waitForLauncherState(t *testing.T, reg *Registry, ok func(*PanelHost) bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		reg.mu.Lock()
		done := ok(reg.panelHosts[PanelLauncher])
		reg.mu.Unlock()
		if done {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("timed out waiting for launcher panel state")
}

func walkActionBounds(n *ui.Node, action string) (ui.Rect, bool) {
	if n == nil {
		return ui.Rect{}, false
	}
	if n.Action == action && n.Bounds.W > 0 {
		return n.Bounds, true
	}
	for _, c := range n.Children {
		if b, ok := walkActionBounds(c, action); ok {
			return b, true
		}
	}
	return ui.Rect{}, false
}

func pressLauncherKey(reqs []wayland.AuxRequest, key uint32) {
	reqs[1].Open.Callbacks.Handle(wayland.Event{Kind: wayland.EventKeyPress, Key: key})
}

func TestParsePanelNameLauncher(t *testing.T) {
	t.Parallel()

	id, err := parsePanelName("launcher")
	if err != nil || id != PanelLauncher {
		t.Fatalf("parsePanelName(launcher) = %v, %v", id, err)
	}
	if got := PanelLauncher.String(); got != "launcher" {
		t.Fatalf("String() = %q", got)
	}
}

func TestLauncherPanelGeometry(t *testing.T) {
	t.Parallel()

	if got := panelTargetSize(PanelLauncher); got.W != 560 || got.H != 700 {
		t.Fatalf("target size = %dx%d, want 560x700", got.W, got.H)
	}

	reg, _, reqs := openLauncherPanel(t, launcherTestEntries())
	panel := reqs[1].Open
	if panel.Width != 560 || panel.Height != 700 {
		t.Fatalf("surface = %dx%d, want 560x700", panel.Width, panel.Height)
	}

	reg.mu.Lock()
	gap, pad := reg.cfg.Panels.Gap, reg.cfg.Panels.Padding
	reg.mu.Unlock()
	anchor := 40 + gap
	avail := 1080 - anchor - pad
	wantTop := anchor + (avail-700)/2
	if int(panel.MarginTop) != wantTop {
		t.Fatalf("margin top = %d, want vertically centred %d", panel.MarginTop, wantTop)
	}
	if panel.MarginLeft != 680 {
		t.Fatalf("margin left = %d, want horizontally centred 680", panel.MarginLeft)
	}
}

func TestLauncherSearchIsFocusedOnOpen(t *testing.T) {
	t.Parallel()

	reg, _, _ := openLauncherPanel(t, launcherTestEntries())
	h := launcherHost(t, reg)
	reg.mu.Lock()
	defer reg.mu.Unlock()
	n := h.focused()
	if n == nil || n.Kind != ui.KindTextField || n.Name != "Search" {
		t.Fatalf("focused = %+v, want the Search field", n)
	}
}

func TestLauncherSearchFieldHasChromeHeight(t *testing.T) {
	t.Parallel()

	reg, _, _ := openLauncherPanel(t, launcherTestEntries())
	h := launcherHost(t, reg)
	reg.mu.Lock()
	defer reg.mu.Unlock()
	// The rail is first; the field follows it.
	field := h.root.Children[1]
	if field.Kind != ui.KindTextField {
		t.Fatalf("second child = %v, want KindTextField", field.Kind)
	}
	if field.Bounds.H != launcherFieldHeight {
		t.Fatalf("search field height = %d, want %d", field.Bounds.H, launcherFieldHeight)
	}
}

func TestLauncherListFillsThePanel(t *testing.T) {
	t.Parallel()

	reg, _, _ := openLauncherPanel(t, launcherTestEntries())
	h := launcherHost(t, reg)
	reg.mu.Lock()
	defer reg.mu.Unlock()
	list := launcherListNode(t, h)
	bottom := list.Bounds.Y + list.Bounds.H
	wantBottom := 700 - 12 - h.launcherFooterHeight() - 8
	// 700 less the padding, rail, field and gaps leaves 576, which is 8.4
	// rows against the 6.2 the 500-tall panel showed.
	if list.Bounds.H != h.launcherListHeight() || bottom != wantBottom {
		t.Fatalf("list %+v, want height %d ending at %d",
			list.Bounds, h.launcherListHeight(), wantBottom)
	}
	if rows := list.Bounds.H / launcherSlotHeight; rows < 8 {
		t.Fatalf("only %d rows visible; the taller panel is the whole point", rows)
	}
}

func TestLauncherTreeShape(t *testing.T) {
	t.Parallel()

	reg, _, _ := openLauncherPanel(t, launcherTestEntries())
	reg.mu.Lock()
	defer reg.mu.Unlock()
	h := reg.panelHosts[PanelLauncher]

	root := h.root
	if root.Kind != ui.KindColumn {
		t.Fatalf("root kind = %v, want KindColumn", root.Kind)
	}
	if root.Children[0].Kind != ui.KindRow {
		t.Fatalf("first child kind = %v, want the SYSC rail row", root.Children[0].Kind)
	}
	if root.Children[1].Kind != ui.KindTextField {
		t.Fatalf("second child kind = %v, want KindTextField", root.Children[1].Kind)
	}
	list := launcherListNode(t, h)
	if list.ItemHeight != launcherSlotHeight {
		t.Fatalf("row height = %d, want %d", list.ItemHeight, launcherSlotHeight)
	}
	if list.ItemCount != 3 {
		t.Fatalf("item count = %d, want 3", list.ItemCount)
	}
	selected := list.Item(0)
	if selected.Kind != ui.KindColumn || selected.Padding != launcherRowGap/2 {
		t.Fatalf("selected wrapper = kind %v pad %d, want gapped KindColumn", selected.Kind, selected.Padding)
	}
	if len(selected.Children) == 0 {
		t.Fatal("selected wrapper has no capsule")
	}
	cap := selected.Children[0]
	if cap.Kind != ui.KindCapsule || cap.Fill != ui.FillSoft {
		t.Fatalf("selected row = kind %v fill %v, want KindCapsule FillSoft", cap.Kind, cap.Fill)
	}
	if !launcherRowHas(selected, "Files") || !launcherRowHas(selected, "Access and organize files") || !launcherRowHas(selected, "F") {
		t.Fatalf("selected row missing name/comment/glyph")
	}
	plain := list.Item(1)
	if plain.Kind != ui.KindColumn || len(plain.Children) == 0 {
		t.Fatalf("unselected wrapper = kind %v", plain.Kind)
	}
	if p := plain.Children[0]; p.Kind != ui.KindCapsule || p.Fill != 0 {
		t.Fatalf("unselected row = kind %v fill %v, want unfilled KindCapsule", p.Kind, p.Fill)
	}
	if !launcherRowHas(plain, "Firefox") || !launcherRowHas(plain, "Browse the Web") {
		t.Fatalf("unselected row missing name/comment")
	}
	if len(list.Children) < 2 || len(list.Children[0].Children) == 0 || len(list.Children[1].Children) == 0 {
		t.Fatal("layout produced fewer than two capsules")
	}
	a, b := list.Children[0].Children[0], list.Children[1].Children[0]
	gap := b.Bounds.Y - (a.Bounds.Y + a.Bounds.H)
	if gap < launcherRowGap/2 {
		t.Fatalf("row gap = %d, want at least %d (%+v then %+v)", gap, launcherRowGap/2, a.Bounds, b.Bounds)
	}
}

func TestLauncherLetterKeyTypesIntoSearch(t *testing.T) {
	t.Parallel()

	reg, _, reqs := openLauncherPanel(t, launcherTestEntries())
	reqs[1].Open.Callbacks.Handle(wayland.Event{Kind: wayland.EventKeyPress, Key: 33}) // F
	h := launcherHost(t, reg)
	reg.mu.Lock()
	query := h.query
	reg.mu.Unlock()
	if query != "f" {
		t.Fatalf("query = %q, want f from the F key", query)
	}
}

func TestLauncherIconFallsBackToALetter(t *testing.T) {
	t.Parallel()
	n := launcherIconNode(nil, &PanelHost{}, launcher.Entry{Name: "Firefox"})
	if n.Kind != ui.KindCapsule || !launcherRowHas(n, "F") {
		t.Fatalf("fallback icon = kind %v, want letter F in a capsule", n.Kind)
	}
}

func TestLauncherIconUsesACachedRaster(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeLauncherPNG(t, filepath.Join(root, "firefox.png"))
	worker := icons.NewWorker(icons.NewResolver("hicolor", []string{root}), nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = worker.Run(ctx) }()
	reg := &Registry{trayIcons: worker}
	h := &PanelHost{scale120: 120}
	key := icons.Square("firefox", launcherIconSlot)
	if _, _, err := worker.Request(key); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if img, ok := worker.Lookup(key); ok && img != nil {
			n := launcherIconNode(reg, h, launcher.Entry{Name: "Firefox", IconName: "firefox"})
			if n.Kind != ui.KindImage || n.Image != img {
				t.Fatalf("icon kind=%v image=%v, want KindImage cache hit", n.Kind, n.Image != nil)
			}
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("icon worker did not cache firefox")
}

func launcherRowHas(n *ui.Node, text string) bool {
	if n == nil {
		return false
	}
	if n.Text == text {
		return true
	}
	for _, c := range n.Children {
		if launcherRowHas(c, text) {
			return true
		}
	}
	return false
}

func TestLauncherTypingReprojects(t *testing.T) {
	t.Parallel()

	reg, _, reqs := openLauncherPanel(t, launcherTestEntries())
	reqs[1].Open.Callbacks.Handle(wayland.Event{Kind: wayland.EventIME, IMECommit: "nautilus"})
	waitForLauncherState(t, reg, func(h *PanelHost) bool {
		return h != nil && len(h.launcherResults) == 1 && h.launcherResults[0].Entry.Name == "Files"
	})
	h := launcherHost(t, reg)
	reg.mu.Lock()
	query := h.query
	reg.mu.Unlock()
	if query != "nautilus" {
		t.Fatalf("query = %q, want nautilus", query)
	}
}

func manyLauncherEntries(n int) []launcher.Entry {
	out := make([]launcher.Entry, n)
	for i := range out {
		out[i] = launcher.Entry{
			ID:   fmt.Sprintf("app-%02d.desktop", i),
			Name: fmt.Sprintf("App %02d", i),
			Argv: []string{"true"},
		}
	}
	return out
}

func TestLauncherWheelMovesSelectionWithoutWrapping(t *testing.T) {
	t.Parallel()

	reg, _, reqs := openLauncherPanel(t, manyLauncherEntries(12))
	for range 40 {
		reqs[1].Open.Callbacks.Handle(wayland.Event{Kind: wayland.EventPointerAxis, AxisDiscrete: 1})
	}
	reg.mu.Lock()
	sel := reg.panelHosts[PanelLauncher].launcherSel
	reg.mu.Unlock()
	if sel != 11 {
		t.Fatalf("selection after wheel = %d, want last row 11 (not wrapped to start)", sel)
	}
}

func TestLauncherArrowsClampSelection(t *testing.T) {
	t.Parallel()

	reg, _, reqs := openLauncherPanel(t, launcherTestEntries())
	for range 5 {
		pressLauncherKey(reqs, keyDown)
	}
	reg.mu.Lock()
	sel := reg.panelHosts[PanelLauncher].launcherSel
	reg.mu.Unlock()
	if sel != 2 {
		t.Fatalf("selection after 5 downs = %d, want clamped 2", sel)
	}
	pressLauncherKey(reqs, keyUp)
	pressLauncherKey(reqs, keyUp)
	pressLauncherKey(reqs, keyUp)
	reg.mu.Lock()
	sel = reg.panelHosts[PanelLauncher].launcherSel
	reg.mu.Unlock()
	if sel != 0 {
		t.Fatalf("selection after 3 ups = %d, want clamped 0", sel)
	}
}

func TestLauncherEnterActivatesSelectedEntry(t *testing.T) {
	t.Parallel()

	reg, run, reqs := openLauncherPanel(t, launcherTestEntries())
	pressLauncherKey(reqs, keyDown)
	pressLauncherKey(reqs, keyEnter)
	if got := run.waitArgv(t); !slices.Equal(got, []string{"niri", "msg", "action", "spawn", "--", "firefox"}) {
		t.Fatalf("spawn argv = %v", got)
	}
	waitForLauncherState(t, reg, func(h *PanelHost) bool { return h == nil })
}

func TestLauncherEnterOnOverviewRowNavigates(t *testing.T) {
	t.Parallel()

	reg, _, reqs := openLauncherPanel(t, launcherTestEntries())
	reqs[1].Open.Callbacks.Handle(wayland.Event{Kind: wayland.EventIME, IMECommit: "/ap"})
	waitForLauncherState(t, reg, func(h *PanelHost) bool {
		return h != nil && len(h.launcherResults) == 1 && h.launcherResults[0].Entry.ID == "/apps"
	})
	pressLauncherKey(reqs, keyEnter)
	waitForLauncherState(t, reg, func(h *PanelHost) bool {
		return h != nil && h.query == "/apps" && len(h.launcherResults) == 3
	})
}

func TestLauncherRightClickOpensActionsMenu(t *testing.T) {
	t.Parallel()

	reg, run, reqs := openLauncherPanel(t, launcherTestEntries())
	h := launcherHost(t, reg)
	reg.mu.Lock()
	bounds, ok := walkActionBounds(h.root, "launch:firefox.desktop")
	reg.mu.Unlock()
	if !ok {
		t.Fatal("firefox row has no laid-out bounds")
	}

	reqs[1].Open.Callbacks.Handle(wayland.Event{
		Kind:   wayland.EventPointerPress,
		Button: btnRight,
		X:      float64(bounds.X + 4),
		Y:      float64(bounds.Y + 4),
	})
	reg.mu.Lock()
	menuOpen := reg.panelHosts[PanelLauncher].menu.Opened()
	reg.mu.Unlock()
	if !menuOpen {
		t.Fatal("right-click did not open the actions menu")
	}

	pressLauncherKey(reqs, keyEnter)
	if got := run.waitArgv(t); !slices.Equal(got, []string{"niri", "msg", "action", "spawn", "--", "firefox", "--new-window"}) {
		t.Fatalf("action argv = %v", got)
	}
	waitForLauncherState(t, reg, func(h *PanelHost) bool { return h == nil })
}

func TestLauncherEscapeCloses(t *testing.T) {
	t.Parallel()

	reg, _, reqs := openLauncherPanel(t, launcherTestEntries())
	pressLauncherKey(reqs, keyEsc)
	_ = drainAux(t, reg, 2)
	reg.mu.Lock()
	_, hosted := reg.panelHosts[PanelLauncher]
	reg.mu.Unlock()
	if hosted {
		t.Fatal("escape left the launcher hosted")
	}
}

func TestLauncherSpawnFailureKeepsPanelOpen(t *testing.T) {
	t.Parallel()

	reg, run, reqs := openLauncherPanel(t, launcherTestEntries())
	run.err = errors.New("niri refused")
	pressLauncherKey(reqs, keyEnter)
	waitForLauncherState(t, reg, func(h *PanelHost) bool {
		return h != nil && h.errLabel != ""
	})
}

func writeLauncherPNG(t *testing.T, path string) {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 8, 8))
	for y := range 8 {
		for x := range 8 {
			img.Set(x, y, color.RGBA{R: 0x20, G: 0x80, B: 0xe0, A: 0xff})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
}

// The live desktop has 482 desktop entries and the browse list used to arrive
// capped at 50, which ends in the E's. End must reach the last of them.
func TestLauncherBrowseListsEveryEntry(t *testing.T) {
	t.Parallel()

	entries := alphabetEntries(482)
	reg, _, reqs := openLauncherPanel(t, entries)
	pressLauncherKey(reqs, keyEnd)

	h := launcherHost(t, reg)
	reg.mu.Lock()
	defer reg.mu.Unlock()
	if got := len(h.launcherResults); got != len(entries) {
		t.Fatalf("browse list holds %d of %d entries", got, len(entries))
	}
	last := h.launcherResults[h.launcherSel]
	if h.launcherSel != len(entries)-1 {
		t.Fatalf("End selected row %d, want %d", h.launcherSel, len(entries)-1)
	}
	if last.Entry.Name[:1] != "Z" {
		t.Fatalf("End landed on %q, want a Z entry", last.Entry.Name)
	}
}

// A panel clears its buffer and fills a rounded body, so its corners are
// transparent. The opaque region is a promise to the compositor that it may
// skip blending, so it has to exclude those corners -- claiming the whole
// rectangle composites the cleared pixels straight out and the corners read as
// black squares behind the border instead of wallpaper. The aux path used to
// hardcode radius 0, which made that claim for every panel in the shell.
func TestPanelSurfaceCarriesItsCornerRadius(t *testing.T) {
	t.Parallel()

	_, _, reqs := openLauncherPanel(t, launcherTestEntries())
	panel := reqs[1].Open
	if panel == nil {
		t.Fatal("no panel surface was opened")
	}
	if got := panel.Callbacks.Radius; got <= 0 {
		t.Fatalf("panel Radius = %d, want the painted corner radius so the "+
			"opaque region can exclude the corners", got)
	}
	if !panel.Callbacks.OpaqueBackground {
		t.Skip("opaque background is off, so no opaque region is set at all")
	}
}

// The rail is the SYSC mark between two six-slash runs, centred. It is the
// panel's title, so the slashes take RoleTitle and the accent tone.
func TestLauncherRailIsTheSyscMark(t *testing.T) {
	t.Parallel()

	reg, _, _ := openLauncherPanel(t, launcherTestEntries())
	h := launcherHost(t, reg)
	reg.mu.Lock()
	defer reg.mu.Unlock()

	rail := h.root.Children[0]
	if rail.Kind != ui.KindRow || !rail.CenterX {
		t.Fatalf("rail kind = %v centred = %v, want a centred KindRow", rail.Kind, rail.CenterX)
	}
	if len(rail.Children) != 3 {
		t.Fatalf("rail has %d children, want slashes, mark, slashes", len(rail.Children))
	}
	for _, i := range []int{0, 2} {
		side := rail.Children[i]
		if side.Kind != ui.KindText || side.Text != "//////" {
			t.Fatalf("rail[%d] = %v %q, want six slashes", i, side.Kind, side.Text)
		}
		if side.Tone != ui.ToneAccent {
			t.Fatalf("rail[%d] tone = %v, want ToneAccent", i, side.Tone)
		}
	}
	mark := rail.Children[1]
	if mark.Kind != ui.KindWordmark {
		t.Fatalf("rail centre = %v, want KindWordmark", mark.Kind)
	}
	if mark.ImageH != launcherMarkHeight || mark.ImageW != render.WordmarkWidth(launcherMarkHeight) {
		t.Fatalf("mark box = %dx%d, want %dx%d from the asset's aspect",
			mark.ImageW, mark.ImageH, render.WordmarkWidth(launcherMarkHeight), launcherMarkHeight)
	}

	// Centred means centred: equal slack either side of the laid-out rail.
	left := rail.Bounds.X - h.root.Bounds.X - 12
	right := (h.root.Bounds.X + h.root.Bounds.W - 12) - (rail.Bounds.X + rail.Bounds.W)
	if diff := left - right; diff > 1 || diff < -1 {
		t.Fatalf("rail slack is %d left and %d right; it is not centred", left, right)
	}
}

// The chrome above the list is measured, not assumed. A constant header height
// left a dead strip along the bottom of the panel when the row laid out
// shorter than the guess.
func TestLauncherListReachesThePanelFloor(t *testing.T) {
	t.Parallel()

	reg, _, _ := openLauncherPanel(t, alphabetEntries(60))
	h := launcherHost(t, reg)
	reg.mu.Lock()
	defer reg.mu.Unlock()

	footer := h.root.Children[len(h.root.Children)-1]
	if got, want := footer.Bounds.Y+footer.Bounds.H, h.place.Panel.H-12; got != want {
		t.Fatalf("footer ends at %d, want the panel floor %d; the chrome "+
			"estimate and the laid-out chrome disagree", got, want)
	}
}

// The footer is DMS's "keyboard hints at the bottom", carrying sysc-greet's
// own help line so the two surfaces read as one family.
func TestLauncherFooterCountsAndHints(t *testing.T) {
	t.Parallel()

	reg, _, _ := openLauncherPanel(t, alphabetEntries(199))
	h := launcherHost(t, reg)
	reg.mu.Lock()
	defer reg.mu.Unlock()

	footer := h.root.Children[len(h.root.Children)-1]
	if footer.Kind != ui.KindText || !footer.CenterX {
		t.Fatalf("footer = %v centred %v, want centred text", footer.Kind, footer.CenterX)
	}
	if footer.TextRole != theme.RoleCaption {
		t.Fatalf("footer role = %v, want RoleCaption", footer.TextRole)
	}
	if !strings.HasPrefix(footer.Text, "199 apps") {
		t.Fatalf("footer = %q, want it to open with the browse count", footer.Text)
	}
	if !strings.Contains(footer.Text, launcherHints) {
		t.Fatalf("footer = %q, want sysc-greet's help line", footer.Text)
	}
}

// Browsing counts apps; searching counts results. The noun has to follow the
// query or the footer states something false the moment you type.
func TestLauncherFooterNounFollowsTheQuery(t *testing.T) {
	t.Parallel()

	reg, _, reqs := openLauncherPanel(t, launcherTestEntries())
	handle := reqs[1].Open.Callbacks.Handle
	handle(wayland.Event{Kind: wayland.EventKeyPress, Key: 33}) // F
	waitForLauncherState(t, reg, func(h *PanelHost) bool {
		return strings.TrimSpace(h.query) != ""
	})

	h := launcherHost(t, reg)
	reg.mu.Lock()
	defer reg.mu.Unlock()
	footer := h.root.Children[len(h.root.Children)-1]
	if !strings.Contains(footer.Text, "results") {
		t.Fatalf("footer = %q, want it to say results while searching", footer.Text)
	}
}

// With nothing matched the hints matter most, so the footer stays.
func TestLauncherFooterSurvivesTheEmptyState(t *testing.T) {
	t.Parallel()

	reg, _, _ := openLauncherPanel(t, nil)
	h := launcherHost(t, reg)
	reg.mu.Lock()
	defer reg.mu.Unlock()
	footer := h.root.Children[len(h.root.Children)-1]
	if footer.Kind != ui.KindText || !strings.Contains(footer.Text, launcherHints) {
		t.Fatalf("empty state footer = %+v, want the help line", footer)
	}
}
