package theming

import (
	"strings"
	"testing"

	"github.com/Nomadcxx/sysc-shell/internal/theme"
)

func TestCatalogEmbedsAllTemplates(t *testing.T) {
	t.Parallel()
	c := Catalog()
	if len(c.Names()) != 16 {
		t.Fatalf("got %d", len(c.Names()))
	}
	for _, n := range c.Names() {
		if c.Template(n) == "" {
			t.Fatalf("empty template %s", n)
		}
	}
}

func TestRenderNiriTemplate(t *testing.T) {
	t.Parallel()
	tok := theme.Fallback
	out := Render(Catalog().Template("niri"), tok)
	if !strings.Contains(out, tok.Primary) {
		t.Fatalf("missing primary %q in %s", tok.Primary, out)
	}
	if !strings.Contains(out, tok.Outline) || !strings.Contains(out, tok.SurfaceContainer) {
		t.Fatalf("missing border/shadow tokens in %s", out)
	}
}

func TestRenderHandlesMissingTokensGracefully(t *testing.T) {
	t.Parallel()
	out := Render("x={{.NoSuchToken}}", theme.Tokens{})
	if out != "x=" {
		t.Fatalf("got %q, want empty substitution", out)
	}
}

// TestCatalogExportsEveryValidatedRole is the parity check between the palette
// and what a template can reach. The export used to be a hand-listed eleven of
// the forty-nine roles, so a template asking for a secondary or a fixed accent
// silently rendered nothing.
func TestCatalogExportsEveryValidatedRole(t *testing.T) {
	t.Parallel()
	data := theme.Fallback.Export()
	complete := theme.Fallback
	if err := complete.Valid(false); err != nil {
		t.Fatalf("the fallback palette is not valid: %v", err)
	}
	for _, name := range []string{
		"Primary", "OnPrimary", "PrimaryContainer", "OnPrimaryContainer",
		"Secondary", "OnSecondary", "Tertiary", "OnTertiary",
		"Error", "OnError", "ErrorContainer", "OnErrorContainer",
		"Surface", "OnSurface", "SurfaceVariant", "OnSurfaceVariant",
		"SurfaceContainerLowest", "SurfaceContainerLow", "SurfaceContainer",
		"SurfaceContainerHigh", "SurfaceContainerHighest",
		"Outline", "OutlineVariant", "InverseSurface", "InverseOnSurface",
		"Shadow", "Scrim", "SurfaceTint", "PrimaryFixed", "OnPrimaryFixedVariant",
	} {
		v, ok := data[name]
		if !ok {
			t.Errorf("%s is not exported to templates", name)
			continue
		}
		if v == "" {
			t.Errorf("%s exports an empty value", name)
		}
	}
	// Every exported value is a colour; nothing else leaks into the context.
	for name, v := range data {
		if _, err := theme.ParseColor(v); err != nil {
			t.Errorf("%s = %q is not a colour", name, v)
		}
	}
}

// TestCatalogDoesNotExportComposition keeps the shell's own axes out of another
// application's colour file. Density and motion are not colours and mean
// nothing to kitty or GTK.
func TestCatalogDoesNotExportComposition(t *testing.T) {
	t.Parallel()
	data := theme.Fallback.Export()
	for _, name := range []string{
		"Density", "FontScale", "FontWeight", "Radius", "Motion",
		"MotionSpeed", "Elevation", "BarOpacity", "PanelOpacity", "OverlayOpacity",
	} {
		if _, ok := data[name]; ok {
			t.Errorf("%s is composition, not palette, and must not be exported", name)
		}
	}
}

// TestCatalogRenderCarriesModeAndSource proves the metadata reaches a template
// so it can branch on which half of the palette it is filling.
func TestCatalogRenderCarriesModeAndSource(t *testing.T) {
	t.Parallel()
	const tpl = "{{.Mode}}/{{.Source}}/{{.Surface}}"
	if got, want := RenderWith(tpl, theme.Fallback, "light", "wallpaper"),
		"light/wallpaper/"+theme.Fallback.Surface; got != want {
		t.Errorf("render = %q, want %q", got, want)
	}
	// The plain form still names a mode rather than leaving it blank.
	if got := Render(tpl, theme.Fallback); got != "dark//"+theme.Fallback.Surface {
		t.Errorf("default render = %q", got)
	}
}

// TestCatalogUnknownTokenRendersEmpty keeps a typo in a user template from
// producing a broken colour file: the key resolves to zero, not to garbage.
func TestCatalogUnknownTokenRendersEmpty(t *testing.T) {
	t.Parallel()
	if got := Render("[{{.NoSuchRole}}]", theme.Fallback); got != "[]" {
		t.Errorf("unknown token rendered %q, want an empty substitution", got)
	}
}
