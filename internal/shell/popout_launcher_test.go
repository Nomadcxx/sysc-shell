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
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	launcher "github.com/Nomadcxx/sysc-launch"
	"github.com/Nomadcxx/sysc-shell/internal/config"
	"github.com/Nomadcxx/sysc-shell/internal/icons"
	"github.com/Nomadcxx/sysc-shell/internal/platform/wayland"
	"github.com/Nomadcxx/sysc-shell/internal/render"
	"github.com/Nomadcxx/sysc-shell/internal/theme"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
	v1 "github.com/Nomadcxx/sysc-shell/plugin/v1"
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
	// gate, when non-nil, holds the spawn until the test closes it, so a
	// test can act while an activation is still in flight.
	gate chan struct{}
}

func (r *recordedSpawn) run(_ context.Context, argv []string) error {
	r.mu.Lock()
	r.argv = append([]string(nil), argv...)
	err, gate := r.err, r.gate
	r.mu.Unlock()
	if gate != nil {
		<-gate
	}
	return err
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
	return openLauncherPanelWithTrigger(t, entries, Trigger{BarEdge: "top", BarZone: 40, OutW: 1920, OutH: 1080})
}

func openLauncherPanelWithTrigger(t *testing.T, entries []launcher.Entry, trigger Trigger) (*Registry, *recordedSpawn, []wayland.AuxRequest) {
	t.Helper()
	reg := newPanelRegistry(t)
	// Production drains invalidations on the owner's bridge without r.mu.
	// Nothing does here, and a launcher publishes from goroutines that hold
	// r.mu (the activation result), so a full cap-8 channel left the next
	// waitForLauncherState parked on the lock for good.
	keepInvalidationsDrained(t, reg)
	run := &recordedSpawn{}
	cfg := reg.launcherServiceConfig()
	cfg.Scan = func() []launcher.Entry { return entries }
	cfg.Run = run.run
	svc := launcher.NewService(cfg)
	reg.mu.Lock()
	reg.launcherSvc = svc
	reg.launcherServiceProviders = cloneLauncherCommandProviders(reg.cfg.Launcher.Providers)
	reg.launcherServiceActions = cloneLauncherActions(reg.cfg.Launcher.Actions)
	reg.launcherRelayStarted = true
	reg.mu.Unlock()
	go reg.relayLauncher(svc)

	if err := reg.OpenPanel(PanelLauncher, 7, trigger); err != nil {
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

func TestNotesLauncherCaptureAndProviderRoutes(t *testing.T) {
	if got, handled := notesLauncherResults("/nt", nil); !handled || len(got) != 1 || got[0].Entry.ID != notesLauncherActionID || got[0].Entry.Name != "Open Notes" {
		t.Fatalf("/nt route = %+v, handled=%v", got, handled)
	}
	if got, handled := notesLauncherResults("/nt Remember this", nil); !handled || got[0].Entry.Name != "Capture note: Remember this" {
		t.Fatalf("capture route = %+v, handled=%v", got, handled)
	}
	if got, handled := notesLauncherResults("/nth", nil); handled || got != nil {
		t.Fatalf("unrelated route = %+v, handled=%v", got, handled)
	}
	if got, handled := notesLauncherResults("/nt "+strings.Repeat("x", v1.MaxInputBytes+1), nil); !handled || got[0].Entry.ID != notesLauncherTooLongID {
		t.Fatalf("oversized route = %+v, handled=%v", got, handled)
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
	reqs[1].Open.Callbacks.Handle(keyEv(key, 0))
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

func TestLauncherListFitsWhenResultsArriveBeforeConfigure(t *testing.T) {
	reg := newPanelRegistry(t)
	keepInvalidationsDrained(t, reg)
	entries := launcherTestEntries()
	run := &recordedSpawn{}
	cfg := reg.launcherServiceConfig()
	cfg.Scan = func() []launcher.Entry { return entries }
	cfg.Run = run.run
	svc := launcher.NewService(cfg)
	reg.mu.Lock()
	reg.launcherSvc = svc
	reg.mu.Unlock()
	go reg.relayLauncher(svc)

	if err := reg.OpenPanel(PanelLauncher, 7, Trigger{BarEdge: "top", BarZone: 40, OutW: 1920, OutH: 1080}); err != nil {
		t.Fatal(err)
	}
	reqs := drainAux(t, reg, 2)
	// A scan result can be relayed before the compositor sends Configure. The
	// first result rebuilds the tree, so its viewport must already use the
	// renderer's metrics rather than the measurement fallback.
	waitForLauncherResults(t, reg, len(entries))
	if err := reqs[1].Open.Callbacks.Configure(560, 500, 120); err != nil {
		t.Fatal(err)
	}

	h := launcherHost(t, reg)
	reg.mu.Lock()
	defer reg.mu.Unlock()
	list := launcherListNode(t, h)
	if list.Bounds.H != h.launcherListHeight() {
		t.Fatalf("list height = %d, want %d", list.Bounds.H, h.launcherListHeight())
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

// The scrollbar sits in the panel's right padding, launcherScrollbarClear from
// the pills, and its ends are level with the pill edges. Inside the list it covered
// the last two pixels of every pill and started above the first one.
func TestLauncherScrollbarClearsThePills(t *testing.T) {
	t.Parallel()

	reg, _, _ := openLauncherPanel(t, alphabetEntries(60))
	h := launcherHost(t, reg)
	reg.mu.Lock()
	defer reg.mu.Unlock()

	list := launcherListNode(t, h)
	bar := ui.ScrollBar(list)
	if bar.W == 0 {
		t.Fatal("an overflowing launcher list has no scrollbar")
	}
	pill := list.Children[0].Children[0]
	if gap := bar.X - (pill.Bounds.X + pill.Bounds.W); gap != launcherScrollbarClear {
		t.Fatalf("scrollbar clears the pills by %d, want %d (bar %+v, pill %+v)", gap, launcherScrollbarClear, bar, pill.Bounds)
	}
	if right := bar.X + bar.W; right > h.place.Panel.W {
		t.Fatalf("scrollbar ends at x=%d, past the %d panel", right, h.place.Panel.W)
	}
	if bar.Y != pill.Bounds.Y {
		t.Fatalf("scrollbar top = %d, want level with the first pill at %d", bar.Y, pill.Bounds.Y)
	}
	if bottom, want := bar.Y+bar.H, list.Bounds.Y+list.Bounds.H-launcherRowGap/2; bottom != want {
		t.Fatalf("scrollbar bottom = %d, want %d", bottom, want)
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
	reqs[1].Open.Callbacks.Handle(keyEv(33, 0)) // F
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

func TestLauncherQueryHistorySurvivesReopen(t *testing.T) {
	t.Parallel()

	entries := launcherTestEntries()
	reg, _, reqs := openLauncherPanel(t, entries)
	query := func(reqs []wayland.AuxRequest, text string) {
		reqs[1].Open.Callbacks.Handle(wayland.Event{Kind: wayland.EventIME, IMECommit: text})
		waitForLauncherState(t, reg, func(h *PanelHost) bool {
			return h != nil && h.query == text && !h.launcherAwaiting
		})
	}
	closePanel := func() {
		reg.ClosePanel(PanelLauncher)
		_ = drainAux(t, reg, 2)
	}
	reopen := func() []wayland.AuxRequest {
		if err := reg.OpenPanel(PanelLauncher, 7, Trigger{BarEdge: "top", BarZone: 40, OutW: 1920, OutH: 1080}); err != nil {
			t.Fatal(err)
		}
		reqs := drainAux(t, reg, 2)
		if err := reqs[1].Open.Callbacks.Configure(560, 500, 120); err != nil {
			t.Fatal(err)
		}
		waitForLauncherResults(t, reg, len(entries))
		return reqs
	}
	expectQuery := func(want string) {
		reg.mu.Lock()
		h := reg.panelHosts[PanelLauncher]
		got := ""
		if h != nil {
			got = h.query
		}
		reg.mu.Unlock()
		if got != want {
			t.Fatalf("launcher query = %q, want %q", got, want)
		}
		waitForLauncherState(t, reg, func(h *PanelHost) bool {
			return h != nil && h.query == want && !h.launcherAwaiting
		})
	}

	query(reqs, "firefox")
	closePanel()
	reqs = reopen()
	query(reqs, "foot")
	closePanel()
	reqs = reopen()

	pressLauncherKey(reqs, keyUp)
	expectQuery("foot")
	pressLauncherKey(reqs, keyUp)
	expectQuery("firefox")
	pressLauncherKey(reqs, keyUp)
	expectQuery("firefox")
	pressLauncherKey(reqs, keyDown)
	expectQuery("foot")
	pressLauncherKey(reqs, keyDown)
	expectQuery("")
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
	row := slices.IndexFunc(h.launcherResults, func(r launcher.Result) bool { return r.Entry.ID == "firefox.desktop" })
	bounds, ok := walkActionBounds(h.root, fmt.Sprintf("launch:%d", row))
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

func TestLauncherSessionFailureRemainsVisible(t *testing.T) {
	t.Parallel()

	reg, _, reqs := openLauncherPanel(t, launcherTestEntries())
	started := make(chan []string, 1)
	reg.runArgv = func(argv []string) error {
		started <- argv
		return errors.New("loginctl refused")
	}
	reqs[1].Open.Callbacks.Handle(wayland.Event{Kind: wayland.EventIME, IMECommit: "reboot"})
	waitForLauncherState(t, reg, func(h *PanelHost) bool {
		return h != nil && !h.launcherAwaiting && slices.ContainsFunc(h.launcherResults, func(row launcher.Result) bool {
			return row.Entry.ID == "session-reboot"
		})
	})
	pressLauncherKey(reqs, keyEnter)

	select {
	case argv := <-started:
		if !slices.Equal(argv, []string{"loginctl", "reboot"}) {
			t.Fatalf("session action argv = %v", argv)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("session action did not start")
	}
	waitForLauncherState(t, reg, func(h *PanelHost) bool {
		return h != nil && h.errLabel == "loginctl refused"
	})
}

func TestLauncherConfiguredActionFailureRemainsVisible(t *testing.T) {
	t.Parallel()

	reg, _, reqs := openLauncherPanel(t, launcherTestEntries())
	started := make(chan []string, 1)
	reg.mu.Lock()
	reg.cfg.Launcher.Actions = []config.LauncherAction{{
		ID: "open-search", Label: "Open search", Argv: []string{"browser", "search terms"},
	}}
	reg.runArgv = func(argv []string) error {
		started <- append([]string(nil), argv...)
		return errors.New("browser refused")
	}
	reg.mu.Unlock()
	reqs[1].Open.Callbacks.Handle(wayland.Event{Kind: wayland.EventIME, IMECommit: "open search"})
	waitForLauncherState(t, reg, func(h *PanelHost) bool {
		return h != nil && !h.launcherAwaiting && slices.ContainsFunc(h.launcherResults, func(row launcher.Result) bool {
			return row.Entry.ID == "launcher-action:open-search"
		})
	})
	pressLauncherKey(reqs, keyEnter)
	select {
	case argv := <-started:
		if !slices.Equal(argv, []string{"browser", "search terms"}) {
			t.Fatalf("configured action argv = %v", argv)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("configured action did not start")
	}
	waitForLauncherState(t, reg, func(h *PanelHost) bool {
		return h != nil && h.errLabel == "browser refused"
	})
}

func TestLauncherCommandProviderConfigReloadRefreshesOpenLauncher(t *testing.T) {
	printf, err := exec.LookPath("printf")
	if err != nil {
		t.Skip("printf is not on PATH")
	}
	reg, _, reqs := openLauncherPanel(t, launcherTestEntries())
	reg.mu.Lock()
	cfg := reg.cfg.Clone()
	reg.mu.Unlock()
	cfg.Launcher.Providers = []config.LauncherCommandProvider{{
		Name: "Files", Prefix: "/files", Glyph: "glyph:description",
		Command:  []string{printf, "%s\\t%s\\n", "{query}", "Document"},
		OnSelect: []string{"file-open", "{selection}"},
	}}
	prepared, err := reg.PrepareConfig(cfg, nil)
	if err != nil {
		t.Fatalf("prepare launcher provider config: %v", err)
	}
	prepared.Commit()

	reqs[1].Open.Callbacks.Handle(wayland.Event{Kind: wayland.EventIME, IMECommit: "/files milk"})
	waitForLauncherState(t, reg, func(h *PanelHost) bool {
		return h != nil && !h.launcherAwaiting && slices.ContainsFunc(h.launcherResults, func(row launcher.Result) bool {
			return row.Entry.ID == "/files:row:milk\tDocument"
		})
	})
}

func TestLauncherActionConfigReloadRefreshesOpenLauncher(t *testing.T) {
	reg, _, reqs := openLauncherPanel(t, launcherTestEntries())
	reg.mu.Lock()
	cfg := reg.cfg.Clone()
	cfg.Launcher.Actions = []config.LauncherAction{{
		ID: "docs", Label: "Open docs", Argv: []string{"browser", "https://example.test/docs"},
	}}
	reg.mu.Unlock()
	prepared, err := reg.PrepareConfig(cfg, nil)
	if err != nil {
		t.Fatalf("prepare initial launcher action config: %v", err)
	}
	prepared.Commit()

	reqs[1].Open.Callbacks.Handle(wayland.Event{Kind: wayland.EventIME, IMECommit: "/action"})
	waitForLauncherState(t, reg, func(h *PanelHost) bool {
		return h != nil && !h.launcherAwaiting && slices.ContainsFunc(h.launcherResults, func(row launcher.Result) bool {
			return row.Entry.ID == "launcher-action:docs" && row.Entry.Name == "Open docs"
		})
	})

	reg.mu.Lock()
	cfg = reg.cfg.Clone()
	cfg.Launcher.Actions[0].Label = "Browse docs"
	reg.mu.Unlock()
	prepared, err = reg.PrepareConfig(cfg, nil)
	if err != nil {
		t.Fatalf("prepare changed launcher action config: %v", err)
	}
	prepared.Commit()

	waitForLauncherState(t, reg, func(h *PanelHost) bool {
		return h != nil && !h.launcherAwaiting && slices.ContainsFunc(h.launcherResults, func(row launcher.Result) bool {
			return row.Entry.ID == "launcher-action:docs" && row.Entry.Name == "Browse docs"
		})
	})
}

func TestLauncherRankFromReplacedServiceCannotEmitIntoCurrentBatch(t *testing.T) {
	reg := newPanelRegistry(t)
	reg.mu.Lock()
	oldConfig := reg.launcherServiceConfig()
	oldConfig.Scan = func() []launcher.Entry { return nil }
	reg.launcherSvc = launcher.NewService(oldConfig)
	reg.launcherServiceProviders = cloneLauncherCommandProviders(reg.cfg.Launcher.Providers)
	reg.launcherServiceActions = cloneLauncherActions(reg.cfg.Launcher.Actions)
	reg.launcherRelayStarted = true
	reg.mu.Unlock()

	started := make(chan struct{}, 1)
	release := make(chan struct{})
	var releaseOnce sync.Once
	unblock := func() { releaseOnce.Do(func() { close(release) }) }
	defer unblock()
	reg.launcherMu.Lock()
	reg.launcherGen = 1
	reg.launcherPlan = launcherPlan{kind: launcherKindApps, passed: "same"}
	reg.launcherBatch = launcherBatch{gen: 1}
	reg.launcherRankWait = func(string) {
		started <- struct{}{}
		<-release
	}
	reg.launcherMu.Unlock()

	oldDone := make(chan struct{})
	go func() {
		oldConfig.Rank([]launcher.Entry{{ID: "stale", Name: "same stale"}}, "same", nil)
		close(oldDone)
	}()
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("old service rank did not reach its hold")
	}

	reg.mu.Lock()
	reg.cfg.Launcher.Providers = []config.LauncherCommandProvider{{
		Name: "Files", Prefix: "/files", Command: []string{"list"}, OnSelect: []string{"open"},
	}}
	reg.launcherServiceLocked()
	newConfig := reg.launcherServiceConfig()
	reg.mu.Unlock()

	reg.launcherMu.Lock()
	reg.launcherRankWait = nil
	reg.launcherMu.Unlock()
	newConfig.Rank([]launcher.Entry{{ID: "fresh", Name: "same fresh"}}, "same", nil)
	select {
	case snap := <-reg.launcherSnaps:
		if len(snap.rows) != 1 || snap.rows[0].Entry.ID != "fresh" {
			t.Fatalf("current batch rows = %+v, want fresh row", snap.rows)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("current service rank did not emit its rows")
	}

	unblock()
	<-oldDone
	select {
	case snap := <-reg.launcherSnaps:
		t.Fatalf("replaced service emitted stale rows into the current batch: %+v", snap.rows)
	default:
	}
}

func TestLauncherActionConfigReplacementRejectsSameQueryRank(t *testing.T) {
	reg := newPanelRegistry(t)
	reg.mu.Lock()
	oldConfig := reg.launcherServiceConfig()
	oldConfig.Scan = func() []launcher.Entry { return nil }
	reg.launcherSvc = launcher.NewService(oldConfig)
	reg.launcherServiceProviders = cloneLauncherCommandProviders(reg.cfg.Launcher.Providers)
	reg.launcherServiceActions = cloneLauncherActions(reg.cfg.Launcher.Actions)
	reg.launcherRelayStarted = true
	reg.mu.Unlock()

	started := make(chan struct{}, 1)
	release := make(chan struct{})
	var releaseOnce sync.Once
	unblock := func() { releaseOnce.Do(func() { close(release) }) }
	defer unblock()
	reg.launcherMu.Lock()
	reg.launcherGen = 1
	reg.launcherPlan = launcherPlan{kind: launcherKindApps, passed: "same"}
	reg.launcherBatch = launcherBatch{gen: 1}
	reg.launcherRankWait = func(string) {
		started <- struct{}{}
		<-release
	}
	reg.launcherMu.Unlock()

	oldDone := make(chan struct{})
	go func() {
		oldConfig.Rank([]launcher.Entry{{ID: "stale", Name: "same stale"}}, "same", nil)
		close(oldDone)
	}()
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("old action-config rank did not reach its hold")
	}

	reg.mu.Lock()
	reg.cfg.Launcher.Actions = []config.LauncherAction{{
		ID: "docs", Label: "Browse docs", Argv: []string{"browser", "https://example.test/docs"},
	}}
	reg.launcherServiceLocked()
	newConfig := reg.launcherServiceConfig()
	reg.mu.Unlock()

	reg.launcherMu.Lock()
	reg.launcherRankWait = nil
	reg.launcherMu.Unlock()
	newConfig.Rank([]launcher.Entry{{ID: "fresh", Name: "same fresh"}}, "same", nil)
	select {
	case snap := <-reg.launcherSnaps:
		if len(snap.rows) != 1 || snap.rows[0].Entry.ID != "fresh" {
			t.Fatalf("current batch rows = %+v, want fresh row", snap.rows)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("current service rank did not emit its rows")
	}

	unblock()
	<-oldDone
	select {
	case snap := <-reg.launcherSnaps:
		t.Fatalf("replaced action-config service emitted stale rows into the current batch: %+v", snap.rows)
	default:
	}
}

func TestClosedLauncherServiceWaitsForItsActivationCallback(t *testing.T) {
	printf, err := exec.LookPath("printf")
	if err != nil {
		t.Skip("printf is not on PATH")
	}
	reg := newPanelRegistry(t)
	oldCallbackStarted := make(chan struct{})
	oldCallbackDone := make(chan struct{})
	releaseOldCallback := make(chan struct{})
	var releaseOnce sync.Once
	unblockOldCallback := func() { releaseOnce.Do(func() { close(releaseOldCallback) }) }
	defer unblockOldCallback()
	run := make(chan []string, 4)
	reg.mu.Lock()
	reg.cfg.Launcher.Providers = []config.LauncherCommandProvider{{
		Name: "Files", Prefix: "/files", Glyph: "glyph:description",
		Command: []string{printf, "%s\\t%s\\n", "item", "Item"}, OnSelect: []string{"old-open", "{selection}"},
	}}
	reg.runArgv = func(argv []string) error {
		run <- slices.Clone(argv)
		return nil
	}
	h := &PanelHost{id: PanelLauncher, query: "/files"}
	reg.panelHosts[PanelLauncher] = h
	t.Cleanup(func() {
		reg.mu.Lock()
		delete(reg.panelHosts, PanelLauncher)
		reg.mu.Unlock()
	})
	oldConfig := reg.launcherServiceConfig()
	for i := range oldConfig.Providers {
		if oldConfig.Providers[i].Prefix != "/files" {
			continue
		}
		activate := oldConfig.Providers[i].Activate
		oldConfig.Providers[i].Activate = func(query, id, action string) error {
			close(oldCallbackStarted)
			<-releaseOldCallback
			defer close(oldCallbackDone)
			return activate(query, id, action)
		}
	}
	oldConfig.Scan = func() []launcher.Entry { return nil }
	oldService := launcher.NewService(oldConfig)
	reg.launcherSvc = oldService
	reg.launcherServiceProviders = cloneLauncherCommandProviders(reg.cfg.Launcher.Providers)
	reg.launcherServiceActions = cloneLauncherActions(reg.cfg.Launcher.Actions)
	reg.launcherRelayStarted = true
	reg.mu.Unlock()

	const rowID = "/files:row:item\tItem"
	waitForLauncherServiceRow := func(svc *launcher.Service) {
		t.Helper()
		deadline := time.After(2 * time.Second)
		for {
			select {
			case rows := <-svc.Results():
				if slices.ContainsFunc(rows.Results, func(row launcher.Result) bool { return row.Entry.ID == rowID }) {
					return
				}
			case <-deadline:
				t.Fatalf("launcher service did not publish %q", rowID)
			}
		}
	}
	oldService.Query("/files")
	waitForLauncherServiceRow(oldService)
	reg.mu.Lock()
	h.launcherSpawn(reg, rowID, "", false)
	reg.mu.Unlock()
	select {
	case <-oldCallbackStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("old provider callback did not start")
	}

	reg.mu.Lock()
	cfg := reg.cfg.Clone()
	cfg.Launcher.Providers[0].OnSelect = []string{"new-open", "{selection}"}
	reg.mu.Unlock()
	prepared, err := reg.PrepareConfig(cfg, nil)
	if err != nil {
		t.Fatalf("prepare changed launcher provider config: %v", err)
	}
	prepared.Commit()

	reg.mu.Lock()
	h.launcherSpawn(reg, rowID, "", false)
	reg.mu.Unlock()

	deadlineBeforeRelease := time.Now().Add(250 * time.Millisecond)
	for time.Now().Before(deadlineBeforeRelease) {
		if reg.launcherActivationMu.TryLock() {
			reg.launcherActivationMu.Unlock()
			t.Fatal("launcher released activation serialization while the retired callback was still running")
		}
		time.Sleep(time.Millisecond)
	}
	unblockOldCallback()
	var got [][]string
	oldDone, newDone := false, false
	deadline := time.After(2 * time.Second)
	for !oldDone || !newDone {
		select {
		case argv := <-run:
			got = append(got, argv)
			if len(argv) > 0 && argv[0] == "new-open" {
				newDone = true
			}
		case <-oldCallbackDone:
			oldDone = true
			oldCallbackDone = nil
		case <-deadline:
			t.Fatalf("activation callbacks did not settle: old done %v, new run %v, argv %v", oldDone, newDone, got)
		}
	}
	select {
	case argv := <-run:
		got = append(got, argv)
	default:
	}
	if len(got) != 1 || !slices.Equal(got[0], []string{"new-open", "item\tItem"}) {
		t.Fatalf("configured provider launches = %v, want only the current action", got)
	}
}

func TestLauncherLateSessionFailureDoesNotReplaceNewSearch(t *testing.T) {
	t.Parallel()

	reg, _, reqs := openLauncherPanel(t, launcherTestEntries())
	started := make(chan []string, 1)
	release := make(chan struct{})
	var releaseOnce sync.Once
	releaseRun := func() { releaseOnce.Do(func() { close(release) }) }
	t.Cleanup(releaseRun)
	reg.runArgv = func(argv []string) error {
		started <- argv
		<-release
		return errors.New("loginctl refused")
	}
	reqs[1].Open.Callbacks.Handle(wayland.Event{Kind: wayland.EventIME, IMECommit: "reboot"})
	waitForLauncherState(t, reg, func(h *PanelHost) bool {
		return h != nil && !h.launcherAwaiting && slices.ContainsFunc(h.launcherResults, func(row launcher.Result) bool {
			return row.Entry.ID == "session-reboot"
		})
	})
	pressLauncherKey(reqs, keyEnter)
	select {
	case argv := <-started:
		if !slices.Equal(argv, []string{"loginctl", "reboot"}) {
			t.Fatalf("session action argv = %v", argv)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("session action did not start")
	}

	reg.mu.Lock()
	h := reg.panelHosts[PanelLauncher]
	if !h.fieldChanged(reg, &ui.Node{Name: "Search"}, ui.NewField("suspend")) {
		reg.mu.Unlock()
		t.Fatal("new session query was not handled")
	}
	reg.mu.Unlock()
	deadline := time.Now().Add(2 * time.Second)
	for {
		reg.mu.Lock()
		h := reg.panelHosts[PanelLauncher]
		found := false
		var query string
		var awaiting bool
		var ids []string
		if h != nil {
			query, awaiting = h.query, h.launcherAwaiting
			for _, row := range h.launcherResults {
				ids = append(ids, row.Entry.ID)
				found = found || row.Entry.ID == "session-suspend"
			}
		}
		reg.mu.Unlock()
		if h != nil && !awaiting && found {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("new session query host=%v query=%q awaiting=%v rows=%v", h != nil, query, awaiting, ids)
		}
		time.Sleep(5 * time.Millisecond)
	}
	releaseRun()
	deadline = time.Now().Add(300 * time.Millisecond)
	for time.Now().Before(deadline) {
		reg.mu.Lock()
		h := reg.panelHosts[PanelLauncher]
		label := ""
		if h != nil {
			label = h.errLabel
		}
		reg.mu.Unlock()
		if label != "" {
			t.Fatalf("late session failure replaced the new search with %q", label)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// A failed activation's error answers that attempt. Typing again starts a new
// search, and the old error must not sit over its results (GH #41).
func TestLauncherTypingClearsAFailedActivationError(t *testing.T) {
	t.Parallel()

	reg, run, reqs := openLauncherPanel(t, launcherTestEntries())
	run.err = errors.New("niri refused")
	pressLauncherKey(reqs, keyEnter)
	waitForLauncherState(t, reg, func(h *PanelHost) bool {
		return h != nil && h.errLabel != ""
	})

	reqs[1].Open.Callbacks.Handle(wayland.Event{Kind: wayland.EventIME, IMECommit: "f"})
	reg.mu.Lock()
	h := reg.panelHosts[PanelLauncher]
	label := h.errLabel
	errorText := launcherErrorText(h.root)
	reg.mu.Unlock()
	if label != "" || errorText != "" {
		t.Fatalf("after typing, errLabel = %q and the tree shows %q, want neither", label, errorText)
	}
}

// A failure that lands after the user has typed again answers the superseded
// attempt; it must not repaint the error over the new search (GH #45).
func TestLauncherLateActivationFailureDoesNotRepaintAfterTyping(t *testing.T) {
	t.Parallel()

	reg, run, reqs := openLauncherPanel(t, launcherTestEntries())
	run.mu.Lock()
	run.err = errors.New("niri refused")
	run.gate = make(chan struct{})
	gate := run.gate
	run.mu.Unlock()
	pressLauncherKey(reqs, keyEnter)
	run.waitArgv(t) // the activation is in flight, held in the spawn

	reqs[1].Open.Callbacks.Handle(wayland.Event{Kind: wayland.EventIME, IMECommit: "f"})
	close(gate)

	deadline := time.Now().Add(300 * time.Millisecond)
	for time.Now().Before(deadline) {
		reg.mu.Lock()
		h := reg.panelHosts[PanelLauncher]
		label := ""
		if h != nil {
			label = h.errLabel
		}
		reg.mu.Unlock()
		if label != "" {
			t.Fatalf("late failure repainted errLabel = %q after typing", label)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// A success that lands after the user has typed again answers the superseded
// attempt; it must not dismiss the launcher the user is still using (GH #49).
func TestLauncherLateActivationSuccessDoesNotCloseAfterTyping(t *testing.T) {
	t.Parallel()

	reg, run, reqs := openLauncherPanel(t, launcherTestEntries())
	run.mu.Lock()
	run.gate = make(chan struct{})
	gate := run.gate
	run.mu.Unlock()
	pressLauncherKey(reqs, keyEnter)
	run.waitArgv(t)

	reqs[1].Open.Callbacks.Handle(wayland.Event{Kind: wayland.EventIME, IMECommit: "f"})
	close(gate)

	deadline := time.Now().Add(300 * time.Millisecond)
	for time.Now().Before(deadline) {
		reg.mu.Lock()
		_, hosted := reg.panelHosts[PanelLauncher]
		reg.mu.Unlock()
		if !hosted {
			t.Fatal("late activation success closed the launcher after the query moved on")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// launcherErrorText returns the first error-toned text in the tree.
func launcherErrorText(n *ui.Node) string {
	if n == nil {
		return ""
	}
	if n.Kind == ui.KindText && n.Tone == ui.ToneError {
		return n.Text
	}
	for _, c := range n.Children {
		if s := launcherErrorText(c); s != "" {
			return s
		}
	}
	return ""
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
		t.Skip("the theme's background and bar surface tokens are not fully opaque, so there is no opaque region")
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
	handle(keyEv(33, 0)) // F
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
		body := launcherRowBody(nil, h, e, launcherLayoutFor(nil, h))
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

func TestLauncherIconSlotConventions(t *testing.T) {
	r := newPanelRegistry(t)
	h := &PanelHost{}
	g := launcherIconNode(r, h, launcher.Entry{Name: "Calculator", IconName: "glyph:calculate"})
	if g.Fill != ui.FillContainer || len(g.Children) != 1 || g.Children[0].Kind != ui.KindIcon || g.Children[0].Icon != "calculate" {
		t.Fatalf("glyph slot %+v", g)
	}
	e := launcherIconNode(r, h, launcher.Entry{Name: "party popper", IconName: "text:🎉"})
	if e.Fill != ui.FillNone || e.Children[0].Text != "🎉" || e.Children[0].TextRole != theme.RoleDisplay {
		t.Fatalf("text slot %+v", e)
	}
}

func TestLauncherHintRowIsMutedUnselectedAndUncounted(t *testing.T) {
	r := newPanelRegistry(t)
	h := &PanelHost{query: "/calc 2+", launcherResults: []launcher.Result{
		{Entry: launcher.Entry{Name: "Invalid expression", Comment: "Try 6*7", IconName: "glyph:calculate"}},
	}}
	row := launcherRow(r, h, h.launcherResults, 0, launcherLayoutFor(r, h))
	capsule := row.Children[0]
	if capsule.Fill != ui.FillNone {
		t.Fatal("hint row highlighted")
	}
	if !treeHasText(launcherFooter(h, 1, launcherLayoutFor(nil, h)), "No results • "+launcherHints) {
		t.Fatal("footer counted the hint")
	}
}

func TestLauncherFooterPluralises(t *testing.T) {
	one := &PanelHost{query: "6*7", launcherResults: []launcher.Result{{Entry: launcher.Entry{ID: "calc:42"}}}}
	if !treeHasText(launcherFooter(one, 1, launcherLayoutFor(nil, one)), "1 result • "+launcherHints) {
		t.Fatal("singular")
	}
	browse := &PanelHost{launcherResults: make([]launcher.Result, 3)}
	for i := range browse.launcherResults {
		browse.launcherResults[i].Entry.ID = fmt.Sprintf("app%d", i)
	}
	if !treeHasText(launcherFooter(browse, 3, launcherLayoutFor(nil, browse)), "3 apps • "+launcherHints) {
		t.Fatal("plural browse")
	}
}

// An action row shares its application's ID, so a click must resolve the row
// itself, not the first row with that ID.
func TestLauncherClickOnActionRowSpawnsTheAction(t *testing.T) {
	t.Parallel()

	reg, run, reqs := openLauncherPanel(t, launcherTestEntries())
	reqs[1].Open.Callbacks.Handle(wayland.Event{Kind: wayland.EventIME, IMECommit: "new window"})
	waitForLauncherState(t, reg, func(h *PanelHost) bool {
		return h != nil && len(h.launcherResults) == 1 && h.launcherResults[0].Action == "new-window"
	})
	h := launcherHost(t, reg)
	reg.mu.Lock()
	bounds, ok := firstLauncherRowBounds(h.root)
	reg.mu.Unlock()
	if !ok {
		t.Fatal("action row has no laid-out bounds")
	}
	reqs[1].Open.Callbacks.Handle(wayland.Event{
		Kind: wayland.EventPointerPress, Button: btnLeft,
		X: float64(bounds.X + 4), Y: float64(bounds.Y + 4),
	})
	if got := run.waitArgv(t); !slices.Equal(got, []string{"niri", "msg", "action", "spawn", "--", "firefox", "--new-window"}) {
		t.Fatalf("spawn argv = %v", got)
	}
}

func firstLauncherRowBounds(n *ui.Node) (ui.Rect, bool) {
	if n == nil {
		return ui.Rect{}, false
	}
	if strings.HasPrefix(n.Action, "launch:") && n.Bounds.W > 0 {
		return n.Bounds, true
	}
	for _, c := range n.Children {
		if b, ok := firstLauncherRowBounds(c); ok {
			return b, true
		}
	}
	return ui.Rect{}, false
}

// Notes capture reaches the Notes action end to end: with no plugin host the
// action reports that Notes is not running and leaves the launcher open.
func TestLauncherNotesCaptureRoutesToNotes(t *testing.T) {
	t.Parallel()

	reg, run, reqs := openLauncherPanel(t, launcherTestEntries())
	reqs[1].Open.Callbacks.Handle(wayland.Event{Kind: wayland.EventIME, IMECommit: "/nt buy milk"})
	waitForLauncherState(t, reg, func(h *PanelHost) bool {
		return h != nil && len(h.launcherResults) == 1 && h.launcherResults[0].Entry.Name == "Capture note: buy milk"
	})
	pressLauncherKey(reqs, keyEnter)
	waitForLauncherState(t, reg, func(h *PanelHost) bool {
		return h != nil && h.errLabel == "Notes plugin is not running"
	})
	run.mu.Lock()
	spawned := run.argv
	run.mu.Unlock()
	if spawned != nil {
		t.Fatalf("a Notes row spawned %v", spawned)
	}
}

func TestLauncherNotesTooLongCaptureShowsTheLimit(t *testing.T) {
	t.Parallel()

	reg, _, reqs := openLauncherPanel(t, launcherTestEntries())
	reqs[1].Open.Callbacks.Handle(wayland.Event{Kind: wayland.EventIME, IMECommit: "/nt " + strings.Repeat("x", v1.MaxInputBytes+1)})
	waitForLauncherState(t, reg, func(h *PanelHost) bool {
		return h != nil && len(h.launcherResults) == 1 && h.launcherResults[0].Entry.ID == notesLauncherTooLongID
	})
	pressLauncherKey(reqs, keyEnter)
	waitForLauncherState(t, reg, func(h *PanelHost) bool {
		return h != nil && h.errLabel == "Capture is too long (maximum 1 MiB)"
	})
}

func TestLauncherOverviewListsProvidersWithGlyphs(t *testing.T) {
	t.Parallel()

	reg, _, reqs := openLauncherPanel(t, launcherTestEntries())
	reqs[1].Open.Callbacks.Handle(wayland.Event{Kind: wayland.EventIME, IMECommit: "/"})
	waitForLauncherState(t, reg, func(h *PanelHost) bool {
		if h == nil {
			return false
		}
		var got []string
		for _, r := range h.launcherResults {
			got = append(got, r.Entry.Name+"|"+r.Entry.IconName)
		}
		return strings.Join(got, ",") == "Applications|glyph:apps,Calculator|glyph:calculate,Emoji|glyph:mood,Clipboard History|glyph:content_paste,Windows|glyph:desktop_windows,Notes|glyph:description,Session|glyph:power_settings_new,Actions|glyph:terminal"
	})
}

// gh #77: between a keystroke and the service's answer the list still shows the
// previous query's rows, so Enter on it must not run one of them.
func TestLauncherEnterWaitsForResultsOfTheCurrentQuery(t *testing.T) {
	t.Parallel()

	reg, run, _ := openLauncherPanel(t, launcherTestEntries())

	// Holding r.mu keeps the relay from applying the new snapshot, which is
	// exactly the window the field and the list disagree in.
	reg.mu.Lock()
	h := reg.panelHosts[PanelLauncher]
	if !h.fieldChanged(reg, &ui.Node{Name: "Search"}, ui.NewField("nautilus")) {
		reg.mu.Unlock()
		t.Fatal("search change not handled")
	}
	h.launcherActivateSelected(reg)
	reg.mu.Unlock()

	time.Sleep(150 * time.Millisecond) // a wrong spawn is asynchronous
	run.mu.Lock()
	spawned := run.argv
	run.mu.Unlock()
	if spawned != nil {
		t.Fatalf("Enter ran %v from the previous query's rows", spawned)
	}

	waitForLauncherState(t, reg, func(h *PanelHost) bool {
		return h != nil && !h.launcherAwaiting && len(h.launcherResults) == 1
	})
	reg.mu.Lock()
	reg.panelHosts[PanelLauncher].launcherActivateSelected(reg)
	reg.mu.Unlock()
	if got := run.waitArgv(t); len(got) == 0 || got[len(got)-1] != "nautilus" {
		t.Fatalf("Enter after the results landed ran %v, want nautilus", got)
	}
}

func TestApplicationPreferenceRefreshInvalidatesOpenLauncherActions(t *testing.T) {
	t.Parallel()

	entries := launcherTestEntries()
	reg, run, reqs := openLauncherPanel(t, entries)
	search := reqs[1].Open.Callbacks.Handle
	search(wayland.Event{Kind: wayland.EventIME, IMECommit: "firefox"})
	waitForLauncherState(t, reg, func(h *PanelHost) bool {
		return h != nil && !h.launcherAwaiting && slices.ContainsFunc(h.launcherResults, func(result launcher.Result) bool {
			return result.Entry.ID == "firefox.desktop" && result.Action == ""
		})
	})

	if err := reg.OpenPanel(PanelSettings, 7, Trigger{OutW: 1920, OutH: 1080}); err != nil {
		t.Fatal(err)
	}
	drainAuxQueue(reg)
	settingsHost := reg.panelHosts[PanelSettings]
	if settingsHost == nil {
		t.Fatal("Settings did not coexist with the Launcher")
	}
	reg.configPath = t.TempDir() + "/config.json"
	reg.mu.Lock()
	settingsHost.section = "Applications"
	settingsHost.draft = reg.cfg.Clone()
	settingsHost.set = reg.settingsForLocked(settingsHost.draft)
	reg.rebuildPanel(settingsHost)
	launcherHost := reg.panelHosts[PanelLauncher]
	row := slices.IndexFunc(launcherHost.launcherResults, func(result launcher.Result) bool {
		return result.Entry.ID == "firefox.desktop"
	})
	if row < 0 {
		reg.mu.Unlock()
		t.Fatal("Launcher lost Firefox before the preference changed")
	}
	bounds, ok := walkActionBounds(launcherHost.root, fmt.Sprintf("launch:%d", row))
	if !ok {
		reg.mu.Unlock()
		t.Fatal("Firefox row has no laid-out bounds")
	}
	if !launcherHost.openLauncherActions(reg, "firefox.desktop") || launcherHost.menu == nil {
		reg.mu.Unlock()
		t.Fatal("Firefox actions menu did not open")
	}
	reg.mu.Unlock()
	waitFor(t, func() bool {
		reg.mu.Lock()
		defer reg.mu.Unlock()
		return slices.ContainsFunc(settingsHost.focus, func(n *ui.Node) bool {
			return n.Action == "applications:hidden:firefox.desktop"
		})
	})

	release := make(chan struct{})
	var releaseOnce sync.Once
	defer releaseOnce.Do(func() { close(release) })
	started := make(chan struct{}, 1)
	reg.launcherMu.Lock()
	reg.launcherRankWait = func(query string) {
		if query == "firefox" {
			select {
			case started <- struct{}{}:
			default:
			}
			<-release
		}
	}
	reg.launcherMu.Unlock()

	activateSettingsAction(t, reg, settingsHost, "applications:hidden:firefox.desktop")
	select {
	case <-started:
	case <-time.After(asyncTestWait):
		t.Fatal("preference update did not requery the current launcher search")
	}

	reg.mu.Lock()
	launcherHost = reg.panelHosts[PanelLauncher]
	if launcherHost == nil || !launcherHost.launcherAwaiting {
		reg.mu.Unlock()
		t.Fatal("Launcher did not mark its stale rows as awaiting refreshed results")
	}
	if launcherHost.menu != nil || launcherHost.launcherMenuID != "" || len(launcherHost.launcherActions) != 0 {
		reg.mu.Unlock()
		t.Fatal("preference update left a stale actions menu open")
	}
	if !launcherHost.launcherPointerPress(reg, wayland.Event{
		Kind: wayland.EventPointerPress, Button: btnRight,
		X: float64(bounds.X + 4), Y: float64(bounds.Y + 4),
	}) {
		reg.mu.Unlock()
		t.Fatal("right click on a stale launcher row was not consumed")
	}
	if launcherHost.menu != nil {
		reg.mu.Unlock()
		t.Fatal("right click reopened the stale Firefox actions menu")
	}
	launcherHost.launcherActivateSelected(reg)
	reg.mu.Unlock()

	run.mu.Lock()
	spawned := run.argv
	run.mu.Unlock()
	if spawned != nil {
		t.Fatalf("stale launcher row spawned %v while refresh was pending", spawned)
	}

	releaseOnce.Do(func() { close(release) })
	waitForLauncherState(t, reg, func(h *PanelHost) bool {
		return h != nil && !h.launcherAwaiting && slices.IndexFunc(h.launcherResults, func(result launcher.Result) bool {
			return result.Entry.ID == "firefox.desktop"
		}) < 0
	})
}

func TestLauncherDropsRankFromOldPreferenceSnapshot(t *testing.T) {
	t.Parallel()

	reg, _, reqs := openLauncherPanel(t, launcherTestEntries())
	reqs[1].Open.Callbacks.Handle(wayland.Event{Kind: wayland.EventIME, IMECommit: "firefox"})
	waitForLauncherState(t, reg, func(h *PanelHost) bool {
		return h != nil && !h.launcherAwaiting && slices.ContainsFunc(h.launcherResults, func(result launcher.Result) bool {
			return result.Entry.ID == "firefox.desktop"
		})
	})

	releaseOld, releaseNew := make(chan struct{}), make(chan struct{})
	var releaseOldOnce, releaseNewOnce sync.Once
	t.Cleanup(func() {
		releaseOldOnce.Do(func() { close(releaseOld) })
		releaseNewOnce.Do(func() { close(releaseNew) })
	})
	oldStarted, newStarted := make(chan struct{}, 1), make(chan struct{}, 1)
	applied := make(chan struct{}, 2)
	rankCalls := 0
	reg.launcherMu.Lock()
	reg.launcherRankWait = func(query string) {
		if query != "firefox" {
			return
		}
		rankCalls++ // The service serializes Rank callbacks on its worker.
		if rankCalls == 1 {
			oldStarted <- struct{}{}
			<-releaseOld
		} else if rankCalls == 2 {
			newStarted <- struct{}{}
			<-releaseNew
		}
	}
	reg.afterLauncherSnap = func() { applied <- struct{}{} }
	reg.launcherMu.Unlock()

	reg.mu.Lock()
	h := reg.panelHosts[PanelLauncher]
	reg.launcherSendQuery(h, h.query)
	reg.mu.Unlock()
	select {
	case <-oldStarted:
	case <-time.After(asyncTestWait):
		t.Fatal("old preference snapshot did not reach Rank")
	}

	reg.mu.Lock()
	prefs := reg.cfg.Applications
	prefs.Hidden = []string{"firefox.desktop"}
	reg.cfg.Applications = prefs
	reg.storeLauncherAppPreferences(prefs)
	if _, ok := reg.refreshOpenLauncherLocked(); !ok {
		reg.mu.Unlock()
		t.Fatal("open Launcher did not refresh after the preference change")
	}
	reg.mu.Unlock()
	releaseOldOnce.Do(func() { close(releaseOld) })
	select {
	case <-newStarted:
	case <-time.After(asyncTestWait):
		t.Fatal("new preference snapshot did not reach Rank")
	}

	select {
	case <-applied:
		t.Fatal("rank from the old preference snapshot was applied as the new query")
	case <-time.After(150 * time.Millisecond):
	}
	reg.mu.Lock()
	h = reg.panelHosts[PanelLauncher]
	awaiting := h != nil && h.launcherAwaiting
	reg.mu.Unlock()
	if !awaiting {
		t.Fatal("old preference results cleared the new query's awaiting state")
	}

	releaseOldOnce.Do(func() { close(releaseOld) })
	releaseNewOnce.Do(func() { close(releaseNew) })
	waitForLauncherState(t, reg, func(h *PanelHost) bool {
		return h != nil && !h.launcherAwaiting && !slices.ContainsFunc(h.launcherResults, func(result launcher.Result) bool {
			return result.Entry.ID == "firefox.desktop"
		})
	})
}

// Inline calc is merged into the stamped snapshot. The service composes it
// ahead of the application rows; the shell's stamp has to do the same.
func TestLauncherInlineExpressionLeadsTheResults(t *testing.T) {
	t.Parallel()

	reg, _, reqs := openLauncherPanel(t, launcherTestEntries())
	reqs[1].Open.Callbacks.Handle(wayland.Event{Kind: wayland.EventIME, IMECommit: "6*7"})
	waitForLauncherState(t, reg, func(h *PanelHost) bool {
		return h != nil && !h.launcherAwaiting && len(h.launcherResults) > 0 &&
			h.launcherResults[0].Entry.ID == "calc:42"
	})
}

func TestClassifyLauncherQueryUsesRegisteredProviderPrefix(t *testing.T) {
	provider := (&Registry{}).emojiProvider()
	got := classifyLauncherQuery(provider.Prefix+" party", []launcher.Provider{provider})
	if got.kind != launcherKindProvider || got.provider != provider.Prefix || got.passed != "party" {
		t.Fatalf("classifyLauncherQuery() = %+v, want provider %q and query %q", got, provider.Prefix, "party")
	}
}

// gh #90: a snapshot already published for query A can be applied after
// Query(B) has set launcherAwaiting. That apply must not clear the flag, and
// Enter must not spawn A's row.
func TestLauncherQueuedSnapshotDoesNotClearAwaiting(t *testing.T) {
	t.Parallel()

	reg, run, _ := openLauncherPanel(t, launcherTestEntries())
	release := make(chan struct{})
	var releaseOnce sync.Once
	releaseRank := func() { releaseOnce.Do(func() { close(release) }) }
	t.Cleanup(releaseRank)

	applied := make(chan struct{}, 4)
	started := make(chan struct{}, 1)
	relayHeld := make(chan struct{})
	releaseRelay := make(chan struct{})
	var relayOnce sync.Once
	unblockRelay := func() { relayOnce.Do(func() { close(releaseRelay) }) }
	t.Cleanup(unblockRelay)
	var heldOnce sync.Once
	reg.launcherMu.Lock()
	reg.launcherRankWait = func(query string) {
		if query != "firefox" {
			return
		}
		select {
		case started <- struct{}{}:
		default:
		}
		<-release
	}
	reg.onLauncherDequeued = func() {
		heldOnce.Do(func() {
			close(relayHeld)
			<-releaseRelay
		})
	}
	reg.launcherMu.Unlock()

	reg.mu.Lock()
	h := reg.panelHosts[PanelLauncher]
	if !h.fieldChanged(reg, &ui.Node{Name: "Search"}, ui.NewField("nautilus")) {
		reg.mu.Unlock()
		t.Fatal("query A was not handled")
	}
	reg.mu.Unlock()
	select {
	case <-relayHeld:
	case <-time.After(2 * time.Second):
		t.Fatal("query A published nothing for the relay to hold")
	}

	reg.mu.Lock()
	h = reg.panelHosts[PanelLauncher]
	if !h.fieldChanged(reg, &ui.Node{Name: "Search"}, ui.NewField("firefox")) {
		reg.mu.Unlock()
		t.Fatal("query B was not handled")
	}
	reg.mu.Unlock()
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("query B did not reach rank")
	}
	// B is ranked but not published. The relay is paused after dequeuing A,
	// before it can apply the snapshot. Enter must not run A's row.
	reg.mu.Lock()
	h = reg.panelHosts[PanelLauncher]
	h.launcherActivateSelected(reg)
	reg.afterLauncherSnap = func() {
		select {
		case applied <- struct{}{}:
		default:
		}
	}
	awaitingBefore := h.launcherAwaiting
	genB := h.launcherQueryGen
	reg.mu.Unlock()
	if !awaitingBefore {
		t.Fatal("query B did not set awaiting")
	}
	unblockRelay()

	select {
	case <-applied:
	case <-time.After(2 * time.Second):
		t.Fatal("relay did not finish the queued snapshot")
	}
	run.mu.Lock()
	spawned := run.argv
	run.mu.Unlock()
	if spawned != nil {
		t.Fatalf("Enter ran %v from the previous query's queued snapshot", spawned)
	}

	reg.mu.Lock()
	h = reg.panelHosts[PanelLauncher]
	if !h.launcherAwaiting || h.launcherQueryGen != genB {
		awaiting, gen := h.launcherAwaiting, h.launcherQueryGen
		reg.mu.Unlock()
		t.Fatalf("queued snapshot cleared awaiting=%v gen=%d, want awaiting gen %d", awaiting, gen, genB)
	}
	if len(h.launcherResults) == 1 && h.launcherResults[0].Entry.ID == "nautilus.desktop" {
		reg.mu.Unlock()
		t.Fatal("queued snapshot installed the previous query's row")
	}
	h.launcherActivateSelected(reg)
	reg.mu.Unlock()

	time.Sleep(150 * time.Millisecond)
	run.mu.Lock()
	spawned = run.argv
	run.mu.Unlock()
	if spawned != nil {
		t.Fatalf("Enter after the queued snapshot ran %v", spawned)
	}

	releaseRank()
	waitForLauncherState(t, reg, func(h *PanelHost) bool {
		return h != nil && !h.launcherAwaiting && len(h.launcherResults) > 0 &&
			h.launcherResults[0].Entry.ID == "firefox.desktop"
	})
	reg.mu.Lock()
	reg.panelHosts[PanelLauncher].launcherActivateSelected(reg)
	reg.mu.Unlock()
	if got := run.waitArgv(t); len(got) == 0 || got[len(got)-1] != "firefox" {
		t.Fatalf("Enter after B's snapshot ran %v, want firefox", got)
	}
}

// setLauncherDisplay changes the saved display mode and rebuilds the open
// launcher, the way the next open would see it.
func setLauncherDisplay(t *testing.T, reg *Registry, mode string) {
	t.Helper()
	reg.mu.Lock()
	defer reg.mu.Unlock()
	reg.cfg.Launcher.Display = mode
	if h := reg.panelHosts[PanelLauncher]; h != nil {
		reg.rebuildPanel(h)
	}
}

func TestLauncherLayoutFallsBackToListForMixedRows(t *testing.T) {
	t.Parallel()

	reg, _, reqs := openLauncherPanel(t, launcherTestEntries())
	setLauncherDisplay(t, reg, config.LauncherDisplayGrid)
	reg.mu.Lock()
	h := reg.panelHosts[PanelLauncher]
	if got := launcherLayoutFor(reg, h); got.mode != config.LauncherDisplayGrid {
		reg.mu.Unlock()
		t.Fatalf("empty query layout = %q, want grid", got.mode)
	}
	reg.mu.Unlock()

	reqs[1].Open.Callbacks.Handle(wayland.Event{Kind: wayland.EventIME, IMECommit: "6*7"})
	waitForLauncherState(t, reg, func(h *PanelHost) bool {
		return h != nil && !h.launcherAwaiting && slices.ContainsFunc(h.launcherResults, func(r launcher.Result) bool {
			return r.Entry.IconName == "glyph:calculate"
		})
	})
	reg.mu.Lock()
	got := launcherLayoutFor(reg, h)
	reg.mu.Unlock()
	if got.mode != config.LauncherDisplayList || got.slot != launcherSlotHeight || got.columns != 1 {
		t.Fatalf("calculator query layout = %+v, want the list", got)
	}

	reg.mu.Lock()
	h.query = "/emo smile"
	h.search = ui.NewField(h.query)
	reg.launcherSendQuery(h, h.query)
	reg.mu.Unlock()
	waitForLauncherState(t, reg, func(h *PanelHost) bool { return h != nil && !h.launcherAwaiting })
	reg.mu.Lock()
	got = launcherLayoutFor(reg, h)
	reg.mu.Unlock()
	if got.mode != config.LauncherDisplayList {
		t.Fatalf("emoji query layout = %q, want list", got.mode)
	}
}

func TestLauncherCompactRowsDropTheComment(t *testing.T) {
	t.Parallel()

	reg, _, _ := openLauncherPanel(t, launcherTestEntries())
	setLauncherDisplay(t, reg, config.LauncherDisplayCompact)
	h := launcherHost(t, reg)
	reg.mu.Lock()
	defer reg.mu.Unlock()
	list := launcherListNode(t, h)
	if list.ItemHeight != 48 {
		t.Fatalf("compact ItemHeight = %d, want 48", list.ItemHeight)
	}
	for i := range h.launcherResults {
		row := list.Item(i)
		comment := h.launcherResults[i].Entry.Comment
		if comment != "" && treeHasText(row, comment) {
			t.Fatalf("compact row %d shows its comment %q", i, comment)
		}
		if !treeHasText(row, h.launcherResults[i].Entry.Name) {
			t.Fatalf("compact row %d lost its name", i)
		}
		icon := launcherRowIcon(row)
		if icon == nil || icon.Width != launcherCompactIcon || icon.Height != launcherCompactIcon {
			t.Fatalf("compact row %d icon = %+v, want a %d px slot", i, icon, launcherCompactIcon)
		}
	}
}

func TestLauncherListIsUnchangedByTheSetting(t *testing.T) {
	t.Parallel()

	reg, _, _ := openLauncherPanel(t, launcherTestEntries())
	setLauncherDisplay(t, reg, config.LauncherDisplayList)
	h := launcherHost(t, reg)
	reg.mu.Lock()
	defer reg.mu.Unlock()
	list := launcherListNode(t, h)
	if list.ItemHeight != 76 || launcherSlotHeight != 76 {
		t.Fatalf("list ItemHeight = %d (slot %d), want 76", list.ItemHeight, launcherSlotHeight)
	}
	for i, res := range h.launcherResults {
		if res.Entry.Comment != "" && !treeHasText(list.Item(i), res.Entry.Comment) {
			t.Fatalf("list row %d lost its comment %q", i, res.Entry.Comment)
		}
	}
	icon := launcherRowIcon(list.Item(0))
	if icon == nil || len(icon.Children) != 1 || icon.Children[0].Kind != ui.KindText {
		t.Fatalf("list fallback icon no longer uses its original capsule text layout: %+v", icon)
	}
}

// launcherRowIcon is the first square glyph slot or image in a row.
func launcherRowIcon(n *ui.Node) *ui.Node {
	if n == nil {
		return nil
	}
	if (n.Kind == ui.KindCapsule && n.Width > 0 && n.Width == n.Height) || n.Kind == ui.KindImage {
		return n
	}
	for _, c := range n.Children {
		if got := launcherRowIcon(c); got != nil {
			return got
		}
	}
	return nil
}

// openLauncherGrid opens a grid launcher over n generated entries, A0000
// first, so result i is entry i.
func openLauncherGrid(t *testing.T, n int) (*Registry, []wayland.AuxRequest, *PanelHost) {
	t.Helper()
	reg, _, reqs := openLauncherPanel(t, alphabetEntries(n))
	setLauncherDisplay(t, reg, config.LauncherDisplayGrid)
	return reg, reqs, launcherHost(t, reg)
}

func TestLauncherGridKeysMoveByTileAndRow(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		from int
		key  uint32
		want int
	}{
		{"right moves one tile", 0, keyRight, 1},
		{"left moves one tile", 1, keyLeft, 0},
		{"right crosses into the next row", 4, keyRight, 5},
		{"down moves one row", 0, keyDown, 5},
		{"down into a short row clamps to the last tile", 3, keyDown, 6},
		{"up moves one row", 6, keyUp, 1},
		{"left at the first tile stays", 0, keyLeft, 0},
		{"end goes to the last tile", 2, keyEnd, 6},
		{"home goes to the first tile", 6, keyHome, 0},
		{"page down clamps to the last tile", 0, keyPageDown, 6},
		{"page up clamps to the first tile", 6, keyPageUp, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			reg, reqs, h := openLauncherGrid(t, 7)
			reg.mu.Lock()
			if l := launcherLayoutFor(reg, h); l.columns != launcherGridColumns || launcherGridColumns != 5 {
				reg.mu.Unlock()
				t.Fatalf("grid columns = %d, want 5", l.columns)
			}
			h.launcherSel = tc.from
			reg.mu.Unlock()
			pressLauncherKey(reqs, tc.key)
			reg.mu.Lock()
			got := h.launcherSel
			reg.mu.Unlock()
			if got != tc.want {
				t.Fatalf("from %d, key %d: selection = %d, want %d", tc.from, tc.key, got, tc.want)
			}
		})
	}

	t.Run("list keeps left and right on the caret", func(t *testing.T) {
		reg, _, reqs := openLauncherPanel(t, alphabetEntries(7))
		reqs[1].Open.Callbacks.Handle(wayland.Event{Kind: wayland.EventIME, IMECommit: "en"})
		waitForLauncherState(t, reg, func(h *PanelHost) bool { return h != nil && !h.launcherAwaiting })
		h := launcherHost(t, reg)
		reg.mu.Lock()
		h.launcherSel = 2
		reg.mu.Unlock()
		pressLauncherKey(reqs, keyLeft)
		reg.mu.Lock()
		sel, cursor := h.launcherSel, h.search.Node("").Cursor
		reg.mu.Unlock()
		if sel != 2 || cursor != 1 {
			t.Fatalf("list Left: selection %d, caret %d; want 2 and 1", sel, cursor)
		}
		pressLauncherKey(reqs, keyRight)
		reg.mu.Lock()
		sel, cursor = h.launcherSel, h.search.Node("").Cursor
		reg.mu.Unlock()
		if sel != 2 || cursor != 2 {
			t.Fatalf("list Right: selection %d, caret %d; want 2 and 2", sel, cursor)
		}
	})
}

func TestLauncherGridWheelMovesByVisualRows(t *testing.T) {
	t.Parallel()

	reg, _, h := openLauncherGrid(t, 15)
	reg.mu.Lock()
	h.launcherSel = 0
	handled := h.scrollAxis(reg, wayland.Event{Kind: wayland.EventPointerAxis, AxisValue120: 120})
	got := h.launcherSel
	reg.mu.Unlock()
	if !handled || got != launcherGridColumns {
		t.Fatalf("wheel selection = %d, handled %v; want one visual row (%d apps)", got, handled, launcherGridColumns)
	}
}

func TestLauncherGridUpReachesHistoryOnlyFromTheFirstRow(t *testing.T) {
	t.Parallel()

	reg, reqs, h := openLauncherGrid(t, 7)
	reg.mu.Lock()
	reg.launcherQueryHistory = []string{"firefox"}
	h.launcherSel = 6
	reg.mu.Unlock()
	pressLauncherKey(reqs, keyUp)
	reg.mu.Lock()
	sel, query := h.launcherSel, h.query
	reg.mu.Unlock()
	if sel != 1 || query != "" {
		t.Fatalf("Up from tile 6: selection %d, query %q; want 1 and empty", sel, query)
	}
	pressLauncherKey(reqs, keyUp)
	reg.mu.Lock()
	query = h.query
	reg.mu.Unlock()
	if query != "firefox" {
		t.Fatalf("Up from the first row recalled %q, want firefox", query)
	}
}

func TestLauncherGridTilesCentreTheIcon(t *testing.T) {
	t.Parallel()

	reg, _, h := openLauncherGrid(t, 7)
	reg.mu.Lock()
	defer reg.mu.Unlock()
	l := launcherLayoutFor(reg, h)
	if l.cellWidth <= 0 || l.tileHeight <= 0 {
		t.Fatalf("grid geometry = %+v", l)
	}
	tile := launcherGridTile(reg, h, h.launcherResults[0], 0, l)
	root := &ui.Node{Kind: ui.KindColumn, Children: []*ui.Node{tile}}
	if err := ui.LayoutColumn(root, ui.Rect{W: l.cellWidth, H: l.tileHeight}, h.measureText()); err != nil {
		t.Fatalf("lay out tile: %v", err)
	}
	icon := launcherRowIcon(tile)
	if icon == nil {
		t.Fatal("tile has no icon slot")
	}
	left := icon.Bounds.X - tile.Bounds.X
	right := tile.Bounds.X + tile.Bounds.W - (icon.Bounds.X + icon.Bounds.W)
	if d := left - right; d < -1 || d > 1 {
		t.Fatalf("icon sits %d from the left and %d from the right of its tile", left, right)
	}
	if top := icon.Bounds.Y - tile.Bounds.Y; top != launcherGridPad {
		t.Fatalf("icon sits %d below the tile top, want %d", top, launcherGridPad)
	}
	var glyph *ui.Node
	walkNodes(icon, func(n *ui.Node) {
		if glyph == nil && n.Kind == ui.KindText {
			glyph = n
		}
	})
	if glyph == nil {
		t.Fatal("fallback icon has no text glyph")
	}
	if glyph.Bounds.W >= icon.Bounds.W || glyph.Bounds.H >= icon.Bounds.H {
		t.Fatalf("fallback glyph fills icon bounds: glyph %+v, icon %+v", glyph.Bounds, icon.Bounds)
	}
	dx := 2*(glyph.Bounds.X-icon.Bounds.X) + glyph.Bounds.W - icon.Bounds.W
	dy := 2*(glyph.Bounds.Y-icon.Bounds.Y) + glyph.Bounds.H - icon.Bounds.H
	if dx < -2 || dx > 2 || dy < -2 || dy > 2 {
		t.Fatalf("fallback glyph bounds %+v are not centred in icon bounds %+v", glyph.Bounds, icon.Bounds)
	}
}

func TestLauncherGridGeometryUsesFiveColumns(t *testing.T) {
	t.Parallel()

	reg, _, h := openLauncherGrid(t, 7)
	reg.mu.Lock()
	defer reg.mu.Unlock()
	l := launcherLayoutFor(reg, h)
	listWidth := h.place.Panel.W - 2*theme.MarginL
	wantCell := (listWidth - (launcherGridColumns-1)*launcherGridGap) / launcherGridColumns
	lineHeight := launcherCaptionLineHeight(h)
	wantTileHeight := 2*launcherGridPad + launcherIconSlot + theme.MarginS + 2*lineHeight
	wantSlot := wantTileHeight + launcherGridGap
	if l.cellWidth != wantCell || l.tileHeight != wantTileHeight || l.slot != wantSlot {
		t.Fatalf("grid geometry = cell %d, tile %d, slot %d; want %d, %d, %d",
			l.cellWidth, l.tileHeight, l.slot, wantCell, wantTileHeight, wantSlot)
	}
	list := launcherListNode(t, h)
	if list.ItemHeight != wantSlot || list.ItemCount != 2 {
		t.Fatalf("virtual grid = %d px × %d rows, want %d px × 2 rows", list.ItemHeight, list.ItemCount, wantSlot)
	}
	var first, last *ui.Node
	if len(list.Children) == 0 {
		t.Fatal("grid has no visible first row")
	}
	walkNodes(list.Children[0], func(n *ui.Node) {
		if !strings.HasPrefix(n.Action, "launch:") {
			return
		}
		if first == nil {
			first = n
		}
		last = n
	})
	left := 0
	right := 0
	if first != nil && last != nil {
		left = first.Bounds.X - list.Bounds.X
		right = list.Bounds.X + list.Bounds.W - (last.Bounds.X + last.Bounds.W)
	}
	wantRight := list.Bounds.W - (launcherGridColumns*wantCell + (launcherGridColumns-1)*launcherGridGap)
	if first == nil || last == nil || left != 0 || right != wantRight {
		t.Fatalf("grid row margins = %d/%d px, want 0/%d px", left, right, wantRight)
	}
}

func TestLauncherGridMenuSpansTheRow(t *testing.T) {
	t.Parallel()

	entries := alphabetEntries(7)
	entries[6].Actions = []launcher.Action{{ID: "new-window", Name: "New Window", Argv: []string{"true"}}}
	reg, _, _ := openLauncherPanel(t, entries)
	setLauncherDisplay(t, reg, config.LauncherDisplayGrid)
	h := launcherHost(t, reg)
	reg.mu.Lock()
	tile := findAction(h.root, "launch:6")
	if tile == nil {
		reg.mu.Unlock()
		t.Fatal("laid-out grid has no tile for result 6")
	}
	x := tile.Bounds.X + tile.Bounds.W/2
	y := tile.Bounds.Y + tile.Bounds.H/2
	if i, ok := launcherRowAt(h.root, x, y); !ok || i != 6 {
		reg.mu.Unlock()
		t.Fatalf("hit test at second-row tile returned %d, %v; want 6", i, ok)
	}
	if !h.launcherPointerPress(reg, wayland.Event{Kind: wayland.EventPointerPress, Button: btnRight, X: float64(x), Y: float64(y)}) {
		reg.mu.Unlock()
		t.Fatal("right click on tile was not handled")
	}
	list := launcherListNode(t, h)
	var item *ui.Node
	for _, child := range list.Children {
		if child.Kind == ui.KindMenu {
			item = child
			break
		}
	}
	width := h.place.Panel.W - 2*theme.MarginL
	listBounds, listSlot := list.Bounds, list.ItemHeight
	wantY := tile.Bounds.Y - launcherGridGap/2
	reg.mu.Unlock()
	if item == nil || item.Kind != ui.KindMenu || item.Width != width || item.Bounds.W != width ||
		item.Bounds.X != listBounds.X || item.Bounds.Y != wantY || item.Bounds.H != listSlot {
		t.Fatalf("grid row 1 with an open menu = %+v, want a %dx%d menu at (%d,%d)",
			item, width, listSlot, listBounds.X, wantY)
	}
}

func TestLauncherGridClickLaunchesTheClickedTile(t *testing.T) {
	t.Parallel()

	reg, run, _ := openLauncherPanel(t, alphabetEntries(7))
	setLauncherDisplay(t, reg, config.LauncherDisplayGrid)
	h := launcherHost(t, reg)
	reg.mu.Lock()
	tile := findAction(h.root, "launch:6")
	if tile == nil {
		reg.mu.Unlock()
		t.Fatal("laid-out grid has no tile for result 6")
	}
	want := append([]string{"niri", "msg", "action", "spawn", "--"}, h.launcherResults[6].Entry.Argv...)
	x := tile.Bounds.X + tile.Bounds.W/2
	y := tile.Bounds.Y + tile.Bounds.H/2
	handled := h.launcherPointerPress(reg, wayland.Event{Kind: wayland.EventPointerPress, Button: btnLeft, X: float64(x), Y: float64(y)})
	reg.mu.Unlock()
	if !handled {
		t.Fatal("click on grid tile was not handled")
	}
	if got := run.waitArgv(t); !slices.Equal(got, want) {
		t.Fatalf("clicked tile launched %v, want %v", got, want)
	}
}

func TestLauncherGridFooterNamesArrowKeys(t *testing.T) {
	t.Parallel()

	reg, _, h := openLauncherGrid(t, 7)
	reg.mu.Lock()
	grid := launcherFooter(h, 7, launcherLayoutFor(reg, h))
	list := launcherFooter(h, 7, launcherLayout{mode: config.LauncherDisplayList, columns: 1, slot: launcherSlotHeight})
	reg.mu.Unlock()
	if !treeHasText(grid, "7 apps • "+launcherGridHints) {
		t.Fatal("grid footer does not name the arrow keys")
	}
	if !treeHasText(list, "7 apps • "+launcherHints) {
		t.Fatal("list footer changed")
	}
}
