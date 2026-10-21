package shell

import (
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	metrics "github.com/Nomadcxx/sysc-metrics"
	"github.com/Nomadcxx/sysc-notify/protocol"

	"github.com/Nomadcxx/sysc-shell/internal/config"
	"github.com/Nomadcxx/sysc-shell/internal/platform/wayland"
	"github.com/Nomadcxx/sysc-shell/internal/platform/wayland/layershell"
	"github.com/Nomadcxx/sysc-shell/internal/render"
	"github.com/Nomadcxx/sysc-shell/internal/services"
	"github.com/Nomadcxx/sysc-shell/internal/theme"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

func TestPanelHostRenderPaintsClockText(t *testing.T) {
	t.Parallel()
	reg := newPanelRegistry(t)
	if err := reg.OpenPanel(PanelClock, 7, Trigger{}); err != nil {
		t.Fatal(err)
	}
	reqs := drainAux(t, reg, 2)
	panel := reqs[1].Open
	settleHostAnimation(reg, reg.panelHosts[PanelClock])
	if err := panel.Callbacks.Configure(360, 420, 120); err != nil {
		t.Fatal(err)
	}
	const w, hgt = 360, 420
	pix := make([]byte, w*hgt*4)
	if err := panel.Callbacks.Render(pix, w, hgt, w*4); err != nil {
		t.Fatal(err)
	}
	fg := reg.panelHosts[PanelClock].theme.Foreground
	n := 0
	for i := 0; i+3 < len(pix); i += 4 {
		if pix[i] == fg.R && pix[i+1] == fg.G && pix[i+2] == fg.B && pix[i+3] == fg.A {
			n++
		}
	}
	if n == 0 {
		t.Fatal("clock panel painted no foreground text")
	}
}

func TestPanelPointerRippleUsesPressPoint(t *testing.T) {
	a, _ := newTestAnimator(true)
	n := &ui.Node{Kind: ui.KindButton, Action: "button", Focusable: true,
		Bounds: ui.Rect{X: 5, Y: 6, W: 10, H: 12}}
	h := &PanelHost{
		id: PanelClock, output: 7, anim: a,
		pointer: interaction{stateLayer: true},
		root:    &ui.Node{Kind: ui.KindRow, Children: []*ui.Node{n}},
		focus:   []*ui.Node{n}, roving: ui.Roving{Count: 1},
	}
	h.handle(&Registry{})(wayland.Event{Kind: wayland.EventPointerPress, X: 8.9, Y: 9.9})
	if phase, x, y, ok := a.Ripple("button"); !ok || phase != 1 || x != 8 || y != 9 {
		t.Fatalf("pointer ripple = %v at %d,%d (ok %v), want centre-independent event point 8,9", phase, x, y, ok)
	}
}

func TestPanelKeyboardRippleUsesControlCentre(t *testing.T) {
	a, _ := newTestAnimator(true)
	n := &ui.Node{Kind: ui.KindButton, Action: "unknown", Focusable: true,
		Bounds: ui.Rect{X: 5, Y: 7, W: 11, H: 9}}
	h := &PanelHost{
		id: PanelClock, anim: a,
		pointer: interaction{stateLayer: true},
		focus:   []*ui.Node{n}, roving: ui.Roving{Count: 1},
	}
	h.keyInput(&Registry{}, ui.KeyInput{Code: keyEnter})
	if phase, x, y, ok := a.Ripple("unknown"); !ok || phase != 1 || x != 10 || y != 11 {
		t.Fatalf("keyboard ripple = %v at %d,%d (ok %v), want centre 10,11", phase, x, y, ok)
	}
}

// Monitor cards are KindCapsule. The panel painter used to omit Capsule from
// Style, so fillRoundedRect skipped the A=0 fill and every card vanished
// into the panel background.
func TestPanelHostRenderPaintsMonitorCards(t *testing.T) {
	t.Parallel()
	reg := newPanelRegistry(t)
	withTestBar(t, reg, 7, reg.cfg)
	if err := reg.OpenPanel(PanelMonitor, 7, Trigger{BarEdge: "top", BarZone: 44}); err != nil {
		t.Fatal(err)
	}
	reqs := drainAux(t, reg, 2)
	panel := reqs[1].Open
	h := reg.panelHosts[PanelMonitor]
	settleHostAnimation(reg, h)
	h.monitorPage = monitorPageMetrics
	reg.rebuildPanel(h)
	// The target body, configured at the surface size the joints widen it to.
	size := panelTargetSize(PanelMonitor)
	w, hgt := size.W+int(panel.Width)-reg.panelHosts[PanelMonitor].place.Panel.W, size.H
	if err := panel.Callbacks.Configure(w, hgt, 120); err != nil {
		t.Fatal(err)
	}
	pix := make([]byte, w*hgt*4)
	if err := panel.Callbacks.Render(pix, w, hgt, w*4); err != nil {
		t.Fatal(err)
	}
	cards := findAllKind(h.root, ui.KindCapsule)
	if len(cards) == 0 {
		t.Fatal("monitor tree has no capsules")
	}
	card := cards[0]
	x, y := card.Bounds.X+card.Bounds.W/2, card.Bounds.Y+6
	if x < 0 || y < 0 || x >= w || y >= hgt {
		t.Fatalf("card sample %d,%d outside %dx%d", x, y, w, hgt)
	}
	i := (y*w + x) * 4
	got := Color{B: pix[i], G: pix[i+1], R: pix[i+2], A: pix[i+3]}
	if got != h.theme.Capsule {
		t.Fatalf("card fill = %+v, want Capsule %+v (panel is %+v)", got, h.theme.Capsule, h.theme.Background)
	}
	// Attached to a top bar: the body meets the bar with a square top and a
	// joint beside it, so (0,0) is painted rather than a seam of wallpaper.
	if pix[3] == 0 {
		t.Fatal("attached panel top-left is transparent; that is the gap under the bar")
	}
}

func TestOpenPanelSendsShieldThenPanel(t *testing.T) {
	t.Parallel()
	reg := newPanelRegistry(t)
	if err := reg.OpenPanel(PanelSession, 7, Trigger{BarEdge: "top", BarZone: 40, Align: "center"}); err != nil {
		t.Fatal(err)
	}
	reqs := drainAux(t, reg, 2)
	if len(reqs) != 2 || reqs[0].Open == nil || reqs[1].Open == nil ||
		!strings.HasPrefix(reqs[0].Open.ID, "shield:") ||
		!strings.HasPrefix(reqs[1].Open.ID, "panel:") {
		t.Fatalf("expected shield then panel, got %+v", reqs)
	}
	if reqs[0].Open.ExclusiveZone != -1 || reqs[1].Open.ExclusiveZone != -1 {
		t.Fatal("both surfaces must use exclusive zone -1")
	}
	if reqs[1].Open.Keyboard != keyboardExclusive {
		t.Fatal("panel must request exclusive keyboard")
	}
}

func TestShieldPressDuringOpenLeavesThePanelUp(t *testing.T) {
	t.Parallel()
	reg := newPanelRegistry(t)
	if err := reg.OpenPanel(PanelSession, 7, Trigger{BarEdge: "top", BarZone: 40}); err != nil {
		t.Fatal(err)
	}
	reqs := drainAux(t, reg, 2)
	// Exercise the opening state even if construction or scheduling used its
	// real 400ms grace. The neighboring quiet-state test fixes the other side.
	reg.mu.Lock()
	reg.panelHosts[PanelSession].shieldQuiet = time.Now().Add(time.Hour)
	reg.mu.Unlock()
	if reqs[0].Open.Callbacks.Handle(wayland.Event{Kind: wayland.EventPointerPress}) {
		t.Fatal("shield press during open reported a close")
	}
	if _, ok := reg.panels.Output(PanelSession); !ok {
		t.Fatal("shield press during open closed the panel")
	}
}

func TestShieldPressAfterQuietClosesThePanel(t *testing.T) {
	t.Parallel()
	reg := newPanelRegistry(t)
	if err := reg.OpenPanel(PanelSession, 7, Trigger{BarEdge: "top", BarZone: 40}); err != nil {
		t.Fatal(err)
	}
	reqs := drainAux(t, reg, 2)
	reg.panelHosts[PanelSession].shieldQuiet = time.Time{}
	if !reqs[0].Open.Callbacks.Handle(wayland.Event{Kind: wayland.EventPointerPress}) {
		t.Fatal("armed shield press did not close")
	}
	if _, ok := reg.panels.Output(PanelSession); ok {
		t.Fatal("armed shield press left the panel open")
	}
}

// A press the compositor delivers to the shield while the pointer is over the
// panel itself is not a click outside. Taking it as one closed Settings under
// a click on its own rail, silently (2026-09-29, laptop).
func TestShieldPressInsideThePanelKeepsItOpen(t *testing.T) {
	t.Parallel()
	reg := newPanelRegistry(t)
	if err := reg.OpenPanel(PanelSettings, 7, Trigger{BarEdge: "top", BarZone: 40, OutW: 1536, OutH: 864}); err != nil {
		t.Fatal(err)
	}
	reqs := drainAux(t, reg, 2)
	reg.mu.Lock()
	h := reg.panelHosts[PanelSettings]
	h.shieldQuiet = time.Time{}
	body := h.place.Rect()
	reg.mu.Unlock()
	inside := wayland.Event{Kind: wayland.EventPointerPress, X: float64(body.X + body.W/2), Y: float64(body.Y + body.H/2)}
	if reqs[0].Open.Callbacks.Handle(inside) {
		t.Fatal("a shield press inside the panel reported a close")
	}
	if _, ok := reg.panels.Output(PanelSettings); !ok {
		t.Fatal("a shield press inside the panel closed it")
	}
	outside := wayland.Event{Kind: wayland.EventPointerPress, X: float64(body.X + body.W + 5), Y: float64(body.Y + body.H/2)}
	if !reqs[0].Open.Callbacks.Handle(outside) {
		t.Fatal("a shield press beside the panel did not close it")
	}
}

func TestPlacementRectIsTheBodyOnTheOutput(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		p    Placement
		want ui.Rect
	}{
		{"top attached", Placement{BarEdge: "top", BarZone: 40, Output: ui.Rect{W: 1000, H: 800}, Panel: ui.Rect{W: 200, H: 100}, Align: "left", Padding: 8}, ui.Rect{X: 8, Y: 40, W: 200, H: 100}},
		{"bottom attached", Placement{BarEdge: "bottom", BarZone: 40, Output: ui.Rect{W: 1000, H: 800}, Panel: ui.Rect{W: 200, H: 100}, Align: "left", Padding: 8}, ui.Rect{X: 8, Y: 660, W: 200, H: 100}},
		{"centred below the bar", Placement{BarEdge: "top", BarZone: 40, Output: ui.Rect{W: 1000, H: 800}, Panel: ui.Rect{W: 200, H: 100}, CenterY: true, Padding: 8}, ui.Rect{X: 400, Y: 366, W: 200, H: 100}},
	} {
		if got := tc.p.Rect(); got != tc.want {
			t.Errorf("%s: Rect() = %+v, want %+v", tc.name, got, tc.want)
		}
	}
}

func TestTriggerKeepsTheOutputWidthSeparateFromASideBar(t *testing.T) {
	cfg := config.Default()
	cfg.Bar.Edge = "left"
	reg := newPanelRegistry(t)
	reg.cfg = cfg
	bar, err := NewWithTheme(ThemeFrom(cfg, cfg.Bar).WithCompositor(true), cfg.Bar, "DP-1")
	if err != nil {
		t.Fatal(err)
	}
	if err := bar.Configure(cfg.Bar.SurfaceExtent(), 864, 120); err != nil {
		t.Fatal(err)
	}
	bar.setOutputSize(1536, 864)
	reg.setTestBar(7, bar)

	reg.mu.Lock()
	got := reg.triggerLocked(7, "DP-1")
	reg.mu.Unlock()
	if got.OutW != 1536 || got.OutH != 864 {
		t.Fatalf("trigger output = %dx%d, want 1536x864", got.OutW, got.OutH)
	}
	if got.BarZone != cfg.Bar.Extent() {
		t.Fatalf("side painted cross extent = %d, want %d", got.BarZone, cfg.Bar.Extent())
	}
}

func TestTriggerAtActionUsesTheWidgetCentreInOutputCoordinates(t *testing.T) {
	bar := &Bar{left: []textWidget{{node: &ui.Node{
		Action: "panel:test", Bounds: ui.Rect{X: 4, Y: 100, W: 20, H: 40},
	}}}}
	bar.configured.width, bar.configured.height, bar.configured.set = 60, 864, true
	trig := Trigger{BarEdge: "right", OutW: 1536, OutH: 864}
	got := triggerAtAction(bar, trig, "panel:test")
	if got.AnchorX != 1490 || got.AnchorY != 120 {
		t.Fatalf("trigger centre = (%d,%d), want (1490,120)", got.AnchorX, got.AnchorY)
	}
}

func TestPanelRevealMovesInwardFromEachBarEdge(t *testing.T) {
	for _, tc := range []struct {
		edge         string
		wantX, wantY int
	}{
		{"top", 0, -panelSlidePx},
		{"bottom", 0, panelSlidePx},
		{"left", -panelSlidePx, 0},
		{"right", panelSlidePx, 0},
	} {
		t.Run(tc.edge, func(t *testing.T) {
			a, _ := newTestAnimator(false)
			key := panelSurfaceID(PanelSession)
			a.Target(key, animVisible, 1)
			h := &PanelHost{id: PanelSession, anim: a, place: Placement{BarEdge: tc.edge}}
			_, gotX, gotY := h.panelReveal()
			if gotX != tc.wantX || gotY != tc.wantY {
				t.Fatalf("reveal offset = (%d,%d), want (%d,%d)", gotX, gotY, tc.wantX, tc.wantY)
			}
		})
	}
}

func TestEscapeClosesPanel(t *testing.T) {
	t.Parallel()
	reg := newPanelRegistry(t)
	if err := reg.OpenPanel(PanelClock, 7, Trigger{BarEdge: "top", BarZone: 40}); err != nil {
		t.Fatal(err)
	}
	reqs := drainAux(t, reg, 2)
	if !reg.Clock().Running() {
		t.Fatal("clock panel did not acquire a lease")
	}
	panel := reqs[1].Open
	if !panel.Callbacks.Handle(wayland.Event{Kind: wayland.EventKeyPress, Key: 1}) {
		t.Fatal("escape did not report a state change")
	}
	closes := drainAux(t, reg, 2)
	if len(closes) != 2 || closes[0].Open != nil || closes[1].Open != nil {
		t.Fatalf("expected two close requests, got %+v", closes)
	}
	if _, ok := reg.panels.Output(PanelClock); ok {
		t.Fatal("panel set still lists the closed clock")
	}
	if reg.Clock().Running() {
		t.Fatal("closing did not release the clock lease")
	}
}

func TestTabMovesRovingFocus(t *testing.T) {
	t.Parallel()
	reg := newPanelRegistry(t)
	if err := reg.OpenPanel(PanelSession, 7, Trigger{}); err != nil {
		t.Fatal(err)
	}
	reqs := drainAux(t, reg, 2)
	handle := reqs[1].Open.Callbacks.Handle
	h := reg.panelHosts[PanelSession]
	handle(wayland.Event{Kind: wayland.EventKeyPress, Key: keyTab})
	handle(wayland.Event{Kind: wayland.EventKeyPress, Key: keyTab})
	handle(wayland.Event{Kind: wayland.EventKeyPress, Key: keyTab, Mods: ui.ModShift})
	if h.roving.Index() != 1 {
		t.Fatalf("focus index = %d, want 1", h.roving.Index())
	}
}

func TestSpaceActivatesFocusedButton(t *testing.T) {
	t.Parallel()
	reg := newPanelRegistry(t)
	reg.runArgv = func([]string) error { return nil }
	if err := reg.OpenPanel(PanelSession, 7, Trigger{}); err != nil {
		t.Fatal(err)
	}
	reqs := drainAux(t, reg, 2)
	reqs[1].Open.Callbacks.Handle(wayland.Event{Kind: wayland.EventKeyPress, Key: keySpace})
	waitSessionPanelClosed(t, reg)
}

func TestRevealAnimationInvalidatesUntilDone(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	reg := NewRegistry(cfg)
	reg.lookPath = func(string) (string, error) { return "", exec.ErrNotFound }
	t.Cleanup(reg.Close)
	// The frame loop ticks on real time but samples the reveal from the
	// animator clock. Freeze that clock across the open and the reveal cannot
	// be starved by a slow tree build: under -race a loaded process once
	// spent longer building the first panel's cold glyph set than the whole
	// reveal lasted, so the animation was over before the loop started and
	// the surface published once. Real time now only bounds how long we wait
	// for frames, never how many must arrive.
	clock := &syncClock{t: time.Unix(0, 0)}
	reg.animClock = clock.now
	if err := reg.OpenPanel(PanelSession, 7, Trigger{}); err != nil {
		t.Fatal(err)
	}
	reg.mu.Lock()
	host := reg.panelHosts[PanelSession]
	enter := host.anim.duration(animVisible, true)
	reg.mu.Unlock()
	if got := awaitSurfaceInvalidations(reg, 5, 5*time.Second); got < 5 {
		t.Fatalf("got %d surface invalidations, want at least 5 while the reveal runs", got)
	}
	// Advance the clock past the transition. The loop publishes its settling
	// frame on the next tick and retires; drain the backlog until the surface
	// goes quiet with no loop left standing. A spring reaches its endpoint
	// after the token it is derived from, so the bound is twice the reveal.
	clock.add(2 * enter)
	settled := false
	deadline := time.Now().Add(5 * time.Second)
	for !settled {
		select {
		case <-reg.Invalidations():
		case <-time.After(50 * time.Millisecond):
			settled = !host.anim.running.Load()
		}
		if !settled && time.Now().After(deadline) {
			t.Fatal("the frame loop outlived its reveal")
		}
	}

	// Reduced motion keeps a short opacity-only fade rather than snapping: the
	// concern is vestibular motion, and the panel still must not appear without
	// warning. It runs no longer than the catalogue's shortest transition, so
	// the surface has to be quiet well before the full enter would have ended.
	still := config.Default()
	still.Accessibility.ReducedMotion = true
	quiet := NewRegistry(still)
	quiet.lookPath = func(string) (string, error) { return "", exec.ErrNotFound }
	t.Cleanup(quiet.Close)
	if err := quiet.OpenPanel(PanelSession, 7, Trigger{}); err != nil {
		t.Fatal(err)
	}
	if got := countSurfaceInvalidations(quiet, reducedPanelCap+50*time.Millisecond); got == 0 {
		t.Fatal("reduced motion produced no invalidations; the panel never appeared")
	}
	if got := countSurfaceInvalidations(quiet, 100*time.Millisecond); got != 0 {
		t.Fatalf("surface still invalidating %d times after the fade settled", got)
	}
}

func TestRightClickingTheBarBatteryOpensSession(t *testing.T) {
	reg := newBatteryPanelRegistry(t)
	cb, err := reg.NewHost(7, "eDP-1")
	if err != nil {
		t.Fatal(err)
	}
	if err := cb.Configure(1536, 44, 120); err != nil {
		t.Fatal(err)
	}
	reg.UpdateMetrics(services.Snapshot{Battery: &metrics.BatterySnapshot{
		Present: true, Charge: 0.84, ChargeValid: true, State: metrics.BatteryDischarging,
	}})
	bar := reg.bars[7]
	if err := bar.Layout(1536, 44); err != nil {
		t.Fatal(err)
	}
	target, ok := batteryClickTarget(bar)
	if !ok {
		t.Fatal("default bar has no laid-out battery")
	}
	drainAuxQueue(reg)
	if click(bar, target.X+target.W/2, target.Y+target.H/2) {
		t.Fatal("left-click on battery must stay inert")
	}
	if !clickButton(bar, target.X+target.W/2, target.Y+target.H/2, buttonRight) {
		t.Fatal("right-click on battery did not activate")
	}
	reqs := drainAux(t, reg, 2)
	if !strings.HasPrefix(reqs[1].Open.ID, "panel:session") {
		t.Fatalf("opened %q, want session", reqs[1].Open.ID)
	}
}

func TestRightClickingBatteryCapsulePaddingOpensSession(t *testing.T) {
	reg := newBatteryPanelRegistry(t)
	cb, err := reg.NewHost(7, "eDP-1")
	if err != nil {
		t.Fatal(err)
	}
	if err := cb.Configure(1536, 44, 120); err != nil {
		t.Fatal(err)
	}
	reg.UpdateMetrics(services.Snapshot{Battery: &metrics.BatterySnapshot{
		Present: true, Charge: 0.84, ChargeValid: true, State: metrics.BatteryDischarging,
	}})
	bar := reg.bars[7]
	if err := bar.Layout(1536, 44); err != nil {
		t.Fatal(err)
	}
	capsule, inner, ok := batteryCapsuleAndInner(bar)
	if !ok {
		t.Fatal("default bar has no laid-out battery")
	}
	x, y := capsule.X, capsule.Y+capsule.H/2
	if !capsule.Contains(x, y) || inner.Contains(x, y) {
		t.Fatalf("no padding point: capsule=%+v inner=%+v at %d,%d", capsule, inner, x, y)
	}
	drainAuxQueue(reg)
	if !clickButton(bar, x, y, buttonRight) {
		t.Fatal("right-click on battery capsule padding did not activate")
	}
	reqs := drainAux(t, reg, 2)
	if !strings.HasPrefix(reqs[1].Open.ID, "panel:session") {
		t.Fatalf("opened %q, want session", reqs[1].Open.ID)
	}
}

func TestClickingABarMetricOpensTheSystemMonitor(t *testing.T) {
	t.Parallel()
	reg := newPanelRegistry(t)
	cb, err := reg.NewHost(7, "eDP-1")
	if err != nil {
		t.Fatal(err)
	}
	if err := cb.Configure(1536, 44, 120); err != nil {
		t.Fatal(err)
	}
	bar := reg.bars[7]
	target, ok := metricClickTarget(bar)
	if !ok {
		t.Fatal("default bar has no laid-out metric")
	}
	drainAuxQueue(reg)
	if !click(bar, target.X+target.W/2, target.Y+target.H/2) {
		t.Fatal("clicking a metric did not activate")
	}
	reqs := drainAux(t, reg, 2)
	if !strings.HasPrefix(reqs[1].Open.ID, "panel:system-monitor") {
		t.Fatalf("opened %q, want the system monitor", reqs[1].Open.ID)
	}
}

func TestRightClickingABarMetricOpensTheSystemMonitor(t *testing.T) {
	t.Parallel()
	reg := newPanelRegistry(t)
	cb, err := reg.NewHost(7, "eDP-1")
	if err != nil {
		t.Fatal(err)
	}
	if err := cb.Configure(1536, 44, 120); err != nil {
		t.Fatal(err)
	}
	bar := reg.bars[7]
	target, ok := metricClickTarget(bar)
	if !ok {
		t.Fatal("default bar has no laid-out metric")
	}
	drainAuxQueue(reg)
	if !clickButton(bar, target.X+target.W/2, target.Y+target.H/2, buttonRight) {
		t.Fatal("right-clicking a metric did not activate")
	}
	reqs := drainAux(t, reg, 2)
	if !strings.HasPrefix(reqs[1].Open.ID, "panel:system-monitor") {
		t.Fatalf("opened %q, want the system monitor", reqs[1].Open.ID)
	}
}

func TestClickingGroupedMetricCapsulePaddingOpensTheSystemMonitor(t *testing.T) {
	t.Parallel()
	reg := newPanelRegistry(t)
	cb, err := reg.NewHost(7, "eDP-1")
	if err != nil {
		t.Fatal(err)
	}
	if err := cb.Configure(1536, 44, 120); err != nil {
		t.Fatal(err)
	}
	bar := reg.bars[7]
	capsule, inner, ok := metricGroupCapsuleAndInner(bar)
	if !ok {
		t.Fatal("default bar has no laid-out metric group")
	}
	x, y := capsule.X, capsule.Y+capsule.H/2
	if !capsule.Contains(x, y) || inner.Contains(x, y) {
		t.Fatalf("no padding point: capsule=%+v inner=%+v at %d,%d", capsule, inner, x, y)
	}
	drainAuxQueue(reg)
	if !click(bar, x, y) {
		t.Fatal("clicking metric group padding did not activate")
	}
	reqs := drainAux(t, reg, 2)
	if !strings.HasPrefix(reqs[1].Open.ID, "panel:system-monitor") {
		t.Fatalf("opened %q, want the system monitor", reqs[1].Open.ID)
	}
}

func batteryCapsuleAndInner(b *Bar) (capsule, inner ui.Rect, ok bool) {
	for _, section := range b.widgets() {
		for _, w := range section {
			if w.inner != nil && w.inner.Action == panelSessionAction && w.node != nil &&
				w.node.Bounds.W > 0 && w.inner.Bounds.W > 0 {
				return w.node.Bounds, w.inner.Bounds, true
			}
		}
	}
	return ui.Rect{}, ui.Rect{}, false
}

func batteryClickTarget(b *Bar) (ui.Rect, bool) {
	for _, section := range b.widgets() {
		for _, w := range section {
			for _, m := range w.members {
				if m.inner != nil && m.inner.Action == panelSessionAction && m.inner.Bounds.W > 0 {
					return m.inner.Bounds, true
				}
				if m.node != nil && m.node.Action == panelSessionAction && m.node.Bounds.W > 0 {
					return m.node.Bounds, true
				}
			}
			if w.inner != nil && w.inner.Action == panelSessionAction && w.inner.Bounds.W > 0 {
				return w.inner.Bounds, true
			}
			if w.node != nil && w.node.Action == panelSessionAction && w.node.Bounds.W > 0 {
				return w.node.Bounds, true
			}
		}
	}
	return ui.Rect{}, false
}

func metricGroupCapsuleAndInner(b *Bar) (capsule, inner ui.Rect, ok bool) {
	for _, section := range b.widgets() {
		for _, w := range section {
			if len(w.members) == 0 || w.node == nil || w.inner == nil {
				continue
			}
			for _, m := range w.members {
				if m.node != nil && m.node.Action == panelMonitorAction &&
					w.node.Bounds.W > 0 && w.inner.Bounds.W > 0 {
					return w.node.Bounds, w.inner.Bounds, true
				}
			}
		}
	}
	return ui.Rect{}, ui.Rect{}, false
}

func metricClickTarget(b *Bar) (ui.Rect, bool) {
	for _, section := range b.widgets() {
		for _, w := range section {
			for _, m := range w.members {
				if m.node != nil && m.node.Action == panelMonitorAction && m.node.Bounds.W > 0 && m.node.Bounds.H > 0 {
					return m.node.Bounds, true
				}
			}
			if w.inner != nil && w.inner.Action == panelMonitorAction && w.inner.Bounds.W > 0 {
				return w.inner.Bounds, true
			}
		}
	}
	return ui.Rect{}, false
}

func drainAuxQueue(reg *Registry) {
	for {
		select {
		case <-reg.AuxRequests():
		default:
			return
		}
	}
}

func TestTogglePanelByNameOpensSession(t *testing.T) {
	t.Parallel()
	reg := newPanelRegistry(t)
	if err := reg.TogglePanelByName("session"); err != nil {
		t.Fatal(err)
	}
	reqs := drainAux(t, reg, 2)
	if !strings.HasPrefix(reqs[1].Open.ID, "panel:session") {
		t.Fatalf("opened %q", reqs[1].Open.ID)
	}
}

func TestPowerIsAnAliasForSession(t *testing.T) {
	t.Parallel()
	id, err := parsePanelName("power")
	if err != nil || id != PanelSession {
		t.Fatalf("parsePanelName(power) = %v, %v", id, err)
	}
}

func TestParsePanelNameNotifications(t *testing.T) {
	t.Parallel()
	id, err := parsePanelName("notifications")
	if err != nil || id != PanelNotifications {
		t.Fatalf("parsePanelName(notifications) = %v, %v", id, err)
	}
	got, ok := panelIDFromAux("panel:notifications")
	if !ok || got != PanelNotifications {
		t.Fatalf("panelIDFromAux = %v ok=%v", got, ok)
	}
}

func TestNotificationsPanelTargetSize(t *testing.T) {
	t.Parallel()
	got := panelTargetSize(PanelNotifications)
	if got.W != 416 {
		t.Fatalf("width = %d, want 416", got.W)
	}
	if got.H != 300 {
		t.Fatalf("height fallback = %d, want 300", got.H)
	}
}

func TestOpeningNotificationsSetsCenterOpenAndMarksSeen(t *testing.T) {
	t.Parallel()
	reg := newPanelRegistry(t)
	withTestBar(t, reg, 7, reg.cfg)
	sender := &fakeNotifySender{}
	reg.notifySender = sender
	reg.applyNotify(snap(1))
	reg.applyNotify(delta(1, 2, protocol.Delta{Kind: protocol.DeltaHistoryAdded,
		History: ptrH(historyEntry(7, "mail", "Mail", "old", time.Unix(1_756_000_000, 0), false))}))

	if err := reg.OpenPanel(PanelNotifications, 7, Trigger{
		BarEdge: "top", BarZone: 44, OutW: 1536, OutH: 1440,
	}); err != nil {
		t.Fatal(err)
	}
	reqs := drainAux(t, reg, 2)
	panel := reqs[1].Open
	if panel == nil || panel.ID != "panel:notifications" {
		t.Fatalf("opened %+v", panel)
	}
	// Right-aligned on an attached bar, it sits flush on the screen edge: a
	// joint on its left only, and a screen-edge wedge below its body.
	if panel.Width != 416+12 {
		t.Fatalf("width = %d, want the body plus its left joint, 428", panel.Width)
	}
	if panel.Height < 300+12 {
		t.Fatalf("height = %d, want at least 300 plus the wedge", panel.Height)
	}
	if want := int32(1536 - 416 - 12); panel.MarginLeft != want {
		t.Fatalf("margin left = %d, want flush %d", panel.MarginLeft, want)
	}
	if panel.MarginTop != 43 {
		t.Fatalf("margin top = %d, want tucked 1 px under the 44 px bar", panel.MarginTop)
	}
	if !reg.panelOpenLocked(PanelNotifications) {
		t.Fatal("opening did not acquire the interactive root")
	}
	reg.notify.mu.Lock()
	open := reg.notify.centerOpen
	reg.notify.mu.Unlock()
	if !open {
		t.Fatal("opening left centerOpen false")
	}
	seen := sender.ofKind(protocol.CommandHistoryMarkSeen)
	if len(seen) != 1 || len(seen[0].IDs) != 1 || seen[0].IDs[0] != 7 {
		t.Fatalf("mark-seen = %+v", seen)
	}

	reg.ClosePanel(PanelNotifications)
	_ = drainAux(t, reg, 2)
	reg.notify.mu.Lock()
	open = reg.notify.centerOpen
	reg.notify.mu.Unlock()
	if open {
		t.Fatal("closing left centerOpen true")
	}
}

func TestNotificationsTabSwitchGrowsSurfaceHeight(t *testing.T) {
	t.Parallel()
	reg := newPanelRegistry(t)
	reg.applyNotify(snap(1))
	older := time.Unix(1_756_000_000, 0)
	for i := uint32(1); i <= 12; i++ {
		reg.applyNotify(delta(1, uint64(i+1), protocol.Delta{Kind: protocol.DeltaHistoryAdded,
			History: ptrH(historyEntry(i, "mail", "Mail", "old", older.Add(time.Duration(i)*time.Second), true))}))
	}
	if err := reg.OpenPanel(PanelNotifications, 7, Trigger{
		BarEdge: "top", BarZone: 44, OutW: 1536, OutH: 1440,
	}); err != nil {
		t.Fatal(err)
	}
	_ = drainAux(t, reg, 2)

	h := reg.panelHosts[PanelNotifications]
	if reg.panelHosts[PanelNotifications] == nil {
		t.Fatal("opening dropped the centre")
	}
	for {
		select {
		case req := <-reg.AuxRequests():
			if req.Open != nil && req.Open.ID == "panel:notifications" {
				t.Fatalf("Open aux remapped the mapped centre: %+v", req.Open)
			}
		default:
			h = reg.panelHosts[PanelNotifications]
			if !containsText(h.root, "old") {
				t.Fatalf("history missing from tree: %v", texts(h.root))
			}
			var scrolls []*ui.Node
			collectByKind(h.root, ui.KindScroll, &scrolls)
			if len(scrolls) == 0 {
				t.Fatal("history body is not scrollable")
			}
			return
		}
	}
}

func TestNotificationsRebuildOnNotifyDelta(t *testing.T) {
	t.Parallel()
	reg := newPanelRegistry(t)
	reg.applyNotify(snap(1))
	if err := reg.OpenPanel(PanelNotifications, 7, Trigger{
		BarEdge: "top", BarZone: 44, OutW: 1536, OutH: 1440,
	}); err != nil {
		t.Fatal(err)
	}
	_ = drainAux(t, reg, 2)

	reg.applyNotify(delta(1, 2, protocol.Delta{Kind: protocol.DeltaAdded, Notification: ptr(note(9, "incoming")),
		Lifetime: &protocol.Lifetime{ID: 9, DurationMS: 5000, RemainingMS: 5000, Running: true}}))

	h := reg.panelHosts[PanelNotifications]
	if !containsText(h.root, "incoming") {
		t.Fatalf("tree after delta = %v", texts(h.root))
	}
}

func TestNotificationsCentreConfiguresAtTargetWidth(t *testing.T) {
	t.Parallel()
	reg := newPanelRegistry(t)
	reg.applyNotify(snap(1))
	if err := reg.OpenPanel(PanelNotifications, 7, Trigger{
		BarEdge: "top", BarZone: 44, OutW: 1536, OutH: 1440,
	}); err != nil {
		t.Fatal(err)
	}
	reqs := drainAux(t, reg, 2)
	panel := reqs[1].Open
	if err := panel.Callbacks.Configure(int(panel.Width), int(panel.Height), 120); err != nil {
		t.Fatalf("configure: %v", err)
	}
}

func TestTogglePanelByNamePowerOpensSession(t *testing.T) {
	t.Parallel()
	reg := newPanelRegistry(t)
	if err := reg.TogglePanelByName("power"); err != nil {
		t.Fatal(err)
	}
	reqs := drainAux(t, reg, 2)
	if !strings.HasPrefix(reqs[1].Open.ID, "panel:session") {
		t.Fatalf("opened %q", reqs[1].Open.ID)
	}
}

func TestTogglePanelByNameCentresFlushUnderTheBar(t *testing.T) {
	t.Parallel()
	reg := newPanelRegistry(t)
	cb, err := reg.NewHost(7, "eDP-1")
	if err != nil {
		t.Fatal(err)
	}
	// The default bar is attached: a 52 px surface whose body, and zone, is 40.
	if err := cb.Configure(1536, 52, 120); err != nil {
		t.Fatal(err)
	}
	if err := reg.TogglePanelByName("system-monitor"); err != nil {
		t.Fatal(err)
	}
	reqs := drainAux(t, reg, 2)
	got := reqs[1].Open
	if got.MarginTop != 39 {
		t.Fatalf("margin top = %d, want tucked 1 px under the 40px attached body", got.MarginTop)
	}
	if want := int32((1536-panelTargetSize(PanelMonitor).W)/2 - 12); got.MarginLeft != want {
		t.Fatalf("margin left = %d, want centred %d", got.MarginLeft, want)
	}
}

func TestToggleMonitorOpensTallerThanTheOldGuess(t *testing.T) {
	t.Parallel()
	reg := newPanelRegistry(t)
	cb, err := reg.NewHost(7, "eDP-1")
	if err != nil {
		t.Fatal(err)
	}
	if err := cb.Configure(1536, 44, 150); err != nil {
		t.Fatal(err)
	}
	if err := reg.TogglePanelByName("system-monitor"); err != nil {
		t.Fatal(err)
	}
	reqs := drainAux(t, reg, 2)
	got := reqs[1].Open
	if got.Height <= 480 {
		t.Fatalf("monitor height %d, want taller than the 480 guess that clipped the last card", got.Height)
	}
	h := reg.panelHosts[PanelMonitor]
	if err := got.Callbacks.Configure(int(got.Width), int(got.Height), 150); err != nil {
		t.Fatal(err)
	}
	bottom := 0
	var walk func(*ui.Node)
	walk = func(n *ui.Node) {
		if n == nil {
			return
		}
		if b := n.Bounds.Y + n.Bounds.H; b > bottom {
			bottom = b
		}
		for _, c := range n.Children {
			walk(c)
		}
	}
	walk(h.root)
	if bottom > int(got.Height) {
		t.Fatalf("content bottom %d exceeds surface %d", bottom, got.Height)
	}
}

func TestReloadKeepsOpenPanels(t *testing.T) {
	t.Parallel()
	reg := newPanelRegistry(t)
	if err := reg.OpenPanel(PanelSession, 7, Trigger{}); err != nil {
		t.Fatal(err)
	}
	_ = drainAux(t, reg, 2)
	if _, err := reg.PrepareConfig(config.Default(), nil); err != nil {
		t.Fatal(err)
	}
	if _, ok := reg.panels.Output(PanelSession); !ok {
		t.Fatal("reload dropped the open panel from the set")
	}
	select {
	case req := <-reg.AuxRequests():
		t.Fatalf("reload sent an aux request: %+v", req)
	case <-time.After(20 * time.Millisecond):
	}
}

func TestClosingDuringRevealStopsTicker(t *testing.T) {
	t.Parallel()
	reg := NewRegistry(config.Default())
	reg.lookPath = func(string) (string, error) { return "", exec.ErrNotFound }
	t.Cleanup(reg.Close)
	if err := reg.OpenPanel(PanelSession, 7, Trigger{}); err != nil {
		t.Fatal(err)
	}
	_ = drainAux(t, reg, 2)
	reg.ClosePanel(PanelSession)
	drainInvalidations(reg)
	if got := countSurfaceInvalidations(reg, 50*time.Millisecond); got != 0 {
		t.Fatalf("ticker kept publishing after close: %d", got)
	}
}

// newPanelRegistry builds a Registry with a stand-in for the owner's bridge:
// its invalidations channel is kept drained, so a test that publishes more than
// the cap-8 buffer holds cannot stall the suite. A test that must read those
// invalidations builds its registry with newPanelRegistryWithoutBridge instead.
func newPanelRegistry(t *testing.T) *Registry {
	t.Helper()
	reg := newPanelRegistryWithoutBridge(t)
	keepInvalidationsDrained(t, reg)
	return reg
}

// newPanelRegistryWithoutBridge is newPanelRegistry without the stand-in
// bridge. Its invalidations channel is left undrained so the test can read it,
// and such a test must consume the channel itself: publish blocks on a full
// buffer rather than dropping (GH #4).
func newPanelRegistryWithoutBridge(t *testing.T) *Registry {
	t.Helper()
	cfg := config.Default()
	cfg.Accessibility.ReducedMotion = true
	reg := NewRegistry(cfg)
	reg.lookPath = func(string) (string, error) { return "", exec.ErrNotFound }
	t.Cleanup(reg.Close)
	return reg
}

// A fixture built by newPanelRegistry cannot stall the suite. publishSurface
// blocks on the cap-8 channel and only a consumer releases it, which is how CI
// hung in TestEdgeReloadRetainsOpenPanels: that fixture published seven and had
// one slot of headroom (sysc-1494).
func TestPanelRegistryFixtureDrainsInvalidations(t *testing.T) {
	reg := newPanelRegistry(t)
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i <= cap(reg.invalidations); i++ {
			reg.publishSurface(7, "probe")
		}
	}()
	select {
	case <-done:
	case <-time.After(asyncTestWait):
		t.Fatal("publishSurface blocked: nothing drains the fixture's invalidations")
	}
}

// newBatteryPanelRegistry is newPanelRegistry on a bar that carries a battery
// item; the product default stopped shipping one in e55ed6d, and these tests
// are about the battery capsule, not about the default composition.
func newBatteryPanelRegistry(t *testing.T) *Registry {
	t.Helper()
	cfg := config.Default()
	cfg.Bar.Right = append(cfg.Bar.Right, config.Item{ID: "battery", WarnBelow: 20, Interval: 30 * time.Second})
	cfg.Accessibility.ReducedMotion = true
	reg := NewRegistry(cfg)
	reg.lookPath = func(string) (string, error) { return "", exec.ErrNotFound }
	t.Cleanup(reg.Close)
	keepInvalidationsDrained(t, reg)
	return reg
}

// withTestBar gives output global a bar built from cfg, releasing its leases
// when the test ends.
func withTestBar(t *testing.T, reg *Registry, global uint32, cfg config.Config) *Bar {
	t.Helper()
	bar, leases, _, err := reg.buildBar(cfg, "DP-1", reg.tokens)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { releaseAll(leases) })
	reg.setTestBar(global, bar)
	return bar
}

func configureCandidateBar(t *testing.T, callbacks wayland.HostCallbacks, policy config.Bar, outW, outH, scale120 int) {
	t.Helper()
	callbacks.OutputSize(outW, outH)
	width, height := outW, policy.SurfaceExtent()
	if policy.Edge == "left" || policy.Edge == "right" {
		width, height = policy.SurfaceExtent(), outH
	}
	if err := callbacks.Configure(width, height, scale120); err != nil {
		t.Fatalf("candidate Configure: %v", err)
	}
}

func settleHostAnimation(reg *Registry, h *PanelHost) {
	reg.mu.Lock()
	defer reg.mu.Unlock()
	if h == nil || h.anim == nil {
		return
	}
	for key, value := range h.anim.values {
		value.start = time.Time{}
		h.anim.values[key] = value
	}
}

// asyncTestWait bounds a wait for something that must happen. It only sets
// how long a failing test takes to say so; a passing test returns as soon as
// the event arrives, so it is long enough for the package's parallel -race
// run on a loaded machine.
const asyncTestWait = 10 * time.Second

func drainAux(t testing.TB, reg *Registry, n int) []wayland.AuxRequest {
	t.Helper()
	out := make([]wayland.AuxRequest, 0, n)
	deadline := time.After(asyncTestWait)
	for len(out) < n {
		select {
		case req := <-reg.AuxRequests():
			out = append(out, req)
		case <-deadline:
			t.Fatalf("got %d aux requests, want %d", len(out), n)
		}
	}
	return out
}

func countSurfaceInvalidations(reg *Registry, d time.Duration) int {
	n := 0
	deadline := time.After(d)
	for {
		select {
		case inv := <-reg.Invalidations():
			if inv.SurfaceID != "" {
				n++
			}
		case <-deadline:
			return n
		}
	}
}

// syncClock is a fake instant a test advances by hand. A panel frame loop
// samples it from its own goroutine while the test advances it, so it carries
// the mutex the animation tests' plain fakeClock does not.
type syncClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *syncClock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *syncClock) add(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

// awaitSurfaceInvalidations drains until want surface frames have arrived or
// the timeout passes, whichever happens first, and reports the count.
func awaitSurfaceInvalidations(reg *Registry, want int, timeout time.Duration) int {
	n := 0
	deadline := time.After(timeout)
	for n < want {
		select {
		case inv := <-reg.Invalidations():
			if inv.SurfaceID != "" {
				n++
			}
		case <-deadline:
			return n
		}
	}
	return n
}

func drainInvalidations(reg *Registry) {
	for {
		select {
		case <-reg.Invalidations():
		default:
			return
		}
	}
}

func TestOpeningAnUnrelatedPanelKeepsTheOldPanelOpen(t *testing.T) {
	reg := newPanelRegistry(t)
	if err := reg.OpenPanel(PanelClock, 7, Trigger{}); err != nil {
		t.Fatal(err)
	}
	_ = drainAux(t, reg, 2)
	if _, ok := reg.panelHosts[PanelClock]; !ok {
		t.Fatal("the first panel never opened")
	}
	owner, generation, ok := reg.roots.current()
	if !ok || owner != panelGroupRoot() {
		t.Fatal("the first panel did not create the panel group root")
	}

	if err := reg.OpenPanel(PanelSession, 7, Trigger{}); err != nil {
		t.Fatal(err)
	}
	_ = drainAux(t, reg, 1)

	if _, ok := reg.panelHosts[PanelClock]; !ok {
		t.Fatal("opening another panel closed the first panel")
	}
	if _, ok := reg.panels.Output(PanelClock); !ok {
		t.Fatal("the first panel is not recorded as open")
	}
	if _, ok := reg.panelHosts[PanelSession]; !ok {
		t.Fatal("the second panel did not open")
	}
	owner, nextGeneration, ok := reg.roots.current()
	if !ok || owner != panelGroupRoot() || nextGeneration != generation {
		t.Fatal("opening a second panel replaced the panel group root")
	}
}

func TestMovingAPanelToAnotherOutputReplacesItsRoot(t *testing.T) {
	reg := newPanelRegistry(t)
	if err := reg.OpenPanel(PanelClock, 7, Trigger{}); err != nil {
		t.Fatal(err)
	}
	_ = drainAux(t, reg, 2)
	_, first, _ := reg.roots.current()

	if err := reg.OpenPanel(PanelClock, 8, Trigger{}); err != nil {
		t.Fatal(err)
	}
	_ = drainAux(t, reg, 4)

	_, second, ok := reg.roots.current()
	if !ok || second == first {
		t.Fatalf("moving outputs kept generation %d", second)
	}
	where, ok := reg.panels.Output(PanelClock)
	if !ok || where != 8 {
		t.Fatalf("panel output = %d (open=%v), want 8", where, ok)
	}
	if host := reg.panelHosts[PanelClock]; host == nil || host.output != 8 {
		t.Fatalf("panel host = %+v, want output 8", host)
	}
}

func TestTogglingTheSamePanelClosesItsRoot(t *testing.T) {
	reg := newPanelRegistry(t)
	if err := reg.TogglePanel(PanelMonitor, 7, Trigger{}); err != nil {
		t.Fatal(err)
	}
	_ = drainAux(t, reg, 2)
	if !reg.panelOpenLocked(PanelMonitor) {
		t.Fatal("toggling open did not publish a root")
	}

	if err := reg.TogglePanel(PanelMonitor, 7, Trigger{}); err != nil {
		t.Fatal(err)
	}
	_ = drainAux(t, reg, 2)
	if _, _, ok := reg.roots.current(); ok {
		t.Fatal("toggling closed left a root open")
	}
	if _, ok := reg.panelHosts[PanelMonitor]; ok {
		t.Fatal("toggling closed left the panel hosted")
	}
}

func TestEveryPanelCloseReleasesItsChainExactlyOnce(t *testing.T) {
	for name, closer := range map[string]func(*Registry){
		"ClosePanel":  func(r *Registry) { r.ClosePanel(PanelClock) },
		"TogglePanel": func(r *Registry) { _ = r.TogglePanel(PanelClock, 7, Trigger{}) },
		"DropAux":     func(r *Registry) { r.DropAux(7, panelSurfaceID(PanelClock)) },
		"closeAll":    func(r *Registry) { r.mu.Lock(); r.closeAllPanelsLocked(); r.mu.Unlock() },
		"replacedByModalRoot": func(r *Registry) {
			r.mu.Lock()
			r.roots.openRoot(trayMenuRoot(7))
			r.mu.Unlock()
		},
	} {
		t.Run(name, func(t *testing.T) {
			reg := newPanelRegistry(t)
			if err := reg.OpenPanel(PanelClock, 7, Trigger{}); err != nil {
				t.Fatal(err)
			}
			_ = drainAux(t, reg, 2)

			released := 0
			_, generation, ok := reg.roots.current()
			if !ok {
				t.Fatal("no root was published")
			}
			reg.mu.Lock()
			reg.roots.onClose(generation, func() { released++ })
			reg.mu.Unlock()

			closer(reg)
			go func() {
				for range reg.AuxRequests() {
				}
			}()

			if released != 1 {
				t.Fatalf("chain released %d times, want exactly 1", released)
			}
			if _, ok := reg.panelHosts[PanelClock]; ok {
				t.Fatal("the panel host survived its close")
			}
			// A late close naming the released generation must do nothing.
			reg.mu.Lock()
			stale := reg.roots.closeRoot(generation)
			reg.mu.Unlock()
			if stale {
				t.Fatal("a stale close released the chain again")
			}
			if released != 1 {
				t.Fatalf("chain released %d times after a stale close", released)
			}
		})
	}
}

func TestPanelFontFamilyFollowsTheOutputConnector(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.Bar.FontFamily = "GlobalSans"
	cfg.Outputs = []config.OutputOverride{{Connector: "DP-2", Bar: config.Bar{FontFamily: "PerOutputSerif"}}}
	reg := NewRegistry(cfg)
	reg.setTestBar(1, &Bar{conn: "DP-1"})
	reg.setTestBar(2, &Bar{conn: "DP-2"})

	if got := reg.panelFontFamily(1); got != "GlobalSans" {
		t.Errorf("DP-1 panel font = %q, want the global family", got)
	}
	if got := reg.panelFontFamily(2); got != "PerOutputSerif" {
		t.Errorf("DP-2 panel font = %q, want the per-output family", got)
	}
	if got := reg.panelFontFamily(99); got != "GlobalSans" {
		t.Errorf("unknown output font = %q, want the global family", got)
	}
}

func TestHotCloseDuringDragCancels(t *testing.T) {
	t.Parallel()
	h := &PanelHost{}
	src := &ui.Node{Kind: ui.KindDragSource, DragType: "zone", Payload: "tokyo", Name: "Reorder"}
	h.drag.Begin(src, 0, 0)
	h.drag.Move(0, 20)
	if !h.drag.Active() {
		t.Fatal("drag did not start")
	}
	h.drag.Cancel()
	if h.drag.Active() {
		t.Fatal("hot close left the drag active")
	}
}

func TestOverlayEditorsPreservesBufferUntilReseed(t *testing.T) {
	t.Parallel()
	eds := map[string]*retainedEditor{}
	first := editorColumn("disk", 1)
	overlayEditors(first, eds)
	eds["note"].field.Text = "typed"

	overlayEditors(first, eds)
	if first.Children[0].Text != "typed" {
		t.Fatalf("same-key snapshot overwrote the buffer: %q", first.Children[0].Text)
	}

	sibling := &ui.Node{Kind: ui.KindColumn, Children: []*ui.Node{
		{Kind: ui.KindTextField, Key: "note", Action: "body", Text: "disk", Reseed: 1},
		{Kind: ui.KindText, Text: "count"},
	}}
	overlayEditors(sibling, eds)
	if sibling.Children[0].Text != "typed" {
		t.Fatalf("sibling patch overwrote the buffer: %q", sibling.Children[0].Text)
	}

	reseed := editorColumn("from-disk", 2)
	overlayEditors(reseed, eds)
	if reseed.Children[0].Text != "from-disk" {
		t.Fatalf("reseed did not replace: %q", reseed.Children[0].Text)
	}

	stale := editorColumn("stale", 1)
	overlayEditors(stale, eds)
	if stale.Children[0].Text != "from-disk" {
		t.Fatalf("stale reseed overwrote: %q", stale.Children[0].Text)
	}

	overlayEditors(&ui.Node{Kind: ui.KindColumn}, eds)
	if len(eds) != 0 {
		t.Fatalf("empty tree left %d editors", len(eds))
	}
}

func TestPanelHostAxisValue120Scrolls(t *testing.T) {
	t.Parallel()
	h := &PanelHost{root: &ui.Node{
		Kind: ui.KindScroll, Padding: 12,
		Bounds:   ui.Rect{W: 400, H: 200},
		ContentH: 800,
		Children: []*ui.Node{{Kind: ui.KindText, Text: "body"}},
	}}
	if !h.scrollAxis(nil, wayland.Event{Kind: wayland.EventPointerAxis, AxisValue120: 120}) {
		t.Fatal("value120 axis must be handled")
	}
	if h.root.ScrollOffset == 0 {
		t.Fatal("value120 wheel left the viewport at 0")
	}
}

func TestPanelHostThumbDragScrolls(t *testing.T) {
	t.Parallel()
	reg := newPanelRegistry(t)
	h := &PanelHost{
		id:     PanelClock,
		output: 7,
		root: &ui.Node{
			Kind:     ui.KindScroll,
			Padding:  12,
			Bounds:   ui.Rect{W: 400, H: 200},
			ContentH: 800,
			Children: []*ui.Node{{Kind: ui.KindText, Text: "body"}},
		},
	}
	track := ui.ScrollTrack(h.root)
	if track.W == 0 {
		t.Fatal("overflowing scroll has no track")
	}
	handle := h.handle(reg)
	x, y := float64(track.X+track.W/2), float64(track.Y+track.H-4)
	_ = handle(wayland.Event{Kind: wayland.EventPointerMotion, X: x, Y: y})
	if !handle(wayland.Event{Kind: wayland.EventPointerPress, X: x, Y: y}) {
		t.Fatal("press on the thumb track must be handled")
	}
	if h.root.ScrollOffset == 0 {
		t.Fatal("press near the bottom of the track left offset at 0")
	}
}

func editorColumn(text string, reseed uint64) *ui.Node {
	return &ui.Node{Kind: ui.KindColumn, Children: []*ui.Node{{
		Kind: ui.KindTextField, Key: "note", Action: "body", Text: text, Reseed: reseed,
		Name: "Note", Role: "textbox",
	}}}
}

// --- Resolved interaction state ---------------------------------------------

// openSessionPanel opens the session panel and returns its host plus a live
// event handle, laid out at a real size so hit testing has bounds to work with.
func openSessionPanel(t *testing.T) (*Registry, *PanelHost, func(wayland.Event) bool) {
	t.Helper()
	reg := newPanelRegistry(t)
	// The session panel's rows launch real session actions, and releasing the
	// pointer on one activates it. Record the argv instead: the default
	// launcher would ask logind to end the session running the test.
	reg.runArgv = func([]string) error { return nil }
	if err := reg.OpenPanel(PanelSession, 7, Trigger{BarEdge: "top", BarZone: 40, Align: "center"}); err != nil {
		t.Fatal(err)
	}
	reqs := drainAux(t, reg, 2)
	h := reg.panelHosts[PanelSession]
	if h == nil {
		t.Fatal("session panel host is missing")
	}
	if err := h.configure(h.place.Panel.W, h.place.Panel.H, 120); err != nil {
		t.Fatal(err)
	}
	return reg, h, reqs[1].Open.Callbacks.Handle
}

// centreOfKey finds the laid-out node with a stable key and returns its centre.
func centreOfKey(t *testing.T, root *ui.Node, key string) (float64, float64) {
	t.Helper()
	var found *ui.Node
	var walk func(*ui.Node)
	walk = func(n *ui.Node) {
		if n == nil || found != nil {
			return
		}
		if ui.Animated(n) && n.StableKey() == key {
			found = n
			return
		}
		for _, c := range n.Children {
			walk(c)
		}
	}
	walk(root)
	if found == nil {
		t.Fatalf("no animated node with key %q", key)
	}
	return float64(found.Bounds.X + found.Bounds.W/2), float64(found.Bounds.Y + found.Bounds.H/2)
}

// animatedKeys lists every animated node key in the tree, in traversal order.
func animatedKeys(root *ui.Node) []string {
	var out []string
	var walk func(*ui.Node)
	walk = func(n *ui.Node) {
		if n == nil {
			return
		}
		if ui.Animated(n) {
			if k := n.StableKey(); k != "" {
				out = append(out, k)
			}
		}
		for _, c := range n.Children {
			walk(c)
		}
	}
	walk(root)
	return out
}

func TestPointerEnteringAControlSetsHoverOnce(t *testing.T) {
	t.Parallel()
	reg, h, handle := openSessionPanel(t)
	_ = reg
	keys := animatedKeys(h.root)
	if len(keys) == 0 {
		t.Fatal("session panel exposes no animated controls")
	}
	x, y := centreOfKey(t, h.root, keys[0])

	if !handle(wayland.Event{Kind: wayland.EventPointerMotion, X: x, Y: y}) {
		t.Fatal("entering a control did not invalidate the surface")
	}
	if h.pointer.hover != keys[0] {
		t.Fatalf("hover = %q, want %q", h.pointer.hover, keys[0])
	}
	if !h.root.Children[0].State.Has(ui.StateHovered) && !hasHovered(h.root) {
		t.Error("the resolved tree carries no hovered control")
	}

	// Moving inside the same control resolves to the same key, so it must not
	// ask for another frame.
	if handle(wayland.Event{Kind: wayland.EventPointerMotion, X: x + 1, Y: y + 1}) {
		t.Error("motion inside the hovered control invalidated the surface")
	}
}

func hasHovered(root *ui.Node) bool {
	found := false
	var walk func(*ui.Node)
	walk = func(n *ui.Node) {
		if n == nil || found {
			return
		}
		if n.State.Has(ui.StateHovered) {
			found = true
			return
		}
		for _, c := range n.Children {
			walk(c)
		}
	}
	walk(root)
	return found
}

func TestPointerLeavingClearsHoverAndPress(t *testing.T) {
	t.Parallel()
	_, h, handle := openSessionPanel(t)
	keys := animatedKeys(h.root)
	x, y := centreOfKey(t, h.root, keys[0])

	handle(wayland.Event{Kind: wayland.EventPointerMotion, X: x, Y: y})
	handle(wayland.Event{Kind: wayland.EventPointerPress, X: x, Y: y})
	if h.pointer.press == "" {
		t.Fatal("press did not resolve a key")
	}
	if !handle(wayland.Event{Kind: wayland.EventPointerLeave}) {
		t.Error("leaving the surface did not invalidate it")
	}
	if h.pointer.hover != "" || h.pointer.press != "" {
		t.Errorf("leave left hover %q and press %q", h.pointer.hover, h.pointer.press)
	}
	if handle(wayland.Event{Kind: wayland.EventPointerLeave}) {
		t.Error("a second leave invalidated an already-clear surface")
	}
}

func TestPressSetsStateAndReleaseClearsIt(t *testing.T) {
	t.Parallel()
	_, h, handle := openSessionPanel(t)
	keys := animatedKeys(h.root)
	x, y := centreOfKey(t, h.root, keys[0])

	handle(wayland.Event{Kind: wayland.EventPointerMotion, X: x, Y: y})
	handle(wayland.Event{Kind: wayland.EventPointerPress, X: x, Y: y})
	if h.pointer.press != keys[0] {
		t.Fatalf("press = %q, want %q", h.pointer.press, keys[0])
	}
	handle(wayland.Event{Kind: wayland.EventPointerRelease, X: x, Y: y})
	if h.pointer.press != "" {
		t.Errorf("release left press at %q", h.pointer.press)
	}
}

func TestDisabledControlsNeitherFocusNorActivate(t *testing.T) {
	t.Parallel()
	_, h, _ := openSessionPanel(t)

	off := &ui.Node{Kind: ui.KindButton, Text: "Lock", Action: "session:lock",
		Focusable: true, State: ui.StateDisabled, Bounds: ui.Rect{W: 100, H: 40}}
	h.root = &ui.Node{Kind: ui.KindColumn, Bounds: ui.Rect{W: 100, H: 40},
		Children: []*ui.Node{off}}
	h.focus = ui.Focusables(h.root)
	h.roving = ui.Roving{Count: len(h.focus)}

	if len(h.focus) != 0 {
		t.Errorf("a disabled control is focusable: %d entries", len(h.focus))
	}
	if n := h.hitFocusable(50, 20); n != nil {
		t.Error("a disabled control was hit-tested as activatable")
	}
	// It still resolves no hover key, so it cannot light up either.
	if got := hoverKeyAt(h.root, 50, 20); got != "" {
		t.Errorf("disabled control resolved hover key %q", got)
	}
}

func TestPanelHitFocusablePrefersMenuPopupOverFollowingControl(t *testing.T) {
	t.Parallel()
	menu := &ui.Node{
		Kind: ui.KindMenu, Action: "set:appearance.seed", Focusable: true,
		Bounds: ui.Rect{X: 10, Y: 10, W: 100, H: 24},
		Children: []*ui.Node{{Kind: ui.KindText, Text: "Theme", Bounds: ui.Rect{
			X: 10, Y: 34, W: 100, H: 20,
		}}},
	}
	cover := &ui.Node{Kind: ui.KindButton, Action: "settings:after", Focusable: true,
		Bounds: ui.Rect{X: 10, Y: 34, W: 100, H: 20}}
	h := &PanelHost{
		root:  &ui.Node{Kind: ui.KindColumn, Bounds: ui.Rect{W: 120, H: 80}, Children: []*ui.Node{menu, cover}},
		focus: []*ui.Node{menu, cover},
	}

	if got := h.hitFocusable(20, 40); got != menu {
		t.Fatalf("popup hit = %p (%v), want menu %p", got, got, menu)
	}
}

func TestReducedMotionSettlesStateWithoutAnimating(t *testing.T) {
	t.Parallel()
	// newPanelRegistry is already reduced-motion.
	_, h, handle := openSessionPanel(t)
	keys := animatedKeys(h.root)
	x, y := centreOfKey(t, h.root, keys[0])

	handle(wayland.Event{Kind: wayland.EventPointerMotion, X: x, Y: y})

	// Interaction state snaps: hover reaches its target on the frame it is
	// resolved, with no transition to schedule. The panel's own fade may still
	// be running, which is why this asserts on the channel and not the whole
	// animator.
	if got := h.anim.duration(animHover, true); got != 0 {
		t.Errorf("hover duration = %v under reduced motion, want 0", got)
	}
	if got := h.anim.Value(keys[0], animHover); got != 1 {
		t.Errorf("hover value = %v, want an immediate 1", got)
	}
	handle(wayland.Event{Kind: wayland.EventPointerLeave})
	if got := h.anim.Value(keys[0], animHover); got != 0 {
		t.Errorf("hover value after leave = %v, want an immediate 0", got)
	}
}

// TestReloadReseedsAnOpenSettingsDraft closes D6. PanelHost.draft is assigned
// when the panel opens and was never refreshed, while PrepareConfig's Commit
// replaces r.cfg without touching it. A change arriving from outside the panel
// — a reload, another tool, a second surface — was therefore reverted by the
// next control write, which put the stale draft back whole. Live apply widens
// that window from rare to the whole time the panel is open.
func TestReloadReseedsAnOpenSettingsDraft(t *testing.T) {
	t.Parallel()
	reg := newPanelRegistry(t)
	if err := reg.OpenPanel(PanelSettings, 7, Trigger{}); err != nil {
		t.Fatal(err)
	}
	_ = drainAux(t, reg, 2)

	next := config.Default()
	next.Session.Locker = "externally-set"
	prepared, err := reg.PrepareConfig(next, nil)
	if err != nil {
		t.Fatal(err)
	}
	prepared.Commit()

	reg.mu.Lock()
	defer reg.mu.Unlock()
	h := reg.panelHosts[PanelSettings]
	if h == nil {
		t.Fatal("the reload closed the settings panel")
	}
	if got := h.draft.Session.Locker; got != "externally-set" {
		t.Fatalf("draft kept the stale value %q", got)
	}
	if e := h.set.ByPath("session.locker"); e == nil || e.Get(h.draft) != "externally-set" {
		t.Fatal("the registry was not rebuilt against the reloaded configuration")
	}
}

func TestEdgeReloadRetainsOpenPanels(t *testing.T) {
	reg := newPanelRegistry(t)
	withTestBar(t, reg, 7, reg.cfg).setOutputSize(1536, 864)
	trigger := Trigger{BarEdge: "top", BarZone: 40, OutW: 1536, OutH: 864}
	if err := reg.OpenPanel(PanelSettings, 7, trigger); err != nil {
		t.Fatal(err)
	}
	_ = drainAux(t, reg, 2)
	if err := reg.OpenPanel(PanelClock, 7, trigger); err != nil {
		t.Fatal(err)
	}
	_ = drainAux(t, reg, 1)
	reg.mu.Lock()
	settingsBefore := reg.panelHosts[PanelSettings]
	clockBefore := reg.panelHosts[PanelClock]
	settingsField := ui.NewField("retained draft")
	settingsBefore.fields["reload-state"] = settingsField
	settingsBefore.settingsScroll = 73
	if len(settingsBefore.focus) > 0 {
		settingsBefore.roving.Set(len(settingsBefore.focus) - 1)
	}
	settingsFocus := settingsBefore.focused()
	settingsFocusName := ""
	if settingsFocus != nil {
		settingsFocusName = settingsFocus.Name
	}
	settingsScroll := scrollBody(settingsBefore.root)
	settingsScrollOffset := -1
	if settingsScroll != nil {
		settingsScroll.ScrollOffset = 73
		settingsScrollOffset = settingsScroll.ScrollOffset
	}
	reg.mu.Unlock()
	reg.dwell.enter(7, ui.Rect{X: 3, Y: 4, W: 10, H: 10}, "pending hint")

	candidate := config.Default()
	override := candidate.Bar
	override.Edge = "right"
	candidate.Outputs = []config.OutputOverride{{Connector: "DP-1", Bar: override}}
	prepared, err := reg.PrepareConfig(candidate, []wayland.HostIdentity{{Global: 7, Connector: "DP-1"}})
	if err != nil {
		t.Fatalf("PrepareConfig: %v", err)
	}
	callbacks := prepared.Hosts[7]
	configureCandidateBar(t, callbacks, override, 1536, 864, 120)
	prepared.Commit()

	reg.mu.Lock()
	settingsHost := reg.panelHosts[PanelSettings]
	clockHost := reg.panelHosts[PanelClock]
	if settingsHost != settingsBefore {
		reg.mu.Unlock()
		t.Fatal("edge reload replaced or closed Settings")
	}
	if clockHost != clockBefore {
		reg.mu.Unlock()
		t.Fatal("edge reload replaced or closed Clock")
	}
	if settingsHost.place.BarEdge != "right" || settingsHost.place.Output != (ui.Rect{W: 1536, H: 864}) {
		got := settingsHost.place
		reg.mu.Unlock()
		t.Fatalf("Settings placement = edge %q output %+v, want right on 1536x864", got.BarEdge, got.Output)
	}
	if clockHost.place.BarEdge != "right" || clockHost.place.Output != (ui.Rect{W: 1536, H: 864}) {
		got := clockHost.place
		reg.mu.Unlock()
		t.Fatalf("Clock placement = edge %q output %+v, want right on 1536x864", got.BarEdge, got.Output)
	}
	if got := settingsHost.place.Rect(); got != settingsHost.rect {
		reg.mu.Unlock()
		t.Fatalf("Settings host rect %+v disagrees with its updated placement %+v", settingsHost.rect, got)
	}
	if settingsHost.fields["reload-state"] != settingsField || settingsHost.settingsScroll != 73 {
		reg.mu.Unlock()
		t.Fatal("edge reload lost a retained settings field or its scroll offset")
	}
	if settingsFocusName != "" && settingsHost.focused() == nil ||
		settingsFocusName != "" && settingsHost.focused().Name != settingsFocusName {
		reg.mu.Unlock()
		t.Fatalf("edge reload changed Settings focus from %q", settingsFocusName)
	}
	if settingsScroll != nil {
		currentScroll := scrollBody(settingsHost.root)
		if currentScroll == nil || currentScroll.ScrollOffset != settingsScrollOffset {
			reg.mu.Unlock()
			t.Fatal("edge reload lost the Settings body scroll position")
		}
	}
	if settingsHost.rect.X == 0 || settingsHost.rect.Y == 0 {
		reg.mu.Unlock()
		t.Fatalf("Settings was not refitted and repositioned: %+v", settingsHost.rect)
	}
	reg.mu.Unlock()
	updatedClock, updatedSettings := false, false
	for pending := true; pending; {
		select {
		case req := <-reg.AuxRequests():
			if req.ID == panelSurfaceID(PanelClock) {
				if req.Open != nil || req.Update == nil {
					t.Fatalf("retained Clock surface received a remap/close request: %+v", req)
				}
				updatedClock = true
			}
			if req.ID == panelSurfaceID(PanelSettings) && req.Update != nil && req.Update.Width != nil && req.Update.Height != nil {
				updatedSettings = true
			}
		default:
			if !updatedClock || !updatedSettings {
				t.Fatalf("edge reload placement updates: Clock=%v Settings=%v", updatedClock, updatedSettings)
			}
			pending = false
		}
	}
	for _, rect := range []ui.Rect{settingsBefore.rect, clockBefore.rect} {
		if rect.X < 0 || rect.Y < 0 || rect.X+rect.W > 1536 || rect.Y+rect.H > 864 {
			t.Fatalf("retained panel is outside output bounds: %+v", rect)
		}
	}
	reg.dwell.mu.Lock()
	dwellArmed := reg.dwell.armedValid
	reg.dwell.mu.Unlock()
	if dwellArmed {
		t.Fatal("edge reload left an old-geometry tooltip dwell armed")
	}

	thicker := override
	thicker.Height += 8
	next := config.Default()
	next.Outputs = []config.OutputOverride{{Connector: "DP-1", Bar: thicker}}
	prepared, err = reg.PrepareConfig(next, []wayland.HostIdentity{{Global: 7, Connector: "DP-1"}})
	if err != nil {
		t.Fatalf("PrepareConfig thickness: %v", err)
	}
	callbacks = prepared.Hosts[7]
	configureCandidateBar(t, callbacks, thicker, 1536, 864, 120)
	prepared.Commit()

	reg.mu.Lock()
	settingsHost = reg.panelHosts[PanelSettings]
	clockHost = reg.panelHosts[PanelClock]
	if settingsHost == nil {
		reg.mu.Unlock()
		t.Fatal("thickness reload closed Settings")
	}
	if clockHost != clockBefore || settingsHost.place.BarZone != thicker.Extent() || clockHost.place.BarZone != thicker.Extent() {
		got := settingsHost.place.BarZone
		reg.mu.Unlock()
		t.Fatalf("thickness reload replaced/closed Clock=%v or left zones Settings=%d Clock=%d, want %d",
			clockHost != clockBefore, got, clockHost.place.BarZone, thicker.Extent())
	}
	reg.mu.Unlock()
}

func TestEdgeReloadClosesAffectedTrayDrawer(t *testing.T) {
	reg := newPanelRegistry(t)
	withTestBar(t, reg, 7, reg.cfg).setOutputSize(1536, 864)
	harness := &hostHarness{}
	reg.mu.Lock()
	reg.trayDrawer = newTrayDrawerHost(reg, harness)
	opened := reg.trayDrawer.open(7, "DP-1", trayArrangement{}, nil)
	reg.mu.Unlock()
	if !opened {
		t.Fatal("could not open test drawer")
	}

	candidate := config.Default()
	candidate.Bar.Edge = "left"
	prepared, err := reg.PrepareConfig(candidate, []wayland.HostIdentity{{Global: 7, Connector: "DP-1"}})
	if err != nil {
		t.Fatalf("PrepareConfig: %v", err)
	}
	callbacks := prepared.Hosts[7]
	configureCandidateBar(t, callbacks, candidate.Bar, 1536, 864, 120)
	prepared.Commit()
	reg.mu.Lock()
	drawerOpen := reg.trayDrawer.open_
	reg.mu.Unlock()
	if drawerOpen || len(harness.closes) != 1 {
		t.Fatalf("edge reload left drawer open=%v with %d close requests", drawerOpen, len(harness.closes))
	}
}

func TestDisablingBarClosesItsPanels(t *testing.T) {
	reg := newPanelRegistry(t)
	withTestBar(t, reg, 7, reg.cfg).setOutputSize(1536, 864)
	if err := reg.OpenPanel(PanelSettings, 7, Trigger{BarEdge: "top", OutW: 1536, OutH: 864}); err != nil {
		t.Fatal(err)
	}
	_ = drainAux(t, reg, 2)
	if err := reg.OpenPanel(PanelClock, 7, Trigger{BarEdge: "top", OutW: 1536, OutH: 864}); err != nil {
		t.Fatal(err)
	}
	_ = drainAux(t, reg, 1)

	candidate := config.Default()
	candidate.Bar.Enabled = false
	prepared, err := reg.PrepareConfig(candidate, nil)
	if err != nil {
		t.Fatalf("PrepareConfig: %v", err)
	}
	prepared.Commit()

	reg.mu.Lock()
	defer reg.mu.Unlock()
	if len(reg.panelHosts) != 0 {
		t.Fatalf("disabling the bar retained panels: %v", reg.panelHosts)
	}
}

func TestRemovingOutputClosesItsPanelGroup(t *testing.T) {
	reg := newPanelRegistry(t)
	if err := reg.OpenPanel(PanelClock, 7, Trigger{OutW: 1536, OutH: 864}); err != nil {
		t.Fatal(err)
	}
	_ = drainAux(t, reg, 2)
	if err := reg.OpenPanel(PanelSession, 7, Trigger{OutW: 1536, OutH: 864}); err != nil {
		t.Fatal(err)
	}
	_ = drainAux(t, reg, 1)

	// The owner calls DropAux for the shield's OnDrop when it tears down an
	// output's auxiliary surfaces.
	reg.DropAux(7, panelShieldSurfaceID(7))
	reg.mu.Lock()
	defer reg.mu.Unlock()
	if len(reg.panelHosts) != 0 {
		t.Fatalf("output removal retained panels: %v", reg.panelHosts)
	}
}

func TestUnrelatedReloadKeepsOpenPanelLifetime(t *testing.T) {
	reg := newPanelRegistry(t)
	withTestBar(t, reg, 7, reg.cfg).setOutputSize(1536, 864)
	if err := reg.OpenPanel(PanelClock, 7, Trigger{BarEdge: "top", BarZone: 40, OutW: 1536, OutH: 864}); err != nil {
		t.Fatal(err)
	}
	_ = drainAux(t, reg, 2)
	reg.mu.Lock()
	before := reg.panelHosts[PanelClock]
	reg.mu.Unlock()

	candidate := config.Default()
	candidate.Session.Locker = "external-locker"
	candidate.Bar.Style = "solid"
	prepared, err := reg.PrepareConfig(candidate, []wayland.HostIdentity{{Global: 7, Connector: "DP-1"}})
	if err != nil {
		t.Fatalf("PrepareConfig: %v", err)
	}
	callbacks := prepared.Hosts[7]
	callbacks.OutputSize(1536, 864)
	if err := callbacks.Configure(1536, candidate.Bar.SurfaceExtent(), 120); err != nil {
		t.Fatalf("candidate Configure: %v", err)
	}
	prepared.Commit()

	reg.mu.Lock()
	defer reg.mu.Unlock()
	if reg.panelHosts[PanelClock] != before {
		t.Fatal("an unrelated edit replaced or closed the open panel")
	}
}

func TestReloadCentersRetainedPanelWhenTriggerActionDisappears(t *testing.T) {
	reg := newPanelRegistry(t)
	bar := withTestBar(t, reg, 7, reg.cfg)
	bar.setOutputSize(1536, 864)
	if err := bar.Configure(1536, reg.cfg.Bar.SurfaceExtent(), 120); err != nil {
		t.Fatal(err)
	}
	trigger := Trigger{BarEdge: "top", BarZone: 40, OutW: 1536, OutH: 864}
	if bounds := bar.actionBounds(panelControlCenterAction); bounds.W <= 0 {
		t.Fatal("default bar has no Control Centre trigger for the fixture")
	}
	trigger = triggerAtAction(bar, trigger, panelControlCenterAction)
	if err := reg.OpenPanel(PanelControlCenter, 7, trigger); err != nil {
		t.Fatal(err)
	}
	_ = drainAux(t, reg, 2)
	reg.mu.Lock()
	before := reg.panelHosts[PanelControlCenter]
	reg.mu.Unlock()

	candidate := config.Default()
	candidate.Bar.Center = []config.Item{{ID: "media"}}
	prepared, err := reg.PrepareConfig(candidate, []wayland.HostIdentity{{Global: 7, Connector: "DP-1"}})
	if err != nil {
		t.Fatalf("PrepareConfig: %v", err)
	}
	callbacks := prepared.Hosts[7]
	configureCandidateBar(t, callbacks, candidate.Bar, 1536, 864, 120)
	prepared.Commit()

	reg.mu.Lock()
	defer reg.mu.Unlock()
	h := reg.panelHosts[PanelControlCenter]
	if h != before {
		t.Fatal("the disappearing trigger action closed or replaced its panel")
	}
	if h.place.AnchorAction != panelControlCenterAction {
		t.Fatalf("placement forgot its trigger action %q", h.place.AnchorAction)
	}
	if h.place.Align != "center" {
		t.Fatalf("missing trigger did not fall back to centering: align=%q anchor=(%d,%d)", h.place.Align, h.place.AnchorX, h.place.AnchorY)
	}
	if got := h.rect.X + h.rect.W/2; got != 768 {
		t.Fatalf("panel center x = %d, want 768 (%+v)", got, h.rect)
	}
}

func TestGeometryReloadRetainsPendingFilesPick(t *testing.T) {
	reg := newPanelRegistry(t)
	withTestBar(t, reg, 7, reg.cfg).setOutputSize(1536, 864)
	pick := make(chan error, 1)
	root := t.TempDir()
	session := &filesSession{root: root, cwd: root, title: "Pick", mode: "pick-file", pick: pick}
	session.cwd = session.root
	reg.mu.Lock()
	reg.files = session
	reg.mu.Unlock()
	if err := reg.OpenPanel(PanelFiles, 7, Trigger{BarEdge: "top", BarZone: 40, OutW: 1536, OutH: 864}); err != nil {
		t.Fatal(err)
	}
	_ = drainAux(t, reg, 2)
	reg.mu.Lock()
	before := reg.panelHosts[PanelFiles]
	reg.mu.Unlock()

	candidate := config.Default()
	candidate.Bar.Edge = "right"
	prepared, err := reg.PrepareConfig(candidate, []wayland.HostIdentity{{Global: 7, Connector: "DP-1"}})
	if err != nil {
		t.Fatalf("PrepareConfig: %v", err)
	}
	callbacks := prepared.Hosts[7]
	configureCandidateBar(t, callbacks, candidate.Bar, 1536, 864, 120)
	prepared.Commit()

	reg.mu.Lock()
	defer reg.mu.Unlock()
	if reg.panelHosts[PanelFiles] != before || reg.files != session || reg.files.pick != pick {
		t.Fatal("geometry reload replaced the files panel or canceled its pending pick")
	}
	select {
	case err := <-pick:
		t.Fatalf("pending pick was canceled by geometry reload: %v", err)
	default:
	}
}

func TestGeometryReloadReflowsTwoRetainedPanelsOnSideBar(t *testing.T) {
	reg := newPanelRegistry(t)
	withTestBar(t, reg, 7, reg.cfg).setOutputSize(1536, 864)
	trigger := Trigger{BarEdge: "top", BarZone: 40, OutW: 1536, OutH: 864}
	if err := reg.OpenPanel(PanelClock, 7, trigger); err != nil {
		t.Fatal(err)
	}
	_ = drainAux(t, reg, 2)
	if err := reg.OpenPanel(PanelSession, 7, trigger); err != nil {
		t.Fatal(err)
	}
	_ = drainAux(t, reg, 1)
	reg.mu.Lock()
	clockBefore := reg.panelHosts[PanelClock]
	sessionBefore := reg.panelHosts[PanelSession]
	reg.mu.Unlock()

	candidate := config.Default()
	candidate.Bar.Edge = "right"
	prepared, err := reg.PrepareConfig(candidate, []wayland.HostIdentity{{Global: 7, Connector: "DP-1"}})
	if err != nil {
		t.Fatalf("PrepareConfig: %v", err)
	}
	callbacks := prepared.Hosts[7]
	configureCandidateBar(t, callbacks, candidate.Bar, 1536, 864, 120)
	prepared.Commit()

	reg.mu.Lock()
	defer reg.mu.Unlock()
	clock, session := reg.panelHosts[PanelClock], reg.panelHosts[PanelSession]
	if clock != clockBefore || session != sessionBefore {
		t.Fatal("side-bar reload replaced or closed a retained panel")
	}
	if clock.place.BarEdge != "right" || session.place.BarEdge != "right" {
		t.Fatalf("retained edges = %q/%q, want right/right", clock.place.BarEdge, session.place.BarEdge)
	}
	if overlaps(clock.rect, session.rect) {
		t.Fatalf("side-bar reload left retained panels overlapping: Clock=%+v Session=%+v", clock.rect, session.rect)
	}
	for _, rect := range []ui.Rect{clock.rect, session.rect} {
		if rect.X < 0 || rect.Y < 0 || rect.X+rect.W > 1536 || rect.Y+rect.H > 864 {
			t.Fatalf("retained side panel is outside output bounds: %+v", rect)
		}
	}
}

func TestGeometryReloadRetainsPanelsAcrossBarGeometryCases(t *testing.T) {
	reg := newPanelRegistry(t)
	withTestBar(t, reg, 7, reg.cfg).setOutputSize(1536, 864)
	if err := reg.OpenPanel(PanelClock, 7, Trigger{BarEdge: "top", OutW: 1536, OutH: 864}); err != nil {
		t.Fatal(err)
	}
	_ = drainAux(t, reg, 2)
	reg.mu.Lock()
	before := reg.panelHosts[PanelClock]
	reg.mu.Unlock()

	right := config.Default().Bar
	right.Edge = "right"
	bottom := right
	bottom.Edge = "bottom"
	islands := bottom
	islands.Style = "islands"
	reserve := 96
	reserved := islands
	reserved.Reserve = &reserve
	thicker := reserved
	thicker.Height += 8
	for _, tc := range []struct {
		name string
		bar  config.Bar
	}{
		{"top to right", right},
		{"right to bottom", bottom},
		{"attached to islands", islands},
		{"reserve", reserved},
		{"thickness", thicker},
	} {
		t.Run(tc.name, func(t *testing.T) {
			candidate := config.Default()
			candidate.Bar = tc.bar
			prepared, err := reg.PrepareConfig(candidate, []wayland.HostIdentity{{Global: 7, Connector: "DP-1"}})
			if err != nil {
				t.Fatalf("PrepareConfig: %v", err)
			}
			callbacks := prepared.Hosts[7]
			configureCandidateBar(t, callbacks, tc.bar, 1536, 864, 120)
			prepared.Commit()
			reg.mu.Lock()
			defer reg.mu.Unlock()
			h := reg.panelHosts[PanelClock]
			if h != before {
				t.Fatal("geometry reload replaced or closed Clock")
			}
			if h.place.BarEdge != tc.bar.Edge || h.place.BarZone != tc.bar.Extent() {
				t.Fatalf("placement edge/zone = %q/%d, want %q/%d", h.place.BarEdge, h.place.BarZone, tc.bar.Edge, tc.bar.Extent())
			}
			if h.place.Detached == tc.bar.Attached() {
				t.Fatalf("placement detached=%v disagrees with bar attached=%v", h.place.Detached, tc.bar.Attached())
			}
		})
	}
}

func TestLaterBarConfigureRefreshesRetainedPanelPlacement(t *testing.T) {
	reg := newPanelRegistry(t)
	bar := withTestBar(t, reg, 7, reg.cfg)
	bar.setOutputSize(1536, 864)
	if err := bar.Configure(reg.cfg.Bar.SurfaceExtent(), 864, 120); err != nil {
		t.Fatal(err)
	}
	if err := reg.OpenPanel(PanelClock, 7, Trigger{BarEdge: "top", OutW: 1536, OutH: 864}); err != nil {
		t.Fatal(err)
	}
	_ = drainAux(t, reg, 2)
	reg.mu.Lock()
	before := reg.panelHosts[PanelClock]
	reg.mu.Unlock()

	callbacks := reg.bindHost(7, bar, wayland.HostCallbacks{Configure: bar.Configure, OutputSize: bar.setOutputSize})
	callbacks.OutputSize(1600, 900)
	if err := callbacks.Configure(1600, reg.cfg.Bar.SurfaceExtent(), 150); err != nil {
		t.Fatalf("scaled Configure: %v", err)
	}
	reg.mu.Lock()
	defer reg.mu.Unlock()
	if reg.panelHosts[PanelClock] != before || before.place.Output != (ui.Rect{W: 1600, H: 900}) {
		t.Fatalf("later configure lost retained placement: host=%p want=%p output=%+v", reg.panelHosts[PanelClock], before, before.place.Output)
	}
}

func TestGeometryReloadLeavesUnrelatedOutputAndMonitorLeasesAlone(t *testing.T) {
	reg := newPanelRegistry(t)
	withTestBar(t, reg, 7, reg.cfg).setOutputSize(1536, 864)
	bar, leases, _, err := reg.buildBar(reg.cfg, "DP-2", reg.tokens)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { releaseAll(leases); bar.stopAnimation() })
	bar.setOutputSize(1536, 864)
	reg.setTestBar(8, bar)
	for _, b := range []*Bar{reg.bars[7], bar} {
		if err := b.Configure(reg.cfg.Bar.SurfaceExtent(), 864, 120); err != nil {
			t.Fatal(err)
		}
	}
	if err := reg.OpenPanel(PanelClock, 7, Trigger{BarEdge: "top", OutW: 1536, OutH: 864}); err != nil {
		t.Fatal(err)
	}
	_ = drainAux(t, reg, 2)
	if err := reg.OpenPanel(PanelMonitor, 8, Trigger{BarEdge: "top", OutW: 1536, OutH: 864}); err != nil {
		t.Fatal(err)
	}
	_ = drainAux(t, reg, 2)
	reg.mu.Lock()
	clockBefore := reg.panelHosts[PanelClock]
	monitorBefore := reg.panelHosts[PanelMonitor]
	monitorLeases := append([]*services.Lease(nil), monitorBefore.leases...)
	reg.mu.Unlock()
	if !reg.Metrics().SourceLeased(services.SourceProcess) {
		t.Fatal("Monitor fixture did not lease process metrics")
	}

	candidate := config.Default()
	override := candidate.Bar
	override.Edge = "right"
	candidate.Outputs = []config.OutputOverride{{Connector: "DP-1", Bar: override}}
	prepared, err := reg.PrepareConfig(candidate, []wayland.HostIdentity{
		{Global: 7, Connector: "DP-1"}, {Global: 8, Connector: "DP-2"},
	})
	if err != nil {
		t.Fatalf("PrepareConfig: %v", err)
	}
	for _, global := range []uint32{7, 8} {
		callbacks := prepared.Hosts[global]
		policy := candidate.ForConnector(map[uint32]string{7: "DP-1", 8: "DP-2"}[global])
		configureCandidateBar(t, callbacks, policy, 1536, 864, 120)
	}
	prepared.Commit()

	reg.mu.Lock()
	defer reg.mu.Unlock()
	if reg.panelHosts[PanelClock] != clockBefore || reg.panelHosts[PanelMonitor] != monitorBefore {
		t.Fatal("geometry reload changed a panel host on either output")
	}
	if reg.panelHosts[PanelClock].place.BarEdge != "right" || reg.panelHosts[PanelMonitor].place.BarEdge != "top" {
		t.Fatalf("output edges = %q/%q, want right/top", reg.panelHosts[PanelClock].place.BarEdge, reg.panelHosts[PanelMonitor].place.BarEdge)
	}
	if len(monitorBefore.leases) != len(monitorLeases) {
		t.Fatalf("Monitor lease count = %d, want %d", len(monitorBefore.leases), len(monitorLeases))
	}
	for i, lease := range monitorLeases {
		if monitorBefore.leases[i] != lease {
			t.Fatalf("Monitor lease %d was replaced", i)
		}
	}
	if !reg.Metrics().SourceLeased(services.SourceProcess) {
		t.Fatal("geometry reload released the final Monitor process lease")
	}
}

func TestOutputScaleConfigureRefitsOpenSettings(t *testing.T) {
	cfg := config.Default()
	cfg.Accessibility.ReducedMotion = true
	cfg.Bar.Edge = "right"
	reg := NewRegistry(cfg)
	reg.lookPath = func(string) (string, error) { return "", exec.ErrNotFound }
	t.Cleanup(reg.Close)
	bar := withTestBar(t, reg, 7, cfg)
	bar.setOutputSize(1536, 864)
	if err := bar.Configure(cfg.Bar.SurfaceExtent(), 864, 120); err != nil {
		t.Fatal(err)
	}
	if err := reg.OpenPanel(PanelSettings, 7,
		Trigger{BarEdge: "right", BarZone: cfg.Bar.Extent(), OutW: 1536, OutH: 864}); err != nil {
		t.Fatal(err)
	}
	_ = drainAux(t, reg, 2)

	callbacks := reg.bindHost(7, bar, wayland.HostCallbacks{
		Configure:  bar.Configure,
		OutputSize: bar.setOutputSize,
	})
	callbacks.OutputSize(1600, 900)
	if err := callbacks.Configure(cfg.Bar.SurfaceExtent(), 900, 150); err != nil {
		t.Fatalf("scaled Configure: %v", err)
	}

	reg.mu.Lock()
	defer reg.mu.Unlock()
	h := reg.panelHosts[PanelSettings]
	if h == nil {
		t.Fatal("scale configure closed Settings")
	}
	if h.place.Output != (ui.Rect{W: 1600, H: 900}) || h.place.BarEdge != "right" {
		t.Fatalf("Settings placement retained stale output geometry: edge %q output %+v", h.place.BarEdge, h.place.Output)
	}
	if h.rect != h.place.Rect() {
		t.Fatalf("Settings rect %+v disagrees with scale-updated placement %+v", h.rect, h.place.Rect())
	}
}

// The owner's bridge drains invalidations continuously, so a full channel is
// momentary. Dropping there leaves a surface stale until an unrelated event;
// publishSurface must block and deliver like publish (GH #4).
func TestPublishSurfaceNeverDrops(t *testing.T) {
	t.Parallel()
	r := &Registry{
		invalidations: make(chan wayland.Invalidation, 1),
		closed:        make(chan struct{}),
	}
	r.invalidations <- wayland.Invalidation{Global: 1, SurfaceID: "occupied"}
	done := make(chan struct{})
	go func() {
		r.publishSurface(2, "panel:test")
		close(done)
	}()
	select {
	case <-done:
		t.Fatal("publishSurface returned while a full channel had no consumer: the invalidation was dropped")
	case <-time.After(20 * time.Millisecond):
	}
	<-r.invalidations // the bridge drains one
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("publishSurface never completed after the channel drained")
	}
	if inv := <-r.invalidations; inv.Global != 2 || inv.SurfaceID != "panel:test" {
		t.Fatalf("invalidation lost or wrong: %+v", inv)
	}
}

// keepInvalidationsDrained stands in for the owner's bridge in tests that
// build a Registry without one: publishSurface blocks (GH #4) instead of
// dropping, so such a test needs a consumer or it stalls at the ninth one.
func keepInvalidationsDrained(t *testing.T, r *Registry) {
	t.Helper()
	stop := make(chan struct{})
	go func() {
		for {
			select {
			case <-r.invalidations:
			case <-r.closed:
				return
			case <-stop:
				return
			}
		}
	}()
	t.Cleanup(func() { close(stop) })
}

// syncIME and syncCursor call these closures from the Wayland goroutine while
// a relay rebuilds the tree under Registry.mu (GH #6). A lock-free read races.
func TestWantIMEAndIBeamAtTakeTheRegistryLock(t *testing.T) {
	t.Parallel()
	reg := newPanelRegistry(t)
	if err := reg.OpenPanel(PanelLauncher, 7, Trigger{}); err != nil {
		t.Fatal(err)
	}
	_ = drainAux(t, reg, 2)
	h := reg.panelHosts[PanelLauncher]
	if h == nil {
		t.Fatal("launcher host is missing")
	}
	spec := reg.panelSpec(h, Margins{})
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			reg.mu.Lock()
			defer reg.mu.Unlock()
			reg.rebuildPanel(h)
		}()
		spec.Callbacks.WantIME()
		spec.Callbacks.IBeamAt(1, 1)
	}
	wg.Wait()
}

func TestFloatingSettingsUsesStableOutputAnchor(t *testing.T) {
	for _, edge := range []string{"top", "bottom", "left", "right"} {
		t.Run(edge, func(t *testing.T) {
			cfg := config.Default()
			cfg.Bar.Edge = edge
			reg := NewRegistry(cfg)
			t.Cleanup(reg.Close)
			withTestBar(t, reg, 7, cfg).setOutputSize(1536, 864)
			reg.mu.Lock()
			trig := reg.triggerLocked(7, "DP-1")
			reg.mu.Unlock()
			if err := reg.OpenPanel(PanelSettings, 7, trig); err != nil {
				t.Fatal(err)
			}
			requests := drainAux(t, reg, 2)
			reg.mu.Lock()
			rect := reg.panelHosts[PanelSettings].place.Rect()
			reg.mu.Unlock()
			for _, request := range requests {
				if spec := request.Open; spec != nil && spec.ID == panelSurfaceID(PanelSettings) {
					wantAnchor := uint32(layershell.ZwlrLayerSurfaceV1AnchorTop | layershell.ZwlrLayerSurfaceV1AnchorLeft)
					if spec.Anchor != wantAnchor || int(spec.MarginLeft) != rect.X || int(spec.MarginTop) != rect.Y {
						t.Fatalf("floating surface anchor=%d margins=(%d,%d), want anchor=%d rect=%+v", spec.Anchor, spec.MarginLeft, spec.MarginTop, wantAnchor, rect)
					}
				}
			}
		})
	}
}

// A per-output edit lands on the panel's draft, which is a copy of the live
// configuration; before gh #205 it aliased r.cfg, so a discarded edit stayed
// behind and the reload commit never saw the geometry change.
func TestSettingsDraftEditsStayOffTheLiveConfig(t *testing.T) {
	r := newPanelRegistry(t)
	r.cfg.Outputs = []config.OutputOverride{{Connector: "DP-1", Bar: config.Bar{Height: 36}}}
	if err := r.OpenPanel(PanelSettings, 7, Trigger{OutW: 1536, OutH: 864}); err != nil {
		t.Fatal(err)
	}
	r.mu.Lock()
	h := r.panelHosts[PanelSettings]
	if h == nil {
		r.mu.Unlock()
		t.Fatal("no settings panel")
	}
	e := h.set.ByPath("outputs.DP-1.height")
	if e == nil {
		r.mu.Unlock()
		t.Fatal("no outputs.DP-1.height entry")
	}
	if err := e.Set(&h.draft, "64"); err != nil {
		r.mu.Unlock()
		t.Fatal(err)
	}
	draftHeight := h.draft.Outputs[0].Bar.Height
	liveHeight := r.cfg.Outputs[0].Bar.Height
	r.mu.Unlock()

	if draftHeight != 64 {
		t.Fatalf("draft height = %d, want 64", draftHeight)
	}
	if liveHeight != 36 {
		t.Fatalf("live height = %d, want 36: the draft edit reached the live config", liveHeight)
	}
}

// A palette reload carries the resolved motion settings onto a running panel
// animation: the animator is kept, reduced motion settles what was in flight,
// and the crossfade still restarts from what is rendering.
func TestPanelRethemeAppliesMotion(t *testing.T) {
	t.Parallel()
	h := &PanelHost{id: PanelWeather, theme: DefaultTheme()}
	h.anim = newAnimator(nil, false, h.theme.Motion)
	anim := h.anim
	h.anim.Target(panelSurfaceID(h.id), animVisible, 1)

	next := DefaultTheme()
	next.Motion = render.MotionSet{
		Durations: theme.BaseMotion.AtSpeed(400),
		Reduced:   true,
	}
	h.retheme(next)

	if h.anim != anim {
		t.Fatal("retheme replaced the panel animator")
	}
	if !h.anim.reduced {
		t.Fatal("reduced motion did not reach the panel animator")
	}
	if h.themeFrom == (Theme{}) {
		t.Fatal("retheme lost the crossfade origin")
	}
	if !h.anim.has(panelSurfaceID(h.id), animTheme) {
		t.Fatal("retheme did not restart the palette blend")
	}
}
