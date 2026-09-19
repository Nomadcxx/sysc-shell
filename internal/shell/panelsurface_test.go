package shell

import (
	"testing"

	"github.com/Nomadcxx/sysc-shell/internal/config"
	"github.com/Nomadcxx/sysc-shell/internal/platform/wayland/layershell"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

// A side bar anchors the panel surface beside the bar and grows the surface
// along the bar's axis, the way the horizontal edges grow it along theirs.
func TestPanelSurfaceFollowsTheEdge(t *testing.T) {
	t.Parallel()
	r := NewRegistry(config.Default())
	t.Cleanup(r.Close)

	for _, tc := range []struct {
		name       string
		edge       string
		wantAnchor uint32
	}{
		{"top", "top", uint32(layershell.ZwlrLayerSurfaceV1AnchorTop | layershell.ZwlrLayerSurfaceV1AnchorLeft)},
		{"lower", "bottom", uint32(layershell.ZwlrLayerSurfaceV1AnchorBottom | layershell.ZwlrLayerSurfaceV1AnchorLeft)},
		{"left", "left", uint32(layershell.ZwlrLayerSurfaceV1AnchorLeft | layershell.ZwlrLayerSurfaceV1AnchorTop)},
		{"right", "right", uint32(layershell.ZwlrLayerSurfaceV1AnchorRight | layershell.ZwlrLayerSurfaceV1AnchorTop)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			theme := DefaultTheme()
			theme.Fillet = 12
			h := &PanelHost{
				id:    PanelClock,
				theme: theme,
				place: Placement{
					BarEdge: tc.edge, BarZone: 40, Gap: 4, Padding: 16,
					Output: ui.Rect{W: 1000, H: 800},
					Panel:  ui.Rect{W: 300, H: 400},
				},
			}
			m := h.place.Margins()
			spec := r.panelSpec(h, m)
			if spec.Anchor != tc.wantAnchor {
				t.Fatalf("anchor = %d, want %d", spec.Anchor, tc.wantAnchor)
			}
			if h.filletMargin() != 12 {
				t.Fatalf("fillet margin = %d, want 12", h.filletMargin())
			}
			if tc.edge == "left" || tc.edge == "right" {
				if spec.Width != 300 || spec.Height != 424 {
					t.Fatalf("surface = %dx%d, want the growth along the bar's axis (300x424)",
						spec.Width, spec.Height)
				}
				if want := int32(m.Top - 12); spec.MarginTop != want {
					t.Fatalf("margin top = %d, want the fillet shift to %d", spec.MarginTop, want)
				}
			} else {
				if spec.Width != 324 || spec.Height != 400 {
					t.Fatalf("surface = %dx%d, want the growth along the bar's axis (324x400)",
						spec.Width, spec.Height)
				}
				if want := int32(m.Left - 12); spec.MarginLeft != want {
					t.Fatalf("margin left = %d, want the fillet shift to %d", spec.MarginLeft, want)
				}
			}
		})
	}
}
