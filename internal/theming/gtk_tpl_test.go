package theming

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Nomadcxx/sysc-shell/internal/theme"
)

func TestRenderContrastKeepsForegroundReadable(t *testing.T) {
	tok := theme.Fallback
	tok.Primary = tok.Surface
	got := Render("{{contrast .Primary .Surface}}", tok)
	fg, err := theme.ParseColor(got)
	if err != nil {
		t.Fatalf("contrast rendered %q: %v", got, err)
	}
	bg, _ := theme.ParseColor(tok.Surface)
	if ratio := theme.ContrastRatio(fg, bg); ratio < theme.TextContrast {
		t.Fatalf("contrast = %.2f, want at least %.1f", ratio, theme.TextContrast)
	}
}

func TestGtkTemplatesRenderReadableRoles(t *testing.T) {
	tok := theme.Fallback
	if err := tok.Valid(false); err != nil {
		t.Fatalf("fallback palette is invalid: %v", err)
	}

	for _, tc := range []struct {
		name  string
		roles map[string]string
		pairs [][2]string
	}{
		{
			name: "gtk3",
			roles: map[string]string{
				"theme_bg_color":          tok.Surface,
				"theme_fg_color":          tok.OnSurface,
				"theme_base_color":        tok.Surface,
				"theme_text_color":        tok.OnSurface,
				"theme_selected_bg_color": tok.Primary,
				"theme_selected_fg_color": tok.OnPrimary,
				"link_color":              gtkTestContrast(tok.Primary, tok.Surface),
				"warning_color":           gtkTestContrast(tok.Tertiary, tok.Surface),
				"error_color":             gtkTestContrast(tok.Error, tok.Surface),
				"success_color":           gtkTestContrast(tok.Secondary, tok.Surface),
			},
			pairs: [][2]string{
				{"theme_fg_color", "theme_bg_color"},
				{"theme_text_color", "theme_base_color"},
				{"theme_selected_fg_color", "theme_selected_bg_color"},
				{"link_color", "theme_bg_color"},
				{"warning_color", "theme_bg_color"},
				{"error_color", "theme_bg_color"},
				{"success_color", "theme_bg_color"},
			},
		},
		{
			name: "gtk4",
			roles: map[string]string{
				"window_bg_color":         tok.Surface,
				"window_fg_color":         tok.OnSurface,
				"view_bg_color":           tok.Surface,
				"view_fg_color":           tok.OnSurface,
				"headerbar_bg_color":      tok.SurfaceContainer,
				"headerbar_fg_color":      tok.OnSurface,
				"card_bg_color":           tok.SurfaceContainerLow,
				"card_fg_color":           tok.OnSurface,
				"dialog_bg_color":         tok.SurfaceContainerHigh,
				"dialog_fg_color":         tok.OnSurface,
				"popover_bg_color":        tok.SurfaceContainerHigh,
				"popover_fg_color":        tok.OnSurface,
				"sidebar_bg_color":        tok.SurfaceContainerLow,
				"sidebar_fg_color":        tok.OnSurface,
				"accent_bg_color":         tok.Primary,
				"accent_fg_color":         tok.OnPrimary,
				"destructive_bg_color":    tok.Error,
				"destructive_fg_color":    tok.OnError,
				"success_bg_color":        tok.Secondary,
				"success_fg_color":        tok.OnSecondary,
				"warning_bg_color":        tok.Tertiary,
				"warning_fg_color":        tok.OnTertiary,
				"error_bg_color":          tok.Error,
				"error_fg_color":          tok.OnError,
				"accent_color":            tok.Primary,
				"destructive_color":       gtkTestContrast(tok.Error, tok.Surface),
				"success_color":           gtkTestContrast(tok.Secondary, tok.Surface),
				"warning_color":           gtkTestContrast(tok.Tertiary, tok.Surface),
				"error_color":             gtkTestContrast(tok.Error, tok.Surface),
				"theme_bg_color":          tok.Surface,
				"theme_fg_color":          tok.OnSurface,
				"theme_base_color":        tok.Surface,
				"theme_text_color":        tok.OnSurface,
				"theme_selected_bg_color": tok.Primary,
				"theme_selected_fg_color": tok.OnPrimary,
			},
			pairs: [][2]string{
				{"window_fg_color", "window_bg_color"},
				{"view_fg_color", "view_bg_color"},
				{"headerbar_fg_color", "headerbar_bg_color"},
				{"card_fg_color", "card_bg_color"},
				{"dialog_fg_color", "dialog_bg_color"},
				{"popover_fg_color", "popover_bg_color"},
				{"sidebar_fg_color", "sidebar_bg_color"},
				{"accent_fg_color", "accent_bg_color"},
				{"destructive_fg_color", "destructive_bg_color"},
				{"success_fg_color", "success_bg_color"},
				{"warning_fg_color", "warning_bg_color"},
				{"error_fg_color", "error_bg_color"},
				{"theme_fg_color", "theme_bg_color"},
				{"theme_text_color", "theme_base_color"},
				{"theme_selected_fg_color", "theme_selected_bg_color"},
				{"destructive_color", "theme_bg_color"},
				{"success_color", "theme_bg_color"},
				{"warning_color", "theme_bg_color"},
				{"error_color", "theme_bg_color"},
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out := Render(Catalog().Template(tc.name), tok)
			if out == "" {
				t.Fatalf("%s template is missing or empty", tc.name)
			}
			for role, want := range tc.roles {
				declaration := "@define-color " + role + " " + want + ";"
				if !strings.Contains(out, declaration) {
					t.Errorf("missing %q", declaration)
				}
			}
			for _, pair := range tc.pairs {
				fg, err := theme.ParseColor(tc.roles[pair[0]])
				if err != nil {
					t.Fatal(err)
				}
				bg, err := theme.ParseColor(tc.roles[pair[1]])
				if err != nil {
					t.Fatal(err)
				}
				if ratio := theme.ContrastRatio(fg, bg); ratio < theme.TextContrast {
					t.Errorf("%s/%s contrast = %.2f, want at least %.1f", pair[0], pair[1], ratio, theme.TextContrast)
				}
			}
		})
	}
}

func TestGtkTemplateStatusColorsReadAcrossPalettes(t *testing.T) {
	roles := map[string]map[string]func(theme.Tokens) string{
		"gtk3": {
			"link_color":    func(tok theme.Tokens) string { return tok.Primary },
			"warning_color": func(tok theme.Tokens) string { return tok.Tertiary },
			"error_color":   func(tok theme.Tokens) string { return tok.Error },
			"success_color": func(tok theme.Tokens) string { return tok.Secondary },
		},
		"gtk4": {
			"destructive_color": func(tok theme.Tokens) string { return tok.Error },
			"success_color":     func(tok theme.Tokens) string { return tok.Secondary },
			"warning_color":     func(tok theme.Tokens) string { return tok.Tertiary },
			"error_color":       func(tok theme.Tokens) string { return tok.Error },
		},
	}
	for _, name := range theme.PaletteNames() {
		for _, mode := range []string{"dark", "light"} {
			tok, ok := theme.NamedPalette(name, mode, false)
			if !ok {
				t.Fatalf("named palette %q disappeared", name)
			}
			if err := tok.Valid(false); err != nil {
				t.Fatalf("%s/%s palette is invalid: %v", name, mode, err)
			}
			for template, mappings := range roles {
				out := Render(Catalog().Template(template), tok)
				for role, source := range mappings {
					want := gtkTestContrast(source(tok), tok.Surface)
					declaration := "@define-color " + role + " " + want + ";"
					if !strings.Contains(out, declaration) {
						t.Errorf("%s/%s %s: missing %q", name, mode, template, declaration)
					}
					fg, _ := theme.ParseColor(want)
					bg, _ := theme.ParseColor(tok.Surface)
					if ratio := theme.ContrastRatio(fg, bg); ratio < theme.TextContrast {
						t.Errorf("%s/%s %s: %s contrast = %.2f", name, mode, template, role, ratio)
					}
				}
			}
		}
	}
}

func gtkTestContrast(foreground, background string) string {
	fg, _ := theme.ParseColor(foreground)
	bg, _ := theme.ParseColor(background)
	return theme.EnsureContrast(fg, bg, theme.TextContrast).Hex()
}

func TestGtkTemplateManagedBlock(t *testing.T) {
	for _, tc := range []struct {
		name, version, accent string
	}{
		{name: "gtk3", version: "gtk-3.0", accent: "theme_selected_bg_color"},
		{name: "gtk4", version: "gtk-4.0", accent: "accent_bg_color"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("XDG_STATE_HOME", t.TempDir())
			home := t.TempDir()
			file := filepath.Join(home, ".config", tc.version, "gtk.css")
			userCSS := "/* user CSS */\n.user { color: #ff00ff; }\n"
			if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(file, []byte(userCSS), 0o644); err != nil {
				t.Fatal(err)
			}
			open := "/* >>> sysc-shell " + tc.name + " >>> */"
			close := "/* <<< sysc-shell " + tc.name + " <<< */"
			apply := func(enabled bool, tok theme.Tokens) {
				t.Helper()
				_, _, err := ApplyEnabled(home, func(name string) bool { return enabled && name == tc.name }, tok, 100)
				if err != nil {
					t.Fatal(err)
				}
			}
			read := func() string {
				t.Helper()
				b, err := os.ReadFile(file)
				if err != nil {
					t.Fatal(err)
				}
				return string(b)
			}
			assertOneBlock := func(content string) {
				t.Helper()
				if strings.Count(content, open) != 1 || strings.Count(content, close) != 1 {
					t.Fatalf("managed block markers are not unique: %s", content)
				}
				if !strings.Contains(content, userCSS) {
					t.Fatalf("user CSS was not preserved: %s", content)
				}
			}

			apply(true, theme.Fallback)
			first := read()
			assertOneBlock(first)

			updated := theme.Fallback
			updated.Primary = "#b0c4de"
			apply(true, updated)
			second := read()
			assertOneBlock(second)
			if !strings.Contains(second, "@define-color "+tc.accent+" #b0c4de;") || first == second {
				t.Fatalf("managed block did not update: %s", second)
			}

			apply(false, theme.Fallback)
			final := read()
			if !strings.Contains(final, userCSS) || strings.Contains(final, open) || strings.Contains(final, close) {
				t.Fatalf("disable did not remove only the managed block: %s", final)
			}
		})
	}
}
