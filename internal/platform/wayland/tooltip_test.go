package wayland

import (
	"testing"

	"github.com/Nomadcxx/sysc-shell/internal/config"
	"github.com/Nomadcxx/sysc-shell/internal/render"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

// Placement follows the panel design's rule: anchored off the bar edge,
// aligned to the triggering widget, clamped fully inside the output.
func TestTooltipPlacementClampsInsideTheOutput(t *testing.T) {
	t.Parallel()
	const outputWidth, outputHeight = 1920, 1080

	cases := []struct {
		name         string
		anchor       ui.Rect
		width        int
		wantXAtLeast int
		wantXAtMost  int
	}{
		{
			name:         "centred under its widget",
			anchor:       ui.Rect{X: 900, Y: 0, W: 40, H: 44},
			width:        200,
			wantXAtLeast: 0,
			wantXAtMost:  outputWidth - 200,
		},
		{
			name:         "clamped at the right edge",
			anchor:       ui.Rect{X: 1900, Y: 0, W: 20, H: 44},
			width:        200,
			wantXAtLeast: 0,
			wantXAtMost:  outputWidth - 200,
		},
		{
			name:         "clamped at the left edge",
			anchor:       ui.Rect{X: 0, Y: 0, W: 20, H: 44},
			width:        200,
			wantXAtLeast: 0,
			wantXAtMost:  outputWidth - 200,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			got := tooltipPlacement(c.anchor, c.width, 30, outputWidth, outputHeight)
			if got.X < c.wantXAtLeast || got.X > c.wantXAtMost {
				t.Fatalf("x = %d, want within [%d, %d]", got.X, c.wantXAtLeast, c.wantXAtMost)
			}
			if got.X+got.W > outputWidth {
				t.Fatalf("tooltip right edge %d exceeds the output", got.X+got.W)
			}
			if got.Y < c.anchor.Y+c.anchor.H {
				t.Fatalf("y = %d, want below the bar edge at %d", got.Y, c.anchor.Y+c.anchor.H)
			}
		})
	}
}

// A tooltip wider than the output is clamped to it rather than placed off it.
func TestATooltipWiderThanTheOutputIsClamped(t *testing.T) {
	t.Parallel()
	got := tooltipPlacement(ui.Rect{X: 10, Y: 0, W: 20, H: 44}, 3000, 30, 1920, 1080)

	if got.X != 0 {
		t.Fatalf("x = %d, want 0 for an over-wide tooltip", got.X)
	}
	if got.W > 1920 {
		t.Fatalf("width = %d, want clamped to the output", got.W)
	}
}

func TestTooltipBufferReconfigurationWaitsForRelease(t *testing.T) {
	gen := &generation{fd: -1}
	gen.retire.attached()
	tt := &tooltipSurface{gen: gen}
	o := &owner{}

	o.retireTooltipGeneration(tt)
	if tt.gen != nil {
		t.Fatal("retired generation remained current")
	}
	if len(tt.retiring) != 1 || tt.retiring[0] != gen {
		t.Fatal("attached generation was freed before wl_buffer.release")
	}

	o.onTooltipBufferRelease(tt, gen)
	if len(tt.retiring) != 0 {
		t.Fatal("released tooltip generation was not freed")
	}
}

func TestTooltipBufferTeardownFreesAllGenerations(t *testing.T) {
	current := &generation{fd: -1}
	retired := &generation{fd: -1}
	current.retire.attached()
	retired.retire.attached()
	tt := &tooltipSurface{gen: current, retiring: []*generation{retired}}
	o := &owner{tooltip: tt}

	if err := o.hideTooltip(); err != nil {
		t.Fatalf("hideTooltip: %v", err)
	}
	if o.tooltip != nil || tt.gen != nil || len(tt.retiring) != 0 {
		t.Fatal("tooltip generations survived teardown")
	}
	if !current.retire.destroyed || !retired.retire.destroyed {
		t.Fatal("teardown freed storage without marking every generation destroyed")
	}
}

func TestTooltipUsesAcceptedThemeAndFont(t *testing.T) {
	cfg := config.Default()
	cfg.Theme.Radius = 7
	override := cfg.Bar
	override.FontSize = 19
	override.FontFamily = "Fixture Sans"
	cfg.Outputs = []config.OutputOverride{{Connector: "DP-1", Bar: override}}

	o := &owner{cfg: &cfg}
	tt := &tooltipSurface{
		host:  &OutputHost{connector: "DP-1"},
		place: ui.Rect{W: 120, H: 30},
		// The shell resolves the palette and sends it with the request.
		style: TooltipStyle{
			Background: render.Color{R: 0x11, G: 0x22, B: 0x33, A: 0x44},
			Foreground: render.Color{R: 0xaa, G: 0xbb, B: 0xcc, A: 0xff},
		},
	}
	style, family := o.tooltipStyle(tt)

	if style.Background != (render.Color{R: 0x11, G: 0x22, B: 0x33, A: 0x44}) {
		t.Fatalf("background = %#v, want the shell's resolved colour", style.Background)
	}
	if style.Foreground != (render.Color{R: 0xaa, G: 0xbb, B: 0xcc, A: 0xff}) {
		t.Fatalf("foreground = %#v, want the shell's resolved colour", style.Foreground)
	}
	if style.Radius != 7 || style.Size != 19 {
		t.Fatalf("radius/size = %d/%d, want 7/19", style.Radius, style.Size)
	}
	if family != "Fixture Sans" {
		t.Fatalf("font family = %q, want connector override", family)
	}
}

// A tooltip raised before the shell has resolved a palette still paints.
func TestTooltipFallsBackWhenNoColourWasSent(t *testing.T) {
	cfg := config.Default()
	o := &owner{cfg: &cfg}
	tt := &tooltipSurface{host: &OutputHost{connector: "DP-1"}, place: ui.Rect{W: 120, H: 30}}

	style, _ := o.tooltipStyle(tt)
	if style.Background.A == 0 || style.Foreground.A == 0 {
		t.Fatalf("fallback left a transparent tooltip: %#v / %#v", style.Background, style.Foreground)
	}
}
