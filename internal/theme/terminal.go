package theme

// Terminal palette block, ported from Noctalia's
// synthesizeTerminalPaletteTokens (src/theme/fixed_palette.cpp). It is not a
// set of matugen wire roles: the values are derived from the palette at
// export time, so every palette source (generated, fallback, named) carries
// the block without a second table to keep in step.

// TerminalContrast is the ANSI text contrast floor a mapped colour must reach
// against the terminal background before it is written.
//
// ponytail: one global floor, the constant Noctalia hard-codes in
// ensureTerminalTextContrast. Raise it per user if a measured complaint
// arrives; black, bright black and the whites are anchors, not text colours,
// and are deliberately not clamped.
const TerminalContrast = 4.5

// TerminalNames lists the export keys of the terminal block in template
// order. The catalogue and the golden tests walk it so a missing key fails
// loudly.
func TerminalNames() []string {
	return []string{
		"TerminalBackground", "TerminalForeground", "TerminalCursor", "TerminalCursorText",
		"TerminalSelectionForeground", "TerminalSelectionBackground",
		"TerminalNormalBlack", "TerminalNormalRed", "TerminalNormalGreen", "TerminalNormalYellow",
		"TerminalNormalBlue", "TerminalNormalMagenta", "TerminalNormalCyan", "TerminalNormalWhite",
		"TerminalBrightBlack", "TerminalBrightRed", "TerminalBrightGreen", "TerminalBrightYellow",
		"TerminalBrightBlue", "TerminalBrightMagenta", "TerminalBrightCyan", "TerminalBrightWhite",
	}
}

// colorRole parses one palette role by its wire name. A missing or unparsable
// role yields black, matching the fallbacks Noctalia threads through tokenOr;
// Complete() has already run for any published palette, so in practice this
// only guards against direct struct misuse in tests.
func (t Tokens) colorRole(name string) Color {
	for _, r := range roles {
		if r.name == name {
			c, err := ParseColor(*r.get(&t))
			if err != nil {
				return Color{}
			}
			return c
		}
	}
	panic("theme: unknown role " + name)
}

// Terminal derives the terminal block from the palette.
func (t Tokens) Terminal() map[string]string {
	bg := t.colorRole("surface_container")
	fg := t.colorRole("on_surface")
	clamp := func(role string) string {
		c := t.colorRole(role)
		if ContrastRatio(c, bg) >= TerminalContrast {
			return c.Hex()
		}
		return EnsureContrast(c, bg, TerminalContrast).Hex()
	}
	white := fg.Hex()
	red, green := clamp("error"), clamp("primary")
	yellow, blue := clamp("secondary"), clamp("tertiary")
	magenta, cyan := clamp("primary_fixed_dim"), clamp("secondary_fixed_dim")
	return map[string]string{
		"TerminalBackground":          bg.Hex(),
		"TerminalForeground":          white,
		"TerminalCursor":              white,
		"TerminalCursorText":          bg.Hex(),
		"TerminalSelectionForeground": t.colorRole("on_surface_variant").Hex(),
		"TerminalSelectionBackground": t.colorRole("surface_variant").Hex(),
		"TerminalNormalBlack":         t.colorRole("surface_variant").Hex(),
		"TerminalNormalRed":           red,
		"TerminalNormalGreen":         green,
		"TerminalNormalYellow":        yellow,
		"TerminalNormalBlue":          blue,
		"TerminalNormalMagenta":       magenta,
		"TerminalNormalCyan":          cyan,
		"TerminalNormalWhite":         white,
		"TerminalBrightBlack":         t.colorRole("outline").Hex(),
		"TerminalBrightRed":           red,
		"TerminalBrightGreen":         green,
		"TerminalBrightYellow":        yellow,
		"TerminalBrightBlue":          blue,
		"TerminalBrightMagenta":       magenta,
		"TerminalBrightCyan":          cyan,
		"TerminalBrightWhite":         white,
	}
}
