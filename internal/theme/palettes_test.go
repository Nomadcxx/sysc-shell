package theme

import (
	"slices"
	"testing"
)

func TestNamedPaletteCatalogMatchesApprovedSources(t *testing.T) {
	dms := []string{
		"amber", "blue", "coral", "cyan", "green", "monochrome", "orange", "pink", "purple", "red",
	}
	noctalia := []string{
		"ayu", "catppuccin", "dracula", "eldritch", "gruvbox", "kanagawa", "noctalia", "nord", "rose-pine", "tokyo-night",
	}
	otherSources := []string{"eldritch-abyss", "rama", "void"}
	want := append(append(append([]string(nil), dms...), noctalia...), otherSources...)
	slices.Sort(want)
	if got := PaletteNames(); !slices.Equal(got, want) {
		t.Fatalf("PaletteNames() = %v, want %v", got, want)
	}
}

func TestNamedPaletteSourceColors(t *testing.T) {
	for _, tc := range []struct {
		name, mode, surface, primary, secondary string
	}{
		{"ayu", "dark", "#0b0e14", "#e6b450", "#aad94c"},
		{"ayu", "light", "#f8f9fa", "#ff8f40", "#86b300"},
		{"catppuccin", "dark", "#1e1e2e", "#cba6f7", "#fab387"},
		{"catppuccin", "light", "#eff1f5", "#8839ef", "#fe640b"},
		{"dracula", "dark", "#282a36", "#bd93f9", "#ff79c6"},
		{"dracula", "light", "#f8f8f2", "#8332f4", "#ff1399"},
		{"eldritch", "dark", "#212337", "#37f499", "#04d1f9"},
		{"eldritch", "light", "#f0f3f4", "#fb5bb6", "#0ad6ff"},
		{"eldritch-abyss", "dark", "#171928", "#2dcc82", "#0396b3"},
		{"eldritch-abyss", "light", "#171928", "#2dcc82", "#0396b3"},
		{"gruvbox", "dark", "#282828", "#b8bb26", "#fabd2f"},
		{"gruvbox", "light", "#fbf1c7", "#98971a", "#d79921"},
		{"kanagawa", "dark", "#1f1f28", "#76946a", "#c0a36e"},
		{"kanagawa", "light", "#f2ecbc", "#6f894e", "#77713f"},
		{"noctalia", "dark", "#070722", "#fff59b", "#a9aefe"},
		{"noctalia", "light", "#e6e8fa", "#5d65f5", "#8e93d8"},
		{"nord", "dark", "#2e3440", "#8fbcbb", "#88c0d0"},
		{"nord", "light", "#eceff4", "#5e81ac", "#64adc2"},
		{"tokyo-night", "dark", "#1a1b26", "#7aa2f7", "#bb9af7"},
		{"tokyo-night", "light", "#e1e2e7", "#2e7de9", "#9854f1"},
		{"rose-pine", "dark", "#191724", "#ebbcba", "#9ccfd8"},
		{"rose-pine", "light", "#fffaf3", "#d7827e", "#56949f"},
		{"blue", "dark", "#101418", "#42a5f5", "#8ab4f8"},
		{"blue", "light", "#f7f9ff", "#1976d2", "#42a5f5"},
		{"purple", "dark", "#141218", "#d0bcff", "#ccc2dc"},
		{"purple", "light", "#fef7ff", "#6750a4", "#625b71"},
		{"green", "dark", "#10140f", "#4caf50", "#81c995"},
		{"green", "light", "#f7fbf1", "#2e7d32", "#4caf50"},
		{"orange", "dark", "#1a120e", "#ff6d00", "#ffb74d"},
		{"orange", "light", "#fff8f6", "#e65100", "#ff9800"},
		{"red", "dark", "#1a1110", "#f44336", "#f28b82"},
		{"red", "light", "#fff8f7", "#d32f2f", "#f44336"},
		{"cyan", "dark", "#0e1416", "#00bcd4", "#4dd0e1"},
		{"cyan", "light", "#f5fafc", "#0097a7", "#00bcd4"},
		{"pink", "dark", "#191112", "#e91e63", "#f8bbd9"},
		{"pink", "light", "#fff8f7", "#c2185b", "#e91e63"},
		{"amber", "dark", "#17130b", "#ffc107", "#ffd54f"},
		{"amber", "light", "#fff8f2", "#ff8f00", "#ffc107"},
		{"coral", "dark", "#1a1110", "#ffb4ab", "#f9dedc"},
		{"coral", "light", "#fff8f7", "#8c1d18", "#ff5449"},
		{"monochrome", "dark", "#2a2a2a", "#ffffff", "#c4c6d0"},
		{"monochrome", "light", "#f5f5f6", "#2b303c", "#4a4d56"},
		{"rama", "dark", "#2b2d42", "#ef233c", "#d90429"},
		{"rama", "light", "#2b2d42", "#ef233c", "#d90429"},
		{"void", "dark", "#000000", "#ffffff", "#ffffff"},
		{"void", "light", "#000000", "#ffffff", "#ffffff"},
	} {
		tok, ok := NamedPalette(tc.name, tc.mode, false)
		if !ok {
			t.Fatalf("NamedPalette(%q, %q) not found", tc.name, tc.mode)
		}
		if tok.Surface != tc.surface || tok.Primary != tc.primary || tok.Secondary != tc.secondary {
			t.Errorf("%s/%s anchors = surface %s primary %s secondary %s, want %s %s %s",
				tc.name, tc.mode, tok.Surface, tok.Primary, tok.Secondary,
				tc.surface, tc.primary, tc.secondary)
		}
	}
}

func TestNoctaliaHighAnchorsMatchRecordedSources(t *testing.T) {
	for _, tc := range []struct{ name, mode, want, source string }{
		{"ayu", "dark", "#1e222a", "surfaceVariant"},
		{"ayu", "light", "#e4e6e9", "surfaceVariant"},
		{"catppuccin", "dark", "#313244", "surfaceVariant"},
		{"catppuccin", "light", "#ccd0da", "surfaceVariant"},
		{"dracula", "dark", "#44475a", "surfaceVariant"},
		{"dracula", "light", "#e6e6ea", "surfaceVariant"},
		{"eldritch", "dark", "#292e42", "surfaceVariant"},
		{"eldritch", "light", "#d5d9db", "surfaceVariant"},
		{"gruvbox", "dark", "#3c3836", "surfaceVariant"},
		{"gruvbox", "light", "#ebdbb2", "surfaceVariant"},
		{"kanagawa", "dark", "#2a2a37", "surfaceVariant"},
		{"kanagawa", "light", "#e5ddb0", "surfaceVariant"},
		{"noctalia", "dark", "#21215f", "outline"},
		{"noctalia", "light", "#c2c3d9", "local anchor"},
		{"nord", "dark", "#3b4252", "surfaceVariant"},
		{"nord", "light", "#c8d0dc", "retained anchor"},
		{"rose-pine", "dark", "#26233a", "surfaceVariant"},
		{"rose-pine", "light", "#f2e9e1", "surfaceVariant"},
		{"tokyo-night", "dark", "#24283b", "surfaceVariant"},
		{"tokyo-night", "light", "#a8aecb", "shadow"},
	} {
		t.Run(tc.name+"/"+tc.mode, func(t *testing.T) {
			a := palettes[tc.name].Dark
			if tc.mode == "light" {
				a = palettes[tc.name].Light
			}
			if a.High != tc.want {
				t.Errorf("%s/%s High = %s, want %s %s", tc.name, tc.mode, a.High, tc.source, tc.want)
			}
			usesSurfaceLow := tc.name == "ayu" || tc.name == "eldritch" || tc.name == "kanagawa" || tc.name == "noctalia"
			if usesSurfaceLow && a.Low != a.Surface {
				t.Errorf("%s/%s Low = %s, want Surface %s because Noctalia publishes no container ladder", tc.name, tc.mode, a.Low, a.Surface)
			}
		})
	}
}

func TestDMSHighAnchorsMatchPublishedContainerHighest(t *testing.T) {
	for _, tc := range []struct{ name, mode, want string }{
		{"amber", "dark", "#39342b"}, {"amber", "light", "#ebe1d4"},
		{"blue", "dark", "#32353a"}, {"blue", "light", "#e0e2e8"},
		{"coral", "dark", "#3d3231"}, {"coral", "light", "#f1dedc"},
		{"cyan", "dark", "#303637"}, {"cyan", "light", "#dee3e5"},
		{"green", "dark", "#323630"}, {"green", "light", "#e0e4db"},
		{"monochrome", "dark", "#505050"}, {"monochrome", "light", "#d0d0d2"},
		{"orange", "dark", "#3d332e"}, {"orange", "light", "#f0dfd8"},
		{"pink", "dark", "#3c3233"}, {"pink", "light", "#f0dee0"},
		{"purple", "dark", "#36343a"}, {"purple", "light", "#e6e0e9"},
		{"red", "dark", "#3d3231"}, {"red", "light", "#f1dedc"},
	} {
		a := palettes[tc.name].Dark
		if tc.mode == "light" {
			a = palettes[tc.name].Light
		}
		if a.High != tc.want {
			t.Errorf("%s/%s High = %s, want published surfaceContainerHighest %s", tc.name, tc.mode, a.High, tc.want)
		}
	}
}

func TestVoidUsesSyscGreetDarkColorsInBothModes(t *testing.T) {
	for _, mode := range []string{"dark", "light"} {
		tok, ok := NamedPalette("void", mode, false)
		if !ok {
			t.Fatalf("NamedPalette(%q, %q) not found", "void", mode)
		}
		if tok.Surface != "#000000" || tok.Primary != "#ffffff" || tok.Secondary != "#ffffff" ||
			tok.Tertiary != "#808080" || tok.Error != "#999999" {
			t.Errorf("void/%s source colors = surface %s, primary %s, secondary %s, tertiary %s, error %s",
				mode, tok.Surface, tok.Primary, tok.Secondary, tok.Tertiary, tok.Error)
		}
	}
}

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
// TestOutlineFloorsSplitByFunction records why the two outline roles carry
// different floors. WCAG 2.1 SC 1.4.11 covers user-interface components and
// focus indication, so Outline stays at 3:1 and derive() floors it there by
// resolving it through on(surface, ..., nonText). A decorative divider carries
// no state and loses no information by being quieter, so OutlineVariant is left
// a plain mix with no floor applied.
func TestOutlineFloorsSplitByFunction(t *testing.T) {
	t.Parallel()
	tk := FallbackFor(false)
	surface := mustColor(tk.Surface)
	if got := ContrastRatio(mustColor(tk.Outline), surface); got < 3.0 {
		t.Errorf("Outline = %.3f:1 against the surface, want at least 3.0: focus rings stay at 3:1", got)
	}
	// Deliberately not asserted in the other direction. The variant clearing
	// 3:1 is allowed, just not required, so this records the value rather than
	// fencing it in.
	t.Logf("OutlineVariant = %.3f:1 against the surface",
		ContrastRatio(mustColor(tk.OutlineVariant), surface))
}

// TestTextFloorsAreUnchanged separates the two questions the nested-surface
// relaxation could be read as conflating. Lowering that floor is about how far
// apart two backgrounds sit; it says nothing about how far text sits from the
// background it is painted on, and derive() still resolves body text against
// every surface level at 4.5 -- 7.0 under high contrast, which the relaxation
// is exempt from.
func TestTextFloorsAreUnchanged(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		hc   bool
		want float64
	}{
		{false, 4.5},
		{true, 7.0},
	} {
		tk := FallbackFor(tc.hc)
		got := ContrastRatio(mustColor(tk.OnSurface), mustColor(tk.Surface))
		if got < tc.want {
			t.Errorf("body text%s = %.3f:1, want at least %.1f",
				contrastLabel(tc.hc), got, tc.want)
		}
	}
}

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
