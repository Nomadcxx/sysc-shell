// Package theme generates the Material 3 token set the shell renders from.
package theme

import (
	"errors"
	"fmt"
)

// Tokens is the Material 3 role family the shell and the application template
// catalogue consume. Dark and light variants are generated together; Mode
// selects which is active.
//
// The struct stays concrete rather than a name-keyed map: a missing role is
// then a compile error at the point that reads it, and the wire template, the
// parser, and the validator can be checked against one another.
type Tokens struct {
	// Accent roles and their containers.
	Primary              string
	OnPrimary            string
	PrimaryContainer     string
	OnPrimaryContainer   string
	Secondary            string
	OnSecondary          string
	SecondaryContainer   string
	OnSecondaryContainer string
	Tertiary             string
	OnTertiary           string
	TertiaryContainer    string
	OnTertiaryContainer  string

	// Error roles and their container pair.
	Error            string
	OnError          string
	ErrorContainer   string
	OnErrorContainer string

	// Surface, the container ladder, and the paired surface foregrounds.
	Surface                 string
	OnSurface               string
	SurfaceVariant          string
	OnSurfaceVariant        string
	SurfaceDim              string
	SurfaceBright           string
	SurfaceContainerLowest  string
	SurfaceContainerLow     string
	SurfaceContainer        string
	SurfaceContainerHigh    string
	SurfaceContainerHighest string

	// Background is the pre-Material-3 name for the root surface. Application
	// templates still reference it, so it is exported rather than derived.
	Background   string
	OnBackground string

	// Boundaries, inverse roles, and the compositing roles.
	Outline          string
	OutlineVariant   string
	InverseSurface   string
	InverseOnSurface string
	InversePrimary   string
	Shadow           string
	Scrim            string
	SurfaceTint      string

	// Fixed accent roles hold one tone across light and dark. The shell has no
	// visual consumer for them; the template catalogue exports them so an
	// application theme can key off a stable accent.
	PrimaryFixed            string
	PrimaryFixedDim         string
	OnPrimaryFixed          string
	OnPrimaryFixedVariant   string
	SecondaryFixed          string
	SecondaryFixedDim       string
	OnSecondaryFixed        string
	OnSecondaryFixedVariant string
	TertiaryFixed           string
	TertiaryFixedDim        string
	OnTertiaryFixed         string
	OnTertiaryFixedVariant  string
}

// role names one field for the parser, the validator, and error messages. The
// wire name matches the matugen template key so a mismatch is one table edit.
type role struct {
	name string
	get  func(*Tokens) *string
}

// roles is the single ordered list of every token. The template, the parser,
// Complete, and Valid all walk it, so a role cannot be added to one and
// forgotten in another.
var roles = []role{
	{"primary", func(t *Tokens) *string { return &t.Primary }},
	{"on_primary", func(t *Tokens) *string { return &t.OnPrimary }},
	{"primary_container", func(t *Tokens) *string { return &t.PrimaryContainer }},
	{"on_primary_container", func(t *Tokens) *string { return &t.OnPrimaryContainer }},
	{"secondary", func(t *Tokens) *string { return &t.Secondary }},
	{"on_secondary", func(t *Tokens) *string { return &t.OnSecondary }},
	{"secondary_container", func(t *Tokens) *string { return &t.SecondaryContainer }},
	{"on_secondary_container", func(t *Tokens) *string { return &t.OnSecondaryContainer }},
	{"tertiary", func(t *Tokens) *string { return &t.Tertiary }},
	{"on_tertiary", func(t *Tokens) *string { return &t.OnTertiary }},
	{"tertiary_container", func(t *Tokens) *string { return &t.TertiaryContainer }},
	{"on_tertiary_container", func(t *Tokens) *string { return &t.OnTertiaryContainer }},
	{"error", func(t *Tokens) *string { return &t.Error }},
	{"on_error", func(t *Tokens) *string { return &t.OnError }},
	{"error_container", func(t *Tokens) *string { return &t.ErrorContainer }},
	{"on_error_container", func(t *Tokens) *string { return &t.OnErrorContainer }},
	{"surface", func(t *Tokens) *string { return &t.Surface }},
	{"on_surface", func(t *Tokens) *string { return &t.OnSurface }},
	{"surface_variant", func(t *Tokens) *string { return &t.SurfaceVariant }},
	{"on_surface_variant", func(t *Tokens) *string { return &t.OnSurfaceVariant }},
	{"surface_dim", func(t *Tokens) *string { return &t.SurfaceDim }},
	{"surface_bright", func(t *Tokens) *string { return &t.SurfaceBright }},
	{"surface_container_lowest", func(t *Tokens) *string { return &t.SurfaceContainerLowest }},
	{"surface_container_low", func(t *Tokens) *string { return &t.SurfaceContainerLow }},
	{"surface_container", func(t *Tokens) *string { return &t.SurfaceContainer }},
	{"surface_container_high", func(t *Tokens) *string { return &t.SurfaceContainerHigh }},
	{"surface_container_highest", func(t *Tokens) *string { return &t.SurfaceContainerHighest }},
	{"background", func(t *Tokens) *string { return &t.Background }},
	{"on_background", func(t *Tokens) *string { return &t.OnBackground }},
	{"outline", func(t *Tokens) *string { return &t.Outline }},
	{"outline_variant", func(t *Tokens) *string { return &t.OutlineVariant }},
	{"inverse_surface", func(t *Tokens) *string { return &t.InverseSurface }},
	{"inverse_on_surface", func(t *Tokens) *string { return &t.InverseOnSurface }},
	{"inverse_primary", func(t *Tokens) *string { return &t.InversePrimary }},
	{"shadow", func(t *Tokens) *string { return &t.Shadow }},
	{"scrim", func(t *Tokens) *string { return &t.Scrim }},
	{"surface_tint", func(t *Tokens) *string { return &t.SurfaceTint }},
	{"primary_fixed", func(t *Tokens) *string { return &t.PrimaryFixed }},
	{"primary_fixed_dim", func(t *Tokens) *string { return &t.PrimaryFixedDim }},
	{"on_primary_fixed", func(t *Tokens) *string { return &t.OnPrimaryFixed }},
	{"on_primary_fixed_variant", func(t *Tokens) *string { return &t.OnPrimaryFixedVariant }},
	{"secondary_fixed", func(t *Tokens) *string { return &t.SecondaryFixed }},
	{"secondary_fixed_dim", func(t *Tokens) *string { return &t.SecondaryFixedDim }},
	{"on_secondary_fixed", func(t *Tokens) *string { return &t.OnSecondaryFixed }},
	{"on_secondary_fixed_variant", func(t *Tokens) *string { return &t.OnSecondaryFixedVariant }},
	{"tertiary_fixed", func(t *Tokens) *string { return &t.TertiaryFixed }},
	{"tertiary_fixed_dim", func(t *Tokens) *string { return &t.TertiaryFixedDim }},
	{"on_tertiary_fixed", func(t *Tokens) *string { return &t.OnTertiaryFixed }},
	{"on_tertiary_fixed_variant", func(t *Tokens) *string { return &t.OnTertiaryFixedVariant }},
}

// RoleNames lists every wire name in template order. The catalogue uses it so
// an exported palette cannot drift from the generated one.
func RoleNames() []string {
	out := make([]string, 0, len(roles))
	for _, r := range roles {
		out = append(out, r.name)
	}
	return out
}

// Contrast floors from design D5. Normal mode asks 4.5:1 of text and 3:1 of a
// meaningful boundary; high contrast raises those to 7:1 and 4.5:1.
const (
	TextContrast        = 4.5
	NonTextContrast     = 3.0
	HighContrastText    = 7.0
	HighContrastNonText = 4.5
)

// TextRatio and NonTextRatio give the floor in force for a mode.
func TextRatio(highContrast bool) float64 {
	if highContrast {
		return HighContrastText
	}
	return TextContrast
}

func NonTextRatio(highContrast bool) float64 {
	if highContrast {
		return HighContrastNonText
	}
	return NonTextContrast
}

// pair is one foreground painted on one background.
type pair struct {
	fg, bg  string
	getFg   func(*Tokens) *string
	getBg   func(*Tokens) *string
	nonText bool
}

func p(fg, bg string, nonText bool) pair {
	find := func(name string) func(*Tokens) *string {
		for _, r := range roles {
			if r.name == name {
				return r.get
			}
		}
		panic("theme: unknown role " + name)
	}
	return pair{fg: fg, bg: bg, getFg: find(fg), getBg: find(bg), nonText: nonText}
}

// pairs are the foreground/background combinations the shell and the template
// catalogue actually paint.
//
// OnSurface is checked against every rung of the container ladder because a
// capsule, a card, and a nested control each pick a different rung and all
// carry the same text colour. OutlineVariant is deliberately absent: Material
// specifies it as a low-emphasis divider, and real generated palettes put it
// as low as 1.6:1 against surface, so holding it to a boundary floor would
// reject valid input. Outline, the boundary the shell draws when a surface
// pair cannot separate on its own, is checked.
var pairs = []pair{
	p("on_surface", "surface", false),
	p("on_surface", "surface_dim", false),
	p("on_surface", "surface_bright", false),
	p("on_surface", "surface_container_lowest", false),
	p("on_surface", "surface_container_low", false),
	p("on_surface", "surface_container", false),
	p("on_surface", "surface_container_high", false),
	p("on_surface", "surface_container_highest", false),
	p("on_surface_variant", "surface", false),
	p("on_surface_variant", "surface_variant", false),
	p("on_background", "background", false),
	p("on_primary", "primary", false),
	p("on_primary_container", "primary_container", false),
	p("on_secondary", "secondary", false),
	p("on_secondary_container", "secondary_container", false),
	p("on_tertiary", "tertiary", false),
	p("on_tertiary_container", "tertiary_container", false),
	p("on_error", "error", false),
	p("on_error_container", "error_container", false),
	p("inverse_on_surface", "inverse_surface", false),
	p("on_primary_fixed", "primary_fixed", false),
	p("on_primary_fixed", "primary_fixed_dim", false),
	p("on_primary_fixed_variant", "primary_fixed", false),
	p("on_secondary_fixed", "secondary_fixed", false),
	p("on_secondary_fixed", "secondary_fixed_dim", false),
	p("on_secondary_fixed_variant", "secondary_fixed", false),
	p("on_tertiary_fixed", "tertiary_fixed", false),
	p("on_tertiary_fixed", "tertiary_fixed_dim", false),
	p("on_tertiary_fixed_variant", "tertiary_fixed", false),
	p("outline", "surface", true),
}

// Complete reports whether every role carries a parseable colour. A partial
// palette must never be published: with half the roles updated a surface
// paints in two themes at once, which is worse than keeping the old one, so a
// generator that returns an incomplete set is rejected whole.
func (t Tokens) Complete() error {
	for _, r := range roles {
		v := *r.get(&t)
		if _, err := ParseColor(v); err != nil {
			return fmt.Errorf("theme: role %s is %q, not a #RRGGBB colour", r.name, v)
		}
	}
	return nil
}

// Valid parses every role and checks each paired foreground against the
// background it is painted on. It is the gate a generated candidate passes
// before the registry publishes it.
func (t Tokens) Valid(highContrast bool) error {
	if err := t.Complete(); err != nil {
		return err
	}
	text, nonText := TextRatio(highContrast), NonTextRatio(highContrast)
	var errs []error
	for _, pr := range pairs {
		fg, err := ParseColor(*pr.getFg(&t))
		if err != nil {
			return err
		}
		bg, err := ParseColor(*pr.getBg(&t))
		if err != nil {
			return err
		}
		want := text
		if pr.nonText {
			want = nonText
		}
		if got := ContrastRatio(fg, bg); got < want {
			errs = append(errs, fmt.Errorf(
				"theme: %s on %s is %.2f:1, below the %.1f:1 floor", pr.fg, pr.bg, got, want))
		}
	}
	return errors.Join(errs...)
}

// Repair returns the palette with every failing paired foreground moved to the
// floor in force. It changes foregrounds only: an accent or surface hue is the
// palette's own choice, and rewriting one to manufacture separation would
// produce a theme the user did not ask for.
func (t Tokens) Repair(highContrast bool) Tokens {
	text, nonText := TextRatio(highContrast), NonTextRatio(highContrast)
	out := t
	for _, pr := range pairs {
		fg, err := ParseColor(*pr.getFg(&out))
		if err != nil {
			continue
		}
		bg, err := ParseColor(*pr.getBg(&out))
		if err != nil {
			continue
		}
		want := text
		if pr.nonText {
			want = nonText
		}
		if ContrastRatio(fg, bg) >= want {
			continue
		}
		*pr.getFg(&out) = EnsureContrast(fg, bg, want).Hex()
	}
	return out
}

type Source struct {
	Kind string // wallpaper | hex | stock
	Seed string
}

type Options struct {
	Mode         string // dark | light
	Scheme       string
	HighContrast bool
}
