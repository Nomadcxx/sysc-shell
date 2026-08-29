package shell

import "testing"

func TestDefaultThemeGeometryMatchesTheBaseline(t *testing.T) {
	t.Parallel()
	surface, body, gap := DefaultTheme().Geometry()
	if gap != 4 {
		t.Fatalf("gap = %d, want 4", gap)
	}
	if body != 40 {
		t.Fatalf("body = %d, want 40, which is height 48 minus twice the gap", body)
	}
	// The surface height is also the exclusive zone, so Niri windows begin 44
	// logical pixels from the screen edge.
	if surface != 44 {
		t.Fatalf("surface = %d, want 44, which is gap plus body", surface)
	}
}

func TestNominalHeightIsATokenNotASurfaceDimension(t *testing.T) {
	t.Parallel()
	th := DefaultTheme()
	surface, _, _ := th.Geometry()
	if surface == th.BarHeight {
		t.Fatal("the surface equals the nominal height; 48 must not reach Wayland as a dimension")
	}
}

func TestThemeGeometryScalesWithTokens(t *testing.T) {
	t.Parallel()
	th := DefaultTheme()
	th.BarHeight, th.BarGap = 60, 6
	surface, body, gap := th.Geometry()
	if gap != 6 || body != 48 || surface != 54 {
		t.Fatalf("geometry = surface %d body %d gap %d, want 54/48/6", surface, body, gap)
	}
}

func TestThemeValidation(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		mutate func(*Theme)
	}{
		{"height leaves no body", func(th *Theme) { th.BarHeight = 8 }},
		{"negative gap", func(th *Theme) { th.BarGap = -1 }},
		{"zero text size", func(th *Theme) { th.TextSize = 0 }},
		{"negative radius", func(th *Theme) { th.Radius = -2 }},
		{"negative padding", func(th *Theme) { th.BarPadding = -1 }},
		{"negative spacing", func(th *Theme) { th.Spacing = -1 }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			th := DefaultTheme()
			c.mutate(&th)
			if err := th.Valid(); err == nil {
				t.Fatalf("Valid() accepted %s", c.name)
			}
		})
	}

	if err := DefaultTheme().Valid(); err != nil {
		t.Fatalf("the default theme is invalid: %v", err)
	}
}
