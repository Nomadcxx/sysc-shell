package shell

import (
	"testing"
	"time"

	"github.com/Nomadcxx/sysc-shell/internal/platform/wayland"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

func TestPanelPlacementPreservesCurrentMarginsWithoutConflicts(t *testing.T) {
	base := Placement{
		Output:  ui.Rect{W: 1920, H: 1080},
		BarZone: 40,
		Gap:     8,
		Padding: 8,
		Panel:   ui.Rect{W: 300, H: 200},
	}
	for _, tc := range []struct {
		name  string
		place Placement
	}{
		{"top left", func() Placement { p := base; p.BarEdge, p.Align = "top", "left"; return p }()},
		{"top centre", func() Placement { p := base; p.BarEdge, p.Align = "top", "center"; return p }()},
		{"top right", func() Placement { p := base; p.BarEdge, p.Align = "top", "right"; return p }()},
		{"top trigger", func() Placement { p := base; p.BarEdge, p.AnchorX = "top", 1360; return p }()},
		{"bottom left", func() Placement { p := base; p.BarEdge, p.Align = "bottom", "left"; return p }()},
		{"bottom centre", func() Placement { p := base; p.BarEdge, p.Align = "bottom", "center"; return p }()},
		{"bottom right", func() Placement { p := base; p.BarEdge, p.Align = "bottom", "right"; return p }()},
		{"bottom trigger", func() Placement { p := base; p.BarEdge, p.AnchorX = "bottom", 480; return p }()},
		{"opaque attached bar", func() Placement {
			p := base
			p.BarEdge, p.Align, p.Overlap = "top", "left", 1
			p.BarShape, p.BarGap, p.BarRadius, p.Fillet = "attached", 8, 16, 24
			return p
		}()},
		{"centred modal", func() Placement { p := base; p.CenterY, p.Align = true, "center"; return p }()},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := tc.place
			want := p.Margins()
			baseRect := p.panelRect()
			got := panelPlacement(baseRect, p.workArea(baseRect), nil,
				ui.Size{W: p.Panel.W, H: p.Panel.H}, false)
			if marginsFor(got, p) != want {
				t.Fatalf("no-conflict margins = %+v, want existing Placement.Margins %+v", marginsFor(got, p), want)
			}
		})
	}
}

func TestPanelPlacementChoosesTheNearestClearSide(t *testing.T) {
	base := ui.Rect{X: 810, Y: 48, W: 300, H: 200}
	work := ui.Rect{X: 8, Y: 48, W: 1904, H: 200}
	for _, tc := range []struct {
		name string
		open []ui.Rect
		want int
	}{
		{"left is nearer", []ui.Rect{{X: 900, Y: 48, W: 300, H: 200}}, 600},
		{"right is nearer", []ui.Rect{{X: 700, Y: 48, W: 300, H: 200}}, 1000},
		{"other edge stays put", []ui.Rect{{X: 810, Y: 832, W: 300, H: 200}}, 810},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := panelPlacement(base, work, tc.open, ui.Size{W: 300, H: 200}, false)
			if got.X != tc.want {
				t.Fatalf("panel x = %d, want %d (%v)", got.X, tc.want, got)
			}
		})
	}
	open := []ui.Rect{{X: 700, Y: 48, W: 300, H: 200}, {X: 1050, Y: 48, W: 300, H: 200}}
	got := panelPlacement(base, work, open, ui.Size{W: 300, H: 200}, false)
	for _, r := range open {
		if got.X < r.X+r.W && r.X < got.X+got.W && got.Y < r.Y+r.H && r.Y < got.Y+got.H {
			t.Fatalf("placed %v still overlaps %v", got, r)
		}
	}
}

func TestSidePanelPlacementChoosesTheNearestClearY(t *testing.T) {
	base := ui.Rect{X: 60, Y: 332, W: 400, H: 200}
	work := ui.Rect{X: 60, Y: 8, W: 400, H: 848}
	for _, tc := range []struct {
		name string
		open []ui.Rect
		want int
	}{
		{"leading wins equal distance", []ui.Rect{{X: 60, Y: 332, W: 400, H: 200}}, 132},
		{"cross-axis gap stays put", []ui.Rect{{X: 500, Y: 332, W: 400, H: 200}}, 332},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := panelPlacement(base, work, tc.open, ui.Size{W: 400, H: 200}, true)
			if got.Y != tc.want || got.X != base.X {
				t.Fatalf("placed rect = %+v, want x=%d y=%d", got, base.X, tc.want)
			}
		})
	}
}

func TestBarRectOnOutputAppliesTheSurfaceOffsetOnce(t *testing.T) {
	local := ui.Rect{X: 12, Y: 24, W: 30, H: 40}
	for _, tc := range []struct {
		edge string
		want ui.Rect
	}{
		{"top", local},
		{"bottom", ui.Rect{X: 12, Y: 824, W: 30, H: 40}},
		{"left", local},
		{"right", ui.Rect{X: 1488, Y: 24, W: 30, H: 40}},
	} {
		if got := barRectOnOutput(local, tc.edge, 1536, 864, 60, 64); got != tc.want {
			t.Errorf("%s trigger = %+v, want %+v", tc.edge, got, tc.want)
		}
	}
}

func TestSidePanelArrangementReflowsAlongYAndRestores(t *testing.T) {
	for _, edge := range []string{"left", "right"} {
		t.Run(edge, func(t *testing.T) {
			reg := newPanelRegistry(t)
			cfg := reg.cfg
			cfg.Bar.Edge = edge
			reg.mu.Lock()
			reg.cfg = cfg
			reg.mu.Unlock()
			bar, err := NewWithTheme(ThemeFrom(cfg, cfg.Bar).WithCompositor(true), cfg.Bar, "DP-1")
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(bar.stopAnimation)
			if err := bar.Configure(cfg.Bar.SurfaceExtent(), 864, 120); err != nil {
				t.Fatal(err)
			}
			bar.setOutputSize(1536, 864)
			reg.setTestBar(7, bar)
			trig := Trigger{BarEdge: edge, BarZone: cfg.Bar.Extent(), Align: "center", OutW: 1536, OutH: 864}
			if err := reg.OpenPanel(PanelClock, 7, trig); err != nil {
				t.Fatal(err)
			}
			_ = drainAux(t, reg, 2)
			reg.mu.Lock()
			first := reg.panelHosts[PanelClock]
			if first == nil {
				reg.mu.Unlock()
				t.Fatal("first panel did not open")
			}
			originalRect := first.rect
			reg.mu.Unlock()

			if err := reg.OpenPanel(PanelSession, 7, trig); err != nil {
				t.Fatal(err)
			}
			_ = drainAux(t, reg, 2)
			reg.mu.Lock()
			firstRect, secondRect := reg.panelHosts[PanelClock].rect, reg.panelHosts[PanelSession].rect
			reg.mu.Unlock()
			if edge == "left" && firstRect.X != secondRect.X || edge == "right" && firstRect.X+firstRect.W != secondRect.X+secondRect.W {
				t.Fatalf("panels left the same attached edge: first=%+v second=%+v", firstRect, secondRect)
			}
			if overlaps(firstRect, secondRect) {
				t.Fatalf("side panels still overlap: %+v and %+v", firstRect, secondRect)
			}

			reg.ClosePanel(PanelSession)
			_ = drainAux(t, reg, 2)
			reg.mu.Lock()
			got := reg.panelHosts[PanelClock].rect
			reg.mu.Unlock()
			if got != originalRect {
				t.Fatalf("remaining panel moved to %+v after close, want restored %+v", got, originalRect)
			}
		})
	}
}

func TestOpenPanelStoresThePlacedRectAndMargins(t *testing.T) {
	t.Parallel()
	reg := newPanelRegistry(t)
	trig := Trigger{BarEdge: "top", BarZone: 40, Align: "right", OutW: 1920, OutH: 1080}
	if err := reg.OpenPanel(PanelSession, 7, trig); err != nil {
		t.Fatal(err)
	}
	reqs := drainAux(t, reg, 2)
	reg.mu.Lock()
	h := reg.panelHosts[PanelSession]
	place, rect := h.place, h.rect
	reg.mu.Unlock()
	margins := marginsFor(rect, place)
	if int(reqs[1].Open.MarginLeft) != margins.Left || int(reqs[1].Open.MarginTop) != margins.Top {
		t.Fatalf("spec margins (%d,%d) disagree with stored rect %v", reqs[1].Open.MarginLeft, reqs[1].Open.MarginTop, rect)
	}
	if rect != place.panelRect() {
		t.Fatalf("host rect %v, want placement rect %v", rect, place.panelRect())
	}
}

func TestOpenPanelsShiftAndKeepTheirPlacementAnchor(t *testing.T) {
	t.Parallel()
	reg := newPanelRegistry(t)
	trig := Trigger{BarEdge: "top", BarZone: 40, Align: "center", OutW: 1920, OutH: 1080}
	if err := reg.OpenPanel(PanelClock, 7, trig); err != nil {
		t.Fatal(err)
	}
	_ = drainAux(t, reg, 2)
	if err := reg.OpenPanel(PanelMonitor, 7, trig); err != nil {
		t.Fatal(err)
	}
	_ = drainAux(t, reg, 2)

	reg.mu.Lock()
	defer reg.mu.Unlock()
	first, second := reg.panelHosts[PanelClock], reg.panelHosts[PanelMonitor]
	if first == nil || second == nil {
		t.Fatal("both panels should remain open")
	}
	a, b := first.rect, second.rect
	if a.X < b.X+b.W && b.X < a.X+a.W && a.Y < b.Y+b.H && b.Y < a.Y+a.H {
		t.Fatalf("open panels overlap: %v and %v", a, b)
	}
	if second.place.AnchorX != b.X+b.W/2 {
		t.Fatalf("shifted anchor x = %d, want %d", second.place.AnchorX, b.X+b.W/2)
	}
	if x, _ := second.place.layout(); x != b.X {
		t.Fatalf("placement layout x = %d, want shifted rect x %d", x, b.X)
	}
}

func TestOutsideDismissalClosesNewestPanelAndKeepsSharedShield(t *testing.T) {
	t.Parallel()
	reg := newPanelRegistry(t)
	trig := Trigger{BarEdge: "top", BarZone: 40, Align: "center", OutW: 1920, OutH: 1080}
	if err := reg.OpenPanel(PanelClock, 7, trig); err != nil {
		t.Fatal(err)
	}
	firstRequests := drainAux(t, reg, 2)
	shield := firstRequests[0].Open
	if shield == nil || shield.Callbacks.Handle == nil {
		t.Fatal("first panel did not open an interactive shield")
	}
	reg.mu.Lock()
	preferredX := reg.panelHosts[PanelClock].preferredMain
	reg.mu.Unlock()

	if err := reg.OpenPanel(PanelMonitor, 7, trig); err != nil {
		t.Fatal(err)
	}
	secondRequests := drainAux(t, reg, 2)
	updatedFirst, openedSecond := false, false
	for _, req := range secondRequests {
		if req.Update != nil && req.ID == panelSurfaceID(PanelClock) {
			updatedFirst = true
		}
		if req.Open != nil {
			if req.Open.ID == shield.ID {
				t.Fatal("second panel reopened the output shield")
			}
			openedSecond = req.Open.ID == panelSurfaceID(PanelMonitor)
		}
	}
	if !updatedFirst || !openedSecond {
		t.Fatalf("second panel requests did not reflow the first and open the second: %+v", secondRequests)
	}

	reg.mu.Lock()
	first := reg.panelHosts[PanelClock]
	second := reg.panelHosts[PanelMonitor]
	if first == nil || second == nil {
		reg.mu.Unlock()
		t.Fatal("both panels must stay open behind one shield")
	}
	first.shieldQuiet = time.Time{}
	reg.mu.Unlock()

	if !shield.Callbacks.Handle(wayland.Event{Kind: wayland.EventPointerPress}) {
		t.Fatal("outside press was not handled")
	}
	closed := drainAux(t, reg, 2)
	closedNewest, restoredEarlier := false, false
	for _, req := range closed {
		closedNewest = closedNewest || req.Open == nil && req.ID == panelSurfaceID(PanelMonitor)
		restoredEarlier = restoredEarlier || req.Update != nil && req.ID == panelSurfaceID(PanelClock)
	}
	if !closedNewest || !restoredEarlier {
		t.Fatalf("outside press did not close the newest and restore the earlier panel: %+v", closed)
	}
	select {
	case req := <-reg.AuxRequests():
		t.Fatalf("shield closed while the earlier panel remained: %+v", req)
	default:
	}

	reg.mu.Lock()
	firstHost := reg.panelHosts[PanelClock]
	firstRemains := firstHost != nil
	secondRemains := reg.panelHosts[PanelMonitor] != nil
	firstX := 0
	if firstHost != nil {
		firstX = firstHost.rect.X
	}
	reg.mu.Unlock()
	if !firstRemains || secondRemains {
		t.Fatal("outside dismissal must keep the earlier panel and close the newest")
	}
	if firstX != preferredX {
		t.Fatalf("earlier panel x = %d after dismissal, want its original x %d", firstX, preferredX)
	}

	reg.ClosePanel(PanelClock)
	lastRequests := drainAux(t, reg, 2)
	if lastRequests[1].ID != shield.ID {
		t.Fatalf("last panel close removed shield %q, want %q", lastRequests[1].ID, shield.ID)
	}
	if _, _, ok := reg.roots.current(); ok {
		t.Fatal("last panel close kept the panel group root open")
	}
}

func TestOpeningOtherModalRootClosesTheWholePanelGroup(t *testing.T) {
	t.Parallel()
	reg := newPanelRegistry(t)
	if err := reg.OpenPanel(PanelClock, 7, Trigger{}); err != nil {
		t.Fatal(err)
	}
	_ = drainAux(t, reg, 2)
	if err := reg.OpenPanel(PanelMonitor, 7, Trigger{}); err != nil {
		t.Fatal(err)
	}
	_ = drainAux(t, reg, 2)

	reg.mu.Lock()
	reg.roots.openRoot(trayMenuRoot(7))
	reg.mu.Unlock()
	_ = drainAux(t, reg, 3)

	reg.mu.Lock()
	defer reg.mu.Unlock()
	if len(reg.panelHosts) != 0 || len(reg.panels.open) != 0 {
		t.Fatalf("modal replacement left panels open: hosts=%d panel set=%d", len(reg.panelHosts), len(reg.panels.open))
	}
	if owner, _, ok := reg.roots.current(); !ok || owner != trayMenuRoot(7) {
		t.Fatalf("root after panel replacement = %+v (open=%v), want tray menu", owner, ok)
	}
}

func TestPanelShieldsAreSharedWithinAnOutput(t *testing.T) {
	t.Parallel()
	reg := newPanelRegistry(t)
	if err := reg.OpenPanel(PanelClock, 7, Trigger{OutW: 1280, OutH: 800}); err != nil {
		t.Fatal(err)
	}
	firstRequests := drainAux(t, reg, 2)
	if err := reg.OpenPanel(PanelMonitor, 8, Trigger{OutW: 1920, OutH: 1080}); err != nil {
		t.Fatal(err)
	}
	secondRequests := drainAux(t, reg, 2)
	if firstRequests[0].Open == nil || secondRequests[0].Open == nil ||
		firstRequests[0].Open.ID == secondRequests[0].Open.ID {
		t.Fatal("panels on separate outputs should have separate shields")
	}

	reg.ClosePanel(PanelClock)
	_ = drainAux(t, reg, 2)
	reg.mu.Lock()
	firstClosed := reg.panelHosts[PanelClock] == nil
	secondOpen := reg.panelHosts[PanelMonitor] != nil
	_, _, rootOpen := reg.roots.current()
	reg.mu.Unlock()
	if !firstClosed || !secondOpen || !rootOpen {
		t.Fatal("closing one output's last panel closed another output's panel group")
	}

	reg.ClosePanel(PanelMonitor)
	_ = drainAux(t, reg, 2)
	if _, _, ok := reg.roots.current(); ok {
		t.Fatal("closing the last output panel kept the panel group open")
	}
}
