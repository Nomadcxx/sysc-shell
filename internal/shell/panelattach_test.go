package shell

import (
	"testing"

	"github.com/Nomadcxx/sysc-shell/internal/config"
	"github.com/Nomadcxx/sysc-shell/internal/theme"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

// TestEveryPanelPlacement: Settings, the launcher and the clipboard float;
// every other panel attaches, unless the bar paints islands or the output has
// no bar, when it detaches.
func TestEveryPanelPlacement(t *testing.T) {
	t.Parallel()
	floating := map[PanelID]bool{PanelSettings: true, PanelLauncher: true, PanelClipboard: true}
	for _, surface := range []struct {
		name, style, shape string
		bar, detached      bool
	}{
		{"frosted attached", "frosted", "attached", true, false},
		{"solid floating", "solid", "floating", true, false},
		{"islands", "islands", "attached", true, true},
		{"no bar", "frosted", "attached", false, true},
	} {
		for id := PanelClock; id <= PanelClipboard; id++ {
			if id == PanelPlugin {
				continue // needs a running plugin; its placement is the default path
			}
			cfg := config.Default()
			cfg.Accessibility.ReducedMotion = true
			cfg.Bar.Style, cfg.Bar.Shape = surface.style, surface.shape
			cfg.Bar.Left, cfg.Bar.Center, cfg.Bar.Right = nil, nil, nil
			reg := newPanelRegistry(t)
			reg.cfg = cfg
			reg.tokens = theme.Fallback
			if surface.bar {
				withTestBar(t, reg, 7, cfg)
			}
			if err := reg.OpenPanel(id, 7, Trigger{BarEdge: "top", BarZone: 40, OutW: 1920, OutH: 1080}); err != nil {
				t.Fatalf("%s %s: %v", surface.name, id, err)
			}
			reg.mu.Lock()
			h := reg.panelHosts[id]
			place := h.place
			reg.mu.Unlock()
			wantFloat := floating[id]
			wantDetached := surface.detached && !wantFloat
			if place.CenterY != wantFloat || place.Detached != wantDetached {
				t.Errorf("%s %s: CenterY %v Detached %v, want %v %v",
					surface.name, id, place.CenterY, place.Detached, wantFloat, wantDetached)
			}
			if place.Attached() != (!wantFloat && !wantDetached) {
				t.Errorf("%s %s: Attached %v", surface.name, id, place.Attached())
			}
			if wantDetached && id != PanelClipboard && place.Gap != theme.MarginS {
				t.Errorf("%s %s: detached gap %d, want %d", surface.name, id, place.Gap, theme.MarginS)
			}
		}
	}
}

// TestJointsFollowTheBarsStraightEdge is Task 12 on a 1920 output: a joint is
// the fillet unless the bar's straight edge ends sooner, and on an attached
// bar a panel too near the screen edge snaps flush to it.
func TestJointsFollowTheBarsStraightEdge(t *testing.T) {
	t.Parallel()
	base := Placement{
		BarEdge: "top", BarZone: 40, Padding: 8, Fillet: 12,
		BarShape: "attached", BarGap: 4, BarRadius: 12,
		Output: ui.Rect{W: 1920, H: 1080}, Panel: ui.Rect{W: 340, H: 400},
	}
	for _, tc := range []struct {
		name  string
		edit  func(*Placement)
		want  Joints
		wantX int
	}{
		{"centred", func(*Placement) {}, Joints{Left: 12, Right: 12}, (1920 - 340) / 2},
		{"near the left edge", func(p *Placement) { p.AnchorX = 60 }, Joints{FlushLeft: true, Right: 12}, 0},
		{"right-aligned", func(p *Placement) { p.Align = "right" }, Joints{Left: 12, FlushRight: true}, 1920 - 340},
		{"right-aligned on a floating bar", func(p *Placement) {
			p.Align, p.BarShape = "right", "floating"
		}, Joints{Left: 12}, 1920 - 340 - 8},
		{"right-aligned on 1366", func(p *Placement) {
			p.Align, p.Output.W, p.Panel.W = "right", 1366, 380
		}, Joints{Left: 12, FlushRight: true}, 1366 - 380},
		{"floating panel", func(p *Placement) { p.CenterY = true }, Joints{}, (1920 - 340) / 2},
		{"detached panel", func(p *Placement) { p.Detached = true }, Joints{}, (1920 - 340) / 2},
	} {
		p := base
		tc.edit(&p)
		if got := p.Joints(); got != tc.want {
			t.Errorf("%s: joints %+v, want %+v", tc.name, got, tc.want)
		}
		if got := p.Margins().Left; got != tc.wantX {
			t.Errorf("%s: x %d, want %d", tc.name, got, tc.wantX)
		}
	}
}

// TestFlushPanelSurface: a flush panel's surface holds its one joint and the
// screen-edge wedge past its far edge, and only its body takes input.
func TestFlushPanelSurface(t *testing.T) {
	t.Parallel()
	for _, edge := range []string{"top", "bottom"} {
		h := &PanelHost{id: PanelSession, theme: DefaultTheme(), place: Placement{
			BarEdge: edge, BarZone: 40, Padding: 8, Fillet: 12, Align: "right",
			BarShape: "attached", Output: ui.Rect{W: 1366, H: 768}, Panel: ui.Rect{W: 380, H: 300},
		}}
		w, hgt := h.surfaceSize()
		if w != 392 || hgt != 312 {
			t.Fatalf("%s: surface %dx%d, want 392x312", edge, w, hgt)
		}
		want := ui.Rect{X: 12, W: 380, H: 300}
		if edge == "bottom" {
			want.Y = 12
		}
		if got := h.surfaceBody(w, hgt); got != want {
			t.Errorf("%s: body %+v, want %+v", edge, got, want)
		}
		r := NewRegistry(config.Default())
		spec := r.panelSpec(h, h.place.Margins())
		r.Close()
		if len(spec.InputRects) != 1 || spec.InputRects[0] != want {
			t.Errorf("%s: input %+v, want the body %+v", edge, spec.InputRects, want)
		}
		if spec.MarginLeft != 1366-380-12 {
			t.Errorf("%s: margin left %d, want %d", edge, spec.MarginLeft, 1366-380-12)
		}
	}
}
