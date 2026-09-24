package shell

import (
	"fmt"
	"image"
	"net/url"
	"time"

	"github.com/Nomadcxx/sysc-shell/internal/platform/wayland"
	"github.com/Nomadcxx/sysc-shell/internal/platform/wayland/layershell"
	"github.com/Nomadcxx/sysc-shell/internal/render"
	"github.com/Nomadcxx/sysc-shell/internal/services"
	"github.com/Nomadcxx/sysc-shell/internal/theme"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
	"github.com/Nomadcxx/sysc-shell/internal/wallpaper"
)

const (
	depthClockNamespace = "sysc-wallpaper-depth-clock"
	depthClockWidth     = 560
	depthClockHeight    = 176
)

type depthClockDescriptor struct {
	owner, wallpaperPath, maskPath string
	mask                           *image.Alpha
}

type depthClockSurface struct {
	global                    uint32
	width, height             int
	outputWidth, outputHeight int
	scale120                  int
	descriptor                depthClockDescriptor
	now                       time.Time
	tree                      *ui.Node
}

type depthClockHost struct {
	r        *Registry
	request  func(wayland.AuxRequest)
	surfaces map[string]*depthClockSurface
	text     *render.TextRenderer
}

type depthClockEffects struct {
	requests      []wayland.AuxRequest
	invalidations []wayland.Invalidation
	release       *services.Lease
}

func newDepthClockHost(r *Registry, harness *hostHarness) *depthClockHost {
	h := &depthClockHost{r: r, surfaces: make(map[string]*depthClockSurface)}
	if harness != nil {
		h.request = harness.request
	} else {
		h.request = func(req wayland.AuxRequest) { r.sendAux(req) }
	}
	return h
}

func depthClockSurfaceID(connector string) string { return "depth-clock:" + url.PathEscape(connector) }

func depthClockBoxSize(outputWidth, outputHeight int) (int, int) {
	if outputWidth <= 0 {
		outputWidth = depthClockWidth
	}
	if outputHeight <= 0 {
		outputHeight = depthClockHeight
	}
	return min(depthClockWidth, outputWidth), min(depthClockHeight, outputHeight)
}

// set is the test and shell boundary for installing one already-validated
// descriptor. Registry state changes under mu; aux work follows the unlock.
func (h *depthClockHost) set(connector string, descriptor depthClockDescriptor) error {
	h.r.mu.Lock()
	effects, err := h.setLocked(connector, descriptor)
	h.r.mu.Unlock()
	h.emit(effects)
	return err
}

func (h *depthClockHost) setLocked(connector string, descriptor depthClockDescriptor) (depthClockEffects, error) {
	if descriptor.owner == "" || descriptor.mask == nil || descriptor.mask.Bounds().Empty() {
		return depthClockEffects{}, fmt.Errorf("shell: invalid depth-clock descriptor")
	}
	globals := h.r.outputGlobalsLocked()
	global, ok := globals[connector]
	if !ok {
		return depthClockEffects{}, fmt.Errorf("shell: depth-clock output %q is unavailable", connector)
	}
	surface, exists := h.surfaces[connector]
	if exists && surface.descriptor.owner != descriptor.owner {
		return depthClockEffects{}, fmt.Errorf("shell: depth-clock output %q belongs to another plugin", connector)
	}
	if h.r.depthClockLease == nil {
		lease, err := h.r.clock.Acquire(time.Minute)
		if err != nil {
			return depthClockEffects{}, err
		}
		h.r.depthClockLease = lease
	}

	effects := depthClockEffects{}
	if !exists {
		surface = &depthClockSurface{}
		h.surfaces[connector] = surface
	} else if surface.global != global {
		effects.requests = append(effects.requests, wayland.AuxRequest{Output: surface.global, ID: depthClockSurfaceID(connector)})
	}
	outputWidth, outputHeight := h.barOutputSizeLocked(connector, global)
	surface.global = global
	surface.outputWidth, surface.outputHeight = outputWidth, outputHeight
	surface.width, surface.height = depthClockBoxSize(outputWidth, outputHeight)
	surface.scale120 = h.outputScaleLocked(global)
	surface.descriptor = descriptor
	surface.now = h.clockNowLocked()
	surface.tree = depthClockTree(surface.now)
	if !exists || len(effects.requests) > 0 {
		effects.requests = append(effects.requests, wayland.AuxRequest{
			Output: global,
			ID:     depthClockSurfaceID(connector),
			Open:   h.spec(connector),
		})
	} else {
		effects.invalidations = append(effects.invalidations, wayland.Invalidation{Global: global, SurfaceID: depthClockSurfaceID(connector)})
	}
	return effects, nil
}

func (h *depthClockHost) clear(connector, owner string) {
	h.r.mu.Lock()
	effects := h.clearLocked(connector, owner)
	h.r.mu.Unlock()
	h.emit(effects)
}

func (h *depthClockHost) clearLocked(connector, owner string) depthClockEffects {
	surface, ok := h.surfaces[connector]
	if !ok || owner == "" || surface.descriptor.owner != owner {
		return depthClockEffects{}
	}
	delete(h.surfaces, connector)
	effects := depthClockEffects{requests: []wayland.AuxRequest{{Output: surface.global, ID: depthClockSurfaceID(connector)}}}
	if len(h.surfaces) == 0 {
		effects.release = h.r.depthClockLease
		h.r.depthClockLease = nil
	}
	return effects
}

func (h *depthClockHost) clearOwner(owner string) {
	h.r.mu.Lock()
	effects := depthClockEffects{}
	for connector, surface := range h.surfaces {
		if surface.descriptor.owner == owner {
			effects = mergeDepthClockEffects(effects, h.clearLocked(connector, owner))
		}
	}
	h.r.mu.Unlock()
	h.emit(effects)
}

func (h *depthClockHost) syncOutputs(globals map[string]uint32) {
	h.r.mu.Lock()
	effects := h.syncOutputsLocked(globals)
	h.r.mu.Unlock()
	h.emit(effects)
}

func (h *depthClockHost) syncOutputsLocked(globals map[string]uint32) depthClockEffects {
	effects := depthClockEffects{}
	for connector, surface := range h.surfaces {
		global, ok := globals[connector]
		if !ok {
			effects = mergeDepthClockEffects(effects, h.clearLocked(connector, surface.descriptor.owner))
			continue
		}
		outputWidth, outputHeight := h.barOutputSizeLocked(connector, global)
		if surface.global != global {
			effects.requests = append(effects.requests, wayland.AuxRequest{Output: surface.global, ID: depthClockSurfaceID(connector)})
			surface.global = global
			surface.outputWidth, surface.outputHeight = outputWidth, outputHeight
			surface.width, surface.height = depthClockBoxSize(outputWidth, outputHeight)
			surface.scale120 = h.outputScaleLocked(global)
			effects.requests = append(effects.requests, wayland.AuxRequest{Output: global, ID: depthClockSurfaceID(connector), Open: h.spec(connector)})
			continue
		}
		if surface.outputWidth != outputWidth || surface.outputHeight != outputHeight {
			surface.outputWidth, surface.outputHeight = outputWidth, outputHeight
			width, height := depthClockBoxSize(outputWidth, outputHeight)
			if surface.width != width || surface.height != height {
				surface.width, surface.height = width, height
				w, hgt := uint32(width), uint32(height)
				effects.requests = append(effects.requests, wayland.AuxRequest{
					Output: global, ID: depthClockSurfaceID(connector),
					Update: &wayland.AuxUpdate{Width: &w, Height: &hgt},
				})
			}
			effects.invalidations = append(effects.invalidations, wayland.Invalidation{Global: global, SurfaceID: depthClockSurfaceID(connector)})
		}
	}
	if len(h.surfaces) == 0 && h.r.depthClockLease != nil {
		effects.release = h.r.depthClockLease
		h.r.depthClockLease = nil
	}
	return effects
}

func (h *depthClockHost) outputSize(connector string, global uint32, width, height int) {
	h.r.mu.Lock()
	effects := h.outputSizeLocked(connector, global, width, height)
	h.r.mu.Unlock()
	h.emit(effects)
}

func (h *depthClockHost) outputSizeLocked(connector string, global uint32, width, height int) depthClockEffects {
	surface, ok := h.surfaces[connector]
	if !ok || surface.global != global || width <= 0 || height <= 0 || (surface.outputWidth == width && surface.outputHeight == height) {
		return depthClockEffects{}
	}
	surface.outputWidth, surface.outputHeight = width, height
	newWidth, newHeight := depthClockBoxSize(width, height)
	effects := depthClockEffects{invalidations: []wayland.Invalidation{{Global: surface.global, SurfaceID: depthClockSurfaceID(connector)}}}
	if surface.width != newWidth || surface.height != newHeight {
		surface.width, surface.height = newWidth, newHeight
		w, hgt := uint32(newWidth), uint32(newHeight)
		effects.requests = append(effects.requests, wayland.AuxRequest{
			Output: surface.global, ID: depthClockSurfaceID(connector),
			Update: &wayland.AuxUpdate{Width: &w, Height: &hgt},
		})
	}
	return effects
}

func (h *depthClockHost) configure(connector string, global uint32, width, height, scale120 int) error {
	if width <= 0 || height <= 0 || scale120 <= 0 {
		return fmt.Errorf("shell: invalid depth-clock configure %dx%d at scale %d", width, height, scale120)
	}
	h.r.mu.Lock()
	surface := h.surfaces[connector]
	var request *wayland.AuxRequest
	if surface != nil && surface.global == global {
		surface.width, surface.height, surface.scale120 = width, height, scale120
		req := wayland.AuxRequest{
			Output: surface.global,
			ID:     depthClockSurfaceID(connector),
			Update: &wayland.AuxUpdate{SetInputRegion: true, InputRects: []ui.Rect{}},
		}
		request = &req
	}
	h.r.mu.Unlock()
	if request != nil {
		h.request(*request)
	}
	return nil
}

func (h *depthClockHost) spec(connector string) *wayland.AuxSpec {
	surface := h.surfaces[connector]
	global := surface.global
	// blur-exempt: wallpaper depth keeps the desktop clock crisp.
	return &wayland.AuxSpec{
		ID:            depthClockSurfaceID(connector),
		Namespace:     depthClockNamespace,
		Layer:         layershell.ZwlrLayerShellV1LayerBottom,
		Width:         int32(surface.width),
		Height:        int32(surface.height),
		ExclusiveZone: -1,
		Keyboard:      keyboardNone,
		Callbacks: wayland.HostCallbacks{
			Configure: func(width, height, scale120 int) error {
				return h.configure(connector, global, width, height, scale120)
			},
			Render: func(pixels []byte, width, height, stride int) error {
				return h.render(connector, global, pixels, width, height, stride)
			},
			Handle: func(wayland.Event) bool { return false },
		},
	}
}

func (h *depthClockHost) render(connector string, global uint32, pixels []byte, width, height, stride int) error {
	h.r.mu.Lock()
	defer h.r.mu.Unlock()
	surface := h.surfaces[connector]
	if surface == nil || surface.global != global || surface.descriptor.mask == nil {
		clear(pixels)
		return nil
	}
	if h.text == nil {
		fonts, err := render.NewSystemFontMap(h.r.cfg.Bar.FontFamily, render.DefaultFontCacheDir())
		if err != nil {
			return err
		}
		h.text = render.NewTextRendererWithFontMap(fonts)
	}
	style := h.r.surfaceTheme().OverlayStyle()
	style.Scale120 = ui.Scale120(max(surface.scale120, int(ui.ScaleUnit)))
	style.Body = ui.Rect{W: surface.width, H: surface.height}
	canvas, err := render.NewCanvas(pixels, width, height, stride)
	if err != nil {
		return err
	}
	if surface.tree == nil {
		surface.tree = depthClockTree(surface.now)
	}
	measure := func(text string, attrs ui.TextAttrs) (int, int) {
		w, height, err := h.text.Measure(text, render.SpecFor(style, attrs), attrs.Tabular)
		if err != nil {
			return 0, 0
		}
		return style.Scale120.Logical(w), style.Scale120.Logical(height)
	}
	if err := ui.LayoutColumn(surface.tree, ui.Rect{W: surface.width, H: surface.height}, measure); err != nil {
		return err
	}
	if err := render.Paint(canvas, surface.tree, h.text, style); err != nil {
		return err
	}
	maskBounds := surface.descriptor.mask.Bounds()
	geometry := wallpaper.DepthGeometry{
		Mode:          h.r.cfg.Wallpaper.Scale,
		Scale120:      int(style.Scale120),
		SurfaceX:      (surface.outputWidth - surface.width) / 2,
		SurfaceY:      (surface.outputHeight - surface.height) / 2,
		SurfaceWidth:  surface.width,
		SurfaceHeight: surface.height,
		OutputWidth:   surface.outputWidth,
		OutputHeight:  surface.outputHeight,
		ImageWidth:    maskBounds.Dx(),
		ImageHeight:   maskBounds.Dy(),
	}
	return wallpaper.ApplyDepthMask(pixels, width, height, stride, surface.descriptor.mask, geometry)
}

func (h *depthClockHost) updateClockLocked(now time.Time) depthClockEffects {
	effects := depthClockEffects{}
	for connector, surface := range h.surfaces {
		surface.now = now
		surface.tree = depthClockTree(now)
		effects.invalidations = append(effects.invalidations, wayland.Invalidation{
			Global: surface.global, SurfaceID: depthClockSurfaceID(connector),
		})
	}
	return effects
}

func (h *depthClockHost) reconfigureLocked(fontChanged bool) depthClockEffects {
	if fontChanged {
		h.text = nil
	}
	return h.updateClockLocked(h.clockNowLocked())
}

func (h *depthClockHost) closeLocked() depthClockEffects {
	effects := depthClockEffects{}
	for connector, surface := range h.surfaces {
		effects.requests = append(effects.requests, wayland.AuxRequest{Output: surface.global, ID: depthClockSurfaceID(connector)})
	}
	h.surfaces = make(map[string]*depthClockSurface)
	effects.release = h.r.depthClockLease
	h.r.depthClockLease = nil
	return effects
}

func (h *depthClockHost) emit(effects depthClockEffects) {
	for _, request := range effects.requests {
		h.request(request)
	}
	for _, invalidation := range effects.invalidations {
		h.r.publishSurface(invalidation.Global, invalidation.SurfaceID)
	}
	if effects.release != nil {
		effects.release.Release()
	}
}

func mergeDepthClockEffects(a, b depthClockEffects) depthClockEffects {
	a.requests = append(a.requests, b.requests...)
	a.invalidations = append(a.invalidations, b.invalidations...)
	if b.release != nil {
		a.release = b.release
	}
	return a
}

func (h *depthClockHost) barOutputSizeLocked(connector string, global uint32) (int, int) {
	if bar := h.r.bars[global]; bar != nil && bar.connector() == connector {
		width, height := bar.outputSize()
		if width > 0 && height > 0 {
			return width, height
		}
	}
	return depthClockWidth, depthClockHeight
}

func (h *depthClockHost) outputScaleLocked(global uint32) int {
	if bar := h.r.bars[global]; bar != nil {
		if scale := bar.scale120(); scale > 0 {
			return scale
		}
	}
	return int(ui.ScaleUnit)
}

func (h *depthClockHost) clockNowLocked() time.Time {
	if h.r.now.IsZero() {
		return time.Now()
	}
	return h.r.now
}

func depthClockTree(now time.Time) *ui.Node {
	return &ui.Node{
		Kind:    ui.KindColumn,
		Padding: theme.MarginM,
		Children: []*ui.Node{{
			Kind:    ui.KindColumn,
			Gap:     theme.MarginS,
			CenterX: true,
			CenterY: true,
			Children: []*ui.Node{
				{Kind: ui.KindText, Text: now.Format("15:04"), TextRole: theme.RoleTitle, Tabular: true, CenterX: true},
				{Kind: ui.KindText, Text: now.Format("Mon 2 Jan"), TextRole: theme.RoleCaption, CenterX: true},
			},
		}},
	}
}
