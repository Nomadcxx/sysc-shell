package theming

import (
	"os"
	"strings"
	"testing"

	"github.com/Nomadcxx/sysc-shell/internal/theme"
)

// renderComplete renders a template with the fallback palette and fails if
// the result lost the marker header or contains an unrendered {{.Key}}.
func renderComplete(t *testing.T, name string) string {
	t.Helper()
	out := Render(Catalog().Template(name), theme.Fallback)
	if !strings.Contains(strings.SplitN(out, "\n", 2)[0], marker) {
		t.Fatalf("%s: marker header lost", name)
	}
	if strings.Contains(out, "{{") {
		t.Fatalf("%s: unrendered template action:\n%s", name, out)
	}
	return out
}

// assertHexAssignments checks every line whose value is a quoted or bare
// #RRGGBB literal; a missing token would render empty and fail here or in
// the caller's required-section checks.
func assertHexAssignments(t *testing.T, out string) {
	t.Helper()
	for _, line := range strings.Split(out, "\n") {
		_, val, ok := strings.Cut(strings.TrimSpace(line), "=")
		if !ok {
			continue
		}
		val = strings.TrimSpace(val)
		if !strings.Contains(val, "#") {
			continue
		}
		hex := strings.TrimRight(val[strings.Index(val, "#"):], "'\" \t")
		if _, err := theme.ParseColor(hex); err != nil {
			t.Errorf("bad colour assignment: %s: %v", line, err)
		}
	}
}

func TestAlacrittyTemplateRendersCompleteOutput(t *testing.T) {
	t.Parallel()
	out := renderComplete(t, "alacritty")
	assertHexAssignments(t, out)
	for _, want := range []string{
		"[colors.primary]", "[colors.cursor]", "[colors.selection]",
		"[colors.normal]", "[colors.bright]", "[colors.dim]",
		"[colors.vi_mode_cursor]", "[colors.search.matches]", "[colors.footer_bar]",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %s", want)
		}
	}
	for _, key := range []string{"black", "red", "green", "yellow", "blue", "magenta", "cyan", "white"} {
		if n := strings.Count(out, key+" "); n < 2 {
			t.Errorf("ANSI key %s assigned %d times, want normal+bright at least", key, n)
		}
	}
	if n := strings.Count(out, "= '"); n < 40 {
		t.Errorf("colour assignments = %d, want the full Noctalia body set", n)
	}
}

func TestAlacrittyAppliesSidecarAndImport(t *testing.T) {
	home := t.TempDir()
	markTemplatesComplete(t, "alacritty")
	if err := ApplyEnabled(home, func(string) bool { return true }, theme.Fallback); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(templateTargets["alacritty"].sidecar(home))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "[colors.primary]") {
		t.Fatalf("sidecar is not the alacritty body: %q", b)
	}
	cfg, err := os.ReadFile(templateTargets["alacritty"].directives(home)[0].file)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(cfg), "import = [") != 1 {
		t.Fatalf("import not managed: %q", cfg)
	}
}
