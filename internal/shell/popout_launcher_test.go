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
	wantBottom := 700 - theme.MarginL - h.launcherFooterHeight() - theme.MarginM
	// The floor is the panel less its bottom inset, the footer, and the gap
	// above it, named rather than spelled: this test exists to catch the
	// chrome estimate and the laid-out chrome disagreeing, so writing either
	// side as a number is what lets them drift apart unnoticed.
	//
	// At the 76-tall slot the remainder is seven whole rows and a cap of the
	// eighth. It was eight rows before the row took DMS's 12/16 padding; the
	// row grew and the panel did not, so a row had to go.
	if list.Bounds.H != h.launcherListHeight() || bottom != wantBottom {
		t.Fatalf("list %+v, want height %d ending at %d",
			list.Bounds, h.launcherListHeight(), wantBottom)
	}
	if rows := list.Bounds.H / launcherSlotHeight; rows < 7 {
		t.Fatalf("only %d rows visible; the taller panel is the whole point", rows)
	}
}

// The list is meant to end part way through a row. There is no scrollbar, so
// that clipped row is the only thing saying the list runs on past the panel
// floor -- and if the viewport ever divides evenly by the slot, the affordance
// disappears with nothing else failing.
func TestLauncherListEndsPartWayThroughARow(t *testing.T) {
	t.Parallel()

	reg, _, _ := openLauncherPanel(t, alphabetEntries(60))
	h := launcherHost(t, reg)
	reg.mu.Lock()
	defer reg.mu.Unlock()

	shown := h.launcherListHeight() % launcherSlotHeight
	if shown == 0 {
		t.Fatalf("the %d viewport is a whole number of %d rows; nothing says the list scrolls",
			h.launcherListHeight(), launcherSlotHeight)
	}
	// Enough of the row to read as a row: past its half of the gap and its top
	// padding, into the glyph. Less than that and the strip is bare background.
	if floor := launcherRowGap/2 + launcherRowPadTop; shown <= floor {
		t.Fatalf("the clipped row shows %d of %d, which is still inside its padding; want more than %d",
			shown, launcherSlotHeight, floor)
	}
}

// DMS's row padding: 12 above the text block and 16 below. ui carries one
// padding scalar per node, so the asymmetry is structural -- the capsule is
// taller than its content and a column inside it spends the difference at the
// foot. Asserted in laid-out pixels rather than against the constants, because
// dropping that column is an easy tidy-up and nothing else would notice.
func TestLauncherRowPadsTwelveAboveAndSixteenBelow(t *testing.T) {
	t.Parallel()

	reg, _, _ := openLauncherPanel(t, alphabetEntries(60))
	h := launcherHost(t, reg)
	reg.mu.Lock()
	defer reg.mu.Unlock()

	list := launcherListNode(t, h)
	if len(list.Children) < 2 {
		t.Fatal("layout produced fewer than two rows")
	}
	first, second := list.Children[0].Children[0], list.Children[1].Children[0]
	body := launcherBodyRow(t, first)
	above := body.Bounds.Y - first.Bounds.Y
	below := first.Bounds.Y + first.Bounds.H - (body.Bounds.Y + body.Bounds.H)
	if above != launcherRowPadTop || below != launcherRowPadBottom {
		t.Fatalf("row padding = %d above, %d below; want %d and %d (capsule %+v, body %+v)",
			above, below, launcherRowPadTop, launcherRowPadBottom, first.Bounds, body.Bounds)
	}
	// The other half of the design: the padding surrounds the text block, the
	// gap separates the rows. Growing one at the other's expense would keep
	// the slot height and lose the point.
	if gap := second.Bounds.Y - (first.Bounds.Y + first.Bounds.H); gap != launcherRowGap {
		t.Fatalf("gap between rows = %d, want %d", gap, launcherRowGap)
	}
}

// launcherBodyRow is the icon-and-labels row inside a laid-out row capsule.
func launcherBodyRow(t *testing.T, capsule *ui.Node) *ui.Node {
	t.Helper()
	if capsule.Kind != ui.KindCapsule {
		t.Fatalf("row wrapper holds a %v, want the capsule", capsule.Kind)
	}
	var find func(*ui.Node) *ui.Node
	find = func(n *ui.Node) *ui.Node {
		if n == nil {
			return nil
		}
		if n.Kind == ui.KindRow {
			return n
		}
		for _, c := range n.Children {
			if got := find(c); got != nil {
				return got
			}
		}
		return nil
	}
	body := find(capsule)
	if body == nil {
		t.Fatal("row capsule holds no body row")
	}
	return body
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
	if got, want := footer.Bounds.Y+footer.Bounds.H, h.place.Panel.H-theme.MarginL; got != want {
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

// TestLauncherRowHeightSurvivesAMissingCommentAndIcon pins the row's floor.
//
// A .desktop file need not carry a Comment, and an icon name need not resolve
// to a raster in the theme. When both were absent the row had nothing setting
// its height: the label column held one line instead of two, and the letter
// capsule standing in for the icon sized to that same single line. The row then
// drew at about half the height of its neighbours, which is what pwvucontrol,
// Sonusmix and the Rofi entries looked like on a real desktop.
func TestLauncherRowHeightSurvivesAMissingCommentAndIcon(t *testing.T) {
	t.Parallel()

	reg, _, _ := openLauncherPanel(t, launcherTestEntries())
	reg.mu.Lock()
	defer reg.mu.Unlock()
	h := reg.panelHosts[PanelLauncher]

	measure := h.measureText()
	height := func(e launcher.Entry) int {
		body := launcherRowBody(nil, h, e)
		root := &ui.Node{Kind: ui.KindColumn, Children: []*ui.Node{body}}
		if err := ui.LayoutColumn(root, ui.Rect{W: h.place.Panel.W - 24, H: launcherRowHeight}, measure); err != nil {
			t.Fatalf("layout %q: %v", e.Name, err)
		}
		return body.Bounds.H
	}

	full := height(launcher.Entry{ID: "a.desktop", Name: "Files", Comment: "Access and organize files"})
	bare := height(launcher.Entry{ID: "b.desktop", Name: "Sonusmix"})
	if bare != full {
		t.Fatalf("row without a comment or icon is %d tall, one with both is %d", bare, full)
	}
	if bare < launcherIconSlot {
		t.Fatalf("row is %d tall, below the %d icon slot it must reserve", bare, launcherIconSlot)
	}
}

func TestLauncherFallbackIconReservesTheSquare(t *testing.T) {
	t.Parallel()
	n := launcherIconNode(nil, &PanelHost{}, launcher.Entry{Name: "Sonusmix"})
	if n.Width != launcherIconSlot || n.Height != launcherIconSlot {
		t.Fatalf("fallback icon slot = %dx%d, want a %d square", n.Width, n.Height, launcherIconSlot)
	}
}

// TestLauncherRowTakesHover checks the claim, made during the plate A design
// pass, that launcher rows have no hover state.
func TestLauncherRowTakesHover(t *testing.T) {
	t.Parallel()

	reg, _, _ := openLauncherPanel(t, launcherTestEntries())
	reg.mu.Lock()
	defer reg.mu.Unlock()
	h := reg.panelHosts[PanelLauncher]

	list := launcherListNode(t, h)
	row := list.Item(1) // not the selected row, so FillSoft cannot mask it
	capsule := row.Children[0]
	key := capsule.StableKey()
	if key == "" {
		t.Fatal("row capsule has no stable key, so the pointer can never address it")
	}

	h.pointer.setHover(key)
	h.pointer.apply(h.root, h.anim)

	// Children, not Item: the materialised row is the one that gets painted.
	again := launcherListNode(t, h).Children[1].Children[0]
	if !again.State.Has(ui.StateHovered) {
		t.Fatalf("row %q did not take hover", key)
	}
}

// With a long query the only way back to the browse list was holding
// Backspace. The trailing glyph is the pointer's way, and because the field is
// a leaf the target has to come from render, which is what drew it.
func TestLauncherClearGlyphEmptiesTheQuery(t *testing.T) {
	t.Parallel()

	reg, _, reqs := openLauncherPanel(t, launcherTestEntries())
	reqs[1].Open.Callbacks.Handle(wayland.Event{Kind: wayland.EventIME, IMECommit: "nautilus"})
	waitForLauncherState(t, reg, func(h *PanelHost) bool {
		return h != nil && len(h.launcherResults) == 1
	})

	h := launcherHost(t, reg)
	reg.mu.Lock()
	field := launcherSearchField(t, h)
	box := render.SearchClearBox(field)
	reg.mu.Unlock()
	if box.W == 0 {
		t.Fatalf("no clear glyph on a field holding %q at %+v", field.Text, field.Bounds)
	}
	if !field.Bounds.Contains(box.X, box.Y) {
		t.Fatalf("clear box %+v is outside the field %+v", box, field.Bounds)
	}

	reqs[1].Open.Callbacks.Handle(wayland.Event{
		Kind: wayland.EventPointerPress, Button: btnLeft,
		X: float64(box.X + box.W/2), Y: float64(box.Y + box.H/2),
	})
	waitForLauncherState(t, reg, func(h *PanelHost) bool {
		return h != nil && h.query == "" && len(h.launcherResults) == len(launcherTestEntries())
	})

	h = launcherHost(t, reg)
	reg.mu.Lock()
	defer reg.mu.Unlock()
	if again := launcherSearchField(t, h); again.Text != "" || again.Cursor != 0 {
		t.Fatalf("field = %q cursor %d after the clear, want empty", again.Text, again.Cursor)
	}
	if render.SearchClearBox(launcherSearchField(t, h)).W != 0 {
		t.Fatal("the clear glyph is still drawn on an empty well")
	}
}

// A press just outside the glyph is a press in the well, not a clear. The box
// is 20 logical pixels in a 536-wide field, so an off-by-one here is a control
// that fires when the pointer is merely near it.
func TestLauncherClearGlyphIgnoresAPressBesideIt(t *testing.T) {
	t.Parallel()

	reg, _, reqs := openLauncherPanel(t, launcherTestEntries())
	reqs[1].Open.Callbacks.Handle(wayland.Event{Kind: wayland.EventIME, IMECommit: "nautilus"})
	waitForLauncherState(t, reg, func(h *PanelHost) bool {
		return h != nil && len(h.launcherResults) == 1
	})

	h := launcherHost(t, reg)
	reg.mu.Lock()
	box := render.SearchClearBox(launcherSearchField(t, h))
	reg.mu.Unlock()

	reqs[1].Open.Callbacks.Handle(wayland.Event{
		Kind: wayland.EventPointerPress, Button: btnLeft,
		X: float64(box.X - 1), Y: float64(box.Y + box.H/2),
	})

	h = launcherHost(t, reg)
	reg.mu.Lock()
	defer reg.mu.Unlock()
	if h.query != "nautilus" {
		t.Fatalf("query = %q after a press beside the glyph, want it untouched", h.query)
	}
}

// The glyph shortens the text box, and the caret rides that box. A query long
// enough to fill the well must still end before the glyph rather than under it.
func TestLauncherClearGlyphKeepsTheTextClear(t *testing.T) {
	t.Parallel()

	reg, _, reqs := openLauncherPanel(t, launcherTestEntries())
	long := strings.Repeat("nautilus ", 12)
	reqs[1].Open.Callbacks.Handle(wayland.Event{Kind: wayland.EventIME, IMECommit: long})
	waitForLauncherState(t, reg, func(h *PanelHost) bool {
		return h != nil && h.query == long
	})

	h := launcherHost(t, reg)
	reg.mu.Lock()
	defer reg.mu.Unlock()
	field := launcherSearchField(t, h)
	box := render.SearchClearBox(field)
	if box.W == 0 {
		t.Fatal("a full well lost its clear glyph")
	}
	if right := field.Bounds.X + field.Bounds.W; box.X+box.W > right {
		t.Fatalf("clear glyph ends at %d, past the field's %d", box.X+box.W, right)
	}
	if field.Cursor != len(long) {
		t.Fatalf("cursor = %d, want the end of a %d-byte query", field.Cursor, len(long))
	}
}

func launcherSearchField(t *testing.T, h *PanelHost) *ui.Node {
	t.Helper()
	var find func(*ui.Node) *ui.Node
	find = func(n *ui.Node) *ui.Node {
		if n == nil {
			return nil
		}
		if n.Kind == ui.KindTextField && n.Name == "Search" {
			return n
		}
		for _, c := range n.Children {
			if got := find(c); got != nil {
				return got
			}
		}
		return nil
	}
	field := find(h.root)
	if field == nil {
		t.Fatal("no Search field in the launcher tree")
	}
	return field
}
