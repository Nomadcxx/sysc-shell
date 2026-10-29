package shell

import (
	"fmt"
	"math"
	"slices"
	"sort"
	"strings"
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
	visible  map[string][]uint32
	queued   map[string][]uint32
	hovered  map[string]map[uint32]bool
	expanded map[uint32]bool
	// noReplay holds active IDs that arrived while DND or the notification
	// centre suppressed their toast. Existing cards can return when suppression
	// ends; these records remain in the centre without becoming new toasts.
	noReplay          map[uint32]struct{}
	fullscreenFocused bool

	// geometry is the size each output's surface was configured at. Until a
	// configure arrives an output has none, and the design default stands in.
	geometry map[string]toastGeometry
	scale120 map[string]int
	measured map[string]bool
	// surfaceWidth and surfaceHeight are the accepted surface dimensions in
	// logical pixels. Geometry still records the full output.
	surfaceWidth    map[string]int
	surfaceHeight   map[string]int
	outputSized     map[string]bool
	resizePending   map[string]int
	pendingGeometry map[string]toastGeometry
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
	press          toastCard
	pressConnector string
	pressing       bool

	anim      *animator
	shown     map[string]map[uint32]ui.Rect
	stopSlide chan struct{}
	sliding   bool

	stopRenew chan struct{}
	renewOnce sync.Once

	text  *render.TextRenderer
	style render.Style

	// editing is the inline reply being typed, nil when none is open. The
	// resolver owns which record it answers; the host owns the text and the
	// keyboard of the one output it was opened on.
	editing *toastReply
}

type toastReply struct {
	connector string
	field     *ui.Field
	// focused is true while the reply holds the keyboard. A reply parked by
	// the pointer leaving keeps its text but neither the keyboard nor its
	// card's hold.
	focused bool
}

const toastNamespace = "sysc-shell-toast"

// hostHarness captures the aux requests a toast host emits in a test.
// A slide publishes input regions from its own goroutine, so appends are
// serialized with each other.
type hostHarness struct {
	mu      sync.Mutex
	opens   []*wayland.AuxSpec
	updates []*wayland.AuxUpdate
	closes  []string
}

func (h *hostHarness) request(r wayland.AuxRequest) {
	h.mu.Lock()
	defer h.mu.Unlock()
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
		r:               r,
		outputs:         map[string]uint32{},
		visible:         map[string][]uint32{},
		queued:          map[string][]uint32{},
		hovered:         map[string]map[uint32]bool{},
		expanded:        map[uint32]bool{},
		noReplay:        map[uint32]struct{}{},
		shown:           map[string]map[uint32]ui.Rect{},
		geometry:        map[string]toastGeometry{},
		scale120:        map[string]int{},
		measured:        map[string]bool{},
		surfaceWidth:    map[string]int{},
		surfaceHeight:   map[string]int{},
		outputSized:     map[string]bool{},
		resizePending:   map[string]int{},
		pendingGeometry: map[string]toastGeometry{},
		cards:           map[string][]toastCard{},
		pointer:         map[string]ui.Rect{},
	}
	if r != nil {
		r.mu.Lock()
		motion := r.panelTheme().Motion
		reduced := r.cfg.Accessibility.ReducedMotion
		r.mu.Unlock()
		h.anim = newAnimator(nil, reduced, motion)
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

func (h *toastHost) suppressArrival(n protocol.Notification) {
	for _, interval := range h.r.notify.suppressionIntervals(time.Now()) {
		if notificationArrivedDuringSuppression(n, interval) {
			h.markNoReplay(n.ID)
			return
		}
	}
}

// Snapshot records are a baseline, so use their service timestamp to identify
// notifications created during an active suppression interval. Older active
// records remain eligible to return when DND or the centre closes.
func (h *toastHost) suppressSnapshotArrivals(active []protocol.Notification) {
	s := h.r.notify
	intervals := s.takeSuppressionIntervals(h.r.clockNow())
	for _, n := range active {
		for _, interval := range intervals {
			if notificationArrivedDuringSuppression(n, interval) {
				h.markNoReplay(n.ID)
				break
			}
		}
	}
}

func notificationArrivedDuringSuppression(n protocol.Notification, interval notifySuppressionInterval) bool {
	if interval.dnd && n.Urgency == protocol.UrgencyCritical {
		return false
	}
	return !n.Timestamp.Before(interval.start) && (interval.end.IsZero() || n.Timestamp.Before(interval.end))
}

func (h *toastHost) markNoReplay(id uint32) {
	if h.noReplay == nil {
		h.noReplay = make(map[uint32]struct{})
	}
	h.noReplay[id] = struct{}{}
}

// restyleLocked rebinds the cached layout style to the published palette and
// relayouts every output. Called with Registry.mu held, from the retheme pass.
func (h *toastHost) restyleLocked() {
	if h.anim != nil {
		// The animator's table follows the published theme, so a speed or
		// reduced-motion change reaches open toasts instead of waiting for the
		// next surface.
		h.anim.Configure(h.r.panelTheme().Motion, h.r.cfg.Accessibility.ReducedMotion)
	}
	h.recompute()
}

func (h *toastHost) harness() *hostHarness { return h.harnessRef }

func toastSurfaceID(connector string) string { return "toast:" + connector }

func toastSlideKey(connector string, id uint32) string {
	return fmt.Sprintf("toast-slide:%s:%d", connector, id)
}

// drop forgets every per-output record for a surface that is gone -- the
// compositor closed it on its own -- so recompute stops sending Update to a
// surface that no longer exists. The next syncOutputs opens a fresh one
// (GitHub #21).
func (h *toastHost) drop(connector string) {
	if h.anim != nil {
		for id := range h.shown[connector] {
			h.anim.Forget(toastSlideKey(connector, id))
		}
	}
	delete(h.outputs, connector)
	delete(h.visible, connector)
	delete(h.queued, connector)
	delete(h.hovered, connector)
	delete(h.geometry, connector)
	delete(h.scale120, connector)
	delete(h.measured, connector)
	delete(h.surfaceWidth, connector)
	delete(h.surfaceHeight, connector)
	delete(h.outputSized, connector)
	delete(h.resizePending, connector)
	delete(h.pendingGeometry, connector)
	delete(h.cards, connector)
	delete(h.pointer, connector)
	if h.pressConnector == connector {
		h.press = toastCard{}
		h.pressConnector = ""
		h.pressing = false
	}
	if h.editing != nil && h.editing.connector == connector {
		h.endReply() // the surface, and its keyboard, are already gone
		h.publishPresentation()
	}
	h.noteTargets(connector, nil, nil)
}

// syncOutputs opens a surface for each new output and closes surfaces whose
// output went away. Outputs are identified by wl_registry global, matching
// the registry's rule that a connector can change globals across a reconnect.
//
// Called with Registry.mu held, like every other writer of this host's state.
func (h *toastHost) syncOutputs(globals map[string]uint32) {
	for connector, global := range h.outputs {
		if _, ok := globals[connector]; !ok {
			h.request(wayland.AuxRequest{Output: global, ID: toastSurfaceID(connector)})
			h.drop(connector)
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

// spec describes a narrow strip at the output's right edge. The strip spans
// the output vertically and leaves room for the card, its margin and a right
// bar inset. OutputSize supplies full-output metrics for layout; pointer and
// blur regions stay local to the strip and cover only the visible cards.
func (h *toastHost) spec(connector string) *wayland.AuxSpec {
	barGeometry := h.applyBarEdge(connector, toastGeometry{})
	_, rightInset := toastSideInsets(barGeometry)
	width := toastCardWidth + 2*toastMargin + rightInset
	h.surfaceWidth[connector] = width
	delete(h.outputSized, connector)
	delete(h.resizePending, connector)
	delete(h.pendingGeometry, connector)
	return &wayland.AuxSpec{
		ID:        toastSurfaceID(connector),
		Namespace: toastNamespace,
		Layer:     layershell.ZwlrLayerShellV1LayerOverlay,
		Anchor: uint32(layershell.ZwlrLayerSurfaceV1AnchorTop |
			layershell.ZwlrLayerSurfaceV1AnchorBottom |
			layershell.ZwlrLayerSurfaceV1AnchorRight),
		Width:         int32(width),
		ExclusiveZone: -1,
		Keyboard:      keyboardNone,
		Callbacks: wayland.HostCallbacks{
			OutputSize: func(width, height int) {
				h.outputSize(connector, width, height)
			},
			Configure: func(width, height, scale120 int) error {
				return h.configure(connector, width, height, scale120)
			},
			Render: func(pixels []byte, width, height, stride int) error {
				return h.render(connector, pixels, width, height, stride)
			},
			Handle: func(event wayland.Event) bool { return h.handle(connector, event) },
			BlurShape: func() []ui.Rect {
				h.r.mu.Lock()
				defer h.r.mu.Unlock()
				return h.blurShape(connector)
			},
		},
	}
}

// blurShape is the region the compositor blurs behind this output's toasts,
// in surface coordinates: each card's rounded silhouette where it draws this
// frame, so the blur follows a card as it slides. Without compositor blur the
// cards keep the overlay ground and nothing blurs. Caller holds r.mu.
func (h *toastHost) blurShape(connector string) []ui.Rect {
	if !h.glass() {
		return nil
	}
	var out []ui.Rect
	for _, card := range h.cards[connector] {
		out = append(out, ui.BlurStrips(ui.SurfaceShape{Body: h.drawnRect(connector, card), Radius: h.style.Radius})...)
	}
	return out
}

// configure records the strip's accepted size and relays out the stack against
// its current output metrics. OutputSize provides the full output dimensions.
func (h *toastHost) configure(connector string, width, height, scale120 int) error {
	h.r.mu.Lock()
	defer h.r.mu.Unlock()
	return h.configureLocked(connector, width, height, scale120)
}

func (h *toastHost) configureLocked(connector string, width, height, scale120 int) error {
	if width > 0 && height > 0 {
		oldWidth := h.surfaceWidth[connector]
		h.surfaceWidth[connector] = width
		h.surfaceHeight[connector] = height
		if pending, ok := h.pendingGeometry[connector]; ok && width >= toastSurfaceWidth(pending) {
			h.geometry[connector] = pending
			delete(h.pendingGeometry, connector)
		}
		if h.resizePending[connector] == width {
			delete(h.resizePending, connector)
		}
		if oldWidth > 0 && oldWidth != width {
			delta := width - oldWidth
			if pointer, ok := h.pointer[connector]; ok {
				pointer.X += delta
				h.pointer[connector] = pointer
			}
			for id, rect := range h.shown[connector] {
				rect.X += delta
				h.shown[connector][id] = rect
				// A card still in flight moves with the surface: its drawn
				// offset and its destination both shift by the same delta.
				if h.anim != nil {
					h.anim.shiftStackRectX(toastSlideKey(connector, id), float64(delta))
				}
			}
			if h.pressing && h.pressConnector == connector {
				h.press.rect.X += delta
			}
		}
		if !h.outputSized[connector] {
			h.geometry[connector] = h.applyBarEdge(connector, toastGeometry{OutputW: width, OutputH: height})
		}
	}
	h.scale120[connector] = scale120
	h.recompute()
	return nil
}

func (h *toastHost) outputSize(connector string, width, height int) {
	if width <= 0 || height <= 0 {
		return
	}
	h.r.mu.Lock()
	defer h.r.mu.Unlock()
	wasSized := h.outputSized[connector]
	previous, hadGeometry := h.geometry[connector]
	next := h.applyBarEdge(connector, toastGeometry{OutputW: width, OutputH: height})
	currentWidth := h.surfaceWidth[connector]
	if currentWidth <= 0 && hadGeometry {
		currentWidth = previous.OutputW
	}
	h.outputSized[connector] = true
	if wasSized && currentWidth > 0 && toastSurfaceWidth(next) > currentWidth {
		h.pendingGeometry[connector] = next
		if hadGeometry {
			layoutGeometry := next
			layoutGeometry.OutputW = previous.OutputW
			h.geometry[connector] = layoutGeometry
			if previous != layoutGeometry {
				h.recompute()
			}
		}
		desiredWidth := toastSurfaceWidth(next)
		if h.resizePending[connector] != desiredWidth {
			h.resizePending[connector] = desiredWidth
			if output, ok := h.outputs[connector]; ok {
				size := uint32(desiredWidth)
				h.request(wayland.AuxRequest{
					Output: output,
					ID:     toastSurfaceID(connector),
					Update: &wayland.AuxUpdate{Width: &size},
				})
			}
		}
		return
	}
	delete(h.pendingGeometry, connector)
	h.geometry[connector] = next
	if wasSized && hadGeometry && previous != next {
		h.recompute()
	}
}

// toastCornerForEdge anchors a bottom bar's stack above the bar. Every other
// edge keeps the top-right stack.
func toastCornerForEdge(edge string) toastCorner {
	if edge == "bottom" {
		return toastBottomRight
	}
	return toastTopRight
}

// applyBarEdge stores the open bar's thickness and edge on g and picks the
// corner from that edge. No bar leaves the top-right stack with no inset.
func (h *toastHost) applyBarEdge(connector string, g toastGeometry) toastGeometry {
	g.BarEdge = ""
	g.BarZone = 0
	g.Corner = toastTopRight
	global, ok := h.outputs[connector]
	if !ok {
		return g
	}
	bar, ok := h.r.bars[global]
	if !ok {
		return g
	}
	g.BarZone = exclusiveBarZone(bar)
	if bar != nil {
		g.BarEdge = bar.themeSnapshot().BarEdge
	}
	g.Corner = toastCornerForEdge(g.BarEdge)
	return g
}

func (h *toastHost) render(connector string, pixels []byte, width, height, stride int) error {
	h.r.mu.Lock()
	defer h.r.mu.Unlock()
	if h.text == nil {
		text, err := h.r.textRenderer(h.r.cfg.Bar.FontFamily)
		if err != nil {
			return err
		}
		h.text = text
	}
	if !h.measured[connector] {
		// Pre-render layout uses fallback text widths. Start from these real
		// font measurements without animating a slot the user never saw.
		h.noteTargets(connector, nil, nil)
		h.rebuild(connector)
		h.measured[connector] = true
		if output, ok := h.outputs[connector]; ok {
			h.request(wayland.AuxRequest{
				Output: output,
				ID:     toastSurfaceID(connector),
				Update: &wayland.AuxUpdate{
					SetInputRegion: true,
					InputRects:     h.inputRegion(connector),
				},
			})
		}
	}
	canvas, err := render.NewCanvas(pixels, width, height, stride)
	if err != nil {
		return err
	}
	style := h.style
	style.Scale120 = ui.Scale120(max(h.scale120[connector], int(ui.ScaleUnit)))

	// The surface is a narrow strip at the output's right edge. Clear its full
	// buffer, then paint each card into its own buffer and copy it in; gaps and
	// the rest of the output stay transparent without a fullscreen allocation.
	clear(pixels)
	for _, card := range h.cards[connector] {
		drawn := card
		drawn.rect = h.drawnRect(connector, card)
		if err := h.paintCard(canvas, drawn, style); err != nil {
			return err
		}
	}
	return nil
}

// toastCard is one placed card: its tree, arranged at the origin, and where
// on the surface it belongs.
type toastCard struct {
	root     *ui.Node
	rect     ui.Rect
	critical bool
}

type toastCardLayout struct {
	root     *ui.Node
	height   int
	critical bool
}

// glass reports whether toast cards take the see-through panel ground: only
// when blur-behind is on and the compositor blurs, the condition panels use.
// Caller holds r.mu.
func (h *toastHost) glass() bool { return h.r.cfg.Theme.BlurBehind && h.r.caps.Blur }

// cardStyle is the ground toast cards paint on. Under compositor blur a card
// is a small floating panel: the panel's opacity, which follows the global
// panel-opacity setting. Without blur it keeps the overlay's higher floor,
// chosen for a surface with nothing behind it. Either way it strokes the
// panel rim. Caller holds r.mu.
func (h *toastHost) cardStyle() render.Style {
	t := h.r.surfaceTheme()
	s := t.OverlayStyle()
	if h.glass() {
		s = t.PanelStyle()
	}
	s.Rim = t.Outline
	return s
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
	if card.critical {
		cardStyle.Rim = style.Error
	}
	if err := render.Paint(cardCanvas, card.root, h.text, cardStyle); err != nil {
		return err
	}
	left := max(0, box.X)
	right := min(canvas.Width, box.X+box.W)
	if right <= left {
		return nil
	}
	sourceX := left - box.X
	copyWidth := (right - left) * 4
	for y := range box.H {
		target := box.Y + y
		if target < 0 || target >= canvas.Height {
			continue
		}
		to := target*canvas.Stride + left*4
		from := y*stride + sourceX*4
		if to < 0 || to+copyWidth > len(canvas.Pix) || from < 0 || from+copyWidth > len(h.scratch) {
			continue
		}
		copy(canvas.Pix[to:to+copyWidth], h.scratch[from:from+copyWidth])
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
		// Leaving an empty reply abandons it; one with text is parked. The
		// surface has no shield, so a click on another window never reaches
		// the shell, and an exclusive grab kept past this point would take
		// that window's keystrokes into the reply.
		left := h.editing != nil && h.editing.connector == connector && h.editing.focused
		if left {
			if h.editing.field.Text == "" {
				h.endReply()
			} else {
				h.parkReply()
			}
		}
		if h.updateHover(connector) || left {
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
		h.pressConnector = connector
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
		h.pressConnector = ""
		h.resolver.release(card.root, x-card.rect.X, y-card.rect.Y)
		if h.resolver.takeReplyHit() {
			h.focusReply(connector)
		}
		return true
	case wayland.EventKeyPress:
		return h.replyKey(connector, ui.KeyInput{Code: event.Key, Sym: event.Sym, Text: event.Text, Mods: event.Mods, Serial: event.Serial})
	case wayland.EventPaste:
		if h.editing == nil || h.editing.connector != connector {
			return false
		}
		h.editing.field.BreakUndo()
		h.editing.field.Commit(flattenPaste(strings.ReplaceAll(event.Paste, "\x00", "")))
		h.repaint(connector)
		return true
	default:
		return false
	}
}

// focusReply opens the reply whose field was pressed, or resumes a parked
// one, taking the keyboard for the toast surface in place and holding the
// card open while it is typed into.
//
// Exclusive, as a panel is, not on-demand: a compositor focuses an on-demand
// surface on the button press, which has already happened by the time this
// runs, so on-demand would need a second click before the first key landed.
// Enter, Escape, or the pointer leaving hands the keyboard back.
func (h *toastHost) focusReply(connector string) {
	if h.editing == nil {
		h.editing = &toastReply{connector: connector, field: ui.NewField("")}
	}
	if h.editing.connector != connector || h.editing.focused {
		return
	}
	h.editing.focused = true
	h.setKeyboard(connector, keyboardExclusive)
	h.repaint(connector)
	h.publishPresentation()
}

// parkReply keeps the typed text and hands the keyboard back. The caller
// republishes presentation.
func (h *toastHost) parkReply() {
	h.parkReplyLocked()
	h.repaint(h.editing.connector)
}

// parkReplyLocked keeps the typed text and hands the keyboard back without
// repainting, for callers that relayout the surface themselves. A reply that
// already parked keeps its text and stays as it is.
func (h *toastHost) parkReplyLocked() {
	if !h.editing.focused {
		return
	}
	h.editing.focused = false
	h.setKeyboard(h.editing.connector, keyboardNone)
}

// endReply closes the reply without sending it and hands the keyboard back.
// The caller republishes presentation.
func (h *toastHost) endReply() {
	if h.editing == nil {
		return
	}
	connector, focused := h.editing.connector, h.editing.focused
	h.editing = nil
	h.resolver.cancelReply()
	if focused {
		h.setKeyboard(connector, keyboardNone)
	}
	h.repaint(connector)
}

// replyKey edits the open reply. Enter sends it, Escape abandons it, and
// every other key goes through the shared field editor.
func (h *toastHost) replyKey(connector string, k ui.KeyInput) bool {
	if h.editing == nil || h.editing.connector != connector || !h.editing.focused {
		return false
	}
	switch k.Sym {
	case ui.SymEscape:
		h.endReply()
		h.publishPresentation()
		return true
	case ui.SymReturn, ui.SymKPEnter:
		h.resolver.submitReply(h.resolver.replyID, h.editing.field.Text)
		h.endReply()
		h.publishPresentation()
		return true
	}
	res := h.editing.field.HandleKey(k)
	h.r.requestClipboard(res, k.Serial)
	if res.Changed || res.Handled {
		h.repaint(connector)
	}
	return res.Handled
}

func (h *toastHost) setKeyboard(connector string, mode uint32) {
	global, ok := h.outputs[connector]
	if !ok {
		return
	}
	h.request(wayland.AuxRequest{
		Output: global,
		ID:     toastSurfaceID(connector),
		Update: &wayland.AuxUpdate{Keyboard: &mode},
	})
}

// repaint rebuilds one output's cards from the current placement and asks
// for a frame.
func (h *toastHost) repaint(connector string) {
	global, ok := h.outputs[connector]
	if !ok {
		return
	}
	h.rebuild(connector)
	h.r.publishSurface(global, toastSurfaceID(connector))
}

// showReply paints the open reply into its laid-out card: the typed text,
// caret and selection, scrolled so the caret stays in view.
func (h *toastHost) showReply(root *ui.Node, id uint32, measure ui.MeasureText) {
	action := fmt.Sprintf("notify:%d:reply", id)
	var find func(*ui.Node) *ui.Node
	find = func(n *ui.Node) *ui.Node {
		if n.Action == action && n.Kind == ui.KindTextField {
			return n
		}
		for _, c := range n.Children {
			if f := find(c); f != nil {
				return f
			}
		}
		return nil
	}
	n := find(root)
	if n == nil {
		return
	}
	f := h.editing.field
	n.Editing = h.editing.focused
	f.SyncTo(n)
	attrs := ui.TextAttrsOf(n)
	caretX, _ := measure(ui.DisplayPrefix(n, n.Cursor)+ui.DisplayPreedit(n), attrs)
	textW, _ := measure(ui.DisplayText(n)+ui.DisplayPreedit(n), attrs)
	f.ScrollX = ui.KeepCaretVisible(f.ScrollX, caretX, textW, render.FieldTextRect(n).W, 8)
	n.ScrollX = f.ScrollX
}

// replyingOn reports whether id's card on connector holds the open reply,
// focused or parked.
func (h *toastHost) replyingOn(connector string, id uint32) bool {
	return h.editing != nil && h.editing.connector == connector && h.resolver.replyID == id
}

// cardAt returns the card drawn under the point. The stored rectangle is the
// slot the card is heading for, so the hit uses the rectangle paint and blur
// use this frame. The returned rectangle is that one, and a press maps into
// the card the pointer is actually on. Later cards are painted over earlier
// ones, and the last one under the point is the one on screen.
func (h *toastHost) cardAt(connector string, x, y int) (toastCard, bool) {
	cards := h.cards[connector]
	for i := len(cards) - 1; i >= 0; i-- {
		card := cards[i]
		hit := h.drawnRect(connector, card)
		if hit.Contains(x, y) {
			card.rect = hit
			return card, true
		}
	}
	return toastCard{}, false
}

// drawnRect is where one card is painted this frame. Caller holds r.mu.
func (h *toastHost) drawnRect(connector string, card toastCard) ui.Rect {
	if id, ok := cardID(card.root); ok {
		return h.displayRect(connector, id, card.rect)
	}
	return card.rect
}

func (h *toastHost) invoke(id uint32, key string) {
	h.r.sendNotify(protocol.Command{Kind: protocol.CommandAction, ID: id, ActionKey: key})
}
func (h *toastHost) dismiss(id uint32) {
	h.r.sendNotify(protocol.Command{Kind: protocol.CommandDismiss, ID: id})
}
func (h *toastHost) toggleExpand(id uint32) {
	if h.expanded[id] {
		delete(h.expanded, id)
	} else {
		h.expanded[id] = true
	}
	h.recompute()
}
func (h *toastHost) reply(id uint32, text string) {
	h.r.sendNotify(protocol.Command{Kind: protocol.CommandReply, ID: id, Text: text})
}
func (h *toastHost) hover(uint32, bool) {}
func (h *toastHost) openLink(href string) {
	if h.r == nil {
		return
	}
	h.r.openNotificationLink(href)
}

// updateHover recomputes the hovered set for one output and reports whether
// it changed. Only a change is worth a frame.
func (h *toastHost) updateHover(connector string) bool {
	at, inside := h.pointer[connector]
	hovered := map[uint32]bool{}
	if inside {
		// The placed cards, not a fresh layout: motion arrives far faster
		// than the stack changes, and rebuilding every tree per event held
		// the registry lock for most of a fast pointer's travel.
		if card, ok := h.cardAt(connector, at.X, at.Y); ok {
			if id, ok := cardID(card.root); ok {
				hovered[id] = true
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
	// Rebound from the published palette on every rebuild, not cached at
	// first paint, so a theme reload reaches open toasts immediately
	// (GitHub #29).
	h.style = h.cardStyle()
	ids := h.visible[connector]
	measure := h.measureText()
	layouts := h.cardLayouts(ids, measure)
	h.rebuildWithLayouts(connector, ids, layouts, measure)
}

func (h *toastHost) cardLayouts(ids []uint32, measure ui.MeasureText) map[uint32]toastCardLayout {
	layouts := make(map[uint32]toastCardLayout, len(ids))
	width := h.cardWidth()
	for _, id := range ids {
		n, ok := h.record(id)
		if !ok {
			layouts[id] = toastCardLayout{height: 96}
			continue
		}
		root := h.cardOf(n)
		layouts[id] = toastCardLayout{
			root: root, height: toastCardHeight(root, width, measure), critical: n.Urgency == protocol.UrgencyCritical,
		}
	}
	return layouts
}

func (h *toastHost) rebuildWithLayouts(connector string, ids []uint32, layouts map[uint32]toastCardLayout, measure ui.MeasureText) {
	heights := make([]int, len(ids))
	for i, id := range ids {
		layout, ok := layouts[id]
		if ok {
			heights[i] = layout.height
		} else {
			heights[i] = 96
		}
	}
	var rects []ui.Rect
	if geometry, known := h.geometryFor(connector); known {
		rects, _ = toastLayout(geometry, heights)
		surfaceWidth := h.surfaceWidth[connector]
		if surfaceWidth <= 0 {
			surfaceWidth = geometry.OutputW
		}
		for i := range rects {
			rects[i] = toastSurfaceRect(geometry, surfaceWidth, rects[i])
		}
	}
	var moved bool
	if h.text != nil {
		moved = h.noteTargets(connector, ids[:min(len(ids), len(rects))], rects)
	}
	if moved && !h.sliding && h.anim != nil && !h.anim.reduced && !h.anim.Settled() {
		h.startSlideLocked()
	}
	cards := make([]toastCard, 0, len(ids))
	for i, id := range ids {
		if i >= len(rects) {
			break
		}
		layout, ok := layouts[id]
		if !ok || layout.root == nil {
			continue
		}
		root := layout.root
		if err := ui.LayoutColumn(root, ui.Rect{W: rects[i].W, H: rects[i].H}, measure); err != nil {
			continue
		}
		if h.replyingOn(connector, id) {
			h.showReply(root, id, measure)
		}
		cards = append(cards, toastCard{root: root, rect: rects[i], critical: layout.critical})
	}
	h.cards[connector] = cards
}

// publishSlideFrame repaints every toast surface and moves its input region
// onto the rectangles drawn this frame. The slide continues after recompute,
// and the compositor delivers clicks to the region it was last given.
// startSlideLocked runs the one frame loop that carries cards to their new
// places and returns a channel closed when the loop stops. Caller holds
// Registry.mu.
func (h *toastHost) startSlideLocked() <-chan struct{} {
	h.sliding = true
	h.stopSlide = make(chan struct{})
	stop := h.stopSlide
	done := make(chan struct{})
	go func() {
		defer close(done)
		animateSurface(stop, func() bool {
			h.r.mu.Lock()
			defer h.r.mu.Unlock()
			settled := h.anim == nil || h.anim.Settled()
			if settled {
				h.sliding = false
				h.stopSlide = nil
			}
			return settled
		}, h.publishSlideFrame, func() time.Duration {
			// Asked per tick, so a restyle's cap reaches a slide in flight.
			h.r.mu.Lock()
			defer h.r.mu.Unlock()
			return h.anim.frameCap()
		})
	}()
	return done
}

func (h *toastHost) publishSlideFrame() {
	if h == nil || h.r == nil {
		return
	}
	h.r.mu.Lock()
	type slideFrame struct {
		output uint32
		id     string
		rects  []ui.Rect
	}
	frames := make([]slideFrame, 0, len(h.outputs))
	for connector, output := range h.outputs {
		frames = append(frames, slideFrame{
			output: output,
			id:     toastSurfaceID(connector),
			rects:  h.inputRegion(connector),
		})
	}
	h.r.mu.Unlock()
	for _, frame := range frames {
		h.request(wayland.AuxRequest{
			Output: frame.output,
			ID:     frame.id,
			Update: &wayland.AuxUpdate{
				SetInputRegion: true,
				InputRects:     frame.rects,
			},
		})
		h.r.publishSurface(frame.output, frame.id)
	}
}

// displayRect is where one output draws a card this frame: its stored geometry
// channels while the stack settles, or the slot itself once they retire. Paint,
// hit testing and the input region all read this one rectangle, so a click
// always lands on the pixels the user can see. Caller holds r.mu.
func (h *toastHost) displayRect(connector string, id uint32, target ui.Rect) ui.Rect {
	if h.anim == nil || h.anim.reduced {
		return target
	}
	node := toastSlideKey(connector, id)
	if !h.anim.has(node, animStackX) {
		return target
	}
	rect := ui.Rect{
		X: int(math.Round(h.anim.Value(node, animStackX))),
		Y: int(math.Round(h.anim.Value(node, animStackY))),
		W: int(math.Round(h.anim.Value(node, animStackW))),
		H: int(math.Round(h.anim.Value(node, animStackH))),
	}
	// A spring may dip a pixel below a component's endpoints; a card is never
	// narrower than nothing. X and Y may stay negative: the surface clips them.
	if rect.W < 0 {
		rect.W = 0
	}
	if rect.H < 0 {
		rect.H = 0
	}
	return rect
}

// noteTargets tracks the visible cards' target rectangles per output. A moved
// card retargets its stored geometry, so a second change never jumps and the
// spring keeps whatever speed the card already had. Caller holds r.mu.
func (h *toastHost) noteTargets(connector string, ids []uint32, rects []ui.Rect) (moved bool) {
	shown := h.shown[connector]
	if shown == nil {
		shown = make(map[uint32]ui.Rect, len(ids))
		h.shown[connector] = shown
	}
	seen := make(map[uint32]bool, len(ids))
	for i, id := range ids {
		if i >= len(rects) {
			break
		}
		seen[id] = true
		old, ok := shown[id]
		if ok && old != rects[i] {
			moved = true
			if h.anim != nil && !h.anim.reduced {
				h.anim.TargetStackRect(toastSlideKey(connector, id), old, rects[i])
			}
		}
		shown[id] = rects[i]
	}
	for id := range shown {
		if !seen[id] {
			delete(shown, id)
			if h.anim != nil {
				h.anim.Forget(toastSlideKey(connector, id))
			}
		}
	}
	return moved
}

// record reads one active notification. A record that has gone between the
// placement and the paint reports false rather than yielding an empty card.
func (h *toastHost) record(id uint32) (protocol.Notification, bool) {
	s := h.r.notify
	s.mu.Lock()
	defer s.mu.Unlock()
	n, ok := s.active[id]
	return n, ok
}

// cardFor projects one active record, or nothing if it has gone.
func (h *toastHost) cardFor(id uint32) *ui.Node {
	n, ok := h.record(id)
	if !ok {
		return nil
	}
	return h.cardOf(n)
}

// cardOf builds a record's card at the width cards lay out at, wrapped over
// several lines once a drag has expanded it.
func (h *toastHost) cardOf(n protocol.Notification) *ui.Node {
	icon := h.r.notificationLeadImageLocked(n.AppIcon, n.DesktopEntry, n.Image)
	metrics := h.r.surfaceTheme().Metrics
	var wrap func(string) []string
	if h.expanded[n.ID] {
		wrap = func(s string) []string { return h.wrapBody(s, metrics.CardPadding) }
	}
	return notificationCardWithMetrics(n, icon, h.r.linksAllowed(), h.measureText(), wrap, time.Now(), h.cardWidth(), cfgT(h.r.cfg), metrics)
}

// cardWidth is the width toast cards lay out at: the design width, or
// narrower where an open output clamps it. Card trees are shared by every
// output, so they fit the narrowest. Caller holds r.mu.
func (h *toastHost) cardWidth() int {
	width := toastCardWidth
	for _, g := range h.geometry {
		if w := toastFitWidth(g); w > 0 && w < width {
			width = w
		}
	}
	return width
}

func (h *toastHost) wrapBody(s string, padding int) []string {
	measure := h.measureText()
	width := h.cardWidth() - 2*padding - cardIconSize - cardLeadGap
	return wrapLines(s, width, func(text string) int {
		w, _ := measure(text, ui.TextAttrs{})
		return w
	}, 8)
}

// recompute relayouts every open output from the current projection and
// publishes each surface's input region. It is safe to call on any record,
// fullscreen, geometry, or output change. Production callers hold Registry.mu.
func (h *toastHost) recompute() {
	fullscreen := h.focusedWindowFillsOutput()
	h.fullscreenFocused = fullscreen
	s := h.r.notify
	s.mu.Lock()
	dnd := s.dndActiveLocked(h.r.clockNow())
	centerOpen := s.centerOpen
	connected := s.generation != 0
	records := make([]uint32, 0, len(s.active))
	active := make(map[uint32]struct{}, len(s.active))
	for id, n := range s.active {
		active[id] = struct{}{}
		_, noReplay := h.noReplay[id]
		if !noReplay && !notificationSuppressed(dnd, centerOpen, fullscreen, n.Urgency) {
			records = append(records, id)
		}
	}
	s.mu.Unlock()
	if connected {
		for id := range h.noReplay {
			if _, ok := active[id]; !ok {
				delete(h.noReplay, id)
			}
		}
	}
	for id := range h.expanded {
		if _, ok := active[id]; !ok {
			delete(h.expanded, id)
		}
	}

	// Newest first: the stack reads down from the freshest card.
	sort.Slice(records, func(i, j int) bool { return records[i] > records[j] })

	for _, connector := range h.outputOrder() {
		global, ok := h.outputs[connector]
		if !ok {
			continue
		}
		geom, known := h.geometryFor(connector)
		candidateGeometry := h.applyBarEdge(connector, geom)
		// An unmeasured output keeps no geometry: writing one back would make
		// the placeholder indistinguishable from a real measurement.
		if known {
			acceptedWidth := h.surfaceWidth[connector]
			pending, hasPending := h.pendingGeometry[connector]
			if hasPending && acceptedWidth > 0 && toastSurfaceWidth(pending) > acceptedWidth {
				// Keep the current layout until the compositor accepts a strip
				// wide enough for the pending geometry.
			} else if !hasPending && acceptedWidth > 0 && toastSurfaceWidth(candidateGeometry) > acceptedWidth {
				h.pendingGeometry[connector] = candidateGeometry
			} else {
				if hasPending {
					geom = pending
					delete(h.pendingGeometry, connector)
				} else {
					geom = candidateGeometry
				}
				h.geometry[connector] = geom
			}
		}
		surfaceWidth := h.surfaceWidth[connector]
		if surfaceWidth <= 0 && known {
			surfaceWidth = geom.OutputW
		}
		var resizeWidth *uint32
		if known && h.outputSized[connector] {
			desiredGeometry := geom
			if pending, ok := h.pendingGeometry[connector]; ok {
				desiredGeometry = pending
			}
			desiredWidth := toastSurfaceWidth(desiredGeometry)
			if desiredWidth == surfaceWidth {
				delete(h.resizePending, connector)
			} else {
				if h.resizePending[connector] != desiredWidth {
					width := uint32(desiredWidth)
					resizeWidth = &width
					h.resizePending[connector] = desiredWidth
				}
			}
		}
		h.style = h.cardStyle()
		measure := h.measureText()
		layouts := make(map[uint32]toastCardLayout, len(records))
		heights := make([]int, 0, len(records))
		ids := make([]uint32, 0, len(records))
		if known {
			layouts = h.cardLayouts(records, measure)
			for _, id := range records {
				heights = append(heights, layouts[id].height)
				ids = append(ids, id)
			}
		}
		visible, queued := placeIDs(ids, heights, geom)
		h.visible[connector] = visible
		h.queued[connector] = queued
		// A reply ends with its card when the card is gone: the record
		// closed, or the stack was suppressed. A card the layout pushed
		// into the queue is still alive, so its reply is parked with the
		// text and resumes when the card comes back to the visible stack.
		if h.editing != nil && h.editing.connector == connector && !slices.Contains(visible, h.resolver.replyID) {
			if slices.Contains(queued, h.resolver.replyID) {
				h.parkReplyLocked()
			} else {
				h.endReply()
			}
		}
		h.rebuildWithLayouts(connector, visible, layouts, measure)
		h.updateHover(connector)
		// The region is surface-local to the accepted width. Commit it before a
		// resize request; configure relays out cards in the new coordinates.
		h.request(wayland.AuxRequest{
			Output: global,
			ID:     toastSurfaceID(connector),
			Update: &wayland.AuxUpdate{
				SetInputRegion: true,
				InputRects:     h.inputRegion(connector),
			},
		})
		if resizeWidth != nil {
			h.request(wayland.AuxRequest{
				Output: global,
				ID:     toastSurfaceID(connector),
				Update: &wayland.AuxUpdate{Width: resizeWidth},
			})
		}
		h.r.publishSurface(global, toastSurfaceID(connector))
	}
	h.publishPresentation()
}

func (h *toastHost) focusedWindowFillsOutput() bool {
	snapshot := h.r.niriSnapshot
	if snapshot.FocusedOutput == "" {
		return false
	}
	geometry, known := h.geometryFor(snapshot.FocusedOutput)
	if !known || geometry.OutputW <= 0 || geometry.OutputH <= 0 {
		return false
	}
	// Niri 26.04 exposes no fullscreen bit. ponytail: use a focused tile that
	// matches the output within one logical pixel; replace this inference if
	// Niri adds an explicit field. A non-fullscreen tile filling the output is
	// a false positive; a fullscreen tile differing by more than one pixel is
	// a false negative.
	for _, window := range snapshot.Windows {
		if window.Focused && window.HasLayout &&
			math.Abs(window.TileW-float64(geometry.OutputW)) <= 1 &&
			math.Abs(window.TileH-float64(geometry.OutputH)) <= 1 {
			return true
		}
	}
	return false
}

// inputRegion is clipped to the accepted surface's coordinates. A pending
// resize does not change those bounds until the compositor accepts configure.
func (h *toastHost) inputRegion(connector string) []ui.Rect {
	rects := h.interactiveRects(connector)
	width := h.surfaceWidth[connector]
	height := h.surfaceHeight[connector]
	if height <= 0 {
		// Before the first configure, output geometry is the available height.
		if geometry, known := h.geometryFor(connector); known {
			height = geometry.OutputH
		}
	}
	if width <= 0 || height <= 0 {
		return toastInputRegion(rects)
	}
	clipped := make([]ui.Rect, 0, len(rects))
	for _, rect := range rects {
		x, y := max(0, rect.X), max(0, rect.Y)
		right := min(width, rect.X+rect.W)
		bottom := min(height, rect.Y+rect.H)
		if right > x && bottom > y {
			clipped = append(clipped, ui.Rect{X: x, Y: y, W: right - x, H: bottom - y})
		}
	}
	return toastInputRegion(clipped)
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

// stopSlideAnimation is called with Registry.mu held during shutdown.
func (h *toastHost) stopSlideAnimation() {
	if h.stopSlide != nil {
		close(h.stopSlide)
		h.stopSlide = nil
	}
	h.sliding = false
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

// geometryFor reports an output's measured size, and whether it has been
// measured at all. There is deliberately no stand-in size: a guess wider than
// the real output places a card off the surface, the platform refuses that
// input region, and the shell exits. Callers place nothing until this is known.
func (h *toastHost) geometryFor(connector string) (toastGeometry, bool) {
	geometry, ok := h.geometry[connector]
	if !ok {
		return toastGeometry{Corner: toastTopRight}, false
	}
	return geometry, true
}

// cardHeight is the layout height of one card, measured from its tree.
// A missing tree or measure falls back to 96 so the card still places.
func (h *toastHost) cardHeight(id uint32) int {
	return toastCardHeight(h.cardFor(id), h.cardWidth(), h.measureText())
}

// measureText measures at the output's scale and rounds up into logical
// pixels, the space cards are laid out in. A shaped run does not scale
// linearly, so a 1x measure could grant less room than the painted run
// takes and the painter would clip it. Card trees are shared by every
// output, so it measures at the largest scale among them.
func (h *toastHost) measureText() ui.MeasureText {
	scale := ui.ScaleUnit
	for _, s := range h.scale120 {
		if v := ui.Scale120(s); v.Valid() && v > scale {
			scale = v
		}
	}
	style := h.style
	style.Scale120 = scale
	return func(text string, attrs ui.TextAttrs) (int, int) {
		if h.text != nil {
			spec := render.SpecFor(style, attrs)
			if w, height, err := h.text.Measure(text, spec, attrs.Tabular); err == nil {
				return scale.Logical(w), scale.Logical(height)
			}
		}
		return len([]rune(text)) * 8, 16
	}
}

// interactiveRects are the rectangles that take clicks this frame: each
// placed card where it is drawn, including one still sliding into its slot.
// Caller holds r.mu.
func (h *toastHost) interactiveRects(connector string) []ui.Rect {
	cards := h.cards[connector]
	out := make([]ui.Rect, 0, len(cards))
	for _, card := range cards {
		out = append(out, h.drawnRect(connector, card))
	}
	return out
}

// cardRects lays out the visible ids in output-local coordinates.
func (h *toastHost) cardRects(connector string, ids []uint32) []ui.Rect {
	heights := make([]int, len(ids))
	for i := range ids {
		heights[i] = h.cardHeight(ids[i])
	}
	geometry, known := h.geometryFor(connector)
	if !known {
		return nil
	}
	rects, _ := toastLayout(geometry, heights)
	return rects
}

// viewFor reports one record's placement for the aggregate presentation
// state. The registry's precedence collapses it.
func (h *toastHost) viewFor(id uint32) presentationView {
	v := presentationView{fullscreen: h.fullscreenFocused}
	for connector := range h.outputs {
		for _, vid := range h.visible[connector] {
			if vid == id {
				if h.hovered[connector][id] || (h.replyingOn(connector, id) && h.editing.focused) {
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
