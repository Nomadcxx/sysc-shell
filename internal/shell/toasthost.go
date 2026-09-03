package shell

import (
	"math"
	"sort"
	"sync"
	"time"

	"github.com/Nomadcxx/sysc-notify/protocol"
	"github.com/Nomadcxx/sysc-shell/internal/platform/wayland"
	"github.com/Nomadcxx/sysc-shell/internal/platform/wayland/layershell"
	"github.com/Nomadcxx/sysc-shell/internal/render"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

// toastHost owns one Overlay aux surface per configured output and projects
// the visible notification stack onto it. It never owns expiry: it lays out
// the records the service says are active and reports placement back through
// the aggregate presentation state.
type toastHost struct {
	r *Registry

	// request emits aux requests; tests capture them. The owner sends them on
	// Registry.aux in production.
	request func(wayland.AuxRequest)
	// harness is the test capture, when one is installed.
	harnessRef *hostHarness

	// outputs maps connector to wl_registry global for the outputs with a
	// toast surface open.
	outputs map[string]uint32
	// visible and queued are the last computed placement per output.
	visible map[string][]uint32
	queued  map[string][]uint32
	hovered map[string]map[uint32]bool

	// geometry is the size each output's surface was configured at. Until a
	// configure arrives an output has none, and the design default stands in.
	geometry map[string]toastGeometry
	scale120 map[string]int
	// cards is the arranged stack per output, rebuilt whenever the projection
	// or the geometry changes.
	cards map[string][]toastCard
	// scratch is the buffer one card is painted into before it is copied onto
	// the surface. One goroutine paints, so one buffer serves every card.
	scratch []byte
	// pointer is the last pointer position per output, in surface pixels.
	pointer map[string]ui.Rect
	// resolver turns press/release over a card into notify commands.
	resolver *notifyResolver
	// press holds the card a button went down on, so a swipe can release
	// outside it.
	press    toastCard
	pressing bool

	stopRenew chan struct{}
	renewOnce sync.Once

	text  *render.TextRenderer
	style render.Style
}

const toastNamespace = "sysc-shell-toast"

// hostHarness captures the aux requests a toast host emits in a test.
type hostHarness struct {
	opens   []*wayland.AuxSpec
	updates []*wayland.AuxUpdate
	closes  []string
}

func (h *hostHarness) request(r wayland.AuxRequest) {
	switch {
	case r.Open != nil:
		h.opens = append(h.opens, r.Open)
	case r.Update != nil:
		h.updates = append(h.updates, r.Update)
	default:
		h.closes = append(h.closes, r.ID)
	}
}

func newToastHost(r *Registry, harness *hostHarness) *toastHost {
	h := &toastHost{
		r:        r,
		outputs:  map[string]uint32{},
		visible:  map[string][]uint32{},
		queued:   map[string][]uint32{},
		hovered:  map[string]map[uint32]bool{},
		geometry: map[string]toastGeometry{},
		scale120: map[string]int{},
		cards:    map[string][]toastCard{},
		pointer:  map[string]ui.Rect{},
	}
	if harness != nil {
		h.request = harness.request
		h.harnessRef = harness
	} else {
		h.request = func(req wayland.AuxRequest) { r.sendAux(req) }
	}
	h.resolver = newNotifyResolver(h)
	return h
}

func (h *toastHost) harness() *hostHarness { return h.harnessRef }

func toastSurfaceID(connector string) string { return "toast:" + connector }

// syncOutputs opens a surface for each new output and closes surfaces whose
// output went away. Outputs are identified by wl_registry global, matching
// the registry's rule that a connector can change globals across a reconnect.
//
// Called with Registry.mu held, like every other writer of this host's state.
func (h *toastHost) syncOutputs(globals map[string]uint32) {
	for connector, global := range h.outputs {
		if _, ok := globals[connector]; !ok {
			h.request(wayland.AuxRequest{Output: global, ID: toastSurfaceID(connector)})
			delete(h.outputs, connector)
			delete(h.visible, connector)
			delete(h.queued, connector)
			delete(h.hovered, connector)
			delete(h.geometry, connector)
			delete(h.scale120, connector)
			delete(h.cards, connector)
			delete(h.pointer, connector)
		}
	}
	for connector, global := range globals {
		if _, ok := h.outputs[connector]; ok {
			continue
		}
		h.outputs[connector] = global
		h.hovered[connector] = map[uint32]bool{}
		h.request(wayland.AuxRequest{Output: global, Open: h.spec(connector)})
	}
	h.recompute()
}

// spec describes one output's toast surface. It is anchored to all four edges
// so the compositor reports the output's own logical size, which is what the
// stack lays out against; the input region is narrowed to the visible cards
// immediately afterwards, so the rest of the output still takes clicks.
func (h *toastHost) spec(connector string) *wayland.AuxSpec {
	return &wayland.AuxSpec{
		ID:        toastSurfaceID(connector),
		Namespace: toastNamespace,
		Layer:     layershell.ZwlrLayerShellV1LayerOverlay,
		Anchor: uint32(layershell.ZwlrLayerSurfaceV1AnchorTop |
			layershell.ZwlrLayerSurfaceV1AnchorBottom |
			layershell.ZwlrLayerSurfaceV1AnchorLeft |
			layershell.ZwlrLayerSurfaceV1AnchorRight),
		ExclusiveZone: -1,
		Keyboard:      keyboardNone,
		Callbacks: wayland.HostCallbacks{
			Configure: func(width, height, scale120 int) error {
				return h.configure(connector, width, height, scale120)
			},
			Render: func(pixels []byte, width, height, stride int) error {
				return h.render(connector, pixels, width, height, stride)
			},
			Handle: func(event wayland.Event) bool { return h.handle(connector, event) },
		},
	}
}

// configure records the output's real logical size and relays out the stack
// against it. Before it arrives the design default stands in, so a card is
// never placed off an output whose size is not known yet.
func (h *toastHost) configure(connector string, width, height, scale120 int) error {
	h.r.mu.Lock()
	defer h.r.mu.Unlock()
	return h.configureLocked(connector, width, height, scale120)
}

func (h *toastHost) configureLocked(connector string, width, height, scale120 int) error {
	if width > 0 && height > 0 {
		h.geometry[connector] = toastGeometry{OutputW: width, OutputH: height, Corner: toastTopRight}
	}
	h.scale120[connector] = scale120
	h.recompute()
	return nil
}

func (h *toastHost) render(connector string, pixels []byte, width, height, stride int) error {
	h.r.mu.Lock()
	defer h.r.mu.Unlock()
	if h.text == nil {
		fonts, err := render.NewSystemFontMap(h.r.cfg.Bar.FontFamily, render.DefaultFontCacheDir())
		if err != nil {
			return err
		}
		h.text = render.NewTextRendererWithFontMap(fonts)
		theme := h.r.surfaceTheme()
		scale, body := h.style.Scale120, h.style.Body
		h.style = theme.Style()
		h.style.Scale120, h.style.Body = scale, body
		h.rebuild(connector)
	}
	canvas, err := render.NewCanvas(pixels, width, height, stride)
	if err != nil {
		return err
	}
	style := h.style
	style.Scale120 = ui.Scale120(max(h.scale120[connector], int(ui.ScaleUnit)))

	// The surface covers the whole output and the cards are separate bodies on
	// it, which the painter cannot express in one pass: it fills exactly one
	// rounded body and clears everything outside it. So the surface is cleared
	// here and each card is painted into its own buffer and copied in, which
	// leaves the gaps and the rest of the output transparent.
	clear(pixels)
	for _, card := range h.cards[connector] {
		if err := h.paintCard(canvas, card, style); err != nil {
			return err
		}
	}
	return nil
}

// toastCard is one placed card: its tree, arranged at the origin, and where
// on the surface it belongs.
type toastCard struct {
	root *ui.Node
	rect ui.Rect
}

// paintCard renders one card into the scratch buffer and copies it onto the
// surface. The painter clears outside the card's rounded body, so the copy
// carries transparent corners rather than a square patch.
func (h *toastHost) paintCard(canvas *render.Canvas, card toastCard, style render.Style) error {
	box := style.Scale120.PhysicalRect(card.rect)
	if box.W <= 0 || box.H <= 0 {
		return nil
	}
	stride := box.W * 4
	if need := stride * box.H; len(h.scratch) < need {
		h.scratch = make([]byte, need)
	}
	cardCanvas, err := render.NewCanvas(h.scratch, box.W, box.H, stride)
	if err != nil {
		return err
	}
	cardStyle := style
	cardStyle.Body = ui.Rect{W: card.rect.W, H: card.rect.H}
	if err := render.Paint(cardCanvas, card.root, h.text, cardStyle); err != nil {
		return err
	}
	for y := range box.H {
		target := box.Y + y
		if target < 0 || target >= canvas.Height {
			continue
		}
		width := min(stride, canvas.Stride-box.X*4)
		to := target*canvas.Stride + box.X*4
		if width <= 0 || to < 0 || to+width > len(canvas.Pix) {
			continue
		}
		copy(canvas.Pix[to:to+width], h.scratch[y*stride:y*stride+width])
	}
	return nil
}

// handle tracks hover and routes press/release through the resolver. Hover is
// what holds a toast open; a click or swipe is what dismisses or invokes it.
func (h *toastHost) handle(connector string, event wayland.Event) bool {
	h.r.mu.Lock()
	defer h.r.mu.Unlock()
	x, y := int(math.Floor(event.X)), int(math.Floor(event.Y))
	switch event.Kind {
	case wayland.EventPointerEnter, wayland.EventPointerMotion:
		h.pointer[connector] = ui.Rect{X: x, Y: y}
		if h.updateHover(connector) {
			h.publishPresentation()
			return true
		}
		return false
	case wayland.EventPointerLeave:
		delete(h.pointer, connector)
		if h.updateHover(connector) {
			h.publishPresentation()
			return true
		}
		return false
	case wayland.EventPointerPress:
		h.pointer[connector] = ui.Rect{X: x, Y: y}
		card, ok := h.cardAt(connector, x, y)
		if !ok {
			return false
		}
		h.press = card
		h.pressing = true
		h.resolver.press(card.root, x-card.rect.X, y-card.rect.Y)
		return true
	case wayland.EventPointerRelease:
		if !h.pressing {
			return false
		}
		card := h.press
		h.pressing = false
		h.press = toastCard{}
		h.resolver.release(card.root, x-card.rect.X, y-card.rect.Y)
		return true
	default:
		return false
	}
}

func (h *toastHost) cardAt(connector string, x, y int) (toastCard, bool) {
	for _, card := range h.cards[connector] {
		if card.rect.Contains(x, y) {
			return card, true
		}
	}
	return toastCard{}, false
}

func (h *toastHost) invoke(id uint32, key string) {
	h.r.sendNotify(protocol.Command{Kind: protocol.CommandAction, ID: id, ActionKey: key})
}
func (h *toastHost) dismiss(id uint32) {
	h.r.sendNotify(protocol.Command{Kind: protocol.CommandDismiss, ID: id})
}
func (h *toastHost) reply(id uint32, text string) {
	h.r.sendNotify(protocol.Command{Kind: protocol.CommandReply, ID: id, Text: text})
}
func (h *toastHost) hover(uint32, bool) {}
func (h *toastHost) openLink(string)    {}

// updateHover recomputes the hovered set for one output and reports whether
// it changed. Only a change is worth a frame.
func (h *toastHost) updateHover(connector string) bool {
	at, inside := h.pointer[connector]
	hovered := map[uint32]bool{}
	if inside {
		ids := h.visible[connector]
		for i, rect := range h.cardRects(connector, ids) {
			if i < len(ids) && rect.Contains(at.X, at.Y) {
				hovered[ids[i]] = true
			}
		}
	}
	previous := h.hovered[connector]
	if len(previous) == len(hovered) {
		same := true
		for id := range hovered {
			if !previous[id] {
				same = false
				break
			}
		}
		if same {
			return false
		}
	}
	h.hovered[connector] = hovered
	return true
}

// rebuild arranges one output's visible cards. Each is laid out at its own
// origin, because each is painted into its own buffer before it is placed.
func (h *toastHost) rebuild(connector string) {
	ids := h.visible[connector]
	rects := h.cardRects(connector, ids)
	measure := h.measureText()
	cards := make([]toastCard, 0, len(ids))
	for i, id := range ids {
		if i >= len(rects) {
			break
		}
		root := h.cardFor(id)
		if root == nil {
			continue
		}
		if err := ui.LayoutColumn(root, ui.Rect{W: rects[i].W, H: rects[i].H}, measure); err != nil {
			continue
		}
		cards = append(cards, toastCard{root: root, rect: rects[i]})
	}
	h.cards[connector] = cards
}

// cardFor projects one active record. A record that has gone between the
// placement and the paint yields nothing rather than an empty card.
func (h *toastHost) cardFor(id uint32) *ui.Node {
	s := h.r.notify
	s.mu.Lock()
	notification, ok := s.active[id]
	lifetime := cloneLifetime(s.lifetimes, id)
	s.mu.Unlock()
	if !ok {
		return nil
	}
	return NotificationCard(notification, lifetime, h.r.lookupNotifyIcon(notification.AppIcon), h.r.linksAllowed())
}

// recompute relayouts every open output from the current projection and
// publishes each surface's input region. It is safe to call on any record,
// geometry, or output change.
func (h *toastHost) recompute() {
	s := h.r.notify
	s.mu.Lock()
	suppressed := s.dnd || s.centerOpen
	records := make([]uint32, 0, len(s.active))
	for id := range s.active {
		records = append(records, id)
	}
	s.mu.Unlock()

	// Newest first: the stack reads down from the freshest card.
	sort.Slice(records, func(i, j int) bool { return records[i] > records[j] })

	for _, connector := range h.outputOrder() {
		global, ok := h.outputs[connector]
		if !ok {
			continue
		}
		geom := h.geometryFor(connector)
		if bar, ok := h.r.bars[global]; ok {
			geom.BarZone = exclusiveBarZone(bar)
		} else {
			geom.BarZone = 0
		}
		h.geometry[connector] = geom
		heights := make([]int, 0, len(records))
		ids := make([]uint32, 0, len(records))
		if !suppressed {
			for _, id := range records {
				heights = append(heights, h.cardHeight(id))
				ids = append(ids, id)
			}
		}
		visible, queued := placeIDs(ids, heights, geom)
		h.visible[connector] = visible
		h.queued[connector] = queued
		h.rebuild(connector)
		h.updateHover(connector)

		h.request(wayland.AuxRequest{
			Output: global,
			ID:     toastSurfaceID(connector),
			Update: &wayland.AuxUpdate{
				SetInputRegion: true,
				InputRects:     toastInputRegion(h.cardRects(connector, visible)),
			},
		})
		h.r.publishSurface(global, toastSurfaceID(connector))
	}
	h.publishPresentation()
}

func (h *toastHost) publishPresentation() {
	ids := h.r.notify.activeIDs()
	if len(ids) == 0 {
		return
	}
	presentations := make([]protocol.Presentation, 0, len(ids))
	for _, id := range ids {
		presentations = append(presentations, protocol.Presentation{
			ID:    id,
			State: h.r.aggregatePresentation(id, h.viewFor(id)),
		})
	}
	h.r.sendNotify(protocol.Command{Kind: protocol.CommandPresentationRenew, Presentations: presentations})
}

const presentationLeaseRenew = 2 * time.Second

// startLeaseRenew keeps presentation.renew alive while cards exist. The
// service drops hover/queue holds after six seconds without a renew.
func (h *toastHost) startLeaseRenew(every time.Duration) {
	if h == nil || every <= 0 || h.stopRenew != nil {
		return
	}
	h.stopRenew = make(chan struct{})
	go func() {
		ticker := time.NewTicker(every)
		defer ticker.Stop()
		for {
			select {
			case <-h.stopRenew:
				return
			case <-ticker.C:
				h.r.mu.Lock()
				if len(h.r.notify.activeIDs()) > 0 {
					h.publishPresentation()
				}
				h.r.mu.Unlock()
			}
		}
	}()
}

func (h *toastHost) stopLeaseRenew() {
	if h == nil {
		return
	}
	h.renewOnce.Do(func() {
		if h.stopRenew != nil {
			close(h.stopRenew)
		}
	})
}

// placeIDs is the id-carrying half of toastLayout: geometry decides which
// records are visible and which queue.
func placeIDs(ids []uint32, heights []int, geom toastGeometry) (visible, queued []uint32) {
	_, queuedIdx := toastLayout(geom, heights)
	queuedSet := map[int]bool{}
	for _, i := range queuedIdx {
		queuedSet[i] = true
	}
	for i, id := range ids {
		if queuedSet[i] {
			queued = append(queued, id)
		} else {
			visible = append(visible, id)
		}
	}
	return visible, queued
}

func (h *toastHost) outputOrder() []string {
	out := make([]string, 0, len(h.outputs))
	for c := range h.outputs {
		out = append(out, c)
	}
	sort.Strings(out)
	return out
}

// geometryFor reports the output's logical geometry. An output the compositor
// has not configured yet uses the design default, so a stack computed before
// the first configure is placed somewhere sane rather than nowhere.
func (h *toastHost) geometryFor(connector string) toastGeometry {
	if geometry, ok := h.geometry[connector]; ok {
		return geometry
	}
	return toastGeometry{OutputW: 1920, OutputH: 1080, Corner: toastTopRight}
}

// cardHeight is the layout height of one card, measured from its tree.
// A missing tree or measure falls back to 96 so the card still places.
func (h *toastHost) cardHeight(id uint32) int {
	return toastCardHeight(h.cardFor(id), toastCardWidth, h.measureText(), h.style.Radius)
}

func (h *toastHost) measureText() ui.MeasureText {
	return func(text string, attrs ui.TextAttrs) (int, int) {
		if h.text != nil {
			spec := render.SpecFor(h.style, attrs)
			if w, height, err := h.text.Measure(text, spec, attrs.Tabular); err == nil {
				return w, height
			}
		}
		return len([]rune(text)) * 8, 16
	}
}

// cardRects lays out the visible ids for one output and returns their rects.
func (h *toastHost) cardRects(connector string, ids []uint32) []ui.Rect {
	heights := make([]int, len(ids))
	for i := range ids {
		heights[i] = h.cardHeight(ids[i])
	}
	rects, _ := toastLayout(h.geometryFor(connector), heights)
	return rects
}

// viewFor reports one record's placement for the aggregate presentation
// state. The registry's precedence collapses it.
func (h *toastHost) viewFor(id uint32) presentationView {
	v := presentationView{}
	for connector := range h.outputs {
		for _, vid := range h.visible[connector] {
			if vid == id {
				if h.hovered[connector][id] {
					v.hovered = append(v.hovered, connector)
				}
				v.visible = append(v.visible, connector)
			}
		}
		for _, qid := range h.queued[connector] {
			if qid == id {
				v.queued = append(v.queued, connector)
			}
		}
	}
	return v
}
