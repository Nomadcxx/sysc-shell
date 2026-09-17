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
type Entry struct {
	Path     string
	Label    string
	Describe string
	Section  string
	Group    string
	Kind     Kind
	Options  []string
	Min, Max int

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
