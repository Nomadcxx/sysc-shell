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

	// CapsulePadding insets a bar item inside its capsule. It is a theme
	// constant this tranche, not a configuration key.
	CapsulePadding  int
	ControlHeight   int
	CompactHeight   int
	ButtonPadding   int
	IconSize        int
	ProfileIconSize int
	OSDIconSize     int
	CardRadius      int

	// Semantic Material roles are the source of truth for chrome composition.
	Surface                 Color
	SurfaceContainer        Color
	SurfaceContainerHigh    Color
	SurfaceContainerHighest Color
	OnSurface               Color
	OnSurfaceVariant        Color
	Primary                 Color
	OnPrimary               Color
	PrimaryContainer        Color
	OnPrimaryContainer      Color
	Outline                 Color
	OutlineVariant          Color
	Error                   Color
	OnError                 Color

	// Legacy painter names remain while existing surfaces move to semantic
	// roles. Theme construction derives them from the fields above.
	Background Color
	Foreground Color
	Accent     Color
	Muted      Color
	// Capsule fills the pill around a bar widget. Container fills a workspace
	// pill that is not focused, and is a distinct container colour rather than
	// the capsule surface. OnAccent and OnContainer keep a pill's numeral
	// legible on its own fill.
	Capsule     Color
	Container   Color
	OnAccent    Color
	OnContainer Color
}

// DefaultTheme is the owner-supplied baseline: nominal height 48, exclusive
// zone 44, spacing 4.
func DefaultTheme() Theme {
	t := Theme{
		BarHeight:  BarHeight,
		BarGap:     BarGap,
		BarPadding: 6,
		Spacing:    4,
		Radius:     12,
		TextSize:   14,

		CapsulePadding:  8,
		ControlHeight:   40,
		CompactHeight:   32,
		ButtonPadding:   12,
		IconSize:        20,
		ProfileIconSize: 18,
		OSDIconSize:     24,
		CardRadius:      12,
	}
	applyTokens(&t, theme.Fallback)
	return t
}

// ThemeFromTokens maps generated Material 3 tokens onto the bar theme.
func ThemeFromTokens(tok theme.Tokens, radius int) Theme {
	t := DefaultTheme()
	t.Radius = radius
	t.CardRadius = radius
	applyTokens(&t, tok)
	return t
}

func applyTokens(t *Theme, tok theme.Tokens) {
	t.Surface = parseColor(tok.Surface, t.Surface)
	t.SurfaceContainer = parseColor(tok.SurfaceContainer, t.SurfaceContainer)
	t.SurfaceContainerHigh = parseColor(tok.SurfaceContainerHigh, t.SurfaceContainerHigh)
	t.SurfaceContainerHighest = parseColor(tok.SurfaceContainerHighest, t.SurfaceContainerHighest)
	t.OnSurface = parseColor(tok.OnSurface, t.OnSurface)
	t.OnSurfaceVariant = parseColor(tok.OnSurfaceVariant, t.OnSurfaceVariant)
	t.Primary = parseColor(tok.Primary, t.Primary)
	t.OnPrimary = parseColor(tok.OnPrimary, t.OnPrimary)
	t.PrimaryContainer = parseColor(tok.PrimaryContainer, t.PrimaryContainer)
	t.OnPrimaryContainer = parseColor(tok.OnPrimaryContainer, t.OnPrimaryContainer)
	t.Outline = parseColor(tok.Outline, t.Outline)
	t.OutlineVariant = parseColor(tok.OutlineVariant, t.OutlineVariant)
	t.Error = parseColor(tok.Error, t.Error)
	t.OnError = parseColor(tok.OnError, t.OnError)

	t.Background = t.Surface
	t.Foreground = t.OnSurface
	t.Accent = t.Primary
	t.Muted = t.OnSurfaceVariant
	// The bar and the panels are one continuous Surface with no gap between
	// them, so a bar capsule and a panel card are the same fill on the same
	// background. Stratifying them into mid and high put two greys inches
	// apart on that shared surface; both resolve to the high container and
	// Highest stays for the controls that sit on top of them.
	t.Capsule = t.SurfaceContainerHigh
	t.Container = t.PrimaryContainer
	t.OnAccent = t.OnPrimary
	t.OnContainer = t.OnPrimaryContainer
}

// ThemeFrom maps a validated configuration onto theme tokens.
//
// Geometry comes from the supplied bar policy rather than the base bar, so a
// per-output override reaches the theme the bar is actually built from.
// Palette colours stay on DefaultTheme until the registry supplies generated tokens.
// ThemeFrom resolves the built-in fallback palette, not the generated one. It
// is what a surface looks like before any palette has been generated, and it
// is only correct where no registry is in reach — a bare Bar in a test. Every
// surface the shell actually paints must use Registry.surfaceTheme, which a
// test in this package enforces.
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
	if t.ControlHeight <= 0 || t.CompactHeight <= 0 || t.IconSize <= 0 {
		return fmt.Errorf("shell: chrome heights %d/%d and icon size %d must be positive",
			t.ControlHeight, t.CompactHeight, t.IconSize)
	}
	if t.ButtonPadding < 0 || t.CardRadius < 0 {
		return fmt.Errorf("shell: button padding %d and card radius %d must not be negative",
			t.ButtonPadding, t.CardRadius)
	}
	return nil
}
