package theme

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// stubColors is real matugen output for one seed, complete in both modes. The
// generator now rejects a partial file, so a stub that names a handful of
// roles would only ever exercise the rejection path.
const stubColors = `{"dark":{"primary":"#b3c5ff","on_primary":"#192e60","primary_container":"#324578","on_primary_container":"#dbe1ff","secondary":"#c1c6dd","on_secondary":"#2a3042","secondary_container":"#414659","on_secondary_container":"#dde1f9","tertiary":"#e1bbdc","on_tertiary":"#422741","tertiary_container":"#5a3d58","on_tertiary_container":"#ffd6f8","error":"#ffb4ab","on_error":"#690005","error_container":"#93000a","on_error_container":"#ffdad6","surface":"#121318","on_surface":"#e3e2e9","surface_variant":"#45464f","on_surface_variant":"#c5c6d0","surface_dim":"#121318","surface_bright":"#38393f","surface_container_lowest":"#0d0e13","surface_container_low":"#1a1b21","surface_container":"#1e1f25","surface_container_high":"#292a2f","surface_container_highest":"#33343a","background":"#121318","on_background":"#e3e2e9","outline":"#8f909a","outline_variant":"#45464f","inverse_surface":"#e3e2e9","inverse_on_surface":"#2f3036","inverse_primary":"#4a5c92","shadow":"#000000","scrim":"#000000","surface_tint":"#b3c5ff","primary_fixed":"#dbe1ff","primary_fixed_dim":"#b3c5ff","on_primary_fixed":"#00184a","on_primary_fixed_variant":"#324578","secondary_fixed":"#dde1f9","secondary_fixed_dim":"#c1c6dd","on_secondary_fixed":"#151b2c","on_secondary_fixed_variant":"#414659","tertiary_fixed":"#ffd6f8","tertiary_fixed_dim":"#e1bbdc","on_tertiary_fixed":"#2b122b","on_tertiary_fixed_variant":"#5a3d58"},"light":{"primary":"#4a5c92","on_primary":"#ffffff","primary_container":"#dbe1ff","on_primary_container":"#00184a","secondary":"#585e72","on_secondary":"#ffffff","secondary_container":"#dde1f9","on_secondary_container":"#151b2c","tertiary":"#735471","on_tertiary":"#ffffff","tertiary_container":"#ffd6f8","on_tertiary_container":"#2b122b","error":"#ba1a1a","on_error":"#ffffff","error_container":"#ffdad6","on_error_container":"#410002","surface":"#faf8ff","on_surface":"#1a1b21","surface_variant":"#e2e2ec","on_surface_variant":"#45464f","surface_dim":"#dad9e0","surface_bright":"#faf8ff","surface_container_lowest":"#ffffff","surface_container_low":"#f4f3fa","surface_container":"#eeedf4","surface_container_high":"#e8e7ef","surface_container_highest":"#e3e2e9","background":"#faf8ff","on_background":"#1a1b21","outline":"#757680","outline_variant":"#c5c6d0","inverse_surface":"#2f3036","inverse_on_surface":"#f1f0f7","inverse_primary":"#b3c5ff","shadow":"#000000","scrim":"#000000","surface_tint":"#4a5c92","primary_fixed":"#dbe1ff","primary_fixed_dim":"#b3c5ff","on_primary_fixed":"#00184a","on_primary_fixed_variant":"#324578","secondary_fixed":"#dde1f9","secondary_fixed_dim":"#c1c6dd","on_secondary_fixed":"#151b2c","on_secondary_fixed_variant":"#414659","tertiary_fixed":"#ffd6f8","tertiary_fixed_dim":"#e1bbdc","on_tertiary_fixed":"#2b122b","on_tertiary_fixed_variant":"#5a3d58"}}`

func TestGenerateFromImageReturnsAValidatedPalette(t *testing.T) {
	dir := t.TempDir()
	fakeMatugen(t, dir)
	g := Generator{CacheDir: dir, Matugen: filepath.Join(dir, "matugen")}
	tok, err := g.Generate(Source{Kind: "wallpaper", Seed: "/tmp/wall.jpg"}, Options{Mode: "dark"})
	if err != nil {
		t.Fatal(err)
	}
	if err := tok.Valid(false); err != nil {
		t.Fatalf("generated palette did not validate: %v", err)
	}
	if tok == Fallback {
		t.Fatal("generated palette is the compiled fallback")
	}
	if _, err := os.Stat(filepath.Join(dir, "colors.json")); err != nil {
		t.Fatalf("cache file missing: %v", err)
	}
}

func TestParseColorsKeepsContainerLevelsDistinct(t *testing.T) {
	t.Parallel()
	// Bar capsules, panel cards, and idle controls each own a level. Folding
	// the high token onto SurfaceContainer is what made cards vanish into the
	// panel; the fix is a separate role per surface, not a brighter mid.
	tok := parseStub(t, "dark")
	for _, c := range []struct{ name, got string }{
		{"SurfaceContainer", tok.SurfaceContainer},
		{"SurfaceContainerHigh", tok.SurfaceContainerHigh},
		{"SurfaceContainerHighest", tok.SurfaceContainerHighest},
	} {
		if c.got == "" {
			t.Errorf("%s is empty", c.name)
		}
	}
	if tok.SurfaceContainer == tok.SurfaceContainerHigh ||
		tok.SurfaceContainerHigh == tok.SurfaceContainerHighest {
		t.Errorf("container levels collapsed: %q %q %q",
			tok.SurfaceContainer, tok.SurfaceContainerHigh, tok.SurfaceContainerHighest)
	}
}

func TestParseColorsReadsTheRequestedMode(t *testing.T) {
	t.Parallel()
	dark, light := parseStub(t, "dark"), parseStub(t, "light")
	if dark.Surface == light.Surface {
		t.Fatalf("dark and light resolved the same surface %q", dark.Surface)
	}
	for _, tok := range []Tokens{dark, light} {
		if err := tok.Valid(false); err != nil {
			t.Errorf("stub palette did not validate: %v", err)
		}
	}
}

func TestParseColorsRejectsAnIncompleteFile(t *testing.T) {
	t.Parallel()
	var file map[string]map[string]string
	if err := json.Unmarshal([]byte(stubColors), &file); err != nil {
		t.Fatal(err)
	}
	delete(file["dark"], "surface_tint")
	path := writeColors(t, file)
	if _, err := parseColors(path, "dark"); err == nil ||
		!strings.Contains(err.Error(), "surface_tint") {
		t.Fatalf("parseColors() = %v, want an error naming surface_tint", err)
	}
}

func TestParseColorsRejectsAMalformedRole(t *testing.T) {
	t.Parallel()
	var file map[string]map[string]string
	if err := json.Unmarshal([]byte(stubColors), &file); err != nil {
		t.Fatal(err)
	}
	file["dark"]["primary"] = "not-a-colour"
	path := writeColors(t, file)
	if _, err := parseColors(path, "dark"); err == nil ||
		!strings.Contains(err.Error(), "primary") {
		t.Fatalf("parseColors() = %v, want an error naming primary", err)
	}
}

func TestGenerateReportsFailureAlongsideTheFallback(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		hc   bool
		want Tokens
	}{
		{"normal", false, Fallback},
		{"high contrast", true, FallbackHighContrast},
	} {
		t.Run(tc.name, func(t *testing.T) {
			g := Generator{CacheDir: t.TempDir(), Matugen: "/nonexistent/matugen"}
			tok, err := g.Generate(
				Source{Kind: "wallpaper", Seed: "/tmp/wall.jpg"},
				Options{Mode: "dark", HighContrast: tc.hc})
			if err == nil {
				t.Fatal("a missing generator must report an error, not a silent fallback")
			}
			if tok != tc.want {
				t.Errorf("tokens = %+v, want the compiled fallback for this mode", tok)
			}
		})
	}
}

func TestGenerateRejectsAnUnknownSource(t *testing.T) {
	t.Parallel()
	g := Generator{CacheDir: t.TempDir()}
	if _, err := g.Generate(Source{Kind: "nonsense"}, Options{Mode: "dark"}); err == nil {
		t.Fatal("an unknown source must report an error")
	}
}

func TestHighContrastPassesContrastFlag(t *testing.T) {
	dir := t.TempDir()
	argsFile := fakeMatugenRecordingArgs(t, dir)
	g := Generator{CacheDir: dir, Matugen: filepath.Join(dir, "matugen")}
	if _, err := g.Generate(Source{Kind: "hex", Seed: "#3050a0"}, Options{Mode: "dark", HighContrast: true}); err != nil {
		t.Fatal(err)
	}
	args, _ := os.ReadFile(argsFile)
	if !strings.Contains(string(args), "--contrast") {
		t.Fatalf("expected --contrast in %q", args)
	}
}

func TestTemplateCoversEveryRoleInBothModes(t *testing.T) {
	t.Parallel()
	var tpl map[string]map[string]string
	if err := json.Unmarshal([]byte(matugenTemplate), &tpl); err != nil {
		t.Fatalf("embedded template is not JSON: %v", err)
	}
	for _, mode := range []string{"dark", "light"} {
		block, ok := tpl[mode]
		if !ok {
			t.Fatalf("template has no %q block", mode)
		}
		for _, name := range RoleNames() {
			want := "{{colors." + name + "." + mode + ".hex}}"
			got, ok := block[name]
			if !ok {
				t.Errorf("%s block omits role %s", mode, name)
				continue
			}
			// background has no matugen role of its own; it mirrors surface.
			if name == "background" || name == "on_background" {
				continue
			}
			if got != want {
				t.Errorf("%s.%s = %q, want %q", mode, name, got, want)
			}
		}
		if len(block) != len(RoleNames()) {
			t.Errorf("%s block has %d roles, want %d", mode, len(block), len(RoleNames()))
		}
	}
}

func TestFallbackSatisfiesItsOwnContract(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		hc   bool
	}{{"normal", false}, {"high contrast", true}} {
		t.Run(tc.name, func(t *testing.T) {
			tok := FallbackFor(tc.hc)
			if err := tok.Complete(); err != nil {
				t.Fatalf("incomplete: %v", err)
			}
			if err := tok.Valid(tc.hc); err != nil {
				t.Errorf("does not satisfy the floors it is validated against: %v", err)
			}
		})
	}
}

func TestCompleteRejectsAPartialPalette(t *testing.T) {
	t.Parallel()
	if err := Fallback.Complete(); err != nil {
		t.Fatalf("the compiled-in fallback is incomplete: %v", err)
	}

	for _, tc := range []struct {
		name    string
		mutate  func(*Tokens)
		wantHit string
	}{
		{"missing role", func(t *Tokens) { t.OutlineVariant = "" }, "outline_variant"},
		{"not a colour", func(t *Tokens) { t.Primary = "blue" }, "primary"},
		{"truncated hex", func(t *Tokens) { t.Surface = "#fff" }, "surface"},
		{"no hash", func(t *Tokens) { t.OnError = "ffffff" }, "on_error"},
		{"non-hex digit", func(t *Tokens) { t.Error = "#gg0000" }, "error"},
		{"missing fixed role", func(t *Tokens) { t.OnTertiaryFixed = "" }, "on_tertiary_fixed"},
	} {
		tok := Fallback
		tc.mutate(&tok)
		err := tok.Complete()
		if err == nil {
			t.Errorf("%s: Complete() = nil, want an error", tc.name)
			continue
		}
		if !strings.Contains(err.Error(), tc.wantHit) {
			t.Errorf("%s: error %q does not name the offending role %q", tc.name, err, tc.wantHit)
		}
	}
}

func TestValidNamesEveryFailingPair(t *testing.T) {
	t.Parallel()
	tok := Fallback
	// Drag two foregrounds onto their own backgrounds so both pairs fail.
	tok.OnSurface = tok.Surface
	tok.OnError = tok.Error
	err := tok.Valid(false)
	if err == nil {
		t.Fatal("Valid() = nil, want failures")
	}
	for _, want := range []string{"on_surface on surface", "on_error on error"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not report %q", err, want)
		}
	}
}

func TestValidAppliesTheHighContrastFloor(t *testing.T) {
	t.Parallel()
	// Fallback is built for the normal floors, so its accents sit below the
	// 7:1 one. That difference is the whole point of the flag.
	if err := Fallback.Valid(false); err != nil {
		t.Fatalf("Fallback must pass the normal floor: %v", err)
	}
	if err := Fallback.Valid(true); err == nil {
		t.Fatal("Valid(true) accepted a palette built for the normal floor")
	}
}

func TestRepairOnlyMovesForegrounds(t *testing.T) {
	t.Parallel()
	tok := Fallback
	tok.OnSurface = tok.Surface // unreadable
	got := tok.Repair(false)
	if got.Surface != tok.Surface {
		t.Errorf("Repair moved a background: surface %q -> %q", tok.Surface, got.Surface)
	}
	if got.OnSurface == tok.OnSurface {
		t.Error("Repair left the failing foreground alone")
	}
	if err := got.Valid(false); err != nil {
		t.Errorf("repaired palette still invalid: %v", err)
	}
}

func parseStub(t *testing.T, mode string) Tokens {
	t.Helper()
	path := filepath.Join(t.TempDir(), "colors.json")
	if err := os.WriteFile(path, []byte(stubColors), 0o644); err != nil {
		t.Fatal(err)
	}
	tok, err := parseColors(path, mode)
	if err != nil {
		t.Fatal(err)
	}
	return tok
}

func writeColors(t *testing.T, file map[string]map[string]string) string {
	t.Helper()
	b, err := json.Marshal(file)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "colors.json")
	if err := os.WriteFile(path, b, 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func fakeMatugen(t *testing.T, dir string) {
	t.Helper()
	writeMatugenStub(t, dir, false)
}

func fakeMatugenRecordingArgs(t *testing.T, dir string) string {
	t.Helper()
	writeMatugenStub(t, dir, true)
	return filepath.Join(dir, "matugen-args.txt")
}

func writeMatugenStub(t *testing.T, dir string, recordArgs bool) {
	t.Helper()
	script := "#!/bin/sh\n"
	if recordArgs {
		script += `printf '%s\n' "$@" > matugen-args.txt` + "\n"
	}
	script += "cat > colors.json <<'EOF'\n" + stubColors + "\nEOF\n"
	path := filepath.Join(dir, "matugen")
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
}
