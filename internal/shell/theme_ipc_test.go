package shell

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Nomadcxx/sysc-shell/internal/config"
	"github.com/Nomadcxx/sysc-shell/internal/platform/niri"
	"github.com/Nomadcxx/sysc-shell/internal/platform/wayland"
	"github.com/Nomadcxx/sysc-shell/internal/render"
	"github.com/Nomadcxx/sysc-shell/internal/theme"
	"github.com/Nomadcxx/sysc-shell/internal/theming"
)

func themeCallRegistry(t *testing.T) (*Registry, string) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_STATE_HOME", t.TempDir())
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
				results, ok := body["results"].(map[string]any)
				if !ok || len(results) == 0 {
					t.Fatalf("body has no per-template results: %v", body)
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

func TestThemePreviewRepaintsOpenPanelsWithCandidatePalette(t *testing.T) {
	r, h := rethemeHost(t, true)
	r.themeGen = theme.Generator{CacheDir: t.TempDir()}

	cfg := config.Default()
	cfg.ThemeGen.Source = "palette"
	cfg.ThemeGen.Seed = "gruvbox"
	tokens, ok := theme.NamedPalette(cfg.ThemeGen.Seed, cfg.ThemeGen.Mode, false)
	if !ok {
		t.Fatal("gruvbox palette is missing")
	}
	want, err := resolveOutputTheme(cfg, "", tokens, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.themePreviewShow(cfg); err != nil {
		t.Fatal(err)
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if got := h.paintTheme().Surface; got != want.Surface {
		t.Fatalf("open panel surface = %+v, want preview surface %+v", got, want.Surface)
	}
}

func TestThemePreviewRepaintsOpenOSDWithCandidatePalette(t *testing.T) {
	cfg := config.Default()
	cfg.Accessibility.ReducedMotion = true
	r := NewRegistry(cfg)
	t.Cleanup(r.Close)
	r.themeGen = theme.Generator{CacheDir: t.TempDir()}
	r.setTestBar(7, &Bar{conn: "DP-1"})
	r.OSD().Show(OSDView{Kind: osdAudio, Level: 40})
	_ = drainAux(t, r, 1)
	r.mu.Lock()
	committedSurface := r.osd.theme.Surface
	r.mu.Unlock()
	for len(r.invalidations) != 0 {
		<-r.invalidations
	}

	candidate := cfg
	candidate.ThemeGen.Source = "palette"
	candidate.ThemeGen.Seed = "gruvbox"
	tokens, ok := theme.NamedPalette(candidate.ThemeGen.Seed, candidate.ThemeGen.Mode, false)
	if !ok {
		t.Fatal("gruvbox palette is missing")
	}
	want, err := resolveOutputTheme(candidate, "", tokens, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.themePreviewShow(candidate); err != nil {
		t.Fatal(err)
	}

	r.mu.Lock()
	got := r.osd.theme.Surface
	r.mu.Unlock()
	if got != want.Surface {
		t.Fatalf("open OSD surface = %+v, want preview surface %+v", got, want.Surface)
	}
	invalidated := false
	for len(r.invalidations) != 0 {
		if inv := <-r.invalidations; inv.Global == 7 && inv.SurfaceID == osdSurfaceID(7) {
			invalidated = true
		}
	}
	if !invalidated {
		t.Fatal("theme preview did not invalidate the open OSD surface")
	}

	r.themePreviewHide()
	r.mu.Lock()
	got = r.osd.theme.Surface
	r.mu.Unlock()
	if got != committedSurface {
		t.Fatalf("hidden preview left OSD surface = %+v, want committed surface %+v", got, committedSurface)
	}
	invalidated = false
	for len(r.invalidations) != 0 {
		if inv := <-r.invalidations; inv.Global == 7 && inv.SurfaceID == osdSurfaceID(7) {
			invalidated = true
		}
	}
	if !invalidated {
		t.Fatal("preview hide did not invalidate the open OSD surface")
	}
}

func TestThemePreviewRepaintsOpenWindowSwitcher(t *testing.T) {
	cfg := config.Default()
	cfg.Accessibility.ReducedMotion = true
	r := NewRegistry(cfg)
	t.Cleanup(r.Close)
	r.themeGen = theme.Generator{CacheDir: t.TempDir()}
	newHosts(t, r, map[uint32]string{7: "DP-1"})
	r.UpdateNiri(switcherSnapshot([]niri.Window{
		{ID: 1, WorkspaceID: 10, HasWorkspace: true, FocusTimestamp: 30},
	}, false))
	if err := r.ShowWindowSwitcher(); err != nil {
		t.Fatal(err)
	}
	h := r.windowSwitcher
	if err := h.spec().Callbacks.Configure(800, 600, 120); err != nil {
		t.Fatal(err)
	}

	// Model the style retained by an already-rendered switcher surface.
	r.mu.Lock()
	committed := r.surfaceTheme().OverlayStyle()
	committed.NoGround = true
	committed.Scale120, committed.Body = h.style.Scale120, h.style.Body
	h.style = committed
	h.text = render.NewTextRenderer(nil)
	r.mu.Unlock()

	candidate := cfg
	candidate.ThemeGen.Source = "palette"
	candidate.ThemeGen.Seed = "gruvbox"
	tokens, ok := theme.NamedPalette(candidate.ThemeGen.Seed, candidate.ThemeGen.Mode, false)
	if !ok {
		t.Fatal("gruvbox palette is missing")
	}
	wantTheme, err := resolveOutputTheme(candidate, "DP-1", tokens, false)
	if err != nil {
		t.Fatal(err)
	}
	want := withBarGeometry(wantTheme, candidate.Bar).OverlayStyle()
	want.NoGround = true
	want.Scale120, want.Body = committed.Scale120, committed.Body

	drainInvalidations(r)
	if _, err := r.themePreviewShow(candidate); err != nil {
		t.Fatal(err)
	}
	r.mu.Lock()
	gotPreview := h.style.Accent
	geometryPreserved := h.style.Scale120 == committed.Scale120 && h.style.Body == committed.Body && h.style.NoGround
	r.mu.Unlock()
	if gotPreview != want.Accent {
		t.Fatalf("open switcher accent = %+v, want preview accent %+v", gotPreview, want.Accent)
	}
	if !geometryPreserved {
		t.Fatal("preview retheme changed the switcher's scale, body, or transparent ground")
	}
	previewInvalidated := false
	for len(r.invalidations) > 0 {
		inv := <-r.invalidations
		if inv.Global == 7 && inv.SurfaceID == windowSwitcherSurfaceID {
			previewInvalidated = true
		}
	}
	if !previewInvalidated {
		t.Fatal("theme preview did not invalidate the open window switcher")
	}

	r.themePreviewHide()
	r.mu.Lock()
	gotRestored := h.style.Accent
	r.mu.Unlock()
	if gotRestored != committed.Accent {
		t.Fatalf("hidden preview left switcher accent = %+v, want committed accent %+v", gotRestored, committed.Accent)
	}
	restoredInvalidated := false
	for len(r.invalidations) > 0 {
		inv := <-r.invalidations
		if inv.Global == 7 && inv.SurfaceID == windowSwitcherSurfaceID {
			restoredInvalidated = true
		}
	}
	if !restoredInvalidated {
		t.Fatal("preview hide did not invalidate the open window switcher")
	}

	if !h.handleLocking(wayland.Event{Kind: wayland.EventKeyPress, Key: keyEsc}) {
		t.Fatal("Escape did not close the switcher")
	}
	drainInvalidations(r)
	if _, err := r.themePreviewShow(candidate); err != nil {
		t.Fatal(err)
	}
	r.mu.Lock()
	closedPreview := h.style.Accent
	stillClosed := !h.open_
	r.mu.Unlock()
	if !stillClosed || closedPreview != want.Accent {
		t.Fatalf("closed switcher cache = %+v, open = %v; want preview accent %+v while closed", closedPreview, !stillClosed, want.Accent)
	}
	for len(r.invalidations) > 0 {
		if inv := <-r.invalidations; inv.Global == 7 && inv.SurfaceID == windowSwitcherSurfaceID {
			t.Fatal("closed switcher received an invalidation")
		}
	}
	r.themePreviewHide()
	if err := r.ShowWindowSwitcher(); err != nil {
		t.Fatal(err)
	}
	r.mu.Lock()
	gotReopened := h.style.Accent
	r.mu.Unlock()
	if gotReopened != committed.Accent {
		t.Fatalf("reopened switcher accent = %+v, want committed accent %+v", gotReopened, committed.Accent)
	}
	if !h.handleLocking(wayland.Event{Kind: wayland.EventKeyPress, Key: keyEsc}) {
		t.Fatal("Escape did not close the reopened switcher")
	}
}

func TestThemePreviewOpensPanelsWithCandidatePalette(t *testing.T) {
	cfg := config.Default()
	cfg.Accessibility.ReducedMotion = true
	r := NewRegistry(cfg)
	t.Cleanup(r.Close)
	r.themeGen = theme.Generator{CacheDir: t.TempDir()}

	candidate := cfg
	candidate.ThemeGen.Source = "palette"
	candidate.ThemeGen.Seed = "gruvbox"
	tokens, ok := theme.NamedPalette(candidate.ThemeGen.Seed, candidate.ThemeGen.Mode, false)
	if !ok {
		t.Fatal("gruvbox palette is missing")
	}
	want, err := resolveOutputTheme(candidate, "", tokens, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.themePreviewShow(candidate); err != nil {
		t.Fatal(err)
	}
	if err := r.OpenPanel(PanelSession, 7, Trigger{}); err != nil {
		t.Fatal(err)
	}
	_ = drainAux(t, r, 2)

	r.mu.Lock()
	defer r.mu.Unlock()
	h := r.panelHosts[PanelSession]
	if h == nil {
		t.Fatal("session panel did not open")
	}
	if got := h.paintTheme().Surface; got != want.Surface {
		t.Fatalf("new panel surface = %+v, want preview surface %+v", got, want.Surface)
	}
}

func TestThemePreviewHotpluggedBarUsesCandidatePalette(t *testing.T) {
	cfg := config.Default()
	r := NewRegistry(cfg)
	t.Cleanup(r.Close)
	r.themeGen = theme.Generator{CacheDir: t.TempDir()}

	candidate := cfg
	candidate.ThemeGen.Source = "palette"
	candidate.ThemeGen.Seed = "gruvbox"
	tokens, ok := theme.NamedPalette(candidate.ThemeGen.Seed, candidate.ThemeGen.Mode, false)
	if !ok {
		t.Fatal("gruvbox palette is missing")
	}
	want, err := resolveOutputTheme(candidate, "DP-1", tokens, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.themePreviewShow(candidate); err != nil {
		t.Fatal(err)
	}
	if _, err := r.NewHost(7, "DP-1"); err != nil {
		t.Fatal(err)
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if got := r.bars[7].themeSnapshot().Surface; got != want.Surface {
		t.Fatalf("hotplugged bar surface = %+v, want preview surface %+v", got, want.Surface)
	}
}

func TestThemePreviewHideInvalidatesPendingGeneration(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("TMPDIR", tmp)
	r := NewRegistry(config.Default())
	t.Cleanup(r.Close)
	r.themeGen = theme.Generator{CacheDir: t.TempDir()}
	candidate := config.Default()
	candidate.ThemeGen.Source = "palette"
	candidate.ThemeGen.Seed = "gruvbox"

	r.themeGenMu.Lock()
	locked := true
	defer func() {
		if locked {
			r.themeGenMu.Unlock()
		}
	}()
	done := make(chan error, 1)
	go func() {
		_, err := r.themePreviewShow(candidate)
		done <- err
	}()
	waitForPreviewCache(t, tmp)
	r.themePreviewHide()
	r.themeGenMu.Unlock()
	locked = false

	if err := waitForPreviewResult(t, done); err == nil {
		t.Fatal("a preview completed after hide")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.previewing || r.previewTheme != nil {
		t.Fatal("a hidden pending preview became active")
	}
}

func TestThemeCommitInvalidatesPendingPreviewGeneration(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("TMPDIR", tmp)
	r := NewRegistry(config.Default())
	t.Cleanup(r.Close)
	r.themeGen = theme.Generator{CacheDir: t.TempDir()}
	candidate := config.Default()
	candidate.ThemeGen.Source = "palette"
	candidate.ThemeGen.Seed = "gruvbox"

	r.themeGenMu.Lock()
	locked := true
	defer func() {
		if locked {
			r.themeGenMu.Unlock()
		}
	}()
	done := make(chan error, 1)
	go func() {
		_, err := r.themePreviewShow(candidate)
		done <- err
	}()
	waitForPreviewCache(t, tmp)

	committed := config.Default()
	committed.ThemeGen.Source = "palette"
	committed.ThemeGen.Seed = "nord"
	tokens, ok := theme.NamedPalette(committed.ThemeGen.Seed, committed.ThemeGen.Mode, false)
	if !ok {
		t.Fatal("nord palette is missing")
	}
	r.mu.Lock()
	r.cfg = committed
	r.mu.Unlock()
	r.paintTheme(committed, tokens, "", true)
	r.themeGenMu.Unlock()
	locked = false

	if err := waitForPreviewResult(t, done); err == nil {
		t.Fatal("a pending preview replaced a newer committed palette")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.previewing || r.previewTheme != nil {
		t.Fatal("a committed palette left a pending preview active")
	}
	if r.tokens != tokens {
		t.Fatal("the pending preview replaced committed palette tokens")
	}
}

func waitForPreviewCache(t *testing.T, dir string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatal(err)
		}
		if len(entries) > 0 {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("preview generation did not reach its cache")
}

func waitForPreviewResult(t *testing.T, done <-chan error) error {
	t.Helper()
	select {
	case err := <-done:
		return err
	case <-time.After(3 * time.Second):
		t.Fatal("preview generation did not finish")
		return nil
	}
}

func TestThemePreviewRepaintsOpenToastsWithCandidatePalette(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	cfg := config.Default()
	cfg.Accessibility.ReducedMotion = true
	r := NewRegistry(cfg)
	t.Cleanup(r.Close)
	r.themeGen = theme.Generator{CacheDir: t.TempDir()}
	hh := &hostHarness{}
	r.toasts = newToastHost(r, hh)
	r.outputsForTest([]string{"eDP-1"})
	r.toasts.syncOutputs(map[string]uint32{"eDP-1": 5})
	if err := r.toasts.configure("eDP-1", 1920, 1080, 120); err != nil {
		t.Fatal(err)
	}
	r.applyNotify(snap(1, note(1, "build done")))
	committed := r.toasts.style.RootFill()

	candidate := cfg
	candidate.ThemeGen.Source = "palette"
	candidate.ThemeGen.Seed = "gruvbox"
	tokens, ok := theme.NamedPalette(candidate.ThemeGen.Seed, candidate.ThemeGen.Mode, false)
	if !ok {
		t.Fatal("gruvbox palette is missing")
	}
	wantTheme, err := resolveOutputTheme(candidate, "", tokens, false)
	if err != nil {
		t.Fatal(err)
	}
	want := withBarGeometry(wantTheme, candidate.Bar).OverlayStyle().RootFill()
	if _, err := r.themePreviewShow(candidate); err != nil {
		t.Fatal(err)
	}
	if got := r.toasts.style.RootFill(); got != want {
		t.Fatalf("open toast root = %+v, want preview root %+v (committed %+v)", got, want, committed)
	}

	// A later toast rebuild must keep using the preview presentation state.
	r.applyNotify(snap(2, note(2, "second toast")))
	if got := r.toasts.style.RootFill(); got != want {
		t.Fatalf("rebuilt toast root = %+v, want preview root %+v", got, want)
	}
}

func TestThemePreviewDoesNotWritePersistentCache(t *testing.T) {
	xdgCache := t.TempDir()
	cache := filepath.Join(xdgCache, "sysc-shell")
	if err := os.MkdirAll(cache, 0o755); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(cache, "keep")
	if err := os.WriteFile(marker, []byte("existing"), 0o600); err != nil {
		t.Fatal(err)
	}
	tmp := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", xdgCache)
	t.Setenv("TMPDIR", tmp)

	r := NewRegistry(config.Default())
	t.Cleanup(r.Close)
	for name, contents := range map[string]string{
		"matugen.toml":          "existing config",
		"matugen-template.json": "existing template",
	} {
		if err := os.WriteFile(filepath.Join(cache, name), []byte(contents), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	cfg := config.Default()
	cfg.ThemeGen.Source = "palette"
	cfg.ThemeGen.Seed = "gruvbox"
	if _, err := r.themePreviewShow(cfg); err != nil {
		t.Fatal(err)
	}

	entries, err := os.ReadDir(cache)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 3 {
		t.Fatalf("persistent cache entries = %v, want the three pre-existing files", entries)
	}
	if got, err := os.ReadFile(marker); err != nil || string(got) != "existing" {
		t.Fatalf("persistent marker = %q, err = %v", got, err)
	}
	for name, want := range map[string]string{
		"matugen.toml":          "existing config",
		"matugen-template.json": "existing template",
	} {
		if got, err := os.ReadFile(filepath.Join(cache, name)); err != nil || string(got) != want {
			t.Errorf("persistent %s = %q, err = %v", name, got, err)
		}
	}
	if entries, err := os.ReadDir(tmp); err != nil || len(entries) != 0 {
		t.Fatalf("temporary preview files = %v, err = %v; want cleanup", entries, err)
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

func TestThemeTemplatesApplyReportsUserModifiedRefusal(t *testing.T) {
	r, path := themeCallRegistry(t)
	t.Cleanup(r.Close)
	home := os.Getenv("HOME")

	cfg := config.Default()
	cfg.ThemeGen.Source = "palette"
	cfg.ThemeGen.Seed = theme.PaletteNames()[0]
	if err := config.Write(path, cfg); err != nil {
		t.Fatal(err)
	}
	on := true
	apply := func() map[string]any {
		body, err := callTheme(t, r, "theme.templates.apply", map[string]any{
			"name": "foot",
			"on":   on,
		})
		if err != nil {
			t.Fatal(err)
		}
		results, ok := body["results"].(map[string]any)
		if !ok {
			t.Fatalf("reply has no per-template results: %v", body)
		}
		foot, ok := results["foot"].(map[string]any)
		if !ok {
			t.Fatalf("foot result = %v", results["foot"])
		}
		return foot
	}
	if got := apply()["status"]; got != "applied" {
		t.Fatalf("first foot result status = %v, want applied", got)
	}

	sidecar := filepath.Join(home, ".config", "foot", "themes", "sysc-shell")
	if err := os.WriteFile(sidecar, []byte("user edit\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := apply(); got["status"] != "refused" || got["error"] == "" {
		t.Fatalf("edited foot result = %v, want refused with a reason", got)
	}
	if got, err := os.ReadFile(sidecar); err != nil || string(got) != "user edit\n" {
		t.Fatalf("sidecar = %q, err = %v; user edit must be preserved", got, err)
	}
}

// A template apply must render from the palette the persisted config names,
// not from the live palette a reload has not published yet.
func TestThemeTemplatesApplyRendersFromPersistedPalette(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	path := filepath.Join(t.TempDir(), "config.toml")

	names := theme.PaletteNames()
	stale, fresh := names[0], names[1]
	cfg := config.Default()
	cfg.ThemeGen.Source = "palette"
	cfg.ThemeGen.Seed = stale
	r := NewRegistry(cfg)
	t.Cleanup(r.Close)
	r.BindPersist(path, nil)

	persisted := cfg
	persisted.ThemeGen.Seed = fresh
	if err := config.Write(path, persisted); err != nil {
		t.Fatal(err)
	}

	on := true
	if _, err := callTheme(t, r, "theme.templates.apply", map[string]any{
		"name": "foot",
		"on":   on,
	}); err != nil {
		t.Fatal(err)
	}

	want, ok := theme.NamedPalette(fresh, cfg.ThemeGen.Mode, cfg.Accessibility.HighContrast)
	if !ok {
		t.Fatalf("palette %q missing", fresh)
	}
	sidecar := filepath.Join(os.Getenv("HOME"), ".config", "foot", "themes", "sysc-shell")
	got, err := os.ReadFile(sidecar)
	if err != nil {
		t.Fatal(err)
	}
	wantBody := theming.Render(theming.Catalog().WithOverlay().Template("foot"), want)
	if string(got) != wantBody {
		t.Fatalf("sidecar rendered from the stale palette:\n got %q\nwant %q", got, wantBody)
	}
}

// A palette the persisted config names but the generator cannot produce leaves
// the apply running on the last complete palette, so the templates are still
// written and this is not an apply failure. Reporting a plain success would
// still be the shape of failure #101 describes, so the reply carries the
// generator's reason the way republishTheme records one in r.themeErr.
func TestThemeTemplatesApplyReportsUngeneratablePalette(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	path := filepath.Join(t.TempDir(), "config.toml")

	cfg := config.Default()
	cfg.ThemeGen.Source = "palette"
	cfg.ThemeGen.Seed = theme.PaletteNames()[0]
	r := NewRegistry(cfg)
	t.Cleanup(r.Close)
	r.BindPersist(path, nil)

	persisted := cfg
	persisted.ThemeGen.Seed = "not-a-real-palette"
	if err := config.Write(path, persisted); err != nil {
		t.Fatal(err)
	}

	body, err := callTheme(t, r, "theme.templates.apply", map[string]any{
		"name": "foot",
		"on":   true,
	})
	if err != nil {
		t.Fatal(err)
	}
	reason, ok := body["theme_error"].(string)
	if !ok || reason == "" {
		t.Fatalf("reply carries no generation reason, so a fallback palette reads as success: %v", body)
	}
	if !strings.Contains(reason, "not a named palette") {
		t.Fatalf("theme_error = %q, want the generator's own reason", reason)
	}
}

func TestThemeTemplatesApplyReportsWriteError(t *testing.T) {
	r, _ := themeCallRegistry(t)
	t.Cleanup(r.Close)
	home := os.Getenv("HOME")
	footDir := filepath.Join(home, ".config", "foot")
	if err := os.MkdirAll(filepath.Dir(footDir), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(footDir, []byte("not a directory\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	on := true
	body, err := callTheme(t, r, "theme.templates.apply", map[string]any{
		"name": "foot",
		"on":   on,
	})
	if err != nil {
		t.Fatal(err)
	}
	results, ok := body["results"].(map[string]any)
	if !ok {
		t.Fatalf("reply has no per-template results: %v", body)
	}
	foot, ok := results["foot"].(map[string]any)
	if !ok || foot["status"] != "error" || foot["error"] == "" {
		t.Fatalf("foot result = %v, want error with a reason", results["foot"])
	}
}

func TestThemeTemplatesApplyOmitsUnattemptedNiri(t *testing.T) {
	r, path := themeCallRegistry(t)
	t.Cleanup(r.Close)
	if err := config.Write(path, config.Default()); err != nil {
		t.Fatal(err)
	}

	body, err := callTheme(t, r, "theme.templates.apply", map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	results, ok := body["results"].(map[string]any)
	if !ok {
		t.Fatalf("reply has no per-template results: %v", body)
	}
	if _, ok := results["niri"]; ok {
		t.Fatalf("reply reports Niri as applied without a Niri config: %v", results["niri"])
	}
}
