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

	// geometry is the size each output's surface was configured at. Until a
	// configure arrives an output has none, and the design default stands in.
	geometry map[string]toastGeometry
	scale120 map[string]int
	measured map[string]bool
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

	anim      *animator
	shown     map[string]map[uint32]ui.Rect
	from      map[string]map[uint32]ui.Rect
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
		r:        r,
		outputs:  map[string]uint32{},
		visible:  map[string][]uint32{},
		queued:   map[string][]uint32{},
		hovered:  map[string]map[uint32]bool{},
		expanded: map[uint32]bool{},
		shown:    map[string]map[uint32]ui.Rect{},
		from:     map[string]map[uint32]ui.Rect{},
		geometry: map[string]toastGeometry{},
		scale120: map[string]int{},
		measured: map[string]bool{},
		cards:    map[string][]toastCard{},
		pointer:  map[string]ui.Rect{},
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

// restyleLocked rebinds the cached layout style to the published palette and
// relayouts every output. Called with Registry.mu held, from the retheme pass.
func (h *toastHost) restyleLocked() { h.recompute() }

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
	delete(h.outputs, connector)
	delete(h.visible, connector)
	delete(h.queued, connector)
	delete(h.hovered, connector)
	delete(h.geometry, connector)
	delete(h.scale120, connector)
	delete(h.measured, connector)
	delete(h.cards, connector)
	delete(h.pointer, connector)
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

// spec describes one output's toast surface. It is anchored to all four edges
// so the compositor reports the output's own logical size, which is what the
// stack lays out against; the input region is narrowed to the visible cards
// immediately afterwards, so the rest of the output still takes clicks.
// Each card blurs through the compositor: BlurShape hands it the cards'
// silhouettes, so nothing is captured and only the cards blur, not the
// output the surface spans (the cost design D13 exempted toasts over).
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

// configure records the output's real logical size and relays out the stack
// against it. Nothing is placed before it arrives, because the only honest
// answer to "how wide is this output" until then is that we do not know.
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
					InputRects:     toastInputRegion(h.interactiveRects(connector)),
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

	// The surface covers the whole output and the cards are separate bodies on
	// it, which the painter cannot express in one pass: it fills exactly one
	// rounded body and clears everything outside it. So the surface is cleared
	// here and each card is painted into its own buffer and copied in, which
	// leaves the gaps and the rest of the output transparent.
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
	h.editing.focused = false
	h.setKeyboard(h.editing.connector, keyboardNone)
	h.repaint(h.editing.connector)
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
func (h *toastHost) openLink(string)    {}

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
	rects := h.cardRects(connector, ids)
	var moved bool
	if h.text != nil {
		moved = h.noteTargets(connector, ids[:min(len(ids), len(rects))], rects)
	}
	if moved && !h.sliding && h.anim != nil && !h.anim.reduced && !h.anim.Settled() {
		h.sliding = true
		h.stopSlide = make(chan struct{})
		stop := h.stopSlide
		frameCap := h.anim.frameCap()
		go animateSurface(stop, func() bool {
			h.r.mu.Lock()
			defer h.r.mu.Unlock()
			settled := h.anim == nil || h.anim.Settled()
			if settled {
				h.sliding = false
				h.stopSlide = nil
			}
			return settled
		}, h.publishSlideFrame, func() time.Duration { return frameCap })
	}
	measure := h.measureText()
	cards := make([]toastCard, 0, len(ids))
	for i, id := range ids {
		if i >= len(rects) {
			break
		}
		n, ok := h.record(id)
		if !ok {
			continue
		}
		root := h.cardOf(n)
		if err := ui.LayoutColumn(root, ui.Rect{W: rects[i].W, H: rects[i].H}, measure); err != nil {
			continue
		}
		if h.replyingOn(connector, id) {
			h.showReply(root, id, measure)
		}
		cards = append(cards, toastCard{root: root, rect: rects[i], critical: n.Urgency == protocol.UrgencyCritical})
	}
	h.cards[connector] = cards
}

// publishSlideFrame repaints every toast surface and moves its input region
// onto the rectangles drawn this frame. The slide continues after recompute,
// and the compositor delivers clicks to the region it was last given.
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
			rects:  toastInputRegion(h.interactiveRects(connector)),
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

// displayRect is where one output draws a card this frame: on its way from
// its old slot to the new one, or in its slot once settled.
func (h *toastHost) displayRect(connector string, id uint32, target ui.Rect) ui.Rect {
	from, ok := h.from[connector][id]
	if !ok || h.anim == nil || h.anim.reduced {
		return target
	}
	p := h.anim.Value(toastSlideKey(connector, id), animVisible)
	if p >= 1 {
		delete(h.from[connector], id)
		return target
	}
	return ui.LerpRect(from, target, p)
}

// noteTargets tracks the visible cards' target rectangles per output. Moved
// cards restart from where they are drawn so a second change never jumps.
func (h *toastHost) noteTargets(connector string, ids []uint32, rects []ui.Rect) (moved bool) {
	shown := h.shown[connector]
	if shown == nil {
		shown = make(map[uint32]ui.Rect, len(ids))
		h.shown[connector] = shown
	}
	from := h.from[connector]
	if from == nil {
		from = make(map[uint32]ui.Rect)
		h.from[connector] = from
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
				from[id] = h.displayRect(connector, id, old)
				key := toastSlideKey(connector, id)
				h.anim.Reset(key, animVisible)
				h.anim.Target(key, animVisible, 1)
			} else {
				delete(from, id)
			}
		}
		shown[id] = rects[i]
	}
	for id := range shown {
		if !seen[id] {
			delete(shown, id)
			delete(from, id)
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
	icon := h.r.notifyIcon(n.AppIcon, n.DesktopEntry)
	var wrap func(string) []string
	if h.expanded[n.ID] {
		wrap = h.wrapBody
	}
	return notificationCard(n, icon, h.r.linksAllowed(), h.measureText(), wrap, time.Now(), h.cardWidth())
}

// cardWidth is the width toast cards lay out at: the design width, or
// narrower where an open output clamps it. Card trees are shared by every
// output, so they fit the narrowest. Caller holds r.mu.
func (h *toastHost) cardWidth() int {
	width := toastCardWidth
	for _, g := range h.geometry {
		if w := g.OutputW - 2*toastMargin; w > 0 && w < width {
			width = w
		}
	}
	return width
}

func (h *toastHost) wrapBody(s string) []string {
	measure := h.measureText()
	width := h.cardWidth() - 2*cardPadding - cardIconSize - cardLeadGap
	return wrapLines(s, width, func(text string) int {
		w, _ := measure(text, ui.TextAttrs{})
		return w
	}, 8)
}

// recompute relayouts every open output from the current projection and
// publishes each surface's input region. It is safe to call on any record,
// geometry, or output change.
func (h *toastHost) recompute() {
	s := h.r.notify
	s.mu.Lock()
	suppressed := s.dndActiveLocked(h.r.clockNow()) || s.centerOpen
	records := make([]uint32, 0, len(s.active))
	active := make(map[uint32]struct{}, len(s.active))
	for id := range s.active {
		records = append(records, id)
		active[id] = struct{}{}
	}
	s.mu.Unlock()
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
		if bar, ok := h.r.bars[global]; ok {
			geom.BarZone = exclusiveBarZone(bar)
		} else {
			geom.BarZone = 0
		}
		// An unmeasured output keeps no geometry: writing one back would make
		// the placeholder indistinguishable from a real measurement.
		if known {
			h.geometry[connector] = geom
		}
		heights := make([]int, 0, len(records))
		ids := make([]uint32, 0, len(records))
		if !suppressed && known {
			for _, id := range records {
				heights = append(heights, h.cardHeight(id))
				ids = append(ids, id)
			}
		}
		visible, queued := placeIDs(ids, heights, geom)
		h.visible[connector] = visible
		h.queued[connector] = queued
		// A reply ends with its card: the record closed, or the stack was
		// suppressed or pushed it into the queue.
		if h.editing != nil && h.editing.connector == connector && !slices.Contains(visible, h.resolver.replyID) {
			h.endReply()
		}
		h.rebuild(connector)
		h.updateHover(connector)

		h.request(wayland.AuxRequest{
			Output: global,
			ID:     toastSurfaceID(connector),
			Update: &wayland.AuxUpdate{
				SetInputRegion: true,
				InputRects:     toastInputRegion(h.interactiveRects(connector)),
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

// cardRects lays out the visible ids for one output and returns their rects.
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
	v := presentationView{}
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
