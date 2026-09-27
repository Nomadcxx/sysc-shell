package theme

import (
	"testing"
)

func terminalSources(t *testing.T) map[string]Tokens {
	t.Helper()
	src := map[string]Tokens{
		"fallback":    Fallback,
		"fallback_hc": FallbackHighContrast,
	}
	for _, name := range PaletteNames() {
		for _, mode := range []string{"dark", "light"} {
			tok, ok := NamedPalette(name, mode, false)
			if !ok {
				t.Fatalf("NamedPalette(%q, %q) not found", name, mode)
			}
			src[name+"/"+mode] = tok
		}
	}
	return src
}

func TestTerminalBlockInExport(t *testing.T) {
	for src, tok := range terminalSources(t) {
		exp := tok.Export()
		for _, k := range TerminalNames() {
			if _, ok := exp[k]; !ok {
				t.Errorf("%s: Export missing %s", src, k)
			}
		}
	}
}

func TestTerminalContrasts(t *testing.T) {
	hues := []string{
		"TerminalNormalRed", "TerminalNormalGreen", "TerminalNormalYellow",
		"TerminalNormalBlue", "TerminalNormalMagenta", "TerminalNormalCyan",
		"TerminalBrightRed", "TerminalBrightGreen", "TerminalBrightYellow",
		"TerminalBrightBlue", "TerminalBrightMagenta", "TerminalBrightCyan",
	}
	for src, tok := range terminalSources(t) {
		exp := tok.Export()
		bg, err := ParseColor(exp["TerminalBackground"])
		if err != nil {
			t.Fatalf("%s: TerminalBackground %q: %v", src, exp["TerminalBackground"], err)
		}
		for _, k := range hues {
			c, err := ParseColor(exp[k])
			if err != nil {
				t.Fatalf("%s: %s %q: %v", src, k, exp[k], err)
			}
			if r := ContrastRatio(c, bg); r < TerminalContrast {
				t.Errorf("%s: %s contrast vs background = %.2f, want >= %.1f", src, k, r, TerminalContrast)
			}
		}
		fg, _ := ParseColor(exp["TerminalForeground"])
		if r := ContrastRatio(fg, bg); r < TerminalContrast {
			t.Errorf("%s: foreground contrast = %.2f, want >= %.1f", src, r, TerminalContrast)
		}
		selFg, _ := ParseColor(exp["TerminalSelectionForeground"])
		selBg, _ := ParseColor(exp["TerminalSelectionBackground"])
		if r := ContrastRatio(selFg, selBg); r < TextContrast {
			t.Errorf("%s: selection contrast = %.2f, want >= %.1f", src, r, TextContrast)
		}
	}
}

func TestTerminalAnchors(t *testing.T) {
	for src, tok := range terminalSources(t) {
		exp := tok.Export()
		if exp["TerminalCursor"] != exp["TerminalForeground"] {
			t.Errorf("%s: cursor %s != foreground %s", src, exp["TerminalCursor"], exp["TerminalForeground"])
		}
		if exp["TerminalCursorText"] != exp["TerminalBackground"] {
			t.Errorf("%s: cursor text %s != background %s", src, exp["TerminalCursorText"], exp["TerminalBackground"])
		}
		if exp["TerminalNormalWhite"] != exp["TerminalForeground"] {
			t.Errorf("%s: normal white != foreground", src)
		}
		if exp["TerminalNormalBlack"] != exp["TerminalSelectionBackground"] {
			t.Errorf("%s: normal black != selection background", src)
		}
	}
}
