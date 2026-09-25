package render

import (
	"testing"

	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

func roleStyle() Style {
	s := Style{Accent: Color{R: 1, A: 255}, Foreground: Color{G: 1, A: 255}}
	s.Roles[ui.PaintSurfaceVariant] = Color{R: 10, G: 20, B: 30, A: 255}
	s.Roles[ui.PaintOnSurfaceVariant] = Color{R: 200, G: 210, B: 220, A: 255}
	return s
}

func TestFillRolePaintsTheNamedRoles(t *testing.T) {
	n := &ui.Node{Kind: ui.KindCapsule, Fill: ui.FillRole, FillRole: ui.PaintSurfaceVariant, InkRole: ui.PaintOnSurfaceVariant}
	s := roleStyle()
	s.Track = s.Roles[ui.PaintOnSurfaceVariant]
	fill, fg := chromeFill(s, n, Color{})
	if fill != (Color{R: 10, G: 20, B: 30, A: 255}) || fg != (Color{R: 200, G: 210, B: 220, A: 255}) {
		t.Fatalf("fill %v fg %v", fill, fg)
	}
}

func TestHoverRoleReplacesTheStateLayerOnARow(t *testing.T) {
	n := &ui.Node{Kind: ui.KindRow, HoverFill: ui.PaintSurfaceVariant, HoverInk: ui.PaintOnSurfaceVariant, State: ui.StateHovered}
	s := roleStyle()
	s.Track = s.Roles[ui.PaintOnSurfaceVariant]
	fill, fg, layered := rowFill(s, n)
	if layered {
		t.Fatal("a role hover also drew the state layer")
	}
	if fill != (Color{R: 10, G: 20, B: 30, A: 255}) || fg != (Color{R: 200, G: 210, B: 220, A: 255}) {
		t.Fatalf("fill %v fg %v", fill, fg)
	}
	n.State = 0
	if fill, _, _ := rowFill(s, n); fill.A != 0 {
		t.Fatalf("resting row filled %v", fill)
	}
}

func TestExistingPaintRolesKeepTheirTokens(t *testing.T) {
	s := roleStyle()
	s.Track = Color{B: 9, A: 255}
	if got := resolvePaintRole(s, ui.PaintOnSurfaceVariant); got != s.Track {
		t.Fatalf("OnSurfaceVariant = %v, want Track %v", got, s.Track)
	}
	if got := resolvePaintRole(s, ui.PaintSurfaceVariant); got != s.Roles[ui.PaintSurfaceVariant] {
		t.Fatalf("SurfaceVariant = %v", got)
	}
}
