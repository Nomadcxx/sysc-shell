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
	if tok.Surface == "" || tok.OnSurface == "" || tok.Primary == "" {
		t.Fatalf("tokens not populated: %+v", tok)
	}
	if _, err := os.Stat(filepath.Join(dir, "colors.json")); err != nil {
		t.Fatalf("cache file missing: %v", err)
	}
}

func TestParseColorsPrefersSurfaceContainerHigh(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "colors.json")
	body := `{"dark":{"surface":"#1d2025","surface_container":"#282c33","surface_container_high":"#3a4149"}}`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	tok, err := parseColors(path, "dark")
	if err != nil {
		t.Fatal(err)
	}
	if tok.SurfaceContainer != "#3a4149" {
		t.Fatalf("SurfaceContainer = %q, want the high token so cards read as pills", tok.SurfaceContainer)
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

const stubColors = `{"dark":{"surface":"#1a1c1e","on_surface":"#e2e2e6","primary":"#a8c7fa","on_primary":"#062e6f","on_surface_variant":"#c3c6cf","error":"#ffb4ab","surface_container":"#1d1f21","outline":"#8d9199","primary_container":"#003a75","on_primary_container":"#d6e3ff","on_error":"#ffffff"},"light":{"surface":"#faf9fd","on_surface":"#1a1c1e","primary":"#3b5ba9","on_primary":"#ffffff","on_surface_variant":"#43474e","error":"#ba1a1a","surface_container":"#eeedf1","outline":"#73777f","primary_container":"#d6e3ff","on_primary_container":"#001b3d","on_error":"#ffffff"}}`

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
