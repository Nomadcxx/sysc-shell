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
