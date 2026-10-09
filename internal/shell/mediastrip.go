package shell

import (
	"log"
	"math"
	"strconv"
	"time"

	"github.com/Nomadcxx/sysc-shell/internal/platform/wayland"
	"github.com/Nomadcxx/sysc-shell/internal/platform/wayland/layershell"
	"github.com/Nomadcxx/sysc-shell/internal/render"
	"github.com/Nomadcxx/sysc-shell/internal/services"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

const (
	mediaStripSurfaceID = "media-strip"
	mediaStripShieldID  = "shield:media-strip"
	mediaStripNamespace = "sysc-shell-media-strip"
	// mediaStripSeekStep is what ← and → move the position by.
	mediaStripSeekStep = 5_000_000
)

// mediaStripHost owns the bar's media control strip: one surface, opened in
// peek mode by hover or pinned by a click. A pinned strip sits above a click
// shield, as the running-app menu does, so a click anywhere else closes it.
// Every field is guarded by Registry.mu.
type mediaStripHost struct {
	r       *Registry
	request func(wayland.AuxRequest)

	open_, pinned bool
	output        uint32
	rootGen       uint64
	place         ui.Rect
	placed        bool

	root     *ui.Node
	focus    []*ui.Node
	roving   ui.Roving
	logicalW int
	logicalH int
	scale120 int
	hoverX   int
	hoverY   int
	pressed  *ui.Node
	pointer  interaction

	seekPending *int64
	lease       *services.Lease
	stopTick    chan struct{}

	text  *render.TextRenderer
	style render.Style
}

func newMediaStripHost(r *Registry, harness *hostHarness) *mediaStripHost {
	h := &mediaStripHost{r: r, pointer: interaction{stateLayer: true}}
	if harness != nil {
		h.request = harness.request
	} else {
		h.request = func(req wayland.AuxRequest) { r.sendAux(req) }
	}
	return h
}

// openLocked opens the strip on output under the pill at local (bar-local).
// An open strip on the same output only upgrades to pinned.
func (h *mediaStripHost) openLocked(output uint32, local ui.Rect, pinned bool) bool {
	if output == 0 || !h.r.mediaState.Available {
		return false
	}
	if h.open_ && h.output == output {
		if pinned && !h.pinned {
			h.pinLocked()
		}
		return true
	}
	h.closeLocked()
	h.open_, h.pinned, h.output = true, pinned, output
	edge, anchor, body, out := h.r.barGeometryOnOutputLocked(output, local)
	connector := ""
	if bar := h.r.bars[output]; bar != nil {
		connector = bar.connector()
	}
	work := barWorkArea(h.r.cfg.ForConnector(connector), out)
	h.place = mediaStripPlacement(edge, anchor, body, work)
	h.placed = h.place.W > 0 && h.place.H > 0
	h.rebuildLocked()
	h.rootGen = h.r.roots.openRoot(mediaStripRoot(output))
	h.r.roots.onClose(h.rootGen, h.releaseForChainClose)
	h.r.dwell.leave()
	if h.r.media != nil {
		if lease, err := h.r.media.Acquire(); err == nil {
			h.lease = lease
		}
	}
	h.startTickLocked()
	if pinned {
		h.request(wayland.AuxRequest{Output: output, Open: h.shieldSpec()})
	}
	h.request(wayland.AuxRequest{Output: output, Open: h.spec()})
	if h.r.mediaIntent != nil {
		h.r.mediaIntent.setShown(true)
	}
	return true
}

// pinLocked turns a peek into a pinned strip. The strip is reopened above a
// new shield, because a surface opened later stacks on top.
func (h *mediaStripHost) pinLocked() {
	h.pinned = true
	h.request(wayland.AuxRequest{Output: h.output, ID: mediaStripSurfaceID})
	h.request(wayland.AuxRequest{Output: h.output, Open: h.shieldSpec()})
	h.request(wayland.AuxRequest{Output: h.output, Open: h.spec()})
}

// togglePinLocked is the pill's left click: pin a peek, open pinned, or
// close a strip that is already pinned on this output.
func (h *mediaStripHost) togglePinLocked(output uint32, local ui.Rect) bool {
	if h.open_ && h.pinned && h.output == output {
		h.closeLocked()
		return true
	}
	return h.openLocked(output, local, true)
}

// closePeekLocked is the hover grace: it never closes a pinned strip.
func (h *mediaStripHost) closePeekLocked() {
	if h.open_ && !h.pinned {
		h.closeLocked()
	}
}

func (h *mediaStripHost) closeLocked() {
	if !h.open_ {
		return
	}
	gen := h.rootGen
	h.releaseForChainClose()
	h.r.roots.closeRoot(gen)
}

// releaseForChainClose runs when another root replaces the strip, and from
// closeLocked. It must be safe to run once per open.
func (h *mediaStripHost) releaseForChainClose() {
	if !h.open_ {
		return
	}
	h.open_ = false
	h.request(wayland.AuxRequest{Output: h.output, ID: mediaStripSurfaceID})
	if h.pinned {
		h.request(wayland.AuxRequest{Output: h.output, ID: mediaStripShieldID})
	}
	h.pinned, h.seekPending, h.pressed = false, nil, nil
	if h.stopTick != nil {
		close(h.stopTick)
		h.stopTick = nil
	}
	if lease := h.lease; lease != nil {
		h.lease = nil
		go lease.Release() // a release can wait on bus enumeration
	}
	if h.r.mediaIntent != nil {
		h.r.mediaIntent.setShown(false)
	}
}

func (h *mediaStripHost) outputLostLocked(global uint32) {
	if h.open_ && h.output == global {
		h.closeLocked()
	}
}

// dropAuxLocked handles a compositor-side close of either surface.
func (h *mediaStripHost) dropAuxLocked(output uint32, surfaceID string) bool {
	if surfaceID != mediaStripSurfaceID && surfaceID != mediaStripShieldID {
		return false
	}
	if h.open_ && h.output == output {
		h.closeLocked()
	}
	return true
}

// refreshLocked rebuilds an open strip against the retained media snapshot.
// A strip whose player has gone closes.
func (h *mediaStripHost) refreshLocked() (uint32, bool) {
	if !h.open_ {
		return 0, false
	}
	if !h.r.mediaState.Available {
		h.closeLocked()
		return 0, false
	}
	h.rebuildLocked()
	return h.output, true
}

func (h *mediaStripHost) rebuildLocked() {
	state, players := mediaBodyStateLocked(h.r)
	position := state.PositionUS
	if h.seekPending != nil {
		position = *h.seekPending
	}
	art := mediaArtImageLocked(h.r, h.output, state.ArtKey, mediaStripArt)
	h.root = mediaStripTree(state, players, position, art)
	h.focus = ui.Focusables(h.root)
	h.roving.Count = len(h.focus)
	if h.logicalW > 0 {
		_ = h.configure(h.logicalW, h.logicalH, h.scale120)
	}
}

// startTickLocked advances the position once a second while playing.
func (h *mediaStripHost) startTickLocked() {
	stop := make(chan struct{})
	h.stopTick = stop
	go func() {
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case <-h.r.closed:
				return
			case <-ticker.C:
				h.r.mu.Lock()
				playing := h.open_ && h.r.mediaState.Status == services.PlaybackPlaying
				if playing {
					h.rebuildLocked()
				}
				out := h.output
				h.r.mu.Unlock()
				if playing {
					h.r.publishSurface(out, mediaStripSurfaceID)
				}
			}
		}
	}()
}

// blur-exempt: a strip, not a panel; it follows the tray drawer's ruling.
func (h *mediaStripHost) spec() *wayland.AuxSpec {
	anchor := uint32(layershell.ZwlrLayerSurfaceV1AnchorTop | layershell.ZwlrLayerSurfaceV1AnchorLeft)
	return &wayland.AuxSpec{
		ID: mediaStripSurfaceID, Namespace: mediaStripNamespace,
		Layer:  layerOverlay,
		Anchor: anchor, MarginTop: int32(h.place.Y), MarginLeft: int32(h.place.X),
		Width: int32(max(h.place.W, mediaStripW)), Height: int32(max(h.place.H, mediaStripH)),
		ExclusiveZone: -1, Keyboard: keyboardOnDemand,
		Callbacks: wayland.HostCallbacks{
			Configure: h.configureLocking, Render: h.renderLocking, Handle: h.handleLocking,
		},
	}
}

// blur-exempt: the shield paints nothing, exactly as the panel shield does.
func (h *mediaStripHost) shieldSpec() *wayland.AuxSpec {
	return &wayland.AuxSpec{
		ID: mediaStripShieldID, Namespace: "sysc-shell-shield", Layer: layerOverlay,
		Anchor: uint32(layershell.ZwlrLayerSurfaceV1AnchorTop | layershell.ZwlrLayerSurfaceV1AnchorBottom |
			layershell.ZwlrLayerSurfaceV1AnchorLeft | layershell.ZwlrLayerSurfaceV1AnchorRight),
		ExclusiveZone: -1, Keyboard: keyboardNone,
		Callbacks: wayland.HostCallbacks{
			Configure: func(int, int, int) error { return nil },
			Render:    func([]byte, int, int, int) error { return nil },
			Handle: func(event wayland.Event) bool {
				h.r.mu.Lock()
				defer h.r.mu.Unlock()
				if h.open_ && event.Kind == wayland.EventPointerPress {
					h.closeLocked()
					return true
				}
				return false
			},
		},
	}
}

func (h *mediaStripHost) configureLocking(width, height, scale120 int) error {
	h.r.mu.Lock()
	defer h.r.mu.Unlock()
	return h.configure(width, height, scale120)
}

func (h *mediaStripHost) renderLocking(pixels []byte, width, height, stride int) error {
	h.r.mu.Lock()
	defer h.r.mu.Unlock()
	return h.render(pixels, width, height, stride)
}

func (h *mediaStripHost) handleLocking(event wayland.Event) bool {
	h.r.mu.Lock()
	defer h.r.mu.Unlock()
	return h.handle(event)
}

func (h *mediaStripHost) configure(width, height, scale120 int) error {
	h.logicalW, h.logicalH, h.scale120 = width, height, scale120
	h.style.Scale120 = ui.Scale120(scale120)
	h.style.Body = ui.Rect{W: width, H: height}
	if h.root == nil {
		return nil
	}
	measure := func(text string, attrs ui.TextAttrs) (int, int) {
		if h.text != nil {
			if w, th, err := h.text.Measure(text, render.SpecFor(h.style, attrs), attrs.Tabular); err == nil {
				return w, th
			}
		}
		return len([]rune(text)) * 8, 16
	}
	return ui.LayoutColumn(h.root, ui.Rect{W: width, H: height}, measure)
}

func (h *mediaStripHost) render(pixels []byte, width, height, stride int) error {
	if h.text == nil {
		fonts, err := render.NewSystemFontMap(h.r.cfg.Bar.FontFamily, render.DefaultFontCacheDir())
		if err != nil {
			return err
		}
		h.text = render.NewTextRendererWithFontMap(fonts)
		scale, body := h.style.Scale120, h.style.Body
		h.style = h.r.surfaceTheme().PanelStyle()
		h.style.Scale120, h.style.Body = scale, body
		if h.logicalW > 0 {
			if err := h.configure(h.logicalW, h.logicalH, h.scale120); err != nil {
				return err
			}
		}
	}
	canvas, err := render.NewCanvas(pixels, width, height, stride)
	if err != nil {
		return err
	}
	return render.Paint(canvas, h.root, h.text, h.style)
}

func (h *mediaStripHost) handle(event wayland.Event) bool {
	switch event.Kind {
	case wayland.EventKeyPress:
		switch event.Key {
		case keyEsc:
			h.closeLocked()
			return true
		case keyTab:
			h.roving.Next()
			return true
		case keySpace:
			return h.activate(&ui.Node{Action: mediaPlayPauseAction})
		case keyEnter:
			if len(h.focus) > 0 {
				return h.activate(h.focus[h.roving.Index()])
			}
		case keyLeft, keyRight:
			state := h.r.mediaState
			if !state.CanSeek || state.LengthUS <= 0 {
				return false
			}
			position := state.PositionUS
			if h.seekPending != nil {
				position = *h.seekPending
			}
			if event.Key == keyLeft {
				position = max(position-mediaStripSeekStep, 0)
			} else {
				position = min(position+mediaStripSeekStep, state.LengthUS)
			}
			return h.activate(&ui.Node{Action: mediaSeekAction + strconv.FormatInt(position, 10)})
		}
	case wayland.EventPointerEnter, wayland.EventPointerMotion:
		if event.Kind == wayland.EventPointerEnter && h.r.mediaIntent != nil {
			h.r.mediaIntent.strip(true)
		}
		h.hoverX, h.hoverY = int(math.Floor(event.X)), int(math.Floor(event.Y))
		if h.pressed != nil && h.pressed.Kind == ui.KindSlider {
			ui.SliderAt(h.pressed, h.hoverX)
			return true
		}
		if h.pointer.setHover(hoverKeyAt(h.root, h.hoverX, h.hoverY)) {
			h.pointer.apply(h.root, nil)
			return true
		}
	case wayland.EventPointerLeave:
		if h.r.mediaIntent != nil {
			h.r.mediaIntent.strip(false)
		}
		h.pressed = nil
		if h.pointer.clear() {
			h.pointer.apply(h.root, nil)
			return true
		}
	case wayland.EventPointerPress:
		h.pressed = h.hitFocusable(h.hoverX, h.hoverY)
		if h.pressed != nil && h.pressed.Kind == ui.KindSlider {
			ui.SliderAt(h.pressed, h.hoverX)
		}
		h.pointer.setPress(h.pressed.StableKey())
		h.pointer.apply(h.root, nil)
		return h.pressed != nil
	case wayland.EventPointerRelease:
		pressed := h.pressed
		h.pressed = nil
		h.pointer.setPress("")
		h.pointer.apply(h.root, nil)
		if pressed == nil {
			return false
		}
		if pressed.Kind == ui.KindSlider || pressed == h.hitFocusable(h.hoverX, h.hoverY) {
			return h.activate(pressed)
		}
		return true
	}
	return false
}

func (h *mediaStripHost) hitFocusable(x, y int) *ui.Node {
	for i := len(h.focus) - 1; i >= 0; i-- {
		if h.focus[i].Bounds.Contains(x, y) {
			return h.focus[i]
		}
	}
	return nil
}

// activate runs one control through the shared media vocabulary. The bus
// write runs off the owner; the strip rebuilds and repaints when it lands.
func (h *mediaStripHost) activate(n *ui.Node) bool {
	if n == nil || n.State.Has(ui.StateDisabled) {
		return false
	}
	run, seekTo, ok := mediaControl(h.r.media, n)
	if !ok {
		return false
	}
	if seekTo != nil {
		value := *seekTo
		h.seekPending = &value
		h.rebuildLocked()
	}
	go func() {
		if err := run(); err != nil {
			log.Printf("shell: media strip %s: %v", n.Action, err)
		}
		h.r.mu.Lock()
		if seekTo != nil && h.seekPending != nil && *h.seekPending == *seekTo {
			h.seekPending = nil
		}
		out, open := h.output, h.open_
		if open {
			h.rebuildLocked()
		}
		h.r.mu.Unlock()
		if open {
			h.r.publishSurface(out, mediaStripSurfaceID)
		}
	}()
	return true
}
