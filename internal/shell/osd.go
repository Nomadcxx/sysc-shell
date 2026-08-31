package shell

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/Nomadcxx/sysc-shell/internal/platform/wayland"
	"github.com/Nomadcxx/sysc-shell/internal/platform/wayland/layershell"
	"github.com/Nomadcxx/sysc-shell/internal/render"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

const (
	osdWidth  = 220
	osdHeight = 64
	osdHide   = 1500 * time.Millisecond
)

// OSDView is one OSD payload: a kind, a 0..100 level, and optional mute.
type OSDView struct {
	Kind  string
	Level int
	Muted bool
}

type OSDManager struct {
	r         *Registry
	hideFor   time.Duration
	timer     *time.Timer
	open      map[uint32]bool
	view      OSDView
	theme     Theme
	animStart time.Time
	stopAnim  chan struct{}
	stopOnce  sync.Once
}

func newOSDManager(r *Registry, hide time.Duration) *OSDManager {
	if hide <= 0 {
		hide = osdHide
	}
	return &OSDManager{r: r, hideFor: hide, open: map[uint32]bool{}}
}

func (m *OSDManager) Visible() bool {
	if m == nil {
		return false
	}
	return len(m.open) > 0
}

func (m *OSDManager) Show(v OSDView) {
	if m == nil || m.r == nil {
		return
	}
	m.r.mu.Lock()
	aux, pubs, startReveal := m.prepareShow(v)
	m.r.mu.Unlock()
	for _, req := range aux {
		m.r.sendAux(req)
	}
	for _, p := range pubs {
		m.r.publishSurface(p.global, p.id)
	}
	if startReveal {
		go m.revealLoop()
	}
}

type osdPub struct {
	global uint32
	id     string
}

func (m *OSDManager) prepareShow(v OSDView) (aux []wayland.AuxRequest, pubs []osdPub, startReveal bool) {
	if v.Level < 0 {
		v.Level = 0
	}
	if v.Level > 100 {
		v.Level = 100
	}
	wasHidden := len(m.open) == 0
	m.view = v
	m.theme = ThemeFromTokens(m.r.tokens, 12)
	pos := m.r.cfg.Panels.OSD
	pad := m.r.cfg.Panels.Padding
	zone := m.r.cfg.Bar.Height
	size := ui.Rect{W: osdWidth, H: osdHeight}
	for global := range m.r.bars {
		out := ui.Rect{W: 1920, H: 1080}
		if bar := m.r.bars[global]; bar != nil && bar.configured.set {
			out.W, out.H = bar.configured.width, bar.configured.height
		}
		anchor, margins := osdPlace(pos, out, size, zone, pad)
		id := osdSurfaceID(global)
		if !m.open[global] {
			aux = append(aux, wayland.AuxRequest{Output: global, Open: m.spec(id, anchor, margins)})
			m.open[global] = true
		}
		pubs = append(pubs, osdPub{global: global, id: id})
	}
	if m.timer == nil {
		m.timer = time.AfterFunc(m.hideFor, m.hideAll)
	} else {
		m.timer.Reset(m.hideFor)
	}
	if wasHidden && !m.r.cfg.Accessibility.ReducedMotion {
		m.animStart = time.Now()
		m.stopAnim = make(chan struct{})
		m.stopOnce = sync.Once{}
		startReveal = true
	}
	return aux, pubs, startReveal
}

func (m *OSDManager) hideAll() {
	if m == nil || m.r == nil {
		return
	}
	m.r.mu.Lock()
	aux := m.prepareHide()
	m.r.mu.Unlock()
	for _, req := range aux {
		m.r.sendAux(req)
	}
}

func (m *OSDManager) prepareHide() []wayland.AuxRequest {
	if m.timer != nil {
		m.timer.Stop()
		m.timer = nil
	}
	if m.stopAnim != nil {
		m.stopOnce.Do(func() { close(m.stopAnim) })
	}
	var aux []wayland.AuxRequest
	for global := range m.open {
		aux = append(aux, wayland.AuxRequest{Output: global, ID: osdSurfaceID(global)})
	}
	m.open = map[uint32]bool{}
	return aux
}

func (m *OSDManager) spec(id string, anchor uint32, mgn Margins) *wayland.AuxSpec {
	return &wayland.AuxSpec{
		ID:            id,
		Namespace:     "sysc-shell-osd",
		Layer:         layerOverlay,
		Anchor:        anchor,
		MarginTop:     int32(mgn.Top),
		MarginBottom:  int32(mgn.Bottom),
		MarginLeft:    int32(mgn.Left),
		MarginRight:   int32(mgn.Right),
		Width:         osdWidth,
		Height:        osdHeight,
		ExclusiveZone: -1,
		Keyboard:      keyboardNone,
		Callbacks: wayland.HostCallbacks{
			Configure: func(int, int, int) error { return nil },
			Render:    m.render,
			Handle:    func(wayland.Event) bool { return false },
		},
	}
}

func (m *OSDManager) render(pixels []byte, width, height, stride int) error {
	c, err := render.NewCanvas(pixels, width, height, stride)
	if err != nil {
		return err
	}
	slide := m.slidePx()
	body := ui.Rect{X: 8, Y: 8 + slide, W: osdWidth - 16, H: osdHeight - 16}
	c.FillRounded(body, 8, m.theme.Background)
	glyph := ui.Rect{X: body.X + 8, Y: body.Y + 4, W: 20, H: 20}
	c.FillRounded(glyph, 4, m.theme.Accent)
	lx := glyph.X + glyph.W + 8
	ly := body.Y + 8
	for range osdLabel(m.view) {
		c.FillRounded(ui.Rect{X: lx, Y: ly, W: 4, H: 8}, 1, m.theme.Foreground)
		lx += 6
	}
	fill := body
	fill.Y += body.H - 8
	fill.H = 6
	fill.W = body.W * m.view.Level / 100
	if fill.W > 0 {
		c.FillRounded(fill, 3, m.theme.Accent)
	}
	return nil
}

func (m *OSDManager) slidePx() int {
	if m == nil || m.r == nil || m.r.cfg.Accessibility.ReducedMotion {
		return 0
	}
	if m.animStart.IsZero() {
		return 0
	}
	left := revealDuration - time.Since(m.animStart)
	if left <= 0 {
		return 0
	}
	return int(8 * left / revealDuration)
}

func (m *OSDManager) revealLoop() {
	tick := time.NewTicker(revealTick)
	defer tick.Stop()
	for {
		select {
		case <-m.stopAnim:
			return
		case <-tick.C:
			m.r.mu.Lock()
			pubs := make([]osdPub, 0, len(m.open))
			for global := range m.open {
				pubs = append(pubs, osdPub{global: global, id: osdSurfaceID(global)})
			}
			done := time.Since(m.animStart) >= revealDuration
			m.r.mu.Unlock()
			for _, p := range pubs {
				m.r.publishSurface(p.global, p.id)
			}
			if done {
				return
			}
		}
	}
}

func osdLabel(v OSDView) string {
	if v.Muted {
		return v.Kind + " muted"
	}
	return v.Kind
}

func osdSurfaceID(global uint32) string { return fmt.Sprintf("osd:%d", global) }

func osdPlace(pos string, output, size ui.Rect, barZone, pad int) (uint32, Margins) {
	m := osdMargins(pos, output, size, barZone, pad)
	anchor := uint32(layershell.ZwlrLayerSurfaceV1AnchorTop | layershell.ZwlrLayerSurfaceV1AnchorLeft)
	if strings.HasPrefix(pos, "bottom") {
		anchor = uint32(layershell.ZwlrLayerSurfaceV1AnchorBottom | layershell.ZwlrLayerSurfaceV1AnchorLeft)
	}
	return anchor, m
}

func osdMargins(pos string, output, size ui.Rect, barZone, pad int) Margins {
	inset := barZone + pad
	if inset < 0 {
		inset = 0
	}
	left := func(align string) int {
		switch align {
		case "left":
			return pad
		case "right":
			return output.W - size.W - pad
		default:
			return (output.W - size.W) / 2
		}
	}
	var m Margins
	switch pos {
	case "top-left":
		m = Margins{Top: inset, Left: left("left")}
	case "top-center":
		m = Margins{Top: inset, Left: left("center")}
	case "top-right":
		m = Margins{Top: inset, Left: left("right")}
	case "center-left":
		m = Margins{Top: (output.H - size.H) / 2, Left: left("left")}
	case "center":
		m = Margins{Top: (output.H - size.H) / 2, Left: left("center")}
	case "center-right":
		m = Margins{Top: (output.H - size.H) / 2, Left: left("right")}
	case "bottom-left":
		m = Margins{Bottom: inset, Left: left("left")}
	case "bottom-right":
		m = Margins{Bottom: inset, Left: left("right")}
	default: // bottom-center
		m = Margins{Bottom: inset, Left: left("center")}
	}
	return clampOSD(m, pos, output, size)
}

func clampOSD(m Margins, pos string, output, size ui.Rect) Margins {
	if m.Left < 0 {
		m.Left = 0
	}
	if maxL := output.W - size.W; maxL >= 0 && m.Left > maxL {
		m.Left = maxL
	}
	if strings.HasPrefix(pos, "bottom") {
		if m.Bottom < 0 {
			m.Bottom = 0
		}
		if maxB := output.H - size.H; maxB >= 0 && m.Bottom > maxB {
			m.Bottom = maxB
		}
		return m
	}
	if m.Top < 0 {
		m.Top = 0
	}
	if maxT := output.H - size.H; maxT >= 0 && m.Top > maxT {
		m.Top = maxT
	}
	return m
}
