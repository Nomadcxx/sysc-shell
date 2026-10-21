package shell

import (
	"testing"
	"time"

	"github.com/Nomadcxx/sysc-shell/internal/config"
	"github.com/Nomadcxx/sysc-shell/internal/platform/wayland"
	"github.com/Nomadcxx/sysc-shell/internal/services"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

// benchInvalidationsDrained is keepInvalidationsDrained for a benchmark: same
// stand-in for the owner's bridge, with testing.TB in place of *testing.T.
func benchInvalidationsDrained(b *testing.B, r *Registry) {
	b.Helper()
	stop := make(chan struct{})
	go func() {
		for {
			select {
			case <-r.invalidations:
			case <-r.closed:
				return
			case <-stop:
				return
			}
		}
	}()
	b.Cleanup(func() { close(stop) })
}

// benchDrainAux consumes the open request pair the placement publishes.
func benchDrainAux(b *testing.B, reg *Registry) []wayland.AuxRequest {
	b.Helper()
	out := make([]wayland.AuxRequest, 0, 2)
	deadline := time.After(asyncTestWait)
	for len(out) < 2 {
		select {
		case req := <-reg.AuxRequests():
			out = append(out, req)
		case <-deadline:
			b.Fatalf("got %d aux requests, want 2", len(out))
		}
	}
	return out
}

// benchSettleReveal pins every animator value to its endpoint so a frame paints
// the settled surface rather than a mid-reveal one.
func benchSettleReveal(b *testing.B, reg *Registry, h *PanelHost) {
	b.Helper()
	reg.mu.Lock()
	defer reg.mu.Unlock()
	if h == nil || h.anim == nil {
		return
	}
	for key, value := range h.anim.values {
		value.start = time.Time{}
		h.anim.values[key] = value
	}
}

// benchPanel opens a panel through the real open path, selects a section, and
// returns the placement's surface callbacks plus a configured frame buffer, so
// each iteration drives the same code a compositor frame callback drives.
func benchPanel(b *testing.B, id PanelID, section string, scale120 int) (*Registry, *wayland.AuxSpec) {
	b.Helper()
	cfg := config.Default()
	cfg.Theme.BlurBehind = false
	cfg.Accessibility.ReducedMotion = true
	reg := NewRegistry(cfg)
	b.Cleanup(reg.Close)
	benchInvalidationsDrained(b, reg)
	if id == PanelControlCenter {
		state := benchMediaState()
		reg.mediaState = state
		reg.mediaPlayers = []services.Player{{Bus: state.Player, Identity: state.Identity, Active: true}}
	}
	if err := reg.OpenPanel(id, 7, Trigger{BarEdge: "top", BarZone: 40, OutW: 3440, OutH: 1440}); err != nil {
		b.Fatal(err)
	}
	reqs := benchDrainAux(b, reg)
	if section != "" {
		reg.mu.Lock()
		err := reg.selectPanelSectionLocked(id, section)
		reg.mu.Unlock()
		if err != nil {
			b.Fatal(err)
		}
	}
	h := reg.panelHosts[id]
	benchSettleReveal(b, reg, h)
	spec := reqs[1].Open
	if err := spec.Callbacks.Configure(int(spec.Width), int(spec.Height), scale120); err != nil {
		b.Fatal(err)
	}
	return reg, spec
}

// benchMediaState is a playing track, which is what builds the media page's
// artwork stack and the foreground opacity group the perf report measures.
func benchMediaState() services.MediaState {
	return services.MediaState{
		Available: true, Player: "org.mpris.MediaPlayer2.player", Identity: "Player",
		Title: "Track title", Artist: "Artist", Album: "Album",
		Status:  services.PlaybackPlaying,
		CanPrev: true, CanPlay: true, CanNext: true, CanSeek: true,
		LengthUS: 180_000_000, PositionUS: 45_000_000,
	}
}

// benchFrames runs the surface's render callback the way a frame callback does,
// over a buffer sized from the configured surface at the requested scale. One
// frame is painted before timing starts: the first frame warms the text
// renderer, the mask caches and the layout, and the steady state the report
// measures is every frame after it.
func benchFrames(b *testing.B, spec *wayland.AuxSpec, scale120 int) {
	b.Helper()
	scale := ui.Scale120(scale120)
	pw, ph := scale.Physical(int(spec.Width)), scale.Physical(int(spec.Height))
	pixels := make([]byte, pw*ph*4)
	if err := spec.Callbacks.Render(pixels, pw, ph, pw*4); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		if err := spec.Callbacks.Render(pixels, pw, ph, pw*4); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkSettingsFrame paints every Settings page at 2x, the frame the perf
// report profiles: the root rim strokes the whole surface on each one.
func BenchmarkSettingsFrame(b *testing.B) {
	for _, section := range settingsSections {
		b.Run(section, func(b *testing.B) {
			_, spec := benchPanel(b, PanelSettings, section, 240)
			benchFrames(b, spec, 240)
		})
	}
}

// BenchmarkControlCentreMediaFrame paints the control-centre Media page at 2x,
// the page whose foreground column carries the group opacity.
func BenchmarkControlCentreMediaFrame(b *testing.B) {
	_, spec := benchPanel(b, PanelControlCenter, "media", 240)
	benchFrames(b, spec, 240)
}
