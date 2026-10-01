package settings

import "github.com/Nomadcxx/sysc-shell/internal/config"

// Kind selects the control a settings surface renders for an entry. It is a
// presentation choice only: validation lives in the entry's own Setter, which
// is what a caller that bypasses the surface still goes through.
type Kind uint8

const (
	KindBool Kind = iota
	KindInt
	KindEnum
	KindString
	// KindHex, KindPath and KindFont are strings the surface can help with:
	// a colour it validates as it is typed, a directory it can browse, and a
	// family it can enumerate. Each still validates in its own Setter, so a
	// caller that bypasses the surface is held to the same rule.
	KindHex
	KindPath
	KindFont
)

// Getter reads one setting out of a configuration.
type Getter func(config.Config) string

// Setter writes one setting, validating the string form itself.
type Setter func(*config.Config, string) error

// Entry is one setting. Its accessors live here rather than in a switch so a
// setting is declared in exactly one place and the compiler enforces that a
// new entry carries them.
// Presentation is how a row shows its control (design D2). The zero value
// keeps the kind's own control, except that a short enum renders segmented.
type Presentation uint8

const (
	PresentAuto Presentation = iota
	PresentMenu
	PresentCards
	// PresentSlider draws an int as a slider even when its range is short
	// enough for a stepper, so related values in one card look alike (owner
	// decision, 2026-10-01: opacities and bar geometry).
	PresentSlider
	// PresentSwatch draws a theme-role enum as a dropdown of readable role
	// names beside a swatch of the selected role (Monitor's colours).
	PresentSwatch
)

type Entry struct {
	Path     string
	Label    string
	Describe string
	Section  string
	Group    string
	// Page is the tab within Section this entry sits on (design D1). Empty in
	// a section with no pages.
	Page string
	// Present overrides how the control is drawn (design D2).
	Present Presentation
	Kind    Kind
	Options []string
	// OptionLabels, when set, is what each option is called on screen, in
	// Options order; the option itself stays the stored value. Saved palettes
	// use it: the slug is stored, the name the user chose is shown.
	OptionLabels []string
	// EmptyLabel names the row a picker offers for the empty value, and is
	// set only where empty is a state the setting can actually hold. Most
	// cannot: the loader refuses an empty appearance font family, so a picker
	// offering the row would write a configuration the shell then declines to
	// start from. A text field could always be cleared, so this is what a menu
	// has to carry in its place.
	EmptyLabel string
	Min, Max   int
	// Unit follows the displayed value of a number ("%", "px"). Display
	// only: the stored value never carries it.
	Unit string

	Get Getter
	Set Setter
	// Default resolves this entry's default. It is per-entry because the
	// writer uses two rules: theme axes diff against the selected preset,
	// everything else against config.Default().
	Default Getter
}

// IsDefault reports whether this setting still sits on its default, which is
// what decides whether a row shows a reset control.
func (e Entry) IsDefault(c config.Config) bool {
	if e.Get == nil || e.Default == nil {
		return true
	}
	return e.Get(c) == e.Default(c)
}
