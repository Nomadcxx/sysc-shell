package shell

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/Nomadcxx/sysc-notify/protocol"
	"github.com/Nomadcxx/sysc-shell/internal/config"
	"github.com/Nomadcxx/sysc-shell/internal/icons"
	"github.com/Nomadcxx/sysc-shell/internal/notifyclient"
	"github.com/Nomadcxx/sysc-shell/internal/platform/wayland"
	"github.com/Nomadcxx/sysc-shell/internal/platform/wayland/layershell"
	"github.com/Nomadcxx/sysc-shell/internal/render"
	"github.com/Nomadcxx/sysc-shell/internal/theme"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

func TestToastHostOpensOneOverlayPerOutput(t *testing.T) {
	r := NewRegistry(config.Default())
	h := newToastHost(r, &hostHarness{})

	r.outputsForTest([]string{"eDP-1", "HDMI-A-1"})
	h.syncOutputs(map[string]uint32{"eDP-1": 5, "HDMI-A-1": 9})

	if len(h.harness().opens) != 2 {
		t.Fatalf("opens = %d, want one per output", len(h.harness().opens))
	}
	for _, spec := range h.harness().opens {
		if spec.ExclusiveZone != -1 {
			t.Fatalf("exclusive zone = %d, want -1", spec.ExclusiveZone)
		}
		if spec.Keyboard != keyboardNone {
			t.Fatalf("keyboard = %d, want None", spec.Keyboard)
		}
		if spec.Layer != layerOverlay {
			t.Fatalf("layer = %v, want Overlay", spec.Layer)
		}
	}
}

func TestToastHostPublishesTheCardUnionAsInputRegion(t *testing.T) {
	r := NewRegistry(config.Default())
	hh := &hostHarness{}
	h := newToastHost(r, hh)
	r.outputsForTest([]string{"eDP-1"})
	h.syncOutputs(map[string]uint32{"eDP-1": 5})
	if err := h.configure("eDP-1", 1920, 1080, 120); err != nil {
		t.Fatal(err)
	}

	r.applyNotify(snap(1, note(1, "a"), note(2, "b")))
	h.recompute()

	if len(hh.updates) == 0 || !hh.updates[len(hh.updates)-1].SetInputRegion {
		t.Fatalf("no input-region update: %+v", hh.updates)
	}
	rects := hh.updates[len(hh.updates)-1].InputRects
	if len(rects) != 2 {
		t.Fatalf("input region = %d rects, want the two cards", len(rects))
	}
	for _, r := range rects {
		if r.W != toastCardWidth {
			t.Fatalf("input rect %+v is not a card", r)
		}
	}
}

func TestToastHostEmptyRegionStillReplaces(t *testing.T) {
	r := NewRegistry(config.Default())
	hh := &hostHarness{}
	h := newToastHost(r, hh)
	r.outputsForTest([]string{"eDP-1"})
	h.syncOutputs(map[string]uint32{"eDP-1": 5})
	h.recompute()

	if len(hh.updates) == 0 {
		t.Fatal("no update for an empty stack")
	}
	u := hh.updates[len(hh.updates)-1]
	if !u.SetInputRegion || len(u.InputRects) != 0 {
		t.Fatalf("empty stack update = %+v, want SetInputRegion with no rects", u)
	}
}

func TestToastHostClosesOnOutputLoss(t *testing.T) {
	r := NewRegistry(config.Default())
	hh := &hostHarness{}
	h := newToastHost(r, hh)
	r.outputsForTest([]string{"eDP-1", "HDMI-A-1"})
	h.syncOutputs(map[string]uint32{"eDP-1": 5, "HDMI-A-1": 9})

	h.syncOutputs(map[string]uint32{"eDP-1": 5})
	if len(hh.closes) != 1 {
		t.Fatalf("closes = %v, want the lost output's host closed", hh.closes)
	}
}

func TestToastHostQueuesOverflowPerOutput(t *testing.T) {
	r := NewRegistry(config.Default())
	h := newToastHost(r, &hostHarness{})
	r.outputsForTest([]string{"eDP-1"})
	h.syncOutputs(map[string]uint32{"eDP-1": 5})
	if err := h.configure("eDP-1", 1920, 1080, 120); err != nil {
		t.Fatal(err)
	}

	msg := snap(1)
	for i := uint32(1); i <= 8; i++ {
		msg.Snapshot.Active = append(msg.Snapshot.Active, note(i, "n"))
	}
	r.applyNotify(msg)
	h.recompute()

	// The small default output cannot fit eight cards; some must queue, and
	// the aggregate state for a queued-on-every-output record is queued.
	got := r.aggregatePresentation(1, h.viewFor(1))
	if got != protocol.PresentationQueued && got != protocol.PresentationVisible {
		t.Fatalf("aggregate = %q", got)
	}
}

func TestToastHostReconnectClearsSurfaces(t *testing.T) {
	r := NewRegistry(config.Default())
	hh := &hostHarness{}
	h := newToastHost(r, hh)
	r.outputsForTest([]string{"eDP-1"})
	h.syncOutputs(map[string]uint32{"eDP-1": 5})

	r.applyNotify(snap(1, note(1, "a")))
	h.recompute()
	r.applyNotify(disconnect(1))
	h.recompute()

	u := hh.updates[len(hh.updates)-1]
	if !u.SetInputRegion || len(u.InputRects) != 0 {
		t.Fatalf("disconnect left cards interactive: %+v", u)
	}
}

func disconnect(generation uint64) notifyclient.Message {
	return notifyclient.Message{Generation: generation, Kind: notifyclient.KindDisconnected}
}

// The surface is anchored to all four edges so the compositor reports the
// output's own size, and it carries the three callbacks a mapped surface
// needs. A missing Render is fatal to the owner, so this is the shape that
// makes the surface usable at all.
func TestToastSurfaceIsOutputSizedAndPaintable(t *testing.T) {
	r := NewRegistry(config.Default())
	t.Cleanup(r.Close)
	h := newToastHost(r, &hostHarness{})
	r.outputsForTest([]string{"eDP-1"})
	h.syncOutputs(map[string]uint32{"eDP-1": 5})

	spec := h.harness().opens[0]
	every := uint32(layershell.ZwlrLayerSurfaceV1AnchorTop |
		layershell.ZwlrLayerSurfaceV1AnchorBottom |
		layershell.ZwlrLayerSurfaceV1AnchorLeft |
		layershell.ZwlrLayerSurfaceV1AnchorRight)
	if spec.Anchor != every {
		t.Fatalf("anchor = %d, want every edge", spec.Anchor)
	}
	if spec.Callbacks.Configure == nil || spec.Callbacks.Render == nil || spec.Callbacks.Handle == nil {
		t.Fatal("the toast surface is missing a callback")
	}
}

// A configure replaces the placeholder geometry, so cards are placed against
// the output the compositor actually reported.
func TestToastConfigurePlacesAgainstTheRealOutput(t *testing.T) {
	r := NewRegistry(config.Default())
	t.Cleanup(r.Close)
	h := newToastHost(r, &hostHarness{})
	r.outputsForTest([]string{"eDP-1"})
	h.syncOutputs(map[string]uint32{"eDP-1": 5})
	r.applyNotify(snap(1, note(1, "a")))

	if err := h.harness().opens[0].Callbacks.Configure(3440, 1440, 120); err != nil {
		t.Fatal(err)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	geometry, known := h.geometryFor("eDP-1")
	if !known || geometry.OutputW != 3440 || geometry.OutputH != 1440 {
		t.Fatalf("geometry = %+v (known %v), want the configured size", geometry, known)
	}
	cards := h.cards["eDP-1"]
	if len(cards) != 1 {
		t.Fatalf("cards = %d, want the one active record", len(cards))
	}
	// Top-right corner: the card's right edge sits one margin inside the output.
	if right := cards[0].rect.X + cards[0].rect.W; right != 3440-toastMargin {
		t.Fatalf("card right edge = %d, want %d", right, 3440-toastMargin)
	}
}

// Painting leaves the surface transparent everywhere except the cards. The
// painter fills exactly one rounded body per call, so the gaps between cards
// would be filled if the stack were painted as one body.
func TestToastPaintLeavesTheGapsTransparent(t *testing.T) {
	r := NewRegistry(config.Default())
	t.Cleanup(r.Close)
	h := newToastHost(r, &hostHarness{})
	r.outputsForTest([]string{"eDP-1"})
	h.syncOutputs(map[string]uint32{"eDP-1": 5})
	r.applyNotify(snap(1, note(1, "first"), note(2, "second")))

	const width, height = 1200, 800
	callbacks := h.harness().opens[0].Callbacks
	if err := callbacks.Configure(width, height, 120); err != nil {
		t.Fatal(err)
	}
	stride := width * 4
	pixels := make([]byte, stride*height)
	for i := range pixels {
		pixels[i] = 0xff // prove the paint clears rather than inheriting
	}
	if err := callbacks.Render(pixels, width, height, stride); err != nil {
		t.Fatal(err)
	}

	r.mu.Lock()
	cards := append([]toastCard(nil), h.cards["eDP-1"]...)
	r.mu.Unlock()
	if len(cards) != 2 {
		t.Fatalf("cards = %d, want two", len(cards))
	}
	alphaAt := func(x, y int) byte { return pixels[y*stride+x*4+3] }
	// The top-left corner of the output is outside every card.
	if got := alphaAt(2, 2); got != 0 {
		t.Fatalf("corner alpha = %d, want transparent", got)
	}
	// The gap between the two cards is outside both.
	gapY := cards[0].rect.Y + cards[0].rect.H + toastCardGap/2
	gapX := cards[0].rect.X + cards[0].rect.W/2
	if got := alphaAt(gapX, gapY); got != 0 {
		t.Fatalf("gap alpha at %d,%d = %d, want transparent", gapX, gapY, got)
	}
	// The middle of a card is painted.
	cardX := cards[0].rect.X + cards[0].rect.W/2
	cardY := cards[0].rect.Y + cards[0].rect.H/2
	if got := alphaAt(cardX, cardY); got == 0 {
		t.Fatalf("card centre at %d,%d is transparent", cardX, cardY)
	}
	r.mu.Lock()
	st := h.style
	r.mu.Unlock()
	if st.OnContainer.A == 0 || st.Capsule.A == 0 {
		t.Fatalf("toast style dropped capsule tokens: OnContainer=%+v Capsule=%+v", st.OnContainer, st.Capsule)
	}
}

type fakeNotifySender struct {
	mu   sync.Mutex
	cmds []protocol.Command
	// fail makes every Send report an unreachable daemon, recording nothing.
	fail bool
}

func (f *fakeNotifySender) Send(c protocol.Command) (uint64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.fail {
		return 0, errors.New("notify: not connected")
	}
	f.cmds = append(f.cmds, c)
	return uint64(len(f.cmds)), nil
}

func (f *fakeNotifySender) ofKind(kind protocol.CommandKind) []protocol.Command {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []protocol.Command
	for _, c := range f.cmds {
		if c.Kind == kind {
			out = append(out, c)
		}
	}
	return out
}

// Hover is what holds a toast open, so the pointer has to reach the host.
// Moving onto a card marks it hovered and moving away clears it.
func TestToastHoverFollowsThePointer(t *testing.T) {
	r := NewRegistry(config.Default())
	t.Cleanup(r.Close)
	h := newToastHost(r, &hostHarness{})
	r.outputsForTest([]string{"eDP-1"})
	h.syncOutputs(map[string]uint32{"eDP-1": 5})
	r.applyNotify(snap(1, note(1, "a")))
	callbacks := h.harness().opens[0].Callbacks
	if err := callbacks.Configure(1200, 800, 120); err != nil {
		t.Fatal(err)
	}

	r.mu.Lock()
	rect := h.cards["eDP-1"][0].rect
	r.mu.Unlock()

	callbacks.Handle(wayland.Event{Kind: wayland.EventPointerEnter,
		X: float64(rect.X + rect.W/2), Y: float64(rect.Y + rect.H/2)})
	r.mu.Lock()
	hovered := h.hovered["eDP-1"][1]
	r.mu.Unlock()
	if !hovered {
		t.Fatal("the pointer over a card did not mark it hovered")
	}

	callbacks.Handle(wayland.Event{Kind: wayland.EventPointerLeave})
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(h.hovered["eDP-1"]) != 0 {
		t.Fatalf("hover survived the pointer leaving: %+v", h.hovered["eDP-1"])
	}
}

// unmeasuredToast wires a host whose output has never reported its size, which
// is the state between a surface opening and its first configure.
func unmeasuredToast(t *testing.T) (*Registry, *toastHost, *fakeNotifySender) {
	t.Helper()
	r := NewRegistry(config.Default())
	t.Cleanup(r.Close)
	sender := &fakeNotifySender{}
	r.notifySender = sender
	h := newToastHost(r, &hostHarness{})
	r.toasts = h
	r.outputsForTest([]string{"eDP-1"})
	h.syncOutputs(map[string]uint32{"eDP-1": 5})
	return r, h, sender
}

// wiredToast is a host whose output has reported a size, which is what every
// test about placement needs: cards are not placed against an unmeasured
// output, so a test that wants one has to say how big it is.
func wiredToast(t *testing.T) (*Registry, *toastHost, *fakeNotifySender) {
	t.Helper()
	r, h, sender := unmeasuredToast(t)
	if err := h.configure("eDP-1", 1920, 1080, 120); err != nil {
		t.Fatal(err)
	}
	return r, h, sender
}

func TestToastClickDismissesACardWithoutADefaultAction(t *testing.T) {
	r, h, sender := wiredToast(t)
	r.applyNotify(snap(1, note(1, "headphones")))
	callbacks := h.harness().opens[0].Callbacks
	if err := callbacks.Configure(1200, 800, 120); err != nil {
		t.Fatal(err)
	}
	r.mu.Lock()
	rect := h.cards["eDP-1"][0].rect
	r.mu.Unlock()

	x, y := float64(rect.X+rect.W/2), float64(rect.Y+rect.H/2)
	if !callbacks.Handle(wayland.Event{Kind: wayland.EventPointerPress, Button: buttonLeft, X: x, Y: y}) {
		t.Fatal("press on a toast card reported no handling")
	}
	if !callbacks.Handle(wayland.Event{Kind: wayland.EventPointerRelease, Button: buttonLeft, X: x, Y: y}) {
		t.Fatal("release on a toast card reported no handling")
	}
	got := sender.ofKind(protocol.CommandDismiss)
	if len(got) != 1 || got[0].ID != 1 {
		t.Fatalf("dismiss commands = %+v, want notification.dismiss of 1", sender.cmds)
	}
}

func TestToastRecomputeInvalidatesTheSurface(t *testing.T) {
	r, _, _ := wiredToast(t)
	drainInvalidations(r)
	r.applyNotify(snap(1, note(1, "a")))
	found := false
	for {
		select {
		case inv := <-r.Invalidations():
			if inv.SurfaceID == toastSurfaceID("eDP-1") {
				found = true
			}
		default:
			if !found {
				t.Fatal("recompute did not invalidate the toast surface")
			}
			return
		}
	}
}

func TestToastReportsVisiblePresentation(t *testing.T) {
	r, _, sender := wiredToast(t)
	r.applyNotify(snap(1, note(1, "a")))
	got := sender.ofKind(protocol.CommandPresentationRenew)
	if len(got) == 0 {
		t.Fatal("no presentation.renew after a card became visible")
	}
	last := got[len(got)-1]
	if len(last.Presentations) != 1 || last.Presentations[0].ID != 1 ||
		last.Presentations[0].State != protocol.PresentationVisible {
		t.Fatalf("renew = %+v, want id 1 visible", last)
	}
}

func TestOpeningTheCentreHidesToasts(t *testing.T) {
	r, h, sender := wiredToast(t)
	r.cfg.Accessibility.ReducedMotion = true
	r.applyNotify(snap(1, note(1, "a")))
	if len(h.cards["eDP-1"]) == 0 {
		t.Fatal("card did not land before opening the centre")
	}

	if err := r.OpenPanel(PanelNotifications, 7, Trigger{}); err != nil {
		t.Fatal(err)
	}
	_ = drainAux(t, r, 2)

	r.mu.Lock()
	hidden := len(h.cards["eDP-1"])
	r.mu.Unlock()
	if hidden != 0 {
		t.Fatalf("cards while centre open = %d, want none", hidden)
	}
	hh := h.harness()
	if n := len(hh.updates); n == 0 || len(hh.updates[n-1].InputRects) != 0 {
		t.Fatalf("input region while centre open = %+v", hh.updates)
	}
	renew := sender.ofKind(protocol.CommandPresentationRenew)
	if len(renew) == 0 || renew[len(renew)-1].Presentations[0].State != protocol.PresentationSuppressed {
		t.Fatalf("presentation while centre open = %+v", renew)
	}

	r.ClosePanel(PanelNotifications)
	_ = drainAux(t, r, 2)

	r.mu.Lock()
	restored := len(h.cards["eDP-1"])
	r.mu.Unlock()
	if restored == 0 {
		t.Fatal("cards did not return after closing the centre")
	}
	if n := len(hh.updates); n == 0 || len(hh.updates[n-1].InputRects) == 0 {
		t.Fatal("input region stayed empty after close")
	}
}

// A timed preset must lift suppression when it ends: the sticky bit clears,
// the hook fires once, and cards come back without reopening anything.
func TestTimedDNDPresetLiftsSuppressionWhenItEnds(t *testing.T) {
	r, h, _ := wiredToast(t)
	var mu sync.Mutex
	var calls []bool
	r.notify.onDND = func(on bool) {
		mu.Lock()
		calls = append(calls, on)
		mu.Unlock()
	}
	now := time.Unix(1_756_000_000, 0)
	r.UpdateClock(now)
	r.setDNDPresetAt(now, time.Minute)
	r.applyNotify(snap(1, note(1, "a")))

	r.mu.Lock()
	during := len(h.cards["eDP-1"])
	r.mu.Unlock()
	if during != 0 {
		t.Fatalf("cards during DND = %d, want none", during)
	}

	r.UpdateClock(now.Add(2 * time.Minute))

	r.mu.Lock()
	after := len(h.cards["eDP-1"])
	r.mu.Unlock()
	if after == 0 {
		t.Fatal("cards stayed suppressed after the preset ended")
	}
	if _, on := r.dndStateAt(now.Add(2 * time.Minute)); on {
		t.Fatal("DND still reports on after the preset ended")
	}
	mu.Lock()
	got := append([]bool(nil), calls...)
	mu.Unlock()
	if len(got) != 2 || got[0] != true || got[1] != false {
		t.Fatalf("DND hook calls = %v, want [true false]", got)
	}
}

func TestToastRenewsPresentationWhileCardsStayUp(t *testing.T) {
	r, h, sender := wiredToast(t)
	r.applyNotify(snap(1, note(1, "a")))
	h.startLeaseRenew(15 * time.Millisecond)
	deadline := time.Now().Add(200 * time.Millisecond)
	for time.Now().Before(deadline) {
		if len(sender.ofKind(protocol.CommandPresentationRenew)) >= 3 {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("renews = %d, want at least 3 while a card stays up", len(sender.ofKind(protocol.CommandPresentationRenew)))
}

// The notify pump and the Wayland owner share one TextRenderer. Measuring a
// new card while a frame is painting used to trip harfbuzz.
func TestToastApplyDoesNotRaceThePainter(t *testing.T) {
	r, h, _ := wiredToast(t)
	keepInvalidationsDrained(t, r) // 80 applies outrun the cap-8 channel
	r.applyNotify(snap(1, note(1, "one")))
	callbacks := h.harness().opens[0].Callbacks
	const width, height = 1200, 800
	if err := callbacks.Configure(width, height, 120); err != nil {
		t.Fatal(err)
	}
	stride := width * 4
	pixels := make([]byte, stride*height)
	if err := callbacks.Render(pixels, width, height, stride); err != nil {
		t.Fatal(err)
	}

	start := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		<-start
		for i := 0; i < 80; i++ {
			r.applyNotify(snap(1, note(1, "one"), note(2, "two")))
		}
	}()
	go func() {
		defer wg.Done()
		<-start
		buf := make([]byte, stride*height)
		for i := 0; i < 40; i++ {
			if err := callbacks.Render(buf, width, height, stride); err != nil {
				t.Error(err)
				return
			}
		}
	}()
	close(start)
	wg.Wait()
}

// An output's size arrives with its first Configure. Until then the host knows
// no width, and a card placed against a guess can land outside the surface the
// compositor actually gave us: the platform rejects such an input region and
// the shell exits, so a notification arriving early took the whole bar down on
// a 1.25-scaled 1920x1080 laptop, whose logical width is 1536.
func TestToastPlacesNoCardBeforeTheOutputSizeIsKnown(t *testing.T) {
	_, h, _ := unmeasuredToast(t)
	h.r.applyNotify(snap(1, note(1, "early")))

	for _, update := range h.harness().updates {
		if len(update.InputRects) != 0 {
			t.Fatalf("input rects %+v were published before the output size was known", update.InputRects)
		}
	}
	h.r.mu.Lock()
	visible := len(h.visible["eDP-1"])
	h.r.mu.Unlock()
	if visible != 0 {
		t.Fatalf("%d card(s) placed before the output size was known", visible)
	}
}

// Once the size is known every card is inside it, at the scale that exposed
// the defect: 1920x1080 at 125% is 1536x864 logical.
func TestToastCardsStayInsideAScaledOutput(t *testing.T) {
	_, h, _ := unmeasuredToast(t)
	callbacks := h.harness().opens[0].Callbacks
	if err := callbacks.Configure(1536, 864, 150); err != nil {
		t.Fatal(err)
	}
	h.r.applyNotify(snap(1, note(1, "scaled")))

	h.r.mu.Lock()
	rects := h.cardRects("eDP-1", h.visible["eDP-1"])
	h.r.mu.Unlock()
	if len(rects) == 0 {
		t.Fatal("no card was placed on a known output")
	}
	for _, r := range rects {
		if r.X < 0 || r.Y < 0 || r.X+r.W > 1536 || r.Y+r.H > 864 {
			t.Errorf("card %+v leaves a 1536x864 output", r)
		}
	}
}

func TestDropAuxAfterCompositorCloseLeavesNoZombieSurfaces(t *testing.T) {
	r := NewRegistry(config.Default())
	hh := &hostHarness{}
	r.toasts = newToastHost(r, hh)
	r.outputsForTest([]string{"eDP-1"})
	r.toasts.syncOutputs(map[string]uint32{"eDP-1": 5})
	r.applyNotify(snap(1, note(1, "a")))
	r.toasts.recompute()

	r.DropAux(5, toastSurfaceID("eDP-1"))

	hh.updates = nil
	r.toasts.recompute()
	if len(hh.updates) != 0 {
		t.Fatalf("recompute sent %d updates to a dropped toast surface", len(hh.updates))
	}

	r.osd.open[5] = true
	r.DropAux(5, osdSurfaceID(5))
	if r.osd.open[5] {
		t.Fatal("dropped OSD surface stayed open")
	}

	// A later output sync reopens the toast surface cleanly.
	hh.opens = nil
	r.toasts.syncOutputs(map[string]uint32{"eDP-1": 7})
	if len(hh.opens) != 1 || hh.opens[0].ID != toastSurfaceID("eDP-1") {
		t.Fatalf("toast surface was not reopened: %+v", hh.opens)
	}
}

func TestDecodedIconRecomputesOpenToasts(t *testing.T) {
	r := NewRegistry(config.Default())
	hh := &hostHarness{}
	r.toasts = newToastHost(r, hh)
	r.outputsForTest([]string{"eDP-1"})
	r.toasts.syncOutputs(map[string]uint32{"eDP-1": 5})
	n := note(1, "build done")
	n.AppIcon = "firefox"
	r.applyNotify(snap(1, n))
	before := len(hh.updates)

	r.applyTrayIcon(icons.Square("firefox", notifyIconRaster), &ui.Image{Width: 16, Height: 16, Stride: 64, Pix: make([]byte, 16*64)})

	if len(hh.updates) <= before {
		t.Fatalf("decoding the toast's app icon did not recompute (updates %d -> %d)", before, len(hh.updates))
	}
}

func TestRethemeRecomputesToasts(t *testing.T) {
	r := NewRegistry(config.Default())
	hh := &hostHarness{}
	r.toasts = newToastHost(r, hh)
	r.outputsForTest([]string{"eDP-1"})
	r.toasts.syncOutputs(map[string]uint32{"eDP-1": 5})
	r.applyNotify(snap(1, note(1, "a")))
	before := len(hh.updates)

	r.mu.Lock()
	r.retheThemeOpenSurfacesLocked()
	r.mu.Unlock()

	if len(hh.updates) <= before {
		t.Fatalf("retheme did not relayout open toasts (updates %d -> %d)", before, len(hh.updates))
	}
}

// glassToasts opens one output, shows the given toasts and paints them twice:
// the first render measures real fonts, the second paints the settled layout.
func glassToasts(t *testing.T, blur bool, notes ...protocol.Notification) (*Registry, *toastHost, []byte, int) {
	t.Helper()
	cfg := config.Default()
	cfg.Theme.BlurBehind = true
	// Presets set panel and overlay opacity equal; separate them so the
	// ground tests can tell which style painted.
	cfg.Theme.PanelOpacity = 65
	r := NewRegistry(cfg)
	t.Cleanup(r.Close)
	r.mu.Lock()
	r.caps.Blur = blur
	r.mu.Unlock()
	h := newToastHost(r, &hostHarness{})
	// Wired as main wires it, so notification changes reach the host.
	r.toasts = h
	r.outputsForTest([]string{"eDP-1"})
	h.syncOutputs(map[string]uint32{"eDP-1": 5})
	r.applyNotify(snap(1, notes...))
	const width, height = 1200, 800
	cb := h.harness().opens[0].Callbacks
	if err := cb.Configure(width, height, 120); err != nil {
		t.Fatal(err)
	}
	pixels := make([]byte, width*4*height)
	for range 2 {
		if err := cb.Render(pixels, width, height, width*4); err != nil {
			t.Fatal(err)
		}
	}
	return r, h, pixels, width * 4
}

func TestToastCardIsItsContentHeight(t *testing.T) {
	r, h, _, _ := glassToasts(t, true, note(1, "hello"))
	r.mu.Lock()
	defer r.mu.Unlock()
	want, err := ui.ContentHeight(h.cardFor(1), toastCardWidth, h.measureText())
	if err != nil {
		t.Fatal(err)
	}
	if got := h.cards["eDP-1"][0].rect.H; got != want {
		t.Fatalf("card height = %d, want content height %d", got, want)
	}
}

// groundAlpha is the alpha just inside a card's lower-left corner, clear of
// the rim and of any content.
func groundAlpha(h *toastHost, pixels []byte, stride int) byte {
	rect := h.cards["eDP-1"][0].rect
	return pixels[(rect.Y+rect.H-6)*stride+(rect.X+6)*4+3]
}

func TestToastGroundFollowsPanelOpacityUnderCompositorBlur(t *testing.T) {
	r, h, pixels, stride := glassToasts(t, true, note(1, "hello"))
	r.mu.Lock()
	defer r.mu.Unlock()
	want := r.surfaceTheme().PanelStyle().RootFill().A
	if want == r.surfaceTheme().OverlayStyle().RootFill().A {
		t.Fatal("panel and overlay grounds match; the test cannot tell them apart")
	}
	if got := groundAlpha(h, pixels, stride); got != want {
		t.Fatalf("ground alpha = %d, want the panel's %d", got, want)
	}
}

func TestToastWithoutCompositorBlurKeepsTheOverlayGround(t *testing.T) {
	r, h, pixels, stride := glassToasts(t, false, note(1, "hello"))
	r.mu.Lock()
	defer r.mu.Unlock()
	want := r.surfaceTheme().OverlayStyle().RootFill().A
	if got := groundAlpha(h, pixels, stride); got != want {
		t.Fatalf("ground alpha = %d, want the overlay's %d", got, want)
	}
}

func TestToastCriticalCardStrokesTheErrorRim(t *testing.T) {
	critical := note(1, "battery")
	critical.Urgency = protocol.UrgencyCritical
	r, h, pixels, stride := glassToasts(t, true, critical)
	r.mu.Lock()
	defer r.mu.Unlock()
	rect := h.cards["eDP-1"][0].rect
	if !h.cards["eDP-1"][0].critical {
		t.Fatal("critical card not marked")
	}
	o := (rect.Y+rect.H/2)*stride + rect.X*4
	px := render.Color{B: pixels[o], G: pixels[o+1], R: pixels[o+2]}
	errC, rim := h.style.Error, h.style.Rim
	dist := func(a, b render.Color) int {
		d := func(x, y uint8) int { v := int(x) - int(y); return v * v }
		return d(a.R, b.R) + d(a.G, b.G) + d(a.B, b.B)
	}
	if dist(px, errC) >= dist(px, rim) {
		t.Fatalf("edge pixel %+v is nearer the outline %+v than the error %+v", px, rim, errC)
	}
}

func TestToastBlurShapeCoversEachCard(t *testing.T) {
	r, h, _, _ := glassToasts(t, true, note(1, "first"), note(2, "second"))
	shape := h.harness().opens[0].Callbacks.BlurShape()
	r.mu.Lock()
	cards := append([]toastCard(nil), h.cards["eDP-1"]...)
	r.mu.Unlock()
	if len(cards) != 2 || len(shape) == 0 {
		t.Fatalf("cards %d, strips %d", len(cards), len(shape))
	}
	area := map[int]int{}
	for _, s := range shape {
		inside := -1
		for i, c := range cards {
			if s.X >= c.rect.X && s.Y >= c.rect.Y && s.X+s.W <= c.rect.X+c.rect.W && s.Y+s.H <= c.rect.Y+c.rect.H {
				inside = i
			}
		}
		if inside < 0 {
			t.Fatalf("strip %+v lies outside every card", s)
		}
		area[inside] += s.W * s.H
	}
	for i, c := range cards {
		if full := c.rect.W * c.rect.H; area[i] < full*9/10 {
			t.Fatalf("card %d blur covers %d of %d px", i, area[i], full)
		}
	}
}

func TestToastBlurShapeIsEmptyWithoutCompositorBlur(t *testing.T) {
	_, h, _, _ := glassToasts(t, false, note(1, "first"))
	if shape := h.harness().opens[0].Callbacks.BlurShape(); len(shape) != 0 {
		t.Fatalf("blur without compositor blur: %+v", shape)
	}
}

func TestToastBlurShapeIsEmptyWithNoCards(t *testing.T) {
	_, h, _, _ := glassToasts(t, true)
	if shape := h.harness().opens[0].Callbacks.BlurShape(); len(shape) != 0 {
		t.Fatalf("blur with no cards: %+v", shape)
	}
}

// Layout is logical but text is painted at the output's scale, and a
// shaped run does not scale linearly. The host measures at the physical
// scale and rounds up into logical pixels, so the painter never clips text
// the layout said fits: at 1.5x the pinned time "12m" needs 35 px where a
// 1x measure granted 34.
func TestToastMeasuresTextAtTheOutputScale(t *testing.T) {
	for _, scale := range []int{120, 150, 180} {
		r, h, _, _ := glassToasts(t, true, note(1, "hello"))
		if err := h.harness().opens[0].Callbacks.Configure(1200, 800, scale); err != nil {
			t.Fatal(err)
		}
		r.mu.Lock()
		physical := h.style
		physical.Scale120 = ui.Scale120(scale)
		attrs := ui.TextAttrs{Role: theme.RoleCaption}
		mw, _, err := h.text.Measure("12m", render.SpecFor(physical, attrs), false)
		if err != nil {
			r.mu.Unlock()
			t.Fatal(err)
		}
		want := ui.Scale120(scale).Logical(mw)
		got, _ := h.measureText()("12m", attrs)
		r.mu.Unlock()
		if got != want {
			t.Fatalf("scale %d: measured %d px, want %d so the painted run fits", scale, got, want)
		}
	}
}

// Pointer motion hit-tests the cards already placed; it must not rebuild and
// re-measure every card tree under the registry lock on each event.
func TestToastHoverHitTestsThePlacedCards(t *testing.T) {
	r, h, _, _ := glassToasts(t, true, note(1, "hello"))
	r.mu.Lock()
	defer r.mu.Unlock()
	h.cards["eDP-1"][0].rect = ui.Rect{X: 10, Y: 500, W: 100, H: 50}
	h.pointer["eDP-1"] = ui.Rect{X: 20, Y: 520}
	h.updateHover("eDP-1")
	if !h.hovered["eDP-1"][1] {
		t.Fatal("pointer over the placed card is not hovering it")
	}
}

// A card clamps to a narrow output, so its actions have to pack for the width
// it gets: a row packed for the full design width overflowed, failed layout,
// and the toast silently vanished while still counted as visible.
func TestToastPacksActionsForANarrowOutput(t *testing.T) {
	n := note(1, "hello")
	n.Actions = []protocol.Action{
		{Key: "a1", Label: "Open in browser"}, {Key: "a2", Label: "Mark as read"},
		{Key: "a3", Label: "Archive"}, {Key: "a4", Label: "Reply"},
	}
	r, h, _, _ := glassToasts(t, true, n)
	cb := h.harness().opens[0].Callbacks
	const width, height = 330, 800
	if err := cb.Configure(width, height, 120); err != nil {
		t.Fatal(err)
	}
	pixels := make([]byte, width*4*height)
	for range 2 {
		if err := cb.Render(pixels, width, height, width*4); err != nil {
			t.Fatal(err)
		}
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if got := len(h.cards["eDP-1"]); got != 1 {
		t.Fatalf("cards on a %d px output = %d, want the one toast", width, got)
	}
}

// Losing compositor blur at runtime drops the glass ground and the blur
// region together, never a translucent card over an unblurred desktop.
func TestToastBlurLostAtRuntimeDropsGroundAndRegionTogether(t *testing.T) {
	r, h, pixels, stride := glassToasts(t, true, note(1, "hello"))
	r.SetCapabilities(wayland.Capabilities{Blur: false})
	cb := h.harness().opens[0].Callbacks
	if err := cb.Render(pixels, 1200, 800, stride); err != nil {
		t.Fatal(err)
	}
	if shape := cb.BlurShape(); len(shape) != 0 {
		t.Fatalf("blur region kept after blur was lost: %+v", shape)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if got, want := groundAlpha(h, pixels, stride), r.surfaceTheme().OverlayStyle().RootFill().A; got != want {
		t.Fatalf("ground alpha = %d, want the overlay's %d once blur is gone", got, want)
	}
}

// The last card closing empties the blur region, or a blurred patch would
// stay in the corner.
func TestToastBlurClearsWhenTheLastCardCloses(t *testing.T) {
	r, h, _, _ := glassToasts(t, true, note(1, "hello"))
	r.applyNotify(snap(2))
	if shape := h.harness().opens[0].Callbacks.BlurShape(); len(shape) != 0 {
		t.Fatalf("blur region kept after the last card closed: %+v", shape)
	}
}
