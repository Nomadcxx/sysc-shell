package theme

import "testing"

// TestNamedPalettesAreCompleteAndValid is the gate that lets a scheme ship. A
// named palette goes to the same surfaces generated output does, so it has to
// clear the same bar: every role present, and every validated pair meeting its
// contrast floor in each mode.
func TestNamedPalettesAreCompleteAndValid(t *testing.T) {
	t.Parallel()
	for _, name := range PaletteNames() {
		for _, mode := range []string{"dark", "light"} {
			for _, hc := range []bool{false, true} {
				t.Run(name+"/"+mode+contrastLabel(hc), func(t *testing.T) {
					t.Parallel()
					tok, ok := NamedPalette(name, mode, hc)
					if !ok {
						t.Fatalf("%s is not a palette", name)
					}
					if err := tok.Complete(); err != nil {
						t.Fatalf("incomplete: %v", err)
					}
					if err := tok.Valid(hc); err != nil {
						t.Fatalf("invalid: %v", err)
					}
				})
			}
		}
	}
}

func contrastLabel(hc bool) string {
	if hc {
		return "/high-contrast"
	}
	return ""
}

// TestNamedPalettesKeepTheirOwnSurface checks the scheme's identity survives
// derivation. The whole reason to ask for Gruvbox by name rather than a brown
// seed is that its background is its background, not a tonal approximation.
func TestNamedPalettesKeepTheirOwnSurface(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, mode, surface string }{
		{"gruvbox", "dark", "#282828"},
		{"gruvbox", "light", "#fbf1c7"},
		{"catppuccin", "dark", "#1e1e2e"},
		{"catppuccin", "light", "#eff1f5"},
		{"nord", "dark", "#2e3440"},
		{"dracula", "dark", "#282a36"},
		{"tokyo-night", "dark", "#1a1b26"},
		{"rose-pine", "dark", "#191724"},
	} {
		tok, ok := NamedPalette(tc.name, tc.mode, false)
		if !ok {
			t.Fatalf("%s is not a palette", tc.name)
		}
		if tok.Surface != tc.surface {
			t.Errorf("%s %s surface = %s, want the scheme's own %s",
				tc.name, tc.mode, tok.Surface, tc.surface)
		}
	}
}

// TestNamedPaletteLadderSeparates keeps a capsule visible against its surface.
// A ladder that collapses is the failure the compiled fallback already had
// once: the steps were present but too near black to see.
func TestNamedPaletteLadderSeparates(t *testing.T) {
	t.Parallel()
	for _, name := range PaletteNames() {
		for _, mode := range []string{"dark", "light"} {
			tok, _ := NamedPalette(name, mode, false)
			surface := mustColor(tok.Surface)
			for _, step := range []struct {
				role string
				hex  string
			}{
				{"surface_container", tok.SurfaceContainer},
				{"surface_container_high", tok.SurfaceContainerHigh},
				{"surface_container_highest", tok.SurfaceContainerHighest},
			} {
				if got := ContrastRatio(mustColor(step.hex), surface); got < 1.06 {
					t.Errorf("%s %s: %s is %.3f:1 against the surface and will not be seen",
						name, mode, step.role, got)
				}
			}
			// The ladder has to climb, not wander.
			a := ContrastRatio(mustColor(tok.SurfaceContainer), surface)
			b := ContrastRatio(mustColor(tok.SurfaceContainerHigh), surface)
			c := ContrastRatio(mustColor(tok.SurfaceContainerHighest), surface)
			if !(a < b && b < c) {
				t.Errorf("%s %s ladder does not climb: %.3f %.3f %.3f", name, mode, a, b, c)
			}
		}
	}
}

// TestUnknownPaletteIsRejected keeps a typo from silently painting something.
func TestUnknownPaletteIsRejected(t *testing.T) {
	t.Parallel()
	if _, ok := NamedPalette("solarised", "dark", false); ok {
		t.Error("an unknown palette resolved")
	}
	if HasPalette("solarised") {
		t.Error("HasPalette accepted an unknown name")
	}
	if len(PaletteNames()) == 0 {
		t.Error("no palettes are registered")
	}
}

// TestPaletteSourceSkipsTheGenerator proves a named palette needs no matugen.
// The scheme carries its own colours, so spawning a generator to derive them
// would be both slower and wrong; pointing the generator at a binary that does
// not exist is the clearest way to assert it is never called.
func TestPaletteSourceSkipsTheGenerator(t *testing.T) {
	t.Parallel()
	g := Generator{CacheDir: t.TempDir(), Matugen: "/nonexistent/matugen"}
	tok, err := g.Generate(Source{Kind: "palette", Seed: "gruvbox"}, Options{Mode: "dark"})
	if err != nil {
		t.Fatalf("palette source failed: %v", err)
	}
	if tok.Surface != "#282828" {
		t.Errorf("surface = %s, want gruvbox's own #282828", tok.Surface)
	}
}

// TestUnknownPaletteSourceReportsAndFallsBack keeps the generator's contract:
// a nil error means the palette is usable, and anything else returns the
// compiled fallback with the reason.
func TestUnknownPaletteSourceReportsAndFallsBack(t *testing.T) {
	t.Parallel()
	g := Generator{CacheDir: t.TempDir(), Matugen: "/nonexistent/matugen"}
	tok, err := g.Generate(Source{Kind: "palette", Seed: "solarised"}, Options{Mode: "dark"})
	if err == nil {
		t.Fatal("an unknown palette was accepted")
	}
	if tok.Surface != Fallback.Surface {
		t.Errorf("surface = %s, want the compiled fallback", tok.Surface)
	}
}
