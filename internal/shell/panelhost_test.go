package shell

import (
	"os/exec"
	"strings"
	"testing"
	"time"

	metrics "github.com/Nomadcxx/sysc-metrics"
	"github.com/Nomadcxx/sysc-notify/protocol"

	"github.com/Nomadcxx/sysc-shell/internal/config"
	"github.com/Nomadcxx/sysc-shell/internal/platform/wayland"
	"github.com/Nomadcxx/sysc-shell/internal/services"
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

// Monitor cards are KindCapsule. The panel painter used to omit Capsule from
// Style, so fillRoundedRect skipped the A=0 fill and every card vanished
// into the panel background.
func TestPanelHostRenderPaintsMonitorCards(t *testing.T) {
	t.Parallel()
	reg := newPanelRegistry(t)
	if err := reg.OpenPanel(PanelMonitor, 7, Trigger{BarEdge: "top", BarZone: 44}); err != nil {
		t.Fatal(err)
	}
	reqs := drainAux(t, reg, 2)
	panel := reqs[1].Open
	const w, hgt = 640, 480
	if err := panel.Callbacks.Configure(w, hgt, 120); err != nil {
		t.Fatal(err)
	}
	pix := make([]byte, w*hgt*4)
	if err := panel.Callbacks.Render(pix, w, hgt, w*4); err != nil {
		t.Fatal(err)
	}
	h := reg.panelHosts[PanelMonitor]
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
	// Attached to a top bar: the body meets the bar with a square top, so the
	// corner at (0,0) is opaque rather than a rounded seam of wallpaper.
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
	handle(wayland.Event{Kind: wayland.EventKeyPress, Key: keyLeftShift})
	handle(wayland.Event{Kind: wayland.EventKeyPress, Key: keyTab})
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
	if _, ok := reg.panelHosts[PanelSession]; ok {
		t.Fatal("space did not activate the focused session action")
	}
}

func TestRevealAnimationInvalidatesUntilDone(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	reg := NewRegistry(cfg)
	reg.lookPath = func(string) (string, error) { return "", exec.ErrNotFound }
	t.Cleanup(reg.Close)
	if err := reg.OpenPanel(PanelSession, 7, Trigger{}); err != nil {
		t.Fatal(err)
	}
	n := countSurfaceInvalidations(reg, 200*time.Millisecond)
	if n < 5 {
		t.Fatalf("got %d surface invalidations, want at least 5 during reveal", n)
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
	if got := countSurfaceInvalidations(quiet, animReducedPanelDuration+50*time.Millisecond); got == 0 {
		t.Fatal("reduced motion produced no invalidations; the panel never appeared")
	}
	if got := countSurfaceInvalidations(quiet, 100*time.Millisecond); got != 0 {
		t.Fatalf("surface still invalidating %d times after the fade settled", got)
	}
}

func TestRightClickingTheBarBatteryOpensSession(t *testing.T) {
	reg := newPanelRegistry(t)
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
	reg := newPanelRegistry(t)
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
	if panel.Width != 416 {
		t.Fatalf("width = %d, want 416", panel.Width)
	}
	if panel.Height < 300 {
		t.Fatalf("height = %d, want at least 300", panel.Height)
	}
	if want := int32(1536 - 416 - 8); panel.MarginLeft != want {
		t.Fatalf("margin left = %d, want trailing %d", panel.MarginLeft, want)
	}
	if panel.MarginTop != 44 {
		t.Fatalf("margin top = %d, want hug bar 44", panel.MarginTop)
	}
	if !reg.roots.owns(panelRoot(PanelNotifications)) {
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
	found := false
	for i, n := range h.focus {
		if n.Action == "notify:center:tab:1" {
			h.roving.Set(i)
			h.activate(reg)
			found = true
			break
		}
	}
	if !found {
		t.Fatal("history tab missing")
	}
	if reg.panelHosts[PanelNotifications] == nil {
		t.Fatal("tab switch dropped the centre")
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
	cur := buttonByAction(h.root, "notify:center:tab:0")
	if cur == nil || cur.Text != "Current (1)" {
		t.Fatalf("current tab after delta = %+v", cur)
	}
	if !containsText(h.root, "incoming") {
		t.Fatalf("tree after delta = %v", texts(h.root))
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
	if err := cb.Configure(1536, 44, 120); err != nil {
		t.Fatal(err)
	}
	if err := reg.TogglePanelByName("system-monitor"); err != nil {
		t.Fatal(err)
	}
	reqs := drainAux(t, reg, 2)
	got := reqs[1].Open
	if got.MarginTop != 44 {
		t.Fatalf("margin top = %d, want flush on the 44px exclusive zone", got.MarginTop)
	}
	if want := int32((1536 - 640) / 2); got.MarginLeft != want {
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

func newPanelRegistry(t *testing.T) *Registry {
	t.Helper()
	cfg := config.Default()
	cfg.Accessibility.ReducedMotion = true
	reg := NewRegistry(cfg)
	reg.lookPath = func(string) (string, error) { return "", exec.ErrNotFound }
	t.Cleanup(reg.Close)
	return reg
}

func drainAux(t *testing.T, reg *Registry, n int) []wayland.AuxRequest {
	t.Helper()
	out := make([]wayland.AuxRequest, 0, n)
	deadline := time.After(time.Second)
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

func drainInvalidations(reg *Registry) {
	for {
		select {
		case <-reg.Invalidations():
		default:
			return
		}
	}
}

func TestOpeningAnUnrelatedPanelClosesTheOldRoot(t *testing.T) {
	reg := newPanelRegistry(t)
	if err := reg.OpenPanel(PanelClock, 7, Trigger{}); err != nil {
		t.Fatal(err)
	}
	_ = drainAux(t, reg, 2)
	if _, ok := reg.panelHosts[PanelClock]; !ok {
		t.Fatal("the first panel never opened")
	}

	if err := reg.OpenPanel(PanelSession, 7, Trigger{}); err != nil {
		t.Fatal(err)
	}
	// Closing the old root emits its panel and shield closes; the new root
	// then emits its own two opens.
	_ = drainAux(t, reg, 4)

	if _, ok := reg.panelHosts[PanelClock]; ok {
		t.Fatal("the replaced panel is still hosted")
	}
	if _, ok := reg.panels.Output(PanelClock); ok {
		t.Fatal("the replaced panel is still recorded as open")
	}
	if _, ok := reg.panelHosts[PanelSession]; !ok {
		t.Fatal("the new root did not open")
	}
	if !reg.roots.owns(panelRoot(PanelSession)) {
		t.Fatal("the chain owner is not the new panel")
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
	if !reg.roots.owns(panelRoot(PanelMonitor)) {
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
		"ClosePanel":   func(r *Registry) { r.ClosePanel(PanelClock) },
		"TogglePanel":  func(r *Registry) { _ = r.TogglePanel(PanelClock, 7, Trigger{}) },
		"DropAux":      func(r *Registry) { r.DropAux(7, panelSurfaceID(PanelClock)) },
		"closeAll":     func(r *Registry) { r.mu.Lock(); r.closeAllPanelsLocked(); r.mu.Unlock() },
		"replacedRoot": func(r *Registry) { _ = r.OpenPanel(PanelSession, 7, Trigger{}) },
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
	reg.bars[1] = &Bar{conn: "DP-1"}
	reg.bars[2] = &Bar{conn: "DP-2"}

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
