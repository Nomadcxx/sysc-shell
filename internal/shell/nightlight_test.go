package shell

import (
	"strings"
	"testing"

	"github.com/Nomadcxx/sysc-shell/internal/config"
	"github.com/Nomadcxx/sysc-shell/internal/platform/wayland"
	"github.com/Nomadcxx/sysc-shell/internal/services"
	"github.com/Nomadcxx/sysc-shell/internal/theme"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

func TestControlCentreNightLightPillTracksGammaAvailability(t *testing.T) {
	cfg := config.Default()
	cfg.NightLight.Mode = config.NightLightModeAlways
	cfg.Accessibility.ReducedMotion = true
	r := NewRegistry(cfg)
	t.Cleanup(r.Close)
	h := &PanelHost{id: PanelControlCenter, section: "home", output: 1, theme: DefaultTheme()}

	row := ccHomeToggles(r, 640, false, "")
	if len(row.Children) != 4 {
		t.Fatalf("Home toggle row has %d pills, want the four standard pills", len(row.Children))
	}
	if findByName(row, "Night") != nil {
		t.Fatal("night light pill appeared before gamma support was reported")
	}

	r.ApplyGammaEvent(wayland.GammaEvent{Global: 1, Connector: "DP-1", State: wayland.GammaFailed})
	row = ccHomeToggles(r, 640, false, "")
	if len(row.Children) != 5 {
		t.Fatalf("supported Home toggle row has %d pills, want 5", len(row.Children))
	}
	night := findByName(row, "Night")
	if night == nil || night.Action != "cc:nightlight" || !strings.Contains(night.Tooltip, "Unavailable") {
		t.Fatalf("failed gamma pill = %+v, want the unavailable status and toggle action", night)
	}

	r.ApplyGammaEvent(wayland.GammaEvent{Global: 1, Connector: "DP-1", State: wayland.GammaReady})
	night = findByName(ccHomeToggles(r, 640, false, ""), "Night")
	if night == nil || night.Fill != ui.FillAccent {
		t.Fatalf("active gamma pill = %+v, want accent fill", night)
	}

	r.mu.Lock()
	handled := h.activateControlCentre(r, &ui.Node{Action: "cc:nightlight"})
	r.mu.Unlock()
	if !handled {
		t.Fatal("night light action was not handled")
	}
	if state := r.NightLight().State(); !state.Override || state.Kelvin != 0 {
		t.Fatalf("night light after tile activation = %+v, want an off override", state)
	}
}

func TestControlCentreHidesNightLightWithoutGamma(t *testing.T) {
	r := NewRegistry(config.Default())
	t.Cleanup(r.Close)
	r.ApplyGammaEvent(wayland.GammaEvent{State: wayland.GammaUnsupported})
	if got := findByName(ccHomeToggles(r, 640, false, ""), "Night"); got != nil {
		t.Fatalf("unsupported night light pill = %+v, want hidden", got)
	}
}

func TestNightLightStatusTextShowsUnavailableReason(t *testing.T) {
	state := services.NightLightState{Reason: "Your compositor does not offer gamma control (wlr-gamma-control)."}
	if got := nightLightStatusText(state); !strings.Contains(got, state.Reason) {
		t.Fatalf("status = %q, want %q", got, state.Reason)
	}
}

func TestNightLightTooltipShowsPartialOutputFailure(t *testing.T) {
	state := services.NightLightState{Available: true, Kelvin: 4000, Target: 4000,
		Reason: "Some outputs cannot adjust colour (DP-1); other outputs remain active."}
	if got := nightLightTooltip(state); !strings.Contains(got, state.Reason) {
		t.Fatalf("tooltip = %q, want partial failure reason %q", got, state.Reason)
	}
}

func TestNightLightSettingsRendersCompositorStatus(t *testing.T) {
	r := NewRegistry(config.Default())
	t.Cleanup(r.Close)
	r.ApplyGammaEvent(wayland.GammaEvent{State: wayland.GammaUnsupported})
	cfg := config.Default()
	h := &PanelHost{
		id: PanelSettings, section: "Night Light", theme: DefaultTheme(), draft: cfg,
		place: Placement{Panel: ui.Rect{W: 900, H: 700}, Output: ui.Rect{W: 1920, H: 1080}},
	}
	entries := r.settingsFor(cfg).Section("Night Light")
	content := settingsSectionColumn(r, h, "Night Light", entries)
	text := renderText(content)
	if !strings.Contains(text, "status") || !strings.Contains(text, "Your compositor does not offer gamma control") {
		t.Fatalf("Night Light settings = %q, want the status card and unsupported reason", text)
	}
	if content == nil || content.Kind != ui.KindScroll || h.metrics().PanelPadding == 0 || theme.MarginM == 0 {
		t.Fatal("Night Light settings did not build its content")
	}
}

func TestNightLightConfigReloadReconfiguresSchedule(t *testing.T) {
	r := NewRegistry(config.Default())
	t.Cleanup(r.Close)
	cfg := config.Default()
	cfg.NightLight.Mode = config.NightLightModeAlways
	cfg.NightLight.NightKelvin = 3500
	prepared, err := r.PrepareConfig(cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	prepared.Commit()
	state := r.NightLight().State()
	if state.Mode != config.NightLightModeAlways || state.Kelvin != 3500 || state.Target != 3500 {
		t.Fatalf("state after config reload = %+v", state)
	}
}
