package shell

import (
	"fmt"

	"github.com/Nomadcxx/sysc-shell/internal/config"
	"github.com/Nomadcxx/sysc-shell/internal/render"
	"github.com/Nomadcxx/sysc-shell/internal/theme"
)

// Color is the painter's colour type, re-exported so components name one type.
type Color = render.Color

// Palette is the parsed Material role set for one resolved theme. It carries
// the roles the shell paints with; the fixed accents stay in theme.Tokens,
// where the application template catalogue reads them, because no surface has
// a use for a role that holds one tone across light and dark.
type Palette struct {
	Surface                 Color
	SurfaceDim              Color
	SurfaceBright           Color
	SurfaceContainerLowest  Color
	SurfaceContainerLow     Color
	SurfaceContainer        Color
	SurfaceContainerHigh    Color
	SurfaceContainerHighest Color
	SurfaceVariant          Color
	OnSurface               Color
	OnSurfaceVariant        Color

	Primary              Color
	OnPrimary            Color
	PrimaryContainer     Color
	OnPrimaryContainer   Color
	Secondary            Color
	OnSecondary          Color
	SecondaryContainer   Color
	OnSecondaryContainer Color
	Tertiary             Color
	OnTertiary           Color
	TertiaryContainer    Color
	OnTertiaryContainer  Color

	Error            Color
	OnError          Color
	ErrorContainer   Color
	OnErrorContainer Color

	Outline          Color
	OutlineVariant   Color
	InverseSurface   Color
	InverseOnSurface Color
	InversePrimary   Color
	Shadow           Color
	Scrim            Color
	SurfaceTint      Color
}

// Surfaces carries the alpha each root surface paints at. Nested fills
// composite over their painted parent rather than inheriting these, which is
// what keeps a card distinct on a translucent panel.
type Surfaces struct {
	Bar     uint8
	Panel   uint8
	Overlay uint8
}

// Theme is the one resolved contract every surface paints from. Components
// request semantic roles; no component carries an independent pixel constant
// or an RGB value.
type Theme struct {
	// Palette is the accessible, validated role set.
	Palette Palette
	// Metrics is the density row in force, with any bar override applied.
	Metrics theme.Metrics
	// Shapes are the resolved corner radii.
	Shapes render.Shapes
	// Type resolves a semantic text role to a family, size, and weight.
	Type render.TypeSet
	// Surfaces is the per-surface root opacity.
	Surfaces Surfaces
	// Elevation selects how much of the shadow renderer a floating surface
	// uses.
	Elevation theme.Elevation
	// Motion is the duration table and curve in force.
	Motion render.MotionSet
	// Outlined forces the structural boundary on. High contrast sets it, and
	// so does a palette whose surface levels cannot separate on their own.
	Outlined bool

	// BarGap is the outer gap between the screen edge and the painted body.
	// It lives inside the surface, so the screen edge stays clickable, and it
	// is a docking choice rather than a density row.
	BarGap int

	// The fields below are the flat names the existing surfaces still read.
	// They are derived from the groups above, and they go away as each tree
	// moves onto roles.
	BarHeight  int
	BarPadding int
	Spacing    int
	Radius     int
	TextSize   int

	CapsulePadding  int
	ControlHeight   int
	CompactHeight   int
	ButtonPadding   int
	IconSize        int
	ProfileIconSize int
	OSDIconSize     int
	CardRadius      int

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

// ResolveTheme is the only path from configuration and a palette to a painted
// surface.
//
// It parses the palette, applies the composition, lets the resolved bar policy
// override the geometry it owns, enforces accessibility last, and validates
// the result before returning it. A caller that gets an error keeps whatever
// theme it already had: a surface painted half in one theme is worse than one
// painted entirely in the previous.
func ResolveTheme(cfg config.Config, bar config.Bar, tok theme.Tokens) (Theme, error) {
	comp := cfg.Theme.Composition
	hc := cfg.Accessibility.HighContrast

	// Accessibility applies to the palette before anything reads it, so every
	// surface and every template sees the same repaired roles.
	if err := tok.Complete(); err != nil {
		return Theme{}, err
	}
	pal, err := parsePalette(tok.Repair(hc))
	if err != nil {
		return Theme{}, err
	}

	m := comp.Metrics()
	// The bar policy is already resolved -- derived, then base, then output --
	// so its geometry wins over the density row it came from.
	m.BarHeight = bar.Height
	m.BarPadding = bar.Padding
	m.BarSpacing = bar.Spacing

	radius := bar.Radius
	t := Theme{
		Palette:   pal,
		Metrics:   m,
		Shapes:    resolveShapes(radius),
		Type:      resolveType(comp, bar),
		Surfaces:  resolveSurfaces(comp, hc),
		Elevation: comp.Elevation,
		Motion: render.MotionSet{
			Durations: comp.Durations(),
			Spatial:   comp.Motion.SpatialCurve(),
			Reduced:   cfg.Accessibility.ReducedMotion,
		},
		// High contrast draws the structural boundary rather than relying on
		// a tonal step the palette may not have room for.
		Outlined: hc,
		BarGap:   bar.Gap,
	}
	applyFlat(&t)
	if err := t.Valid(); err != nil {
		return Theme{}, err
	}
	return t, nil
}

// resolveShapes derives the shape roles from the base radius. Stadium and
// circle geometry is not here: those stay geometric invariants the painter
// applies, so a zero radius still leaves a pill a pill.
func resolveShapes(radius int) render.Shapes {
	if radius < 0 {
		radius = 0
	}
	large := radius * 3 / 2
	if large > theme.RadiusMax {
		large = theme.RadiusMax
	}
	return render.Shapes{
		Small:  radius / 2,
		Medium: radius,
		Large:  large,
		Card:   radius,
		Panel:  radius,
	}
}

// resolveType fills the role table once, so measurement and paint cannot pick
// different sizes for the same role. The bar's family and size stay local
// overrides, and they win over the composition for the body role the bar
// actually paints with.
func resolveType(comp theme.Composition, bar config.Bar) render.TypeSet {
	family := comp.FontFamily
	if bar.FontFamily != "" {
		family = bar.FontFamily
	}
	set := render.TypeSet{Family: family, MonoFamily: comp.MonoFontFamily}
	for role := range set.Roles {
		r := theme.TextRole(role)
		spec := render.TextSpec{
			Family: family,
			Size:   comp.TextSize(r),
			Weight: comp.TextWeight(r),
		}
		if theme.TypeFor(r).Mono {
			spec.Family = comp.MonoFontFamily
		}
		set.Roles[role] = spec
	}
	// The bar carries an explicit size for its own body text.
	if bar.FontSize > 0 {
		spec := set.Roles[theme.RoleBody]
		spec.Size = bar.FontSize
		set.Roles[theme.RoleBody] = spec
	}
	return set
}

// resolveSurfaces converts the percentage axes to alpha. High contrast forces
// every root opaque: translucency behind text is the first thing to go when
// legibility is the point.
func resolveSurfaces(comp theme.Composition, highContrast bool) Surfaces {
	if highContrast {
		return Surfaces{Bar: 0xff, Panel: 0xff, Overlay: 0xff}
	}
	return Surfaces{
		Bar:     opacityAlpha(comp.BarOpacity),
		Panel:   opacityAlpha(comp.PanelOpacity),
		Overlay: opacityAlpha(comp.OverlayOpacity),
	}
}

func opacityAlpha(percent int) uint8 {
	if percent < theme.OpacityMin {
		percent = theme.OpacityMin
	}
	if percent > theme.OpacityMax {
		percent = theme.OpacityMax
	}
	return uint8((percent*255 + 50) / 100)
}

// applyFlat mirrors the grouped values onto the flat names the existing
// surfaces read. It is the one place the legacy shape is produced, so deleting
// those fields is a matter of removing callers and then this function.
func applyFlat(t *Theme) {
	p := t.Palette
	t.BarHeight = t.Metrics.BarHeight
	t.BarPadding = t.Metrics.BarPadding
	t.Spacing = t.Metrics.BarSpacing
	t.Radius = t.Shapes.Medium
	t.CardRadius = t.Shapes.Card
	t.TextSize = t.Type.Spec(theme.RoleBody).Size

	t.CapsulePadding = 8
	t.ControlHeight = t.Metrics.StandardControl
	t.CompactHeight = t.Metrics.CompactControl
	t.ButtonPadding = 12
	t.IconSize = t.Metrics.IconNormal
	t.ProfileIconSize = t.Metrics.IconSmall + 2
	t.OSDIconSize = t.Metrics.IconLarge

	t.Surface = p.Surface
	t.SurfaceContainer = p.SurfaceContainer
	t.SurfaceContainerHigh = p.SurfaceContainerHigh
	t.SurfaceContainerHighest = p.SurfaceContainerHighest
	t.OnSurface = p.OnSurface
	t.OnSurfaceVariant = p.OnSurfaceVariant
	t.Primary = p.Primary
	t.OnPrimary = p.OnPrimary
	t.PrimaryContainer = p.PrimaryContainer
	t.OnPrimaryContainer = p.OnPrimaryContainer
	t.Outline = p.Outline
	t.OutlineVariant = p.OutlineVariant
	t.Error = p.Error
	t.OnError = p.OnError

	t.Background = p.Surface
	t.Foreground = p.OnSurface
	t.Accent = p.Primary
	t.Muted = p.OnSurfaceVariant
	// The bar and the panels are one continuous Surface with no gap between
	// them, so a bar capsule and a panel card are the same fill on the same
	// background. Stratifying them into mid and high put two greys inches
	// apart on that shared surface; each resolves to the high container and
	// Highest stays for the controls that sit on top of them.
	t.Capsule = p.SurfaceContainerHigh
	t.Container = p.PrimaryContainer
	t.OnAccent = p.OnPrimary
	t.OnContainer = p.OnPrimaryContainer
}

// parsePalette converts the validated token strings to painter colours.
func parsePalette(tok theme.Tokens) (Palette, error) {
	var p Palette
	for _, f := range []struct {
		value string
		dest  *Color
	}{
		{tok.Surface, &p.Surface},
		{tok.SurfaceDim, &p.SurfaceDim},
		{tok.SurfaceBright, &p.SurfaceBright},
		{tok.SurfaceContainerLowest, &p.SurfaceContainerLowest},
		{tok.SurfaceContainerLow, &p.SurfaceContainerLow},
		{tok.SurfaceContainer, &p.SurfaceContainer},
		{tok.SurfaceContainerHigh, &p.SurfaceContainerHigh},
		{tok.SurfaceContainerHighest, &p.SurfaceContainerHighest},
		{tok.SurfaceVariant, &p.SurfaceVariant},
		{tok.OnSurface, &p.OnSurface},
		{tok.OnSurfaceVariant, &p.OnSurfaceVariant},
		{tok.Primary, &p.Primary},
		{tok.OnPrimary, &p.OnPrimary},
		{tok.PrimaryContainer, &p.PrimaryContainer},
		{tok.OnPrimaryContainer, &p.OnPrimaryContainer},
		{tok.Secondary, &p.Secondary},
		{tok.OnSecondary, &p.OnSecondary},
		{tok.SecondaryContainer, &p.SecondaryContainer},
		{tok.OnSecondaryContainer, &p.OnSecondaryContainer},
		{tok.Tertiary, &p.Tertiary},
		{tok.OnTertiary, &p.OnTertiary},
		{tok.TertiaryContainer, &p.TertiaryContainer},
		{tok.OnTertiaryContainer, &p.OnTertiaryContainer},
		{tok.Error, &p.Error},
		{tok.OnError, &p.OnError},
		{tok.ErrorContainer, &p.ErrorContainer},
		{tok.OnErrorContainer, &p.OnErrorContainer},
		{tok.Outline, &p.Outline},
		{tok.OutlineVariant, &p.OutlineVariant},
		{tok.InverseSurface, &p.InverseSurface},
		{tok.InverseOnSurface, &p.InverseOnSurface},
		{tok.InversePrimary, &p.InversePrimary},
		{tok.Shadow, &p.Shadow},
		{tok.Scrim, &p.Scrim},
		{tok.SurfaceTint, &p.SurfaceTint},
	} {
		c, err := theme.ParseColor(f.value)
		if err != nil {
			return Palette{}, err
		}
		*f.dest = Color{R: c.R, G: c.G, B: c.B, A: c.A}
	}
	return p, nil
}

// DefaultTheme is the resolved theme for the default configuration and the
// compiled fallback palette. It is what a surface looks like before any
// palette has been generated.
func DefaultTheme() Theme {
	t, err := ResolveTheme(config.Default(), config.Default().Bar, theme.Fallback)
	if err != nil {
		panic("shell: the default theme does not resolve: " + err.Error())
	}
	return t
}

// ThemeFromTokens maps generated Material 3 tokens onto the default
// composition at one radius.
func ThemeFromTokens(tok theme.Tokens, radius int) Theme {
	cfg := config.Default()
	cfg.Theme.Radius = radius
	bar := cfg.Bar
	bar.Radius = radius
	t, err := ResolveTheme(cfg, bar, tok)
	if err != nil {
		return DefaultTheme()
	}
	return t
}

// ThemeFrom resolves a validated configuration against the compiled fallback
// palette. It is what a bare Bar in a test paints with; every surface the
// shell actually paints resolves through the registry, which a test in this
// package enforces.
func ThemeFrom(cfg config.Config, bar config.Bar) Theme {
	t, err := ResolveTheme(cfg, bar, theme.Fallback)
	if err != nil {
		return DefaultTheme()
	}
	return t
}

func withBarGeometry(t Theme, bar config.Bar) Theme {
	t.Metrics.BarHeight = bar.Height
	t.Metrics.BarPadding = bar.Padding
	t.Metrics.BarSpacing = bar.Spacing
	t.BarGap = bar.Gap
	if bar.FontSize > 0 {
		spec := t.Type.Roles[theme.RoleBody]
		spec.Size = bar.FontSize
		t.Type.Roles[theme.RoleBody] = spec
	}
	applyFlat(&t)
	return t
}

// LerpColors blends this theme's colours toward another. Geometry is not
// interpolated: heights, padding, and radii come from the incoming theme at
// once, because a control that changes size mid-fade would relayout every
// frame. Only the palette travels.
func (t Theme) LerpColors(to Theme, progress float64) Theme {
	out := to
	blend := func(from, dest Color) Color { return render.LerpColor(from, dest, progress) }
	a, b := t.Palette, to.Palette
	p := &out.Palette
	for _, f := range []struct {
		from, dest Color
		out        *Color
	}{
		{a.Surface, b.Surface, &p.Surface},
		{a.SurfaceDim, b.SurfaceDim, &p.SurfaceDim},
		{a.SurfaceBright, b.SurfaceBright, &p.SurfaceBright},
		{a.SurfaceContainerLowest, b.SurfaceContainerLowest, &p.SurfaceContainerLowest},
		{a.SurfaceContainerLow, b.SurfaceContainerLow, &p.SurfaceContainerLow},
		{a.SurfaceContainer, b.SurfaceContainer, &p.SurfaceContainer},
		{a.SurfaceContainerHigh, b.SurfaceContainerHigh, &p.SurfaceContainerHigh},
		{a.SurfaceContainerHighest, b.SurfaceContainerHighest, &p.SurfaceContainerHighest},
		{a.SurfaceVariant, b.SurfaceVariant, &p.SurfaceVariant},
		{a.OnSurface, b.OnSurface, &p.OnSurface},
		{a.OnSurfaceVariant, b.OnSurfaceVariant, &p.OnSurfaceVariant},
		{a.Primary, b.Primary, &p.Primary},
		{a.OnPrimary, b.OnPrimary, &p.OnPrimary},
		{a.PrimaryContainer, b.PrimaryContainer, &p.PrimaryContainer},
		{a.OnPrimaryContainer, b.OnPrimaryContainer, &p.OnPrimaryContainer},
		{a.Secondary, b.Secondary, &p.Secondary},
		{a.OnSecondary, b.OnSecondary, &p.OnSecondary},
		{a.SecondaryContainer, b.SecondaryContainer, &p.SecondaryContainer},
		{a.OnSecondaryContainer, b.OnSecondaryContainer, &p.OnSecondaryContainer},
		{a.Tertiary, b.Tertiary, &p.Tertiary},
		{a.OnTertiary, b.OnTertiary, &p.OnTertiary},
		{a.TertiaryContainer, b.TertiaryContainer, &p.TertiaryContainer},
		{a.OnTertiaryContainer, b.OnTertiaryContainer, &p.OnTertiaryContainer},
		{a.Error, b.Error, &p.Error},
		{a.OnError, b.OnError, &p.OnError},
		{a.ErrorContainer, b.ErrorContainer, &p.ErrorContainer},
		{a.OnErrorContainer, b.OnErrorContainer, &p.OnErrorContainer},
		{a.Outline, b.Outline, &p.Outline},
		{a.OutlineVariant, b.OutlineVariant, &p.OutlineVariant},
		{a.InverseSurface, b.InverseSurface, &p.InverseSurface},
		{a.InverseOnSurface, b.InverseOnSurface, &p.InverseOnSurface},
		{a.InversePrimary, b.InversePrimary, &p.InversePrimary},
		{a.Shadow, b.Shadow, &p.Shadow},
		{a.Scrim, b.Scrim, &p.Scrim},
		{a.SurfaceTint, b.SurfaceTint, &p.SurfaceTint},
	} {
		*f.out = blend(f.from, f.dest)
	}
	applyFlat(&out)
	return out
}

// Style projects the resolved theme onto the painter's view. Every surface
// builds its style here so a control cannot inherit a different palette from
// the pill beside it; callers supply only geometry that varies per surface --
// scale, body, and attach edge.
func (t Theme) Style() render.Style {
	p := t.Palette
	return render.Style{
		Size:       t.TextSize,
		Radius:     t.Radius,
		CardRadius: t.CardRadius,

		Background: p.Surface,
		Foreground: p.OnSurface,
		Track:      p.OnSurfaceVariant,
		Accent:     p.Primary,
		// AccentOn is the toggled accent, which the bar spends on a failure
		// state rather than a second brand colour.
		AccentOn:         p.Error,
		Error:            p.Error,
		OnPrimary:        p.OnPrimary,
		OnError:          p.OnError,
		Capsule:          p.SurfaceContainerHigh,
		ContainerHighest: p.SurfaceContainerHighest,
		Container:        p.PrimaryContainer,
		OnAccent:         p.OnPrimary,
		OnContainer:      p.OnPrimaryContainer,
		Outline:          p.Outline,
		OutlineVariant:   p.OutlineVariant,

		Metrics:        t.Metrics,
		Shapes:         t.Shapes,
		Type:           t.Type,
		SurfaceOpacity: 0xff,
		Elevation:      t.Elevation,
		Shadow:         p.Shadow,
		Scrim:          p.Scrim,
		Motion:         t.Motion,
	}
}

// BackgroundOpaque reports whether the surface token is fully opaque.
func (t Theme) BackgroundOpaque() bool {
	return t.Background.A == 0xff && t.Surfaces.Bar == 0xff
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
	for _, s := range []struct {
		name  string
		alpha uint8
	}{{"bar", t.Surfaces.Bar}, {"panel", t.Surfaces.Panel}, {"overlay", t.Surfaces.Overlay}} {
		if s.alpha == 0 {
			return fmt.Errorf("shell: %s surface is fully transparent", s.name)
		}
	}
	return nil
}

// parseColor reads #RRGGBB or #RRGGBBAA, falling back when the string is not
// one of those shapes. The palette itself is parsed strictly by parsePalette;
// this stays for the few sites that carry a colour string of their own.
func parseColor(s string, fallback Color) Color {
	c, err := theme.ParseColor(s)
	if err != nil {
		return fallback
	}
	return Color{R: c.R, G: c.G, B: c.B, A: c.A}
}
