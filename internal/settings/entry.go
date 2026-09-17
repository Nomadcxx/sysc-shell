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
