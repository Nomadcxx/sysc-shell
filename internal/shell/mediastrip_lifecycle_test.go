package shell

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Nomadcxx/sysc-shell/internal/config"
	"github.com/Nomadcxx/sysc-shell/internal/platform/wayland"
	"github.com/Nomadcxx/sysc-shell/internal/services"
	"github.com/Nomadcxx/sysc-shell/internal/theme"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

func TestAuditMediaStripFitsEveryDensity(t *testing.T) {
	state := services.MediaState{Available: true, Identity: "mpv", Title: "Track", CanSeek: true, CanPause: true, CanNext: true, CanPrev: true, LengthUS: 300000000}

	for _, density := range theme.Densities() {
		t.Run(string(density), func(t *testing.T) {
			r, h, _ := stripTestRegistry(t, state)
			r.mu.Lock()
			defer r.mu.Unlock()
			r.cfg.Theme.Density = density
			r.bars[7].setOutputSize(1536, 864)
			if err := r.bars[7].Configure(1536, r.cfg.Bar.SurfaceExtent(), 120); err != nil {
				t.Fatal(err)
			}
			openStripForTest(t, h, ui.Rect{X: 1500, W: 36, H: 32}, true)
			spec := h.spec()
			if err := h.configure(int(spec.Width), int(spec.Height), 120); err != nil {
				t.Fatal(err)
			}
			for _, n := range ui.Focusables(h.root) {
				if n.Bounds.Y+n.Bounds.H > int(spec.Height)-mediaStripPad {
					t.Errorf("%s at %+v exceeds configured strip height %d with bottom padding", n.Action, n.Bounds, spec.Height)
				}
			}
			for _, edge := range []string{"top", "bottom", "left", "right"} {
				work := ui.Rect{W: 1536, H: 824}
				place := mediaStripPlacement(edge, ui.Rect{X: 1534, Y: 822}, ui.Rect{W: 40, H: 40}, work, int(spec.Height))
				if place.X < 0 || place.Y < 0 || place.X+place.W > work.W || place.Y+place.H > work.H {
					t.Errorf("%s: %+v leaves %+v", edge, place, work)
				}
			}
		})
	}
}

func TestAuditPinnedMediaStripReloadsDensity(t *testing.T) {
	r, strip, harness := stripTestRegistry(t, services.MediaState{Available: true, Identity: "mpv", Title: "Track", CanSeek: true, CanPause: true, LengthUS: 300000000})
	r.mu.Lock()
	defer r.mu.Unlock()
	r.cfg.Theme.Density = theme.DensityDefault
	openStripForTest(t, strip, ui.Rect{X: 800, W: 160, H: 32}, true)
	if err := strip.configure(int(strip.spec().Width), int(strip.spec().Height), 120); err != nil {
		t.Fatal(err)
	}
	slider := findKind(strip.root, ui.KindSlider)
	if slider == nil || slider.Bounds.H != 44 {
		t.Fatalf("default slider: %+v", slider)
	}

	for _, tc := range []struct {
		density theme.Density
		height  int
	}{{theme.DensityMini, 28}, {theme.DensitySpacious, 68}} {
		strip.pressed = slider
		strip.pointer.setPress(slider.StableKey())
		r.cfg.Theme.Density = tc.density
		pubs := r.retheThemeOpenSurfacesLocked(r.cfg, r.tokens)
		slider = findKind(strip.root, ui.KindSlider)
		if slider == nil || slider.Bounds.H != tc.height {
			t.Errorf("%s reload slider retained old layout: %+v", tc.density, slider)
		}
		found := false
		for _, pub := range pubs {
			if pub.SurfaceID == mediaStripSurfaceID && pub.Global == 7 {
				found = true
			}
		}
		if strip.pressed != nil || strip.pointer.press != "" {
			t.Error("strip retained detached seek press")
		}
		if !found || !strip.pinned {
			t.Error("pinned strip was not retained and invalidated")
		}
		if len(harness.updates) == 0 {
			t.Error("density reload did not update surface geometry")
		}
		if strip.style.Metrics != r.panelThemeForState(7, r.cfg, r.tokens).Metrics {
			t.Error("strip retained old style")
		}
	}
}

func TestAuditDensityReloadDoesNotKeepDetachedSliderDrag(t *testing.T) {
	r := newPanelRegistry(t)
	dir := t.TempDir()
	logPath := filepath.Join(dir, "calls")
	bin := filepath.Join(dir, "wpctl")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\nprintf '%s\\n' \"$*\" >> "+logPath+"\n"), 0755); err != nil {
		t.Fatal(err)
	}
	r.setAudio(services.NewAudio(time.Hour, bin))
	r.mu.Lock()

	cfg := r.cfg
	cfg.Theme.Density = theme.DensityDefault
	h := &PanelHost{id: PanelAudio, theme: r.panelThemeForState(0, cfg, r.tokens), stopAnim: make(chan struct{})}
	h.place.Panel = ui.Rect{W: 700, H: 700}
	r.panelHosts[PanelAudio] = h
	h.root = r.panelTree(h)
	if err := h.configure(700, 700, 120); err != nil {
		t.Fatal(err)
	}
	old := findKind(h.root, ui.KindSlider)
	if old == nil {
		t.Fatal("missing slider")
	}
	old.Action = "audio-vol:42"
	old.State &^= ui.StateDisabled
	old.Value = 25
	h.sliderDrag = old
	h.pressed = old.StableKey()
	h.pointer.setPress(h.pressed)
	value := old.Value
	cfg.Theme.Density = theme.DensitySpacious
	r.retheThemeOpenSurfacesLocked(cfg, r.tokens)
	current := findKind(h.root, ui.KindSlider)
	if h.sliderDrag != nil && h.sliderDrag != current {
		t.Errorf("drag retains detached slider %p (height %d); current slider %p (height %d)", h.sliderDrag, h.sliderDrag.Bounds.H, current, current.Bounds.H)
	}
	if h.sliderDrag != nil || h.pressed != "" || h.pointer.press != "" {
		t.Error("tree replacement retained transient press/drag")
	}
	r.mu.Unlock()
	handle := h.handle(r)
	handle(wayland.Event{Kind: wayland.EventPointerMotion, X: 650, Y: 100})
	if handled := handle(wayland.Event{Kind: wayland.EventPointerRelease, X: 650, Y: 100}); handled {
		t.Error("detached release was committed")
	}
	if old.Value != value {
		t.Error("motion changed detached slider")
	}
	time.Sleep(50 * time.Millisecond)
	if data, _ := os.ReadFile(logPath); strings.Contains(string(data), "set-volume") {
		t.Errorf("stale service write: %s", data)
	}
}

func TestMediaStripMeasuresLogicalTextBeforeOpenAndReload(t *testing.T) {
	r, h, _ := stripTestRegistry(t, services.MediaState{Available: true, Identity: "mpv", Title: "Track", CanSeek: true, CanPause: true, LengthUS: 300000000})
	r.mu.Lock()
	defer r.mu.Unlock()
	r.cfg.Theme.Density = theme.DensityMini
	r.cfg.Theme.FontScale = 200
	h.scale120 = 150
	openStripForTest(t, h, ui.Rect{X: 800, W: 160, H: 32}, true)
	for _, scale := range []int{120, 150} {
		if err := h.configure(int(h.spec().Width), int(h.spec().Height), scale); err != nil {
			t.Fatal(err)
		}
		next := r.panelThemeForState(7, r.cfg, r.tokens)
		h.rethemeLocked(next, r.cfg.Bar.FontFamily)
		// Use the panel's established physical-to-logical measurement contract.
		expected := &PanelHost{theme: next, text: h.text, scale120: scale}
		measure := expected.measureText()
		attrs := ui.TextAttrs{Role: theme.RoleCaption}
		wantW, wantH := measure("05:00", attrs)
		gotW, gotH := h.measureText("05:00", attrs)
		if gotW != wantW || gotH != wantH {
			t.Errorf("scale %d: strip text %dx%d, panel logical text %dx%d", scale, gotW, gotH, wantW, wantH)
		}
		height, err := ui.ContentHeight(h.root, mediaStripW, measure)
		if err != nil {
			t.Fatal(err)
		}
		if int(h.spec().Height) < height {
			t.Errorf("scale %d: requested %d below intrinsic logical height %d", scale, h.spec().Height, height)
		}
		for _, n := range ui.Focusables(h.root) {
			if n.Bounds.Y+n.Bounds.H > int(h.spec().Height)-mediaStripPad {
				t.Errorf("scale %d: %s at %+v clips", scale, n.Action, n.Bounds)
			}
		}
	}
}

func TestMediaStripPinnedDensitySurvivesConfigAdoption(t *testing.T) {
	r, h, _ := stripTestRegistry(t, services.MediaState{Available: true, Identity: "mpv", Title: "Track", Status: services.PlaybackPaused, CanSeek: true, CanPause: true, LengthUS: 300000000})
	keepInvalidationsDrained(t, r)
	r.mu.Lock()
	bar := r.bars[7]
	connector := bar.connector()
	bar.setOutputSize(1536, 864)
	if err := bar.Configure(1536, r.cfg.Bar.SurfaceExtent(), 120); err != nil {
		r.mu.Unlock()
		t.Fatal(err)
	}
	openStripForTest(t, h, ui.Rect{X: 800, W: 160, H: 32}, true)
	generation := h.rootGen
	r.mu.Unlock()
	for _, tc := range []struct {
		density    theme.Density
		height     int
		removeLeft bool
	}{{theme.DensityDefault, 44, false}, {theme.DensityMini, 28, false}, {theme.DensitySpacious, 68, false}, {theme.DensitySpacious, 68, true}} {
		r.mu.Lock()
		before := r.cfg.Clone()
		candidate := before.Clone()
		r.mu.Unlock()
		candidate.Theme.Density = tc.density
		if tc.removeLeft {
			candidate.Bar.Left = nil
		}
		config.RebaseDerivedBar(&candidate, before.Theme, before.Bar)
		prepared, err := r.PrepareConfig(candidate, []wayland.HostIdentity{{Global: 7, Connector: connector}})
		if err != nil {
			t.Fatal(err)
		}
		// The owner configures the replacement before publishing the commit.
		hooks := prepared.Hosts[7]
		hooks.OutputSize(1536, 864)
		if err := hooks.Configure(1536, candidate.ForConnector(connector).SurfaceExtent(), 120); err != nil {
			t.Fatal(err)
		}
		prepared.Commit()
		hooks.OutputSize(1536, 864)
		r.mu.Lock()
		retained := h.open_ && h.pinned && h.rootGen == generation
		r.mu.Unlock()
		if !retained {
			t.Fatalf("%s adoption lost pinned root before first paint", tc.density)
		}
		width, height := 1536, candidate.ForConnector(connector).SurfaceExtent()
		if err := hooks.Render(make([]byte, width*height*4), width, height, width*4); err != nil {
			t.Fatal(err)
		}
		r.mu.Lock()
		open, pinned, sameGeneration := h.open_, h.pinned, h.rootGen == generation
		slider := findKind(h.root, ui.KindSlider)
		sliderHeight := slider.Bounds.H
		anchor := r.bars[7].actionBounds(panelMediaAction)
		followsAnchor := h.anchor == anchor
		r.mu.Unlock()
		if !open || !pinned || !sameGeneration {
			t.Fatalf("%s adoption lost pinned root: open=%v pinned=%v sameRoot=%v", tc.density, open, pinned, sameGeneration)
		}
		if sliderHeight != tc.height || !followsAnchor {
			t.Errorf("%s: height %d want %d; follows new pill=%v", tc.density, sliderHeight, tc.height, followsAnchor)
		}
		r.mu.Lock()
		retainedTree := h.root
		r.mu.Unlock()
		if err := hooks.Render(make([]byte, width*height*4), width, height, width*4); err != nil {
			t.Fatal(err)
		}
		r.mu.Lock()
		unchanged := h.root == retainedTree
		r.mu.Unlock()
		if !unchanged {
			t.Error("unchanged bar frame rebuilt the strip")
		}

	}
	r.mu.Lock()
	disabled := r.cfg.Clone()
	r.mu.Unlock()
	disabled.Bar.Enabled = false
	prepared, err := r.PrepareConfig(disabled, nil)
	if err != nil {
		t.Fatal(err)
	}
	prepared.Commit()
	r.mu.Lock()
	open := h.open_
	r.mu.Unlock()
	if open {
		t.Error("disabled output bar retained its strip")
	}
}

// A work area shorter than the strip clamped the surface and cut off the
// transport row (sysc-2248). The strip steps its controls down a density at a
// time until it fits, keeping every control.
func TestMediaStripStepsItsControlsDownToFitAShortWorkArea(t *testing.T) {
	r, h, _ := stripTestRegistry(t, services.MediaState{Available: true, Identity: "mpv", Title: "Track", Artist: "Artist",
		Status: services.PlaybackPlaying, CanSeek: true, CanPause: true, CanPlay: true, CanNext: true, CanPrev: true, LengthUS: 300000000})
	r.mu.Lock()
	defer r.mu.Unlock()
	r.cfg.Theme.Density = theme.DensitySpacious
	h.rethemeLocked(r.panelThemeForState(7, r.cfg, r.tokens), r.cfg.Bar.FontFamily)
	spacious := h.style.Metrics
	bar := r.bars[7]
	full := 0
	for _, tc := range []struct {
		name  string
		work  int
		fewer bool
	}{{"tall", 600, false}, {"short", 200, true}} {
		bar.setOutputSize(1536, tc.work+r.cfg.ForConnector(bar.connector()).ExclusiveZone())
		h.closeLocked()
		openStripForTest(t, h, ui.Rect{X: 800, W: 160, H: 32}, true)
		height := int(h.spec().Height)
		if height > tc.work {
			t.Fatalf("%s: strip is %d tall in a %d px work area", tc.name, height, tc.work)
		}
		for _, n := range ui.Focusables(h.root) {
			if n.Bounds.Y+n.Bounds.H > height-mediaStripPad {
				t.Errorf("%s: %s at %+v is cut off by the %d px strip", tc.name, n.Name, n.Bounds, height)
			}
		}
		slider := findKind(h.root, ui.KindSlider)
		if slider == nil {
			t.Fatalf("%s: the seek slider is gone", tc.name)
		}
		if full == 0 {
			full = slider.Bounds.H
		}
		if stepped := slider.Bounds.H < full; stepped != tc.fewer {
			t.Errorf("%s: slider %d tall against spacious %d; stepped down = %v, want %v",
				tc.name, slider.Bounds.H, full, stepped, tc.fewer)
		}
		if h.style.Metrics != spacious {
			t.Errorf("%s: fitting overwrote the theme's own metrics", tc.name)
		}
	}
}
