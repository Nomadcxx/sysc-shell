package shell

import (
	"fmt"
	"strings"
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
	r       *Registry
	hideFor time.Duration
	timer   *time.Timer
	open    map[uint32]bool
	view    OSDView
	theme   Theme
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
	defer m.r.mu.Unlock()
	m.showLocked(v)
}

func (m *OSDManager) showLocked(v OSDView) {
	if v.Level < 0 {
		v.Level = 0
	}
	if v.Level > 100 {
		v.Level = 100
	}
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
			m.r.sendAux(wayland.AuxRequest{Output: global, Open: m.spec(id, anchor, margins)})
			m.open[global] = true
		}
		m.r.publishSurface(global, id)
	}
	if m.timer == nil {
		m.timer = time.AfterFunc(m.hideFor, m.hideAll)
	} else {
		m.timer.Reset(m.hideFor)
	}
}

func (m *OSDManager) hideAll() {
	if m == nil || m.r == nil {
		return
	}
	m.r.mu.Lock()
	defer m.r.mu.Unlock()
	m.hideLocked()
}

func (m *OSDManager) hideLocked() {
	if m.timer != nil {
		m.timer.Stop()
		m.timer = nil
	}
	for global := range m.open {
		m.r.sendAux(wayland.AuxRequest{Output: global, ID: osdSurfaceID(global)})
	}
	m.open = map[uint32]bool{}
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
	body := ui.Rect{X: 8, Y: 8, W: osdWidth - 16, H: osdHeight - 16}
	c.FillRounded(body, 8, m.theme.Background)
	fill := body
	fill.Y += body.H - 8
	fill.H = 6
	fill.W = body.W * m.view.Level / 100
	if fill.W > 0 {
		c.FillRounded(fill, 3, m.theme.Accent)
	}
	return nil
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
