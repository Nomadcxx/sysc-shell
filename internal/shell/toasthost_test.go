package shell

import (
	"testing"

	"github.com/Nomadcxx/sysc-notify/protocol"
	"github.com/Nomadcxx/sysc-shell/internal/config"
	"github.com/Nomadcxx/sysc-shell/internal/notifyclient"
	"github.com/Nomadcxx/sysc-shell/internal/platform/wayland"
	"github.com/Nomadcxx/sysc-shell/internal/platform/wayland/layershell"
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
	if geometry := h.geometryFor("eDP-1"); geometry.OutputW != 3440 || geometry.OutputH != 1440 {
		t.Fatalf("geometry = %+v, want the configured size", geometry)
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
