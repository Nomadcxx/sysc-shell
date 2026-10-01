package theme

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

// MaxPaletteBytes caps a palette file on disk and an import.
const MaxPaletteBytes = 64 << 10

const (
	maxPaletteNameRunes = 80
	// maxSlugLen leaves room for a "-999" collision suffix after a 44-byte base.
	maxSlugLen  = 48
	maxBaseSlug = 44
)

var (
	slugPattern = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)
	strictHex   = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)
)

// roleSet is the closed vocabulary a palette file may name.
var roleSet = func() map[string]bool {
	m := make(map[string]bool, len(roles))
	for _, r := range roles {
		m[r.name] = true
	}
	return m
}()

// PaletteFile is the on-disk and interchange shape of a custom palette.
type PaletteFile struct {
	Name  string            `json:"name"`
	Dark  map[string]string `json:"dark"`
	Light map[string]string `json:"light"`
}

// ValidSlug reports whether s can name a stored palette. The slug is also the
// filename stem, so this is what keeps a name from leaving the directory.
func ValidSlug(s string) bool { return len(s) <= maxSlugLen && slugPattern.MatchString(s) }

// ValidRoleColor reports whether s is a stored role value: #RRGGBB, no alpha.
func ValidRoleColor(s string) bool { return strictHex.MatchString(s) }

// Slugify reduces a name to its [a-z0-9-] stem, or "" when it has none.
func Slugify(name string) string {
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(name) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			if dash && b.Len() > 0 {
				b.WriteByte('-')
			}
			dash = false
			b.WriteRune(r)
			continue
		}
		dash = true
	}
	s := b.String()
	if len(s) > maxBaseSlug {
		s = strings.TrimRight(s[:maxBaseSlug], "-")
	}
	return s
}

// baseSlug is the slug a name starts from; a name with no ASCII letters or
// digits (a valid name in another script) falls back to "palette".
func baseSlug(name string) string {
	if s := Slugify(name); s != "" {
		return s
	}
	return "palette"
}

// cleanName trims a display name and refuses ones that cannot name anything.
func cleanName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", errors.New("theme: a palette needs a name")
	}
	if utf8.RuneCountInString(name) > maxPaletteNameRunes {
		return "", fmt.Errorf("theme: a palette name is at most %d characters", maxPaletteNameRunes)
	}
	word := false
	for _, r := range name {
		if unicode.IsControl(r) {
			return "", errors.New("theme: a palette name cannot contain control characters")
		}
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			word = true
		}
	}
	if !word {
		return "", errors.New("theme: a palette name needs a letter or a digit")
	}
	return name, nil
}

func (f PaletteFile) normalized() PaletteFile {
	lower := func(in map[string]string) map[string]string {
		out := make(map[string]string, len(in))
		for k, v := range in {
			out[k] = strings.ToLower(v)
		}
		return out
	}
	return PaletteFile{Name: strings.TrimSpace(f.Name), Dark: lower(f.Dark), Light: lower(f.Light)}
}

func checkRoleMap(mode string, m map[string]string) error {
	for name := range m {
		if !roleSet[name] {
			return fmt.Errorf("theme: %s: %q is not a palette role", mode, name)
		}
	}
	for _, r := range roles {
		v, ok := m[r.name]
		if !ok {
			return fmt.Errorf("theme: %s: role %s is missing", mode, r.name)
		}
		if !strictHex.MatchString(v) {
			return fmt.Errorf("theme: %s: role %s is %q, not #RRGGBB", mode, r.name, v)
		}
	}
	return nil
}

// validateShape is the read-path check: name and exact role sets.
func (f PaletteFile) validateShape() error {
	if _, err := cleanName(f.Name); err != nil {
		return err
	}
	if err := checkRoleMap("dark", f.Dark); err != nil {
		return err
	}
	return checkRoleMap("light", f.Light)
}

// Validate is the write-path check: shape, then both modes pass the contrast
// floors exactly as written. Nothing the shell stores is silently adjusted.
func (f PaletteFile) Validate() error {
	if err := f.validateShape(); err != nil {
		return err
	}
	for _, m := range []struct {
		name  string
		roles map[string]string
	}{{"dark", f.Dark}, {"light", f.Light}} {
		tok, err := ParseRoles(m.roles)
		if err != nil {
			return fmt.Errorf("%s: %w", m.name, err)
		}
		if err := tok.Valid(false); err != nil {
			return fmt.Errorf("%s: %w", m.name, err)
		}
	}
	return nil
}

// Tokens is the read-path resolution of one mode: parse, check against the
// floor in force, repair foregrounds if needed, refuse only if still failing.
func (f PaletteFile) Tokens(mode string, highContrast bool) (Tokens, error) {
	src := f.Dark
	if strings.EqualFold(mode, "light") {
		src = f.Light
	}
	tok, err := ParseRoles(src)
	if err != nil {
		return Tokens{}, err
	}
	if tok.Valid(highContrast) == nil {
		return tok, nil
	}
	tok = tok.Repair(highContrast)
	if err := tok.Valid(highContrast); err != nil {
		return Tokens{}, fmt.Errorf("theme: palette cannot be made readable: %w", err)
	}
	return tok, nil
}
