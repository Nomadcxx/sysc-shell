package shell

import (
	"errors"
	"image/color"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/Nomadcxx/sysc-notify/protocol"
	"github.com/Nomadcxx/sysc-shell/internal/config"
	"github.com/Nomadcxx/sysc-shell/internal/icons"
	"github.com/Nomadcxx/sysc-shell/internal/notifyclient"
	"github.com/Nomadcxx/sysc-shell/internal/platform/niri"
	"github.com/Nomadcxx/sysc-shell/internal/platform/wayland"
	"github.com/Nomadcxx/sysc-shell/internal/platform/wayland/layershell"
	"github.com/Nomadcxx/sysc-shell/internal/render"
	"github.com/Nomadcxx/sysc-shell/internal/theme"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

func TestToastCardReusesRasterForNotificationRevision(t *testing.T) {
	n := protocol.Notification{
		ID: 77, AppName: "Mail", Summary: "New message", Timestamp: time.Now(),
		Image: notificationTestImage(t, 2, 2, color.RGBA{R: 0x11, G: 0x22, B: 0x33, A: 0xff}),
	}
	r := NewRegistry(config.Default())
	t.Cleanup(r.Close)
	h := newToastHost(r, &hostHarness{})

	r.mu.Lock()
	first, second := h.cardOf(n), h.cardOf(n)
	r.mu.Unlock()
	firstLead := first.Children[0].Children[0]
	secondLead := second.Children[0].Children[0]
	if firstLead.Image == nil || secondLead.Image == nil {
		t.Fatal("notification image did not reach the toast cards")
	}
	if firstLead.Image != secondLead.Image {
		t.Fatal("the same notification revision decoded a new raster for each card")
	}
}

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

func TestToastHostShrinksStripToNarrowOutput(t *testing.T) {
	r := NewRegistry(config.Default())
	hh := &hostHarness{}
	h := newToastHost(r, hh)
	r.outputsForTest([]string{"eDP-1"})
	h.syncOutputs(map[string]uint32{"eDP-1": 5})
	spec := hh.opens[0]
	spec.Callbacks.OutputSize(1920, 800)
	if err := spec.Callbacks.Configure(int(spec.Width), 800, 120); err != nil {
		t.Fatal(err)
	}
	r.applyNotify(snap(1, note(1, "a")))
	h.recompute()
	h.anim.reduced = true
	spec.Callbacks.OutputSize(300, 800)
	h.recompute()

	resizeIndex := -1
	for i, update := range hh.updates {
		if update.Width != nil && *update.Width == 300 {
			resizeIndex = i
		}
	}
	if resizeIndex < 0 {
		t.Fatal("narrow output kept the oversized toast strip")
	}
	if resize := hh.updates[resizeIndex]; resize.SetInputRegion {
		t.Fatalf("resize update changed the input region before configure: %+v", resize)
	}
	if resizeIndex == 0 {
		t.Fatal("width request had no accepted-width input region before it")
	}
	previous := hh.updates[resizeIndex-1]
	if !previous.SetInputRegion || len(previous.InputRects) != 1 || previous.InputRects[0] != h.cards["eDP-1"][0].rect {
		t.Fatalf("input update before resize = %+v, want the card at its accepted-width coordinates %+v", previous, h.cards["eDP-1"][0].rect)
	}
	oldWidth := int(spec.Width)
	if err := spec.Callbacks.Render(make([]byte, oldWidth*4*800), oldWidth, 800, oldWidth*4); err != nil {
		t.Fatal(err)
	}
	last := hh.updates[len(hh.updates)-1]
	if len(last.InputRects) != 1 || last.InputRects[0] != h.cards["eDP-1"][0].rect || last.InputRects[0].X+last.InputRects[0].W > oldWidth {
		t.Fatalf("pending resize input region = %+v, want the drawn card inside the accepted %dpx strip", last.InputRects, oldWidth)
	}

	if err := spec.Callbacks.Configure(300, 800, 120); err != nil {
		t.Fatal(err)
	}
	if got := h.cards["eDP-1"][0].rect; got.X != toastMargin || got.W != 300-2*toastMargin {
		t.Fatalf("narrow surface card = %+v, want x=%d width=%d", got, toastMargin, 300-2*toastMargin)
	}
	if got := lastToastInput(h); len(got) != 1 || got[0] != h.cards["eDP-1"][0].rect {
		t.Fatalf("configured input region = %+v, want the resized card %+v", got, h.cards["eDP-1"][0].rect)
	}
}

func TestToastConfigureMovesAnInFlightPressWithTheCard(t *testing.T) {
	r, h, sender := unmeasuredToast(t)
	hh := h.harness()
	spec := hh.opens[0]
	spec.Callbacks.OutputSize(300, 800)
	if err := spec.Callbacks.Configure(int(spec.Width), 800, 120); err != nil {
		t.Fatal(err)
	}
	n := note(1, "a")
	n.Actions = []protocol.Action{{Key: "open", Label: "Open"}}
	h.anim.reduced = true
	r.applyNotify(snap(1, n))
	h.recompute()

	r.mu.Lock()
	card := h.cards["eDP-1"][0]
	var action *ui.Node
	var findAction func(*ui.Node)
	findAction = func(node *ui.Node) {
		if node.Action == "notify:1:action:open" {
			action = node
			return
		}
		for _, child := range node.Children {
			findAction(child)
		}
	}
	findAction(card.root)
	oldWidth := h.surfaceWidth["eDP-1"]
	r.mu.Unlock()
	if action == nil {
		t.Fatal("notification action was not laid out")
	}

	x := float64(card.rect.X + action.Bounds.X + action.Bounds.W/2)
	y := float64(card.rect.Y + action.Bounds.Y + action.Bounds.H/2)
	if !spec.Callbacks.Handle(wayland.Event{Kind: wayland.EventPointerPress, Button: buttonLeft, X: x, Y: y}) {
		t.Fatal("press on the notification action was not handled")
	}
	if err := spec.Callbacks.Configure(300, 800, 120); err != nil {
		t.Fatal(err)
	}
	newX := x + float64(300-oldWidth)
	if !spec.Callbacks.Handle(wayland.Event{Kind: wayland.EventPointerRelease, Button: buttonLeft, X: newX, Y: y}) {
		t.Fatal("release after the surface resize was not handled")
	}
	if got := sender.ofKind(protocol.CommandAction); len(got) != 1 || got[0].ActionKey != "open" {
		t.Fatalf("action commands = %+v, want open after resize", got)
	}
}

func TestToastOutputWideningWaitsForAUsableStripWidth(t *testing.T) {
	r, h, _ := unmeasuredToast(t)
	spec := h.harness().opens[0]
	spec.Callbacks.OutputSize(300, 800)
	if err := spec.Callbacks.Configure(300, 800, 120); err != nil {
		t.Fatal(err)
	}
	h.anim.reduced = true
	r.applyNotify(snap(1, note(1, "a")))
	h.recompute()
	if got := h.cards["eDP-1"][0].rect; got.X != toastMargin || got.W != 300-2*toastMargin {
		t.Fatalf("narrow card before output change = %+v", got)
	}

	spec.Callbacks.OutputSize(1920, 800)
	if got := h.geometry["eDP-1"].OutputW; got != 300 {
		t.Fatalf("geometry width while resize is pending = %d, want accepted 300px geometry", got)
	}
	if got := h.resizePending["eDP-1"]; got != toastCardWidth+2*toastMargin {
		t.Fatalf("pending strip width = %d, want %d", got, toastCardWidth+2*toastMargin)
	}

	if err := spec.Callbacks.Configure(toastCardWidth+2*toastMargin, 800, 120); err != nil {
		t.Fatal(err)
	}
	if got := h.geometry["eDP-1"].OutputW; got != 1920 {
		t.Fatalf("accepted output width = %d, want 1920", got)
	}
	if got := h.cards["eDP-1"][0].rect; got.X != toastMargin || got.W != toastCardWidth {
		t.Fatalf("wide card after resize = %+v, want x=%d width=%d", got, toastMargin, toastCardWidth)
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

// The surface is a card-width strip anchored to the right edge. OutputSize
// keeps layout on the complete output while rendering stays surface-local.
func TestToastSurfaceIsRightAnchoredStripAndPaintable(t *testing.T) {
	r := NewRegistry(config.Default())
	t.Cleanup(r.Close)
	h := newToastHost(r, &hostHarness{})
	r.outputsForTest([]string{"eDP-1"})
	h.syncOutputs(map[string]uint32{"eDP-1": 5})

	spec := h.harness().opens[0]
	strip := uint32(layershell.ZwlrLayerSurfaceV1AnchorTop |
		layershell.ZwlrLayerSurfaceV1AnchorBottom |
		layershell.ZwlrLayerSurfaceV1AnchorRight)
	if spec.Anchor != strip {
		t.Fatalf("anchor = %d, want top/bottom/right strip", spec.Anchor)
	}
	if spec.Width != toastCardWidth+2*toastMargin {
		t.Fatalf("strip width = %d, want %d", spec.Width, toastCardWidth+2*toastMargin)
	}
	if spec.Callbacks.OutputSize == nil || spec.Callbacks.Configure == nil || spec.Callbacks.Render == nil || spec.Callbacks.Handle == nil {
		t.Fatal("the toast surface is missing a callback")
	}
	configureToastOutput(t, h, 1920, 1080, 120)
	r.applyNotify(snap(1, note(1, "a")))
	h.recompute()
	if got := h.cards["eDP-1"][0].rect; got.X != toastMargin || got.W != toastCardWidth {
		t.Fatalf("surface-local card = %+v, want x=%d width=%d", got, toastMargin, toastCardWidth)
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

	configureToastOutput(t, h, 3440, 1440, 120)
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
	if right := cards[0].rect.X + cards[0].rect.W + geometry.OutputW - h.surfaceWidth["eDP-1"]; right != 3440-toastMargin {
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

	const outputWidth, height = 1200, 800
	spec := h.harness().opens[0]
	callbacks := spec.Callbacks
	callbacks.OutputSize(outputWidth, height)
	if err := callbacks.Configure(outputWidth, height, 120); err != nil {
		t.Fatal(err)
	}
	fullStride := outputWidth * 4
	fullPixels := make([]byte, fullStride*height)
	for i := range fullPixels {
		fullPixels[i] = 0xff // prove the paint clears rather than inheriting
	}
	if err := callbacks.Render(fullPixels, outputWidth, height, fullStride); err != nil {
		t.Fatal(err)
	}

	r.mu.Lock()
	fullCards := append([]toastCard(nil), h.cards["eDP-1"]...)
	r.mu.Unlock()
	if len(fullCards) != 2 {
		t.Fatalf("full-surface cards = %d, want two", len(fullCards))
	}
	if err := callbacks.Configure(int(spec.Width), height, 120); err != nil {
		t.Fatal(err)
	}
	width := int(spec.Width)
	stride := width * 4
	pixels := make([]byte, stride*height)
	for i := range pixels {
		pixels[i] = 0xff
	}
	if err := callbacks.Render(pixels, width, height, stride); err != nil {
		t.Fatal(err)
	}
	r.mu.Lock()
	cards := append([]toastCard(nil), h.cards["eDP-1"]...)
	r.mu.Unlock()
	if len(cards) != 2 {
		t.Fatalf("strip cards = %d, want two", len(cards))
	}
	if got, want := cards[0].rect.X+outputWidth-width, fullCards[0].rect.X; got != want {
		t.Fatalf("strip card output x = %d, want full-surface x %d", got, want)
	}
	for y := 0; y < cards[0].rect.H; y++ {
		fullStart := (fullCards[0].rect.Y+y)*fullStride + fullCards[0].rect.X*4
		stripStart := (cards[0].rect.Y+y)*stride + cards[0].rect.X*4
		if !slices.Equal(fullPixels[fullStart:fullStart+cards[0].rect.W*4], pixels[stripStart:stripStart+cards[0].rect.W*4]) {
			t.Fatalf("strip card pixels differ from the full-surface frame on row %d", y)
		}
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
	configureToastOutput(t, h, 1200, 800, 120)

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
	configureToastOutput(t, h, 1920, 1080, 120)
	return r, h, sender
}

func configureToastOutput(t *testing.T, h *toastHost, outputWidth, outputHeight, scale120 int) {
	t.Helper()
	spec := h.harness().opens[0]
	if spec.Callbacks.OutputSize != nil {
		spec.Callbacks.OutputSize(outputWidth, outputHeight)
	}
	h.r.mu.Lock()
	width := toastSurfaceWidth(h.geometry["eDP-1"])
	h.r.mu.Unlock()
	if err := spec.Callbacks.Configure(width, outputHeight, scale120); err != nil {
		t.Fatal(err)
	}
}

func TestToastClickDismissesACardWithoutADefaultAction(t *testing.T) {
	r, h, sender := wiredToast(t)
	r.applyNotify(snap(1, note(1, "headphones")))
	callbacks := h.harness().opens[0].Callbacks
	configureToastOutput(t, h, 1200, 800, 120)
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

func TestCriticalToastBypassesDNDButOrdinaryToastDoesNot(t *testing.T) {
	r, h, sender := wiredToast(t)
	ordinary := note(1, "ordinary")
	critical := note(2, "critical")
	critical.Urgency = protocol.UrgencyCritical
	r.applyNotify(snap(1, ordinary, critical))

	r.setDNDForTest(true)
	h.recompute()

	if got := h.visible["eDP-1"]; !slices.Equal(got, []uint32{critical.ID}) {
		t.Fatalf("visible during DND = %v, want only critical notification %d", got, critical.ID)
	}
	renewals := sender.ofKind(protocol.CommandPresentationRenew)
	if len(renewals) == 0 {
		t.Fatal("toast host did not publish presentation during DND")
	}
	states := make(map[uint32]protocol.PresentationState)
	for _, p := range renewals[len(renewals)-1].Presentations {
		states[p.ID] = p.State
	}
	if states[ordinary.ID] != protocol.PresentationSuppressed || states[critical.ID] != protocol.PresentationVisible {
		t.Fatalf("presentation states during DND = %v, want ordinary suppressed and critical visible", states)
	}
}

func TestDNDDoesNotReplayNotificationsAddedWhileSuppressed(t *testing.T) {
	for _, tc := range []struct {
		name       string
		dnd        bool
		centerOpen bool
	}{
		{name: "DND", dnd: true},
		{name: "centre", centerOpen: true},
		{name: "DND and centre", dnd: true, centerOpen: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r, h, _ := wiredToast(t)
			prior := note(1, "before suppression")
			r.applyNotify(snap(1, prior))
			if got := h.visible["eDP-1"]; !slices.Equal(got, []uint32{prior.ID}) {
				t.Fatalf("visible before suppression = %v, want prior notification %d", got, prior.ID)
			}

			r.setDNDForTest(tc.dnd)
			r.setCenterOpenForTest(tc.centerOpen)
			h.recompute()
			arrivedWhileSuppressed := note(2, "during suppression")
			arrivedWhileSuppressed.Timestamp = time.Now()
			r.applyNotify(delta(1, 2, protocol.Delta{
				Kind: protocol.DeltaAdded, Notification: &arrivedWhileSuppressed,
				Lifetime: &protocol.Lifetime{ID: arrivedWhileSuppressed.ID, DurationMS: 5000, RemainingMS: 5000, Running: true},
			}))
			if got := h.visible["eDP-1"]; len(got) != 0 {
				t.Fatalf("visible while suppressed = %v, want none", got)
			}

			r.setDNDForTest(false)
			r.setCenterOpenForTest(false)
			h.recompute()
			if got := h.visible["eDP-1"]; !slices.Equal(got, []uint32{prior.ID}) {
				t.Fatalf("visible after suppression clears = %v, want only prior notification %d", got, prior.ID)
			}
		})
	}
}

func TestSnapshotDoesNotReplayNotificationsAddedDuringSuppression(t *testing.T) {
	for _, tc := range []struct {
		name       string
		dnd        bool
		centerOpen bool
	}{
		{name: "DND", dnd: true},
		{name: "centre", centerOpen: true},
		{name: "DND and centre", dnd: true, centerOpen: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r, h, _ := wiredToast(t)
			prior := note(1, "before suppression")
			prior.Timestamp = time.Now().Add(-time.Minute)
			r.applyNotify(snap(1, prior))
			drainInvalidations(r)
			if got := h.visible["eDP-1"]; !slices.Equal(got, []uint32{prior.ID}) {
				t.Fatalf("visible before suppression = %v, want prior notification %d", got, prior.ID)
			}

			r.setDNDForTest(tc.dnd)
			r.setCenterOpenForTest(tc.centerOpen)
			h.recompute()
			drainInvalidations(r)
			if got := h.visible["eDP-1"]; len(got) != 0 {
				t.Fatalf("visible while suppressed = %v, want none", got)
			}

			arrivedDuringSuppression := note(2, "during suppression")
			arrivedDuringSuppression.Timestamp = time.Now().Add(time.Second)
			r.applyNotify(notifyclient.Message{Generation: 1, Kind: notifyclient.KindDisconnected})
			drainInvalidations(r)
			r.applyNotify(snap(2, prior, arrivedDuringSuppression))
			drainInvalidations(r)

			r.setDNDForTest(false)
			h.recompute()
			drainInvalidations(r)
			if tc.centerOpen {
				if got := h.visible["eDP-1"]; len(got) != 0 {
					t.Fatalf("visible while centre remains open = %v, want none", got)
				}
				r.setCenterOpenForTest(false)
				drainInvalidations(r)
			}
			if got := h.visible["eDP-1"]; !slices.Equal(got, []uint32{prior.ID}) {
				t.Fatalf("visible after suppression clears = %v, want only prior notification %d", got, prior.ID)
			}
		})
	}
}

func TestReconnectSnapshotDoesNotReplayNotificationsReceivedDuringCompletedSuppression(t *testing.T) {
	for _, tc := range []struct {
		name       string
		dnd        bool
		centerOpen bool
	}{
		{name: "DND", dnd: true},
		{name: "centre", centerOpen: true},
		{name: "DND and centre", dnd: true, centerOpen: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r, h, _ := wiredToast(t)
			start := time.Now()
			end := start.Add(time.Second)
			r.UpdateClock(start)
			if tc.dnd {
				r.notify.setDNDAt(true, start)
			}
			if tc.centerOpen {
				r.setCenterOpenForTest(true)
			}

			r.applyNotify(notifyclient.Message{Generation: 1, Kind: notifyclient.KindDisconnected})
			drainInvalidations(r)
			r.UpdateClock(end)
			if tc.dnd {
				r.notify.setDNDAt(false, end)
			}
			if tc.centerOpen {
				r.setCenterOpenForTest(false)
			}

			during := note(1, "received during suppression")
			during.Timestamp = start.Add(time.Millisecond)
			after := note(2, "received after suppression")
			after.Timestamp = end.Add(time.Millisecond)
			r.applyNotify(snap(2, during, after))
			drainInvalidations(r)

			if got := h.visible["eDP-1"]; !slices.Equal(got, []uint32{after.ID}) {
				t.Fatalf("visible after reconnect = %v, want only notification %d received after suppression", got, after.ID)
			}
		})
	}
}

func TestReconnectKeepsKnownSuppressedNotificationIDs(t *testing.T) {
	r, h, _ := wiredToast(t)
	prior := note(1, "before suppression")
	prior.Timestamp = time.Now().Add(-time.Minute)
	r.applyNotify(snap(1, prior))

	r.setDNDForTest(true)
	during := note(2, "received during DND")
	during.Timestamp = time.Now()
	r.applyNotify(delta(1, 2, protocol.Delta{
		Kind: protocol.DeltaAdded, Notification: &during,
		Lifetime: &protocol.Lifetime{ID: during.ID, DurationMS: 5000, RemainingMS: 5000, Running: true},
	}))
	r.setDNDForTest(false)
	r.applyNotify(notifyclient.Message{Generation: 1, Kind: notifyclient.KindDisconnected})
	drainInvalidations(r)

	after := note(3, "received after DND")
	after.Timestamp = time.Now().Add(time.Second)
	r.applyNotify(snap(2, prior, during, after))
	drainInvalidations(r)
	if got, want := h.visible["eDP-1"], []uint32{after.ID, prior.ID}; !slices.Equal(got, want) {
		t.Fatalf("visible after reconnect = %v, want %v", got, want)
	}
}

func TestResyncSnapshotDoesNotReplayNotificationsReceivedDuringCompletedSuppression(t *testing.T) {
	r, h, _ := wiredToast(t)
	prior := note(1, "before suppression")
	prior.Timestamp = time.Now().Add(-time.Minute)
	r.applyNotify(snap(1, prior))

	start := time.Now()
	r.notify.setDNDAt(true, start)
	during := note(2, "received during DND")
	during.Timestamp = start.Add(time.Millisecond)
	r.notify.setDNDAt(false, start.Add(time.Second))
	after := note(3, "received after DND")
	after.Timestamp = start.Add(time.Second + time.Millisecond)

	// A same-generation snapshot repairs the active set after a dropped delta.
	resync := snap(2, prior, during, after)
	resync.Generation = 1
	r.applyNotify(resync)
	drainInvalidations(r)
	if got, want := h.visible["eDP-1"], []uint32{after.ID, prior.ID}; !slices.Equal(got, want) {
		t.Fatalf("visible after resync = %v, want %v", got, want)
	}
}

func TestSuppressionStartsUseCurrentTimeInsteadOfCachedRegistryClock(t *testing.T) {
	r := NewRegistry(config.Default())
	t.Cleanup(r.Close)
	stale := time.Now().Add(-time.Minute)
	r.mu.Lock()
	r.now = stale
	r.mu.Unlock()

	r.setDND(true)
	r.notify.mu.Lock()
	dndSince := r.notify.dndSince
	r.notify.mu.Unlock()
	if !dndSince.After(stale) {
		t.Fatalf("DND start = %v, want current time after cached clock %v", dndSince, stale)
	}
	r.setDND(false)

	r.mu.Lock()
	r.now = stale
	r.mu.Unlock()
	r.setCenterOpen(true)
	r.notify.mu.Lock()
	centerOpenSince := r.notify.centerOpenSince
	r.notify.mu.Unlock()
	if !centerOpenSince.After(stale) {
		t.Fatalf("centre start = %v, want current time after cached clock %v", centerOpenSince, stale)
	}
}

func TestDelayedNotificationDeltasUseTheirServiceTimestamp(t *testing.T) {
	for _, suppression := range []struct {
		name  string
		start func(*Registry, time.Time)
		end   func(*Registry, time.Time)
	}{
		{
			name:  "DND",
			start: func(r *Registry, at time.Time) { r.notify.setDNDAt(true, at) },
			end:   func(r *Registry, at time.Time) { r.notify.setDNDAt(false, at) },
		},
		{
			name:  "centre",
			start: func(r *Registry, at time.Time) { r.setCenterOpenAt(true, at) },
			end:   func(r *Registry, at time.Time) { r.setCenterOpenAt(false, at) },
		},
	} {
		for _, tc := range []struct {
			name           string
			deliveredAfter bool
			wantVisible    bool
		}{
			{name: "created during suppression and delivered after it ends", deliveredAfter: true},
			{name: "created before suppression and delivered during it", wantVisible: true},
		} {
			t.Run(suppression.name+"/"+tc.name, func(t *testing.T) {
				r, h, _ := wiredToast(t)
				r.applyNotify(snap(1))

				start := time.Now().Add(-2 * time.Second)
				end := start.Add(time.Second)
				suppression.start(r, start)
				during := note(1, "delayed arrival")
				if tc.deliveredAfter {
					during.Timestamp = start.Add(time.Millisecond)
					suppression.end(r, end)
				} else {
					during.Timestamp = start.Add(-time.Millisecond)
				}
				r.applyNotify(delta(1, 2, protocol.Delta{
					Kind: protocol.DeltaAdded, Notification: &during,
					Lifetime: &protocol.Lifetime{ID: during.ID, DurationMS: 5000, RemainingMS: 5000, Running: true},
				}))
				if !tc.deliveredAfter {
					if got := h.visible["eDP-1"]; len(got) != 0 {
						t.Fatalf("visible during suppression = %v, want none", got)
					}
					suppression.end(r, end)
					h.recompute()
				}

				got := h.visible["eDP-1"]
				if tc.wantVisible && !slices.Equal(got, []uint32{during.ID}) {
					t.Fatalf("visible after suppression = %v, want notification %d", got, during.ID)
				}
				if !tc.wantVisible && len(got) != 0 {
					t.Fatalf("visible after suppression = %v, want none", got)
				}
			})
		}
	}
}

func TestArrivalAfterTimedDNDEndUsesTheServiceTimestamp(t *testing.T) {
	r, h, _ := wiredToast(t)
	r.applyNotify(snap(1))

	start := time.Now().Add(-2 * time.Second)
	end := start.Add(time.Second)
	r.mu.Lock()
	r.now = start.Add(time.Millisecond)
	r.mu.Unlock()
	r.notify.setDNDPreset(start, time.Second)

	after := note(1, "after DND")
	after.Timestamp = end.Add(time.Millisecond)
	r.applyNotify(delta(1, 2, protocol.Delta{
		Kind: protocol.DeltaAdded, Notification: &after,
		Lifetime: &protocol.Lifetime{ID: after.ID, DurationMS: 5000, RemainingMS: 5000, Running: true},
	}))
	r.UpdateClock(time.Now())
	if got := h.visible["eDP-1"]; !slices.Equal(got, []uint32{after.ID}) {
		t.Fatalf("visible after the timed DND expired = %v, want notification %d", got, after.ID)
	}
}

func TestFullscreenFocusedWindowHoldsToastsUntilFocusLeaves(t *testing.T) {
	r, h, sender := wiredToast(t)
	t.Cleanup(r.Close)
	ordinary := note(1, "ordinary")
	critical := note(2, "critical")
	critical.Urgency = protocol.UrgencyCritical
	r.applyNotify(snap(1, ordinary, critical))

	fullscreen := niri.Snapshot{
		FocusedOutput: "eDP-1",
		Windows: []niri.Window{{
			ID: 10, Focused: true, HasLayout: true, TileW: 1920, TileH: 1080,
		}},
	}
	r.UpdateNiri(fullscreen)
	if got := h.visible["eDP-1"]; len(got) != 0 {
		t.Fatalf("visible with fullscreen focused = %v, want no toasts", got)
	}
	renewals := sender.ofKind(protocol.CommandPresentationRenew)
	states := make(map[uint32]protocol.PresentationState)
	for _, p := range renewals[len(renewals)-1].Presentations {
		states[p.ID] = p.State
	}
	if states[ordinary.ID] != protocol.PresentationSuppressed || states[critical.ID] != protocol.PresentationSuppressed {
		t.Fatalf("presentation with fullscreen focused = %v, want both suppressed", states)
	}

	r.UpdateNiri(niri.Snapshot{
		FocusedOutput: "eDP-1",
		Windows: []niri.Window{{
			ID: 10, Focused: true, HasLayout: true, TileW: 960, TileH: 540,
		}},
	})
	if got := h.visible["eDP-1"]; !slices.Equal(got, []uint32{critical.ID, ordinary.ID}) {
		t.Fatalf("visible after focus leaves fullscreen = %v, want both notifications", got)
	}

	r.UpdateNiri(niri.Snapshot{
		FocusedOutput: "eDP-1",
		Windows: []niri.Window{{
			ID: 10, Focused: false, HasLayout: true, TileW: 1920, TileH: 1080,
		}},
	})
	if got := h.visible["eDP-1"]; !slices.Equal(got, []uint32{critical.ID, ordinary.ID}) {
		t.Fatalf("visible without a focused window = %v, want both notifications", got)
	}
}

func TestToastLifetimeRepliesDoNotRenewOrRebuild(t *testing.T) {
	r, h, sender := wiredToast(t)
	keepInvalidationsDrained(t, r)
	r.applyNotify(snap(1, note(1, "a")))
	renewals := len(sender.ofKind(protocol.CommandPresentationRenew))
	updates := len(h.harness().updates)

	for i := 0; i < 10; i++ {
		r.applyNotify(notifyclient.Message{Generation: 1, Kind: notifyclient.KindReply,
			RequestID: uint64(i + 1), Reply: protocol.Reply{OK: true,
				Lifetimes: []protocol.Lifetime{{ID: 1, DurationMS: 5000, RemainingMS: 4000, Running: true}}}})
	}

	if got := len(sender.ofKind(protocol.CommandPresentationRenew)); got != renewals {
		t.Fatalf("renewals after unchanged lifetime replies = %d, want %d", got, renewals)
	}
	if got := len(h.harness().updates); got != updates {
		t.Fatalf("toast surface updates after unchanged lifetime replies = %d, want %d", got, updates)
	}
	if got := r.notifyLifetime(1); got == nil || got.RemainingMS != 4000 {
		t.Fatalf("cached lifetime = %+v, want the authoritative reply", got)
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
	prior := note(1, "a")
	prior.Timestamp = now.Add(-time.Second)
	r.applyNotify(snap(1, prior))

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

	// Re-enabling after the preset ended must reach the hook, so the OSD
	// describes the new state instead of being skipped by a stale flag.
	r.setDND(true)
	mu.Lock()
	got = append([]bool(nil), calls...)
	mu.Unlock()
	if len(got) != 3 || got[2] != true {
		t.Fatalf("DND hook calls after re-enabling = %v, want [true false true]", got)
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
	const outputWidth, height = 1200, 800
	configureToastOutput(t, h, outputWidth, height, 120)
	width := int(h.harness().opens[0].Width)
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
	configureToastOutput(t, h, 1536, 864, 150)
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
	r.retheThemeOpenSurfacesLocked(r.cfg, r.tokens)
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
	const outputWidth, height = 1200, 800
	cb := h.harness().opens[0].Callbacks
	configureToastOutput(t, h, outputWidth, height, 120)
	width := int(h.harness().opens[0].Width)
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
		configureToastOutput(t, h, 1200, 800, scale)
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
	const width, height = 330, 800
	configureToastOutput(t, h, width, height, 120)
	cb := h.harness().opens[0].Callbacks
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
	if err := cb.Render(pixels, stride/4, 800, stride); err != nil {
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

// Cards follow the bar's edge. A right bar shifts the stack sideways by the
// bar thickness; a bottom bar anchors the stack above the bar. Top and left
// bars keep the top-right stack.
func TestToastHostPlacesCardsClearOfEveryBarEdge(t *testing.T) {
	const outW, outH = 1920, 1080
	for _, edge := range []string{"top", "right", "bottom", "left"} {
		t.Run(edge, func(t *testing.T) {
			r, h, _ := wiredToast(t)
			theme := DefaultTheme()
			theme.BarEdge = edge
			bar := &Bar{theme: theme}
			zone := exclusiveBarZone(bar)
			if zone <= toastMargin {
				t.Fatalf("bar zone %d does not extend past the toast margin", zone)
			}
			r.mu.Lock()
			r.bars[h.outputs["eDP-1"]] = bar
			h.recompute()
			pendingWidth := h.resizePending["eDP-1"]
			r.mu.Unlock()
			if pendingWidth > 0 {
				if err := h.configure("eDP-1", pendingWidth, outH, 120); err != nil {
					t.Fatal(err)
				}
			}

			r.applyNotify(snap(1, note(1, "older"), note(2, "newer")))

			r.mu.Lock()
			defer r.mu.Unlock()
			geom := h.geometry["eDP-1"]
			if geom.BarEdge != edge {
				t.Fatalf("geometry edge = %q, want %q", geom.BarEdge, edge)
			}
			if geom.BarZone != zone {
				t.Fatalf("geometry zone = %d, want %d", geom.BarZone, zone)
			}
			wantCorner := toastTopRight
			if edge == "bottom" {
				wantCorner = toastBottomRight
			}
			if geom.Corner != wantCorner {
				t.Fatalf("corner = %d, want %d", geom.Corner, wantCorner)
			}
			rects := append([]ui.Rect(nil), h.cardRects("eDP-1", h.visible["eDP-1"])...)
			if len(rects) != 2 {
				t.Fatalf("cards = %d, want 2", len(rects))
			}
			anchor, next := rects[0], rects[1]
			switch edge {
			case "top", "left":
				if anchor.Y != zone+toastMargin {
					t.Fatalf("Y = %d, want %d", anchor.Y, zone+toastMargin)
				}
				if anchor.X+anchor.W != outW-toastMargin {
					t.Fatalf("right edge = %d, want %d", anchor.X+anchor.W, outW-toastMargin)
				}
				if next.Y <= anchor.Y {
					t.Fatalf("stack grew upward: %+v", rects)
				}
			case "right":
				if anchor.X+anchor.W != outW-toastMargin-zone {
					t.Fatalf("right edge = %d, want %d", anchor.X+anchor.W, outW-toastMargin-zone)
				}
				if anchor.Y != toastMargin {
					t.Fatalf("Y = %d, want %d", anchor.Y, toastMargin)
				}
				if next.Y <= anchor.Y {
					t.Fatalf("stack grew upward: %+v", rects)
				}
			case "bottom":
				if anchor.Y+anchor.H != outH-toastMargin-zone {
					t.Fatalf("bottom edge = %d, want %d", anchor.Y+anchor.H, outH-toastMargin-zone)
				}
				if anchor.Y <= zone+toastMargin {
					t.Fatalf("Y = %d, bar thickness still reserved at the top", anchor.Y)
				}
				if next.Y >= anchor.Y {
					t.Fatalf("bottom stack grew downward: %+v", rects)
				}
			}
			inputs := lastToastInput(h)
			if len(inputs) != len(rects) {
				t.Fatalf("input region = %+v, want the cards %+v", inputs, rects)
			}
			inputWidth := h.surfaceWidth["eDP-1"]
			for i, rect := range rects {
				surfaceRect := toastSurfaceRect(geom, inputWidth, rect)
				if inputs[i] != surfaceRect {
					t.Fatalf("input[%d] = %+v, want surface-local card %+v", i, inputs[i], surfaceRect)
				}
				if overlapsBarStrip(rect, edge, outW, outH, zone) {
					t.Fatalf("card %+v overlaps the %s bar", rect, edge)
				}
			}
		})
	}
}

func lastToastInput(h *toastHost) []ui.Rect {
	updates := h.harness().updates
	for i := len(updates) - 1; i >= 0; i-- {
		if updates[i].SetInputRegion {
			return updates[i].InputRects
		}
	}
	return nil
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
