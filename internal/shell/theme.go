package shell

import (
	"fmt"
	"strconv"

	"github.com/Nomadcxx/sysc-shell/internal/config"
	"github.com/Nomadcxx/sysc-shell/internal/render"
	"github.com/Nomadcxx/sysc-shell/internal/theme"
)

// Color is the painter's colour type, re-exported so components name one type.
type Color = render.Color

// Theme holds every named visual token. Components read tokens; no component
// carries an independent pixel constant.
type Theme struct {
	// BarHeight is the nominal height token. It is deliberately not a Wayland
	// dimension: the painted body is BarHeight-2*BarGap, and the surface is
	// BarGap plus that body, which is also the exclusive zone.
	BarHeight int
	// BarGap is the outer gap between the screen edge and the painted body. It
	// lives inside the surface, so the screen edge stays clickable.
	BarGap int
	// BarPadding insets the content band inside the body.
	BarPadding int
	// Spacing separates adjacent items within a section.
	Spacing int
	// Radius rounds the body's corners and shapes the opaque region.
	Radius int

	TextSize int

	Background Color
	Foreground Color
	Accent     Color
	Muted      Color
	Error      Color
	// OnPrimary is the text colour on a Primary-filled (selected) surface.
	OnPrimary Color
}

// DefaultTheme is the owner-supplied baseline: nominal height 48, exclusive
// zone 44, spacing 4.
func DefaultTheme() Theme {
	return Theme{
		BarHeight:  BarHeight,
		BarGap:     BarGap,
		BarPadding: 6,
		Spacing:    4,
		Radius:     12,
		TextSize:   14,
		Background: Color{R: 0x10, G: 0x14, B: 0x18, A: 0xff},
		Foreground: Color{R: 0xe8, G: 0xec, B: 0xf0, A: 0xff},
		Accent:     Color{R: 0x00, G: 0x80, B: 0xff, A: 0xff},
		Muted:      Color{R: 0x30, G: 0x34, B: 0x38, A: 0xff},
		Error:      Color{R: 0xff, G: 0x40, B: 0x40, A: 0xff},
		OnPrimary:  Color{R: 0xff, G: 0xff, B: 0xff, A: 0xff},
	}
}

// ThemeFromTokens maps generated Material 3 tokens onto the bar theme.
func ThemeFromTokens(tok theme.Tokens, radius int) Theme {
	t := DefaultTheme()
	t.Radius = radius
	t.Background = parseColor(tok.Surface, t.Background)
	t.Foreground = parseColor(tok.OnSurface, t.Foreground)
	t.Accent = parseColor(tok.Primary, t.Accent)
	t.Muted = parseColor(tok.OnSurfaceVariant, t.Muted)
	t.Error = parseColor(tok.Error, t.Error)
	t.OnPrimary = parseColor(tok.OnPrimary, t.OnPrimary)
	return t
}

// ThemeFrom maps a validated configuration onto theme tokens.
//
// Geometry comes from the supplied bar policy rather than the base bar, so a
// per-output override reaches the theme the bar is actually built from.
// Palette colours stay on DefaultTheme until the registry supplies generated tokens.
func ThemeFrom(cfg config.Config, bar config.Bar) Theme {
	return withBarGeometry(ThemeFromTokens(theme.Fallback, cfg.Theme.Radius), bar)
}

func withBarGeometry(t Theme, bar config.Bar) Theme {
	t.BarHeight = bar.Height
	t.BarGap = bar.Gap
	t.BarPadding = bar.Padding
	t.Spacing = bar.Spacing
	t.TextSize = bar.FontSize
	return t
}

// BackgroundOpaque reports whether the surface token is fully opaque.
func (t Theme) BackgroundOpaque() bool {
	return t.Background.A == 0xff
}

// parseColor reads #RRGGBB or #RRGGBBAA, falling back when the string is not
// one of those shapes.
func parseColor(s string, fallback Color) Color {
	if len(s) != 7 && len(s) != 9 {
		return fallback
	}
	v, err := strconv.ParseUint(s[1:], 16, 32)
	if err != nil {
		return fallback
	}
	if len(s) == 7 {
		return Color{R: uint8(v >> 16), G: uint8(v >> 8), B: uint8(v), A: 0xff}
	}
	return Color{R: uint8(v >> 24), G: uint8(v >> 16), B: uint8(v >> 8), A: uint8(v)}
}

// Geometry derives the Wayland dimensions from the tokens. The surface height
// equals the exclusive zone, and the gap lives inside the surface.
func (t Theme) Geometry() (surface, body, gap int) {
	body = t.BarHeight - 2*t.BarGap
	return t.BarGap + body, body, t.BarGap
}

// Valid reports whether the tokens produce a usable bar.
func (t Theme) Valid() error {
	if t.BarGap < 0 {
		return fmt.Errorf("shell: bar gap %d is negative", t.BarGap)
	}
	if body := t.BarHeight - 2*t.BarGap; body <= 0 {
		return fmt.Errorf("shell: bar height %d with gap %d leaves a body of %d",
			t.BarHeight, t.BarGap, body)
	}
	if t.TextSize <= 0 {
		return fmt.Errorf("shell: text size %d is not positive", t.TextSize)
	}
	if t.Radius < 0 {
		return fmt.Errorf("shell: radius %d is negative", t.Radius)
	}
	if t.BarPadding < 0 || t.Spacing < 0 {
		return fmt.Errorf("shell: padding %d and spacing %d must not be negative",
			t.BarPadding, t.Spacing)
	}
	return nil
}
