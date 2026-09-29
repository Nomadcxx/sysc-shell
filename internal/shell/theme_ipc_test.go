package shell

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Nomadcxx/sysc-shell/internal/config"
	"github.com/Nomadcxx/sysc-shell/internal/theme"
)

func themeCallRegistry(t *testing.T) (*Registry, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.toml")
	r := NewRegistry(config.Default())
	r.BindPersist(path, nil)
	return r, path
}

func callTheme(t *testing.T, r *Registry, method string, params any) (map[string]any, error) {
	t.Helper()
	raw, err := json.Marshal(params)
	if err != nil {
		t.Fatal(err)
	}
	return r.ThemeCall(method, raw)
}

func TestThemeCallVerbs(t *testing.T) {
	palette := theme.PaletteNames()[0]
	cases := []struct {
		name    string
		method  string
		params  any
		wantErr string
		check   func(t *testing.T, path string, body map[string]any)
	}{
		{
			name:   "mode get",
			method: "theme.mode.get",
			check: func(t *testing.T, _ string, body map[string]any) {
				if body["mode"] != "dark" {
					t.Fatalf("body = %v", body)
				}
			},
		},
		{
			name:   "mode set persists",
			method: "theme.mode.set",
			params: map[string]any{"mode": "light"},
			check: func(t *testing.T, path string, _ map[string]any) {
				cfg, err := config.Load(path)
				if err != nil {
					t.Fatal(err)
				}
				if cfg.ThemeGen.Mode != "light" {
					t.Fatalf("mode = %q", cfg.ThemeGen.Mode)
				}
			},
		},
		{
			name:    "mode set rejects outside the vocabulary",
			method:  "theme.mode.set",
			params:  map[string]any{"mode": "sideways"},
			wantErr: "appearance.mode",
		},
		{
			name:   "mode toggle flips",
			method: "theme.mode.toggle",
			check: func(t *testing.T, path string, body map[string]any) {
				if body["value"] != "light" {
					t.Fatalf("body = %v", body)
				}
				cfg, err := config.Load(path)
				if err != nil {
					t.Fatal(err)
				}
				if cfg.ThemeGen.Mode != "light" {
					t.Fatalf("mode = %q", cfg.ThemeGen.Mode)
				}
			},
		},
		{
			name:   "palette set names a bundled palette",
			method: "theme.palette.set",
			params: map[string]any{"source": "palette", "seed": palette},
			check: func(t *testing.T, path string, _ map[string]any) {
				cfg, err := config.Load(path)
				if err != nil {
					t.Fatal(err)
				}
				if cfg.ThemeGen.Source != "palette" || cfg.ThemeGen.Seed != palette {
					t.Fatalf("source %q seed %q", cfg.ThemeGen.Source, cfg.ThemeGen.Seed)
				}
			},
		},
		{
			name:    "palette set rejects an unknown palette",
			method:  "theme.palette.set",
			params:  map[string]any{"source": "palette", "seed": "no-such-palette"},
			wantErr: "appearance.palette",
		},
		{
			name:    "palette set rejects an unknown source",
			method:  "theme.palette.set",
			params:  map[string]any{"source": "spiritual"},
			wantErr: "appearance.source",
		},
		{
			name:    "palette set needs something to set",
			method:  "theme.palette.set",
			wantErr: "source or seed",
		},
		{
			name:   "templates apply enables one template",
			method: "theme.templates.apply",
			params: map[string]any{"name": "foot"},
			check: func(t *testing.T, path string, body map[string]any) {
				if body["applied"] != "foot" {
					t.Fatalf("body = %v", body)
				}
				cfg, err := config.Load(path)
				if err != nil {
					t.Fatal(err)
				}
				if !cfg.TemplateEnabled("foot") {
					t.Fatal("foot not enabled on disk")
				}
			},
		},
		{
			name:    "templates apply rejects an unknown template",
			method:  "theme.templates.apply",
			params:  map[string]any{"name": "emacs"},
			wantErr: "unknown template",
		},
		{
			name:   "templates apply without a name pokes a reload",
			method: "theme.templates.apply",
			check: func(t *testing.T, _ string, body map[string]any) {
				if body["applied"] != "all" {
					t.Fatalf("body = %v", body)
				}
			},
		},
		{
			name:    "unknown verb errors like an unknown method",
			method:  "theme.mode.flip",
			wantErr: "unknown method theme.mode.flip",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r, path := themeCallRegistry(t)
			body, err := callTheme(t, r, tc.method, tc.params)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("err = %v, want %q", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if tc.check != nil {
				tc.check(t, path, body)
			}
		})
	}
}

func TestThemeCallMalformedParams(t *testing.T) {
	r, _ := themeCallRegistry(t)
	if _, err := r.ThemeCall("theme.mode.set", json.RawMessage(`7`)); err == nil ||
		!strings.Contains(err.Error(), "malformed params") {
		t.Fatalf("err = %v", err)
	}
}

func TestThemePreviewShowHide(t *testing.T) {
	palette := theme.PaletteNames()[0]
	r, path := themeCallRegistry(t)

	body, err := callTheme(t, r, "theme.preview.show", map[string]any{
		"source": "palette", "seed": palette, "mode": "light",
	})
	if err != nil {
		t.Fatal(err)
	}
	if body["previewing"] != true || body["seed"] != palette || body["mode"] != "light" {
		t.Fatalf("body = %v", body)
	}
	r.mu.Lock()
	previewing := r.previewing
	r.mu.Unlock()
	if !previewing {
		t.Fatal("preview flag not set")
	}
	// A preview never writes the config file (D5): disk is still the default.
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ThemeGen.Source == "palette" && cfg.ThemeGen.Seed == palette {
		t.Fatal("preview persisted to disk")
	}

	if _, err := callTheme(t, r, "theme.preview.hide", nil); err != nil {
		t.Fatal(err)
	}
	r.mu.Lock()
	previewing = r.previewing
	r.mu.Unlock()
	if previewing {
		t.Fatal("hide left the preview flag set")
	}

	// A seed the entry rejects is refused before any candidate is painted.
	if _, err := callTheme(t, r, "theme.preview.show", map[string]any{
		"source": "palette", "seed": "not-a-palette",
	}); err == nil || !strings.Contains(err.Error(), "appearance.palette") {
		t.Fatalf("err = %v, want appearance.palette", err)
	}
}

// TestThemePreviewCommitKeepsReason: a config commit landing mid-preview with
// its own generation reason must survive the hide. The preview restores the
// committed reason of record, not the pre-preview one.
func TestThemePreviewCommitKeepsReason(t *testing.T) {
	r, _ := themeCallRegistry(t)
	palette := theme.PaletteNames()[0]
	if _, err := callTheme(t, r, "theme.preview.show", map[string]any{
		"source": "palette", "seed": palette,
	}); err != nil {
		t.Fatal(err)
	}
	r.paintTheme(config.Default(), theme.FallbackFor(false), "template foot refused", true)
	if _, err := callTheme(t, r, "theme.preview.hide", nil); err != nil {
		t.Fatal(err)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.previewing {
		t.Fatal("preview flag set after hide")
	}
	if r.themeErr != "template foot refused" {
		t.Fatalf("hide restored the pre-preview reason: %q", r.themeErr)
	}
}

// A theme write must route from what is on disk, not from a live view that has
// not absorbed the reload poke yet.
func TestThemeCallRoutesFromPersistedConfig(t *testing.T) {
	r, path := themeCallRegistry(t)
	persisted := config.Default()
	persisted.ThemeGen.Source = "palette"
	persisted.ThemeGen.Seed = "nord"
	if err := config.Write(path, persisted); err != nil {
		t.Fatal(err)
	}
	if _, err := callTheme(t, r, "theme.palette.set", map[string]any{
		"seed": "not-a-palette",
	}); err == nil || !strings.Contains(err.Error(), "appearance.palette") {
		t.Fatalf("err = %v", err)
	}
	if _, err := callTheme(t, r, "theme.mode.set", map[string]any{"mode": "light"}); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ThemeGen.Source != "palette" || cfg.ThemeGen.Seed != "nord" {
		t.Fatalf("theme = %+v", cfg.ThemeGen)
	}
}
