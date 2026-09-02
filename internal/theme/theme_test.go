package theme

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGenerateFromImageWritesCacheFile(t *testing.T) {
	dir := t.TempDir()
	fakeMatugen(t, dir)
	g := Generator{CacheDir: dir, Matugen: filepath.Join(dir, "matugen")}
	tok, err := g.Generate(Source{Kind: "wallpaper", Seed: "/tmp/wall.jpg"}, Options{Mode: "dark"})
	if err != nil {
		t.Fatal(err)
	}
	if tok.Surface == "" || tok.SurfaceContainerHigh == "" ||
		tok.SurfaceContainerHighest == "" || tok.OutlineVariant == "" ||
		tok.OnSurface == "" || tok.Primary == "" {
		t.Fatalf("tokens not populated: %+v", tok)
	}
	if _, err := os.Stat(filepath.Join(dir, "colors.json")); err != nil {
		t.Fatalf("cache file missing: %v", err)
	}
}

func TestParseColorsKeepsContainerLevelsDistinct(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "colors.json")
	body := `{"dark":{"surface":"#1d2025","surface_container":"#282c33",` +
		`"surface_container_high":"#3a4149","surface_container_highest":"#464e58"}}`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	tok, err := parseColors(path, "dark")
	if err != nil {
		t.Fatal(err)
	}
	// Bar capsules, panel cards, and idle controls each own a level. Folding
	// the high token onto SurfaceContainer is what made cards vanish into the
	// panel; the fix is a separate role per surface, not a brighter mid.
	for _, c := range []struct {
		name string
		got  string
		want string
	}{
		{"SurfaceContainer", tok.SurfaceContainer, "#282c33"},
		{"SurfaceContainerHigh", tok.SurfaceContainerHigh, "#3a4149"},
		{"SurfaceContainerHighest", tok.SurfaceContainerHighest, "#464e58"},
	} {
		if c.got != c.want {
			t.Errorf("%s = %q, want %q", c.name, c.got, c.want)
		}
	}
}

func TestGenerateFallbackWhenMatugenMissing(t *testing.T) {
	g := Generator{CacheDir: t.TempDir(), Matugen: "/nonexistent/matugen"}
	tok, err := g.Generate(Source{Kind: "wallpaper", Seed: "/tmp/wall.jpg"}, Options{Mode: "dark"})
	if err != nil {
		t.Fatalf("fallback must not error: %v", err)
	}
	if tok != Fallback {
		t.Fatalf("expected fallback palette, got %+v", tok)
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

const stubColors = `{"dark":{"surface":"#1a1c1e","surface_container":"#1d1f21","surface_container_high":"#292a2d","surface_container_highest":"#343538","on_surface":"#e2e2e6","on_surface_variant":"#c3c6cf","primary":"#a8c7fa","on_primary":"#062e6f","primary_container":"#003a75","on_primary_container":"#d6e3ff","outline":"#8d9199","outline_variant":"#44474e","error":"#ffb4ab","on_error":"#ffffff"},"light":{"surface":"#faf9fd","surface_container":"#eeedf1","surface_container_high":"#e8e7eb","surface_container_highest":"#e2e2e5","on_surface":"#1a1c1e","on_surface_variant":"#43474e","primary":"#3b5ba9","on_primary":"#ffffff","primary_container":"#d6e3ff","on_primary_container":"#001b3d","outline":"#73777f","outline_variant":"#c3c6cf","error":"#ba1a1a","on_error":"#ffffff"}}`

func TestFallbackDefinesEveryChromeRole(t *testing.T) {
	t.Parallel()
	roles := map[string]string{
		"surface":                   Fallback.Surface,
		"surface container":         Fallback.SurfaceContainer,
		"surface container high":    Fallback.SurfaceContainerHigh,
		"surface container highest": Fallback.SurfaceContainerHighest,
		"on surface":                Fallback.OnSurface,
		"on surface variant":        Fallback.OnSurfaceVariant,
		"primary":                   Fallback.Primary,
		"on primary":                Fallback.OnPrimary,
		"primary container":         Fallback.PrimaryContainer,
		"on primary container":      Fallback.OnPrimaryContainer,
		"outline":                   Fallback.Outline,
		"outline variant":           Fallback.OutlineVariant,
		"error":                     Fallback.Error,
		"on error":                  Fallback.OnError,
	}
	for name, value := range roles {
		if len(value) != 7 || value[0] != '#' {
			t.Errorf("%s = %q, want #RRGGBB", name, value)
		}
	}
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
