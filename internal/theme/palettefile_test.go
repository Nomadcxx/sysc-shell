package theme

import (
	"strings"
	"testing"
)

// testFile is a valid palette built from the compiled Nord scheme, so no test
// depends on hand-copied colour values.
func testFile(t *testing.T) PaletteFile {
	t.Helper()
	dark, ok := NamedPalette("nord", "dark", false)
	if !ok {
		t.Fatal("no nord palette")
	}
	light, ok := NamedPalette("nord", "light", false)
	if !ok {
		t.Fatal("no nord light palette")
	}
	return PaletteFile{Name: "My Nord", Dark: dark.Roles(), Light: light.Roles()}
}

func TestNamedPalettesAreStrictHex(t *testing.T) {
	for _, name := range PaletteNames() {
		for _, mode := range []string{"dark", "light"} {
			tok, _ := NamedPalette(name, mode, false)
			for role, v := range tok.Roles() {
				if !ValidRoleColor(v) {
					t.Errorf("%s/%s role %s = %q, not #RRGGBB", name, mode, role, v)
				}
			}
		}
	}
}

func TestRolesRoundTripThroughParseRoles(t *testing.T) {
	tok, _ := NamedPalette("gruvbox", "dark", false)
	back, err := ParseRoles(tok.Roles())
	if err != nil {
		t.Fatal(err)
	}
	if back != tok {
		t.Fatal("ParseRoles(Roles()) did not reproduce the tokens")
	}
	if len(tok.Roles()) != len(RoleNames()) {
		t.Fatalf("Roles has %d keys, want %d", len(tok.Roles()), len(RoleNames()))
	}
}

func TestSlugify(t *testing.T) {
	cases := []struct{ in, want string }{
		{"My Nord", "my-nord"},
		{"  Nord!! ", "nord"},
		{"a   b", "a-b"},
		{"---", ""},
		{"Café", "caf"},
		{"日本語", ""},
		{strings.Repeat("a", 60), strings.Repeat("a", 44)},
	}
	for _, c := range cases {
		if got := Slugify(c.in); got != c.want {
			t.Errorf("Slugify(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestBaseSlugFallsBackForNonASCIINames(t *testing.T) {
	if got := baseSlug("日本語"); got != "palette" {
		t.Fatalf("baseSlug = %q, want palette", got)
	}
	if got := baseSlug("Café"); got != "caf" {
		t.Fatalf("baseSlug = %q, want caf", got)
	}
}

func TestCleanName(t *testing.T) {
	ok := []string{"My Nord", "日本語", " padded "}
	for _, n := range ok {
		if _, err := cleanName(n); err != nil {
			t.Errorf("cleanName(%q): %v", n, err)
		}
	}
	bad := []string{"", "   ", "!!!", "a\nb", strings.Repeat("x", 81)}
	for _, n := range bad {
		if _, err := cleanName(n); err == nil {
			t.Errorf("cleanName(%q) accepted", n)
		}
	}
}

func TestValidSlug(t *testing.T) {
	for _, s := range []string{"nord", "my-nord", "my-nord-2", "a1"} {
		if !ValidSlug(s) {
			t.Errorf("ValidSlug(%q) = false", s)
		}
	}
	for _, s := range []string{"", "Nord", "../x", "a/b", "a--b", "-a", "a-", "a b", strings.Repeat("a", 49)} {
		if ValidSlug(s) {
			t.Errorf("ValidSlug(%q) = true", s)
		}
	}
}

func TestPaletteFileValidate(t *testing.T) {
	base := testFile(t)
	if err := base.Validate(); err != nil {
		t.Fatalf("valid file refused: %v", err)
	}
	mutate := func(f func(*PaletteFile)) PaletteFile {
		c := testFile(t)
		f(&c)
		return c
	}
	cases := []struct {
		name string
		file PaletteFile
		want string
	}{
		{"missing role", mutate(func(f *PaletteFile) { delete(f.Dark, "primary") }), "primary"},
		{"unknown role", mutate(func(f *PaletteFile) { f.Light["not_a_role"] = "#000000" }), "not_a_role"},
		{"alpha colour", mutate(func(f *PaletteFile) { f.Dark["primary"] = "#112233ff" }), "primary"},
		{"not hex", mutate(func(f *PaletteFile) { f.Light["surface"] = "red" }), "surface"},
		{"empty name", mutate(func(f *PaletteFile) { f.Name = "" }), "name"},
		{"weak contrast", mutate(func(f *PaletteFile) { f.Dark["on_surface"] = f.Dark["surface"] }), "on_surface"},
	}
	for _, c := range cases {
		err := c.file.Validate()
		if err == nil {
			t.Errorf("%s: accepted", c.name)
			continue
		}
		if !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: error %q does not mention %q", c.name, err, c.want)
		}
	}
}

func TestPaletteFileTokensRepairsOnRead(t *testing.T) {
	f := testFile(t)
	f.Dark["on_surface"] = f.Dark["surface"]
	if f.Validate() == nil {
		t.Fatal("setup: weak palette passed Validate")
	}
	tok, err := f.Tokens("dark", false)
	if err != nil {
		t.Fatalf("Tokens refused a repairable palette: %v", err)
	}
	if err := tok.Valid(false); err != nil {
		t.Fatalf("returned tokens are not valid: %v", err)
	}
}

func TestContrastFailuresAreStructuredAndMatchValid(t *testing.T) {
	tok, _ := NamedPalette("nord", "dark", false)
	if got := tok.ContrastFailures(false); len(got) != 0 {
		t.Fatalf("a compiled palette reports failures: %+v", got)
	}
	tok.OnSurface = tok.Surface
	got := tok.ContrastFailures(false)
	if len(got) == 0 {
		t.Fatal("on_surface equal to surface reported no failure")
	}
	first := got[0]
	if first.Fg != "on_surface" || first.Bg != "surface" || first.Ratio >= first.Floor || first.Floor != TextRatio(false) {
		t.Fatalf("first failure = %+v", first)
	}
	err := tok.Valid(false)
	if err == nil || !strings.Contains(err.Error(), "on_surface on surface is 1.00:1, below the 4.5:1 floor") {
		t.Fatalf("Valid text changed: %v", err)
	}
}

func TestPaletteFileTokensHonoursHighContrast(t *testing.T) {
	// A palette whose accents allow it is resolved at the stricter floor.
	dark, _ := NamedPalette("nord", "dark", true)
	light, _ := NamedPalette("nord", "light", true)
	f := PaletteFile{Name: "Nord HC", Dark: dark.Roles(), Light: light.Roles()}
	tok, err := f.Tokens("light", true)
	if err != nil {
		t.Fatal(err)
	}
	if err := tok.Valid(true); err != nil {
		t.Fatalf("high-contrast tokens fail the stricter floor: %v", err)
	}
	// Repair moves foregrounds only, so a mid-tone accent that no text colour
	// clears at 7:1 is refused with the reason, never painted below the floor.
	if _, err := testFile(t).Tokens("light", true); err == nil || !strings.Contains(err.Error(), "7.0:1") {
		t.Fatalf("unreachable high contrast: err = %v, want a refusal naming the floor", err)
	}
}
