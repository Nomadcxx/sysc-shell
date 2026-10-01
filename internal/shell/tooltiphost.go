package shell

import (
	"fmt"

	"github.com/Nomadcxx/sysc-shell/internal/platform/wayland"
	"github.com/Nomadcxx/sysc-shell/internal/platform/wayland/layershell"
	"github.com/Nomadcxx/sysc-shell/internal/render"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

// tooltipRequest asks for a tooltip anchored to a bar widget or a panel
// node, or for the current one to go when Text and Root are both empty.
// Anchor is in the bar surface's logical coordinates unless OnOutput says it
// is already on the output, as a panel node's is.
type tooltipRequest struct {
	Global uint32
	Anchor ui.Rect
	Text   string
	// Root is a structured read-only tree; when set it is shown instead of
	// Text.
	Root     *ui.Node
	OnOutput bool
}

func (q tooltipRequest) empty() bool { return q.Text == "" && q.Root == nil }

const tooltipNamespace = "sysc-shell-tooltip"

// tooltipHost owns the one process-wide hover card as an aux surface, the way
// the toast and OSD hosts own theirs (plan T1). The shape is decision D6's:
// Overlay, so a fullscreen window does not hide it; zone -1; no keyboard and
// no dismiss shield, because hovering drives it.
//
// Every field is guarded by Registry.mu, like the other hosts.
type tooltipHost struct {
	r *Registry
	// request emits aux requests and publish asks for a repaint; tests
	// capture the first and drop the second.
	request    func(wayland.AuxRequest)
	publish    func(global uint32, id string)
	harnessRef *hostHarness

	// seq numbers the surfaces, so a compositor's report that one closed
	// cannot be mistaken for the one that replaced it.
	seq int
	// open is the card on screen, nil when none is.
	open *tooltipCardState

	text   *render.TextRenderer
	family string
}

// tooltipCardState is the card on screen and what it was built against.
type tooltipCardState struct {
	id     string
	global uint32
	card   *ui.Node
	place  ui.Rect
	// glass is whether the compositor blurs behind it; without that the card
	// paints the overlay ground over backdrop, captured before it mapped.
	glass    bool
	backdrop *ui.Image
	scale120 ui.Scale120
}

func newTooltipHost(r *Registry, harness *hostHarness) *tooltipHost {
	h := &tooltipHost{r: r}
	if harness != nil {
		h.request = harness.request
		h.publish = func(uint32, string) {}
		h.harnessRef = harness
	} else {
		h.request = r.sendAux
		h.publish = r.publishSurface
	}
	return h
}

// show opens the card for req. It takes Registry.mu itself and sends after
// releasing it, so it must be called without the lock.
func (h *tooltipHost) show(req tooltipRequest) {
	h.r.mu.Lock()
	reqs := h.showLocked(req)
	h.r.mu.Unlock()
	for _, q := range reqs {
		h.request(q)
		// A resize is answered by a configure, which repaints; a card that
		// kept its size still holds new content.
		if q.Update != nil {
			h.publish(q.Output, q.ID)
		}
	}
}

func (h *tooltipHost) showLocked(req tooltipRequest) []wayland.AuxRequest {
	bar := h.r.bars[req.Global]
	if bar == nil || req.empty() {
		return h.hideLocked()
	}
	t := h.r.panelThemeFor(req.Global)
	scale := ui.Scale120(bar.scale120())
	if !scale.Valid() {
		scale = ui.ScaleUnit
	}
	if err := h.ensureText(t.Type.Family); err != nil {
		return h.hideLocked()
	}
	card, size := tooltipCard(req.Text, req.Root, h.measure(t, scale))
	if card == nil || size.W <= 0 || size.H <= 0 {
		return h.hideLocked()
	}

	// Widget bounds are in the bar surface; placement is in the output.
	policy := h.r.cfg.ForConnector(bar.connector())
	outW, outH := bar.outputSize()
	if outW <= 0 || outH <= 0 {
		outW, outH = 1920, 1080
	}
	anchor := req.Anchor
	if policy.Edge == "bottom" && !req.OnOutput {
		_, barH := bar.configuredSize()
		if barH <= 0 {
			barH = policy.Extent()
		}
		anchor.Y += outH - barH
	}
	place := tooltipPlacement(policy.Edge, anchor, size.W, size.H, outW, outH)

	glass := h.r.cfg.Theme.BlurBehind && h.r.caps.Blur

	// Sweeping along the bar moves the card in place (plan T7). A card over
	// a captured backdrop cannot move: the capture belongs to where it was,
	// and a fresh one has to be taken before the surface maps.
	if st := h.open; st != nil && st.global == req.Global && st.glass && glass {
		st.card, st.place = card, place
		w, hgt := uint32(place.W), uint32(place.H)
		top, left := int32(place.Y), int32(place.X)
		return []wayland.AuxRequest{{Output: st.global, ID: st.id, Update: &wayland.AuxUpdate{
			Width: &w, Height: &hgt, MarginTop: &top, MarginLeft: &left,
		}}}
	}

	reqs := h.hideLocked()
	h.seq++
	st := &tooltipCardState{
		id:       fmt.Sprintf("tooltip:%d", h.seq),
		global:   req.Global,
		card:     card,
		place:    place,
		glass:    glass,
		scale120: scale,
	}
	h.open = st
	return append(reqs, wayland.AuxRequest{Output: req.Global, Open: h.spec(st)})
}

// hide closes the card on screen, if there is one. Called without the lock.
func (h *tooltipHost) hide() {
	h.r.mu.Lock()
	reqs := h.hideLocked()
	h.r.mu.Unlock()
	for _, q := range reqs {
		h.request(q)
	}
}

func (h *tooltipHost) hideLocked() []wayland.AuxRequest {
	st := h.open
	if st == nil {
		return nil
	}
	h.open = nil
	return []wayland.AuxRequest{{Output: st.global, ID: st.id}}
}

// drop forgets a card the owner reports gone -- the compositor closed it, or
// its output left -- so the next hover opens afresh instead of updating a
// surface that no longer exists. A report for a card already replaced leaves
// its successor alone. Caller holds r.mu.
func (h *tooltipHost) drop(output uint32, id string) {
	if st := h.open; st != nil && st.global == output && st.id == id {
		h.open = nil
	}
}

// outputLost forgets a card on an output that has gone; the owner tore its
// surfaces down with it. Caller holds r.mu.
func (h *tooltipHost) outputLost(global uint32) {
	if st := h.open; st != nil && st.global == global {
		h.open = nil
	}
}

func (h *tooltipHost) spec(st *tooltipCardState) *wayland.AuxSpec {
	var capture *ui.Rect
	if h.r.cfg.Theme.BlurBehind && !st.glass {
		region := st.place
		capture = &region
	}
	return &wayland.AuxSpec{
		ID:        st.id,
		Namespace: tooltipNamespace,
		Layer:     layershell.ZwlrLayerShellV1LayerOverlay,
		Anchor: uint32(layershell.ZwlrLayerSurfaceV1AnchorTop |
			layershell.ZwlrLayerSurfaceV1AnchorLeft),
		MarginTop:     int32(st.place.Y),
		MarginLeft:    int32(st.place.X),
		Width:         int32(st.place.W),
		Height:        int32(st.place.H),
		ExclusiveZone: -1,
		Keyboard:      keyboardNone,
		// A hover card takes no pointer input: the pointer passes through to
		// whatever is under it.
		InputRects: []ui.Rect{},
		BlurRegion: capture,
		BlurRadius: h.r.cfg.Theme.BlurRadius,
		Callbacks: wayland.HostCallbacks{
			Configure: func(_, _, scale120 int) error {
				h.r.mu.Lock()
				defer h.r.mu.Unlock()
				if s := ui.Scale120(scale120); s.Valid() && h.open == st {
					st.scale120 = s
				}
				return nil
			},
			Render: func(pixels []byte, width, height, stride int) error {
				h.r.mu.Lock()
				defer h.r.mu.Unlock()
				return h.render(st, pixels, width, height, stride)
			},
			Handle: func(wayland.Event) bool { return false },
			Backdrop: func(img *ui.Image) {
				h.r.mu.Lock()
				defer h.r.mu.Unlock()
				st.backdrop = img
			},
			BlurShape: func() []ui.Rect {
				h.r.mu.Lock()
				defer h.r.mu.Unlock()
				if !st.glass || h.open != st {
					return nil
				}
				return ui.BlurStrips(ui.SurfaceShape{Body: ui.Rect{W: st.place.W, H: st.place.H}, Radius: h.cardStyle().Radius})
			},
		},
	}
}

// cardStyle is the ground the card paints on (plan T2, T5): the toast card
// rule for the ground, the small corner role, and the quieter outline for a
// rim around a small box. Caller holds r.mu.
func (h *tooltipHost) cardStyle() render.Style {
	global := uint32(0)
	glass := h.r.cfg.Theme.BlurBehind && h.r.caps.Blur
	if h.open != nil {
		global, glass = h.open.global, h.open.glass
	}
	t := h.r.panelThemeFor(global)
	s := t.OverlayStyle()
	if glass {
		s = t.PanelStyle()
	}
	s.Radius = t.Shapes.Small
	s.Rim = t.OutlineVariant
	return s
}

func (h *tooltipHost) render(st *tooltipCardState, pixels []byte, width, height, stride int) error {
	if h.open != st {
		clear(pixels)
		return nil
	}
	c, err := render.NewCanvas(pixels, width, height, stride)
	if err != nil {
		return err
	}
	style := h.cardStyle()
	style.Scale120 = st.scale120
	style.Body = ui.Rect{W: st.place.W, H: st.place.H}
	if !st.glass {
		style.Backdrop = st.backdrop
	}
	return render.Paint(c, st.card, h.text, style)
}

// ensureText loads the font map for family once; a family change reloads it.
func (h *tooltipHost) ensureText(family string) error {
	if h.text != nil && h.family == family {
		return nil
	}
	fonts, err := render.NewSystemFontMap(family, render.DefaultFontCacheDir())
	if err != nil {
		return err
	}
	h.text, h.family = render.NewTextRendererWithFontMap(fonts), family
	return nil
}

// measure resolves each node's role through the shell's type table and
// measures at the output's scale, rounding up into logical pixels: a shaped
// run does not scale linearly, so a 1x measure could reserve less room than
// the painted run takes.
func (h *tooltipHost) measure(t Theme, scale ui.Scale120) ui.MeasureText {
	style := t.Style()
	style.Scale120 = scale
	return func(text string, attrs ui.TextAttrs) (int, int) {
		spec := render.SpecFor(style, attrs)
		if w, hgt, err := h.text.Measure(text, spec, attrs.Tabular); err == nil {
			return scale.Logical(w), scale.Logical(hgt)
		}
		return len([]rune(text)) * 8, 16
	}
}
