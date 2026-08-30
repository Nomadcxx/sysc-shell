package wayland

import (
	"fmt"

	"github.com/Nomadcxx/sysc-shell/internal/platform/wayland/layershell"
	"github.com/Nomadcxx/sysc-shell/internal/platform/wayland/viewporter"
	"github.com/Nomadcxx/sysc-shell/internal/render"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
	"github.com/Nomadcxx/sysc-wayland/client"
)

// TooltipRequest asks the owner to show a tooltip anchored to a widget, or to
// hide the current one when Text is empty.
//
// The requester never touches a proxy: it sends this and the owner goroutine
// does the Wayland work, which is what keeps the single-owner invariant.
type TooltipRequest struct {
	Global uint32
	Anchor ui.Rect
	Text   string
}

// tooltipGap is the space between the bar edge and the tooltip.
const tooltipGap = 4

const tooltipNamespace = "sysc-shell:tooltip"

// tooltipPlacement positions a tooltip below its anchor, centred on it, and
// clamped fully inside the output.
//
// This is the panel design's D5 rule: anchored off the triggering bar's edge,
// aligned to the triggering widget, clamped inside the output. Tranche 4A
// adopts this rule rather than reconciling two.
func tooltipPlacement(anchor ui.Rect, width, height, outputWidth, outputHeight int) ui.Rect {
	if width > outputWidth {
		width = outputWidth
	}
	x := anchor.X + anchor.W/2 - width/2
	if x+width > outputWidth {
		x = outputWidth - width
	}
	if x < 0 {
		x = 0
	}

	y := anchor.Y + anchor.H + tooltipGap
	if y+height > outputHeight {
		y = outputHeight - height
	}
	if y < 0 {
		y = 0
	}
	return ui.Rect{X: x, Y: y, W: width, H: height}
}

// tooltipLayer is the layer and interactivity a tooltip uses.
//
// Overlay because a fullscreen window hides Top but not Overlay. Keyboard none
// and no dismiss shield because a tooltip is hover-driven: it takes no focus
// and needs no outside-click dismissal, which is the panel design's OSD shape
// rather than its panel shape.
const (
	tooltipLayer         = layershell.ZwlrLayerShellV1LayerOverlay
	tooltipKeyboard      = uint32(layershell.ZwlrLayerSurfaceV1KeyboardInteractivityNone)
	tooltipExclusiveZone = int32(-1)
)

// tooltipPad is the logical inset around the tooltip text.
const tooltipPad = 6

// tooltipSurface is the one process-wide hover surface.
type tooltipSurface struct {
	host     *OutputHost
	surface  *client.Surface
	layer    *layershell.ZwlrLayerSurfaceV1
	viewport *viewporter.WpViewport
	gen      *generation
	place    ui.Rect
	text     string
	scale120 ui.Scale120
}

func (o *owner) handleTooltip(req TooltipRequest) {
	if req.Text == "" {
		o.fail(o.hideTooltip())
		return
	}
	o.fail(o.showTooltip(req))
}

func (o *owner) showTooltip(req TooltipRequest) error {
	if err := o.hideTooltip(); err != nil {
		return err
	}
	h, ok := o.hosts.get(req.Global)
	if !ok || !h.alive || h.proxy == nil {
		return nil
	}

	width, height := o.measureTooltip(req.Text)
	outW := h.ss.logicalWidth
	if outW <= 0 {
		outW = int(h.modeWidth)
	}
	outH := int(h.modeHeight)
	if outH <= 0 {
		outH = 1080
	}
	place := tooltipPlacement(req.Anchor, width, height, outW, outH)
	if place.W <= 0 || place.H <= 0 {
		return nil
	}

	surface, err := o.compositor.CreateSurface()
	if err != nil {
		return fmt.Errorf("wayland: tooltip surface: %w", err)
	}
	layer, err := o.layerShell.GetLayerSurface(surface, h.proxy, uint32(tooltipLayer), tooltipNamespace)
	if err != nil {
		_ = surface.Destroy()
		return fmt.Errorf("wayland: tooltip layer: %w", err)
	}
	if err := layer.SetSize(uint32(place.W), uint32(place.H)); err != nil {
		_ = layer.Destroy()
		_ = surface.Destroy()
		return err
	}
	anchor := uint32(layershell.ZwlrLayerSurfaceV1AnchorTop | layershell.ZwlrLayerSurfaceV1AnchorLeft)
	if err := layer.SetAnchor(anchor); err != nil {
		_ = layer.Destroy()
		_ = surface.Destroy()
		return err
	}
	if err := layer.SetMargin(int32(place.Y), 0, 0, int32(place.X)); err != nil {
		_ = layer.Destroy()
		_ = surface.Destroy()
		return err
	}
	if err := layer.SetExclusiveZone(tooltipExclusiveZone); err != nil {
		_ = layer.Destroy()
		_ = surface.Destroy()
		return err
	}
	if err := layer.SetKeyboardInteractivity(tooltipKeyboard); err != nil {
		_ = layer.Destroy()
		_ = surface.Destroy()
		return err
	}

	viewport, err := o.viewporter.GetViewport(surface)
	if err != nil {
		_ = layer.Destroy()
		_ = surface.Destroy()
		return fmt.Errorf("wayland: tooltip viewport: %w", err)
	}

	tt := &tooltipSurface{
		host:     h,
		surface:  surface,
		layer:    layer,
		viewport: viewport,
		place:    place,
		text:     req.Text,
		scale120: h.ss.scale120,
	}
	if tt.scale120 == 0 {
		tt.scale120 = ui.ScaleUnit
	}
	o.tooltip = tt

	layer.SetConfigureHandler(func(e layershell.ZwlrLayerSurfaceV1ConfigureEvent) {
		if o.tooltip != tt {
			return
		}
		o.fail(o.configureTooltip(tt, e))
	})
	layer.SetClosedHandler(func(layershell.ZwlrLayerSurfaceV1ClosedEvent) {
		if o.tooltip == tt {
			o.fail(o.hideTooltip())
		}
	})

	return surface.Commit()
}

func (o *owner) configureTooltip(tt *tooltipSurface, e layershell.ZwlrLayerSurfaceV1ConfigureEvent) error {
	if err := tt.layer.AckConfigure(e.Serial); err != nil {
		return fmt.Errorf("wayland: tooltip ack: %w", err)
	}
	w, h := int(e.Width), int(e.Height)
	if w <= 0 {
		w = tt.place.W
	}
	if h <= 0 {
		h = tt.place.H
	}
	if err := tt.viewport.SetDestination(int32(w), int32(h)); err != nil {
		return err
	}

	bufW := tt.scale120.Physical(w)
	bufH := tt.scale120.Physical(h)
	gen, err := newGeneration(o.shm, 0, int32(bufW), int32(bufH))
	if err != nil {
		return err
	}
	if tt.gen != nil {
		_ = tt.gen.destroy()
	}
	tt.gen = gen

	if err := o.paintTooltip(tt, gen.pixels(0), bufW, bufH, int(gen.stride)); err != nil {
		return err
	}
	if err := tt.surface.Attach(gen.slots[0], 0, 0); err != nil {
		return err
	}
	if err := tt.surface.DamageBuffer(0, 0, gen.width, gen.height); err != nil {
		return err
	}
	gen.retire.attached()
	return tt.surface.Commit()
}

func (o *owner) paintTooltip(tt *tooltipSurface, pix []byte, width, height, stride int) error {
	c, err := render.NewCanvas(pix, width, height, stride)
	if err != nil {
		return err
	}
	style := render.ProofStyle{
		Size:       14,
		Scale120:   tt.scale120,
		Background: render.Color{R: 0x10, G: 0x14, B: 0x18, A: 0xff},
		Foreground: render.Color{R: 0xe6, G: 0xea, B: 0xef, A: 0xff},
		Body:       ui.Rect{X: 0, Y: 0, W: tt.place.W, H: tt.place.H},
		Radius:     6,
	}
	root := &ui.Node{Kind: ui.KindRow, Bounds: style.Body, Children: []*ui.Node{{
		Kind:   ui.KindText,
		Text:   tt.text,
		Bounds: ui.Rect{X: tooltipPad, Y: tooltipPad, W: tt.place.W - 2*tooltipPad, H: tt.place.H - 2*tooltipPad},
	}}}
	text := o.tooltipText()
	if text == nil {
		clear(pix)
		return nil
	}
	return render.Paint(c, root, text, style)
}

func (o *owner) tooltipText() *render.TextRenderer {
	if o.tooltipRenderer != nil {
		return o.tooltipRenderer
	}
	fonts, err := render.NewSystemFontMap("", render.DefaultFontCacheDir())
	if err != nil {
		return nil
	}
	o.tooltipRenderer = render.NewTextRendererWithFontMap(fonts)
	return o.tooltipRenderer
}

func (o *owner) measureTooltip(text string) (int, int) {
	height := 14 + 2*tooltipPad
	width := 8*len(text) + 2*tooltipPad
	if r := o.tooltipText(); r != nil {
		if w, h, err := r.Measure(text, 14, false); err == nil {
			width, height = w+2*tooltipPad, h+2*tooltipPad
		}
	}
	if width < 16 {
		width = 16
	}
	if height < 16 {
		height = 16
	}
	return width, height
}

func (o *owner) hideTooltip() error {
	tt := o.tooltip
	o.tooltip = nil
	if tt == nil {
		return nil
	}
	var errs []error
	if tt.gen != nil {
		if err := tt.gen.destroy(); err != nil {
			errs = append(errs, err)
		}
	}
	if tt.viewport != nil {
		if err := tt.viewport.Destroy(); err != nil {
			errs = append(errs, err)
		}
	}
	if tt.layer != nil {
		if err := tt.layer.Destroy(); err != nil {
			errs = append(errs, err)
		}
	}
	if tt.surface != nil {
		if err := tt.surface.Destroy(); err != nil {
			errs = append(errs, err)
		}
	}
	if len(errs) == 0 {
		return nil
	}
	return fmt.Errorf("wayland: hide tooltip: %v", errs)
}
