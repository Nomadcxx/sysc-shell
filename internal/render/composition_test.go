package render

import (
	"testing"

	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

// lightStyle is the same role set resolved from a light palette. Every value
// differs from testStyle's, so a recipe that reaches for the wrong role fails
// in one mode even when it happens to be right in the other.
func lightStyle() Style {
	s := testStyle
	s.Background = Color{R: 0xfa, G: 0xf9, B: 0xfd, A: 0xff}
	s.Foreground = Color{R: 0x1a, G: 0x1c, B: 0x1e, A: 0xff}
	s.Track = Color{R: 0x74, G: 0x77, B: 0x7f, A: 0xff}
	s.Accent = Color{R: 0x00, G: 0x5c, B: 0xbb, A: 0xff}
	s.OnPrimary = Color{R: 0xff, G: 0xff, B: 0xff, A: 0xff}
	s.OnAccent = Color{R: 0xff, G: 0xff, B: 0xff, A: 0xff}
	s.Capsule = Color{R: 0xe9, G: 0xed, B: 0xf4, A: 0xff}
	s.ContainerHighest = Color{R: 0xe3, G: 0xe6, B: 0xed, A: 0xff}
	s.Container = Color{R: 0xd4, G: 0xe3, B: 0xff, A: 0xff}
	s.OnContainer = Color{R: 0x00, G: 0x1b, B: 0x3d, A: 0xff}
	s.Error = Color{R: 0xba, G: 0x1a, B: 0x1a, A: 0xff}
	s.OnError = Color{R: 0xff, G: 0xff, B: 0xff, A: 0xff}
	s.ErrorContainer = Color{R: 0xff, G: 0xda, B: 0xd6, A: 0xff}
	s.OnErrorContainer = Color{R: 0x41, G: 0x00, B: 0x02, A: 0xff}
	s.Outline = Color{R: 0x74, G: 0x77, B: 0x7f, A: 0xff}
	s.OutlineVariant = Color{R: 0xc4, G: 0xc6, B: 0xcf, A: 0xff}
	s.Scrim = Color{R: 0x00, G: 0x00, B: 0x00, A: 0xff}
	return s
}

// darkStyle completes testStyle with the roles Task 6 adds. testStyle itself
// predates them, which is what keeps the fallback accessors under test.
func darkStyle() Style {
	s := testStyle
	s.ErrorContainer = Color{R: 0x93, G: 0x00, B: 0x0a, A: 0xff}
	s.OnErrorContainer = Color{R: 0xff, G: 0xda, B: 0xd6, A: 0xff}
	s.Scrim = Color{R: 0x00, G: 0x00, B: 0x00, A: 0xff}
	return s
}

func compositionModes() []struct {
	name  string
	style Style
} {
	return []struct {
		name  string
		style Style
	}{
		{"dark", darkStyle()},
		{"light", lightStyle()},
	}
}

// TestCompositionRowsResolveInBothModes walks design D6's role table. Each row
// names a fill and the pair it must resolve to; nothing here reads a literal
// colour, so a palette swap cannot silently change which role paints what.
func TestCompositionRowsResolveInBothModes(t *testing.T) {
	t.Parallel()
	rows := []struct {
		row   string
		fill  ui.Fill
		state ui.Interaction
		tone  ui.Tone
		want  func(Style) (Color, Color)
	}{
		{
			row:  "bar capsule or panel card",
			fill: ui.FillContainerHigh,
			want: func(s Style) (Color, Color) { return s.Capsule, s.Foreground },
		},
		{
			row:  "nested control or chip",
			fill: ui.FillContainerHighest,
			want: func(s Style) (Color, Color) { return s.ContainerHighest, s.Foreground },
		},
		{
			row:   "selected control",
			state: ui.StateSelected,
			want:  func(s Style) (Color, Color) { return s.Accent, s.OnPrimary },
		},
		{
			row:  "tonal selection",
			fill: ui.FillContainer,
			want: func(s Style) (Color, Color) { return s.Container, s.OnContainer },
		},
		{
			row:  "destructive control, filled",
			fill: ui.FillError,
			want: func(s Style) (Color, Color) { return s.Error, s.OnError },
		},
		{
			row:  "destructive control, container pair",
			fill: ui.FillErrorContainer,
			want: func(s Style) (Color, Color) { return s.ErrorContainer, s.OnErrorContainer },
		},
		{
			row:  "boundary",
			fill: ui.FillOutline,
			want: func(s Style) (Color, Color) { return Color{}, s.Foreground },
		},
		{
			row:  "destructive boundary",
			fill: ui.FillOutline,
			tone: ui.ToneError,
			want: func(s Style) (Color, Color) { return Color{}, s.Error },
		},
	}

	for _, mode := range compositionModes() {
		for _, tc := range rows {
			t.Run(mode.name+"/"+tc.row, func(t *testing.T) {
				t.Parallel()
				n := &ui.Node{Kind: ui.KindButton, Fill: tc.fill, State: tc.state, Tone: tc.tone}
				wantFill, wantFg := tc.want(mode.style)
				gotFill, gotFg := chromeFill(mode.style, n, mode.style.Capsule)
				if gotFill != wantFill {
					t.Errorf("fill = %+v, want %+v", gotFill, wantFill)
				}
				if gotFg != wantFg {
					t.Errorf("foreground = %+v, want %+v", gotFg, wantFg)
				}
			})
		}
	}
}

// TestCompositionSeparatesNestedControlFromItsCard is the panel-card separation
// check: a chip inside a card must not resolve to the card's own fill, or the
// nesting reads as one flat block.
func TestCompositionSeparatesNestedControlFromItsCard(t *testing.T) {
	t.Parallel()
	for _, mode := range compositionModes() {
		t.Run(mode.name, func(t *testing.T) {
			t.Parallel()
			card, _ := chromeFill(mode.style, &ui.Node{Fill: ui.FillContainerHigh}, mode.style.Capsule)
			chip, _ := chromeFill(mode.style, &ui.Node{Fill: ui.FillContainerHighest}, mode.style.Capsule)
			if card == chip {
				t.Errorf("card and nested chip both resolved to %+v; the chip needs the level above", card)
			}
		})
	}
}

// TestCompositionCapsuleAndCardShareOneContainer records the chrome catalogue's
// amendment to D6: the bar and the panels are one continuous Surface with no
// gap, so a capsule and a card are the same lift off it. Stratifying them put
// two container greys inches apart on one plane.
func TestCompositionCapsuleAndCardShareOneContainer(t *testing.T) {
	t.Parallel()
	style := darkStyle()
	capsule := capsuleFill(style, ui.FillContainerHigh)
	card, _ := chromeFill(style, &ui.Node{Fill: ui.FillContainerHigh}, style.Capsule)
	if capsule != card {
		t.Errorf("capsule = %+v, card = %+v; they share one container token", capsule, card)
	}
	if capsule != style.Capsule {
		t.Errorf("capsule = %+v, want the high container %+v", capsule, style.Capsule)
	}
}

// TestCompositionScrimDimsWhatSitsBehindIt covers the modal shield row. The
// scrim is a partial wash over live content, not an opaque black plate, so the
// pixel underneath has to survive it.
func TestCompositionScrimDimsWhatSitsBehindIt(t *testing.T) {
	t.Parallel()
	for _, mode := range compositionModes() {
		t.Run(mode.name, func(t *testing.T) {
			t.Parallel()
			style := mode.style
			n := &ui.Node{Kind: ui.KindButton, Fill: ui.FillScrim, Height: 40, Width: 120}
			c := paintChromeNode(t, n, style)

			fx, fy := fillPointOf(n)
			want := overlay(style.Background, style.Scrim, scrimAlpha)
			got := pixelAt(t, c, fx, fy)
			if got != want {
				t.Errorf("scrim = %+v, want a %g wash over the background %+v", got, scrimAlpha, want)
			}
			if got == style.Scrim {
				t.Error("scrim painted opaque; a modal shield dims what is behind it rather than hiding it")
			}
		})
	}
}

// TestCompositionOutlineFallsBackToTheForeground is the structural-outline
// fallback: a style assembled before the outline token existed still has to
// draw a boundary that separates, rather than nothing at all.
func TestCompositionOutlineFallsBackToTheForeground(t *testing.T) {
	t.Parallel()
	style := darkStyle()
	style.Outline = Color{}
	style.OutlineVariant = Color{}
	if got := style.outline(); got != style.Foreground {
		t.Errorf("outline = %+v, want the foreground %+v", got, style.Foreground)
	}
	if got := style.outlineVariant(); got != style.outline() {
		t.Errorf("outlineVariant = %+v, want the outline %+v", got, style.outline())
	}
}

// TestCompositionSurfaceSeparatorUsesTheQuietBoundary keeps a divider on the
// boundary role rather than on OnSurfaceVariant, which is a text colour. D6
// gives a quiet boundary OutlineVariant.
func TestCompositionSurfaceSeparatorUsesTheQuietBoundary(t *testing.T) {
	t.Parallel()
	for _, mode := range compositionModes() {
		t.Run(mode.name, func(t *testing.T) {
			t.Parallel()
			style := mode.style
			n := &ui.Node{Kind: ui.KindSeparator, Height: 1, Width: 120}
			c := paintChromeNode(t, n, style)

			cx, cy := centreOf(n)
			got := pixelAt(t, c, cx, cy)
			if got != style.OutlineVariant {
				t.Errorf("separator = %+v, want OutlineVariant %+v", got, style.OutlineVariant)
			}
			if got == style.Track {
				t.Error("separator painted in the text colour; a divider is a boundary role")
			}
		})
	}
}

// TestStateLayerCompositesOverEveryResolvedFill checks the nested source-over
// order: the layer sits on the fill the row resolved to, not on the surface
// beneath it, and it is the fill's own paired foreground rather than invented
// hover RGB.
func TestStateLayerCompositesOverEveryResolvedFill(t *testing.T) {
	t.Parallel()
	fills := []struct {
		name string
		fill ui.Fill
	}{
		{"containerHigh", ui.FillContainerHigh},
		{"containerHighest", ui.FillContainerHighest},
		{"tonal", ui.FillContainer},
		{"error", ui.FillError},
		{"errorContainer", ui.FillErrorContainer},
	}
	states := []struct {
		name  string
		state ui.Interaction
		alpha float64
	}{
		{"hovered", ui.StateHovered, hoverLayerAlpha},
		{"pressed", ui.StatePressed, pressedLayerAlpha},
	}

	for _, mode := range compositionModes() {
		for _, f := range fills {
			for _, st := range states {
				t.Run(mode.name+"/"+f.name+"/"+st.name, func(t *testing.T) {
					t.Parallel()
					style := mode.style
					n := &ui.Node{Kind: ui.KindButton, Fill: f.fill, State: st.state,
						Height: 40, Width: 120, Padding: 12}
					c := paintChromeNode(t, n, style)

					fill, fg := chromeFill(style, &ui.Node{Fill: f.fill}, style.Capsule)
					want := overlay(fill, fg, st.alpha)
					fx, fy := fillPointOf(n)
					if got := pixelAt(t, c, fx, fy); got != want {
						t.Errorf("layered fill = %+v, want %+v (%s over %+v)", got, want, st.name, fill)
					}
				})
			}
		}
	}
}

// TestStateLayerDisabledDimsForegroundAndKeepsGeometry covers the disabled row:
// 38 percent foreground emphasis, no state layer, and the settled geometry
// unchanged so a control does not move when it greys out.
func TestStateLayerDisabledDimsForegroundAndKeepsGeometry(t *testing.T) {
	t.Parallel()
	for _, mode := range compositionModes() {
		t.Run(mode.name, func(t *testing.T) {
			t.Parallel()
			style := mode.style
			rest := &ui.Node{Kind: ui.KindButton, Text: "Reboot", Fill: ui.FillContainerHighest,
				Height: 40, Width: 120, Padding: 12}
			paintChromeNode(t, rest, style)
			settled := rest.Bounds

			off := &ui.Node{Kind: ui.KindButton, Text: "Reboot", Fill: ui.FillContainerHighest,
				Height: 40, Width: 120, Padding: 12, State: ui.StateDisabled}
			c := paintChromeNode(t, off, style)

			if off.Bounds != settled {
				t.Errorf("disabled bounds = %+v, want the settled geometry %+v", off.Bounds, settled)
			}
			// A disabled control carries no state layer: its fill is untouched
			// and only the contents dim.
			fx, fy := fillPointOf(off)
			if got := pixelAt(t, c, fx, fy); got != style.ContainerHighest {
				t.Errorf("disabled fill = %+v, want the resting fill %+v", got, style.ContainerHighest)
			}
			if got := stateLayer(style.Foreground, ui.StateDisabled); got != (Color{}) {
				t.Errorf("disabled state layer = %+v, want none", got)
			}
		})
	}
}

// TestCompositionErrorContainerFallsBackToTheErrorPair keeps a style assembled
// before the token usable: the control still reads as destructive rather than
// painting a hole.
func TestCompositionErrorContainerFallsBackToTheErrorPair(t *testing.T) {
	t.Parallel()
	style := darkStyle()
	style.ErrorContainer = Color{}
	style.OnErrorContainer = Color{}

	fill, fg := chromeFill(style, &ui.Node{Fill: ui.FillErrorContainer}, style.Capsule)
	if fill != style.Error {
		t.Errorf("fill = %+v, want the Error token %+v", fill, style.Error)
	}
	if fg != style.OnError {
		t.Errorf("foreground = %+v, want OnError %+v", fg, style.OnError)
	}
}

// TestCompositionCapsuleFillTracksChromeFill keeps the node-free accessors in
// step with the painted node. A stroke resolves a colour through capsuleFill
// and a label through capsuleForeground; a node resolves both through
// chromeFill. All three read one table, and this is what says so.
func TestCompositionCapsuleFillTracksChromeFill(t *testing.T) {
	t.Parallel()
	for _, mode := range compositionModes() {
		for _, f := range []ui.Fill{
			ui.FillNone, ui.FillAccent, ui.FillContainer, ui.FillContainerHigh,
			ui.FillContainerHighest, ui.FillError, ui.FillErrorContainer,
			ui.FillSoft, ui.FillOutline, ui.FillScrim,
		} {
			t.Run(mode.name, func(t *testing.T) {
				t.Parallel()
				style := mode.style
				wantFill, wantFg := chromeFill(style, &ui.Node{Fill: f}, style.Capsule)
				if got := capsuleFill(style, f); got != wantFill {
					t.Errorf("capsuleFill(%d) = %+v, want %+v", f, got, wantFill)
				}
				if got := capsuleForeground(style, f); got != wantFg {
					t.Errorf("capsuleForeground(%d) = %+v, want %+v", f, got, wantFg)
				}
			})
		}
	}
}
