package shell

import (
	"strings"
	"testing"

	"github.com/Nomadcxx/sysc-shell/internal/config"
	"github.com/Nomadcxx/sysc-shell/internal/platform/wayland"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

func TestBarDisplaysDiscoversHiddenAndDisconnectedOutputs(t *testing.T) {
	r, h := openSettingsForPreview(t)
	r.UpdateDisplayOutputs([]wayland.OutputInfo{{Global: 7, Connector: "DP-1", Width: 1536, Height: 864}, {Global: 8, Connector: "HDMI-A-1", Make: "Dell", Model: "Office", Width: 1920, Height: 1080}})
	r.mu.Lock()
	defer r.mu.Unlock()
	cfg, err := config.Parse([]byte(`{"outputs":[{"connector":"DP-9","bar":{"edge":"left"}}]}`))
	if err != nil {
		t.Fatal(err)
	}
	h.draft = cfg
	h.set = r.settingsForLocked(cfg)
	h.section, h.settingsPage = "Bar", "Displays"
	r.rebuildPanel(h)
	for _, conn := range []string{"DP-1", "HDMI-A-1", "DP-9"} {
		if findAction(h.root, "set:outputs."+conn+".enabled") == nil {
			t.Errorf("missing %s visibility", conn)
		}
		if findAction(h.root, "bar-display-layout:"+conn) == nil {
			t.Errorf("missing %s layout shortcut", conn)
		}
	}
	text := renderText(h.root)
	for _, want := range []string{"Dell Office", "Disconnected", "Shared", "Custom"} {
		if !strings.Contains(text, want) {
			t.Errorf("missing %q in %s", want, text)
		}
	}
	if len(h.draft.Outputs) != 1 {
		t.Fatal("opening page created overrides")
	}
	if findAction(h.root, "reset:outputs.DP-9.edge") == nil {
		t.Fatal("explicit edge has no reset")
	}
	if findAction(h.root, "reset:outputs.HDMI-A-1.edge") != nil {
		t.Fatal("inherited edge has reset")
	}
}

func TestBarDisplayResetPreservesLayoutAndAdvancedFields(t *testing.T) {
	r, h := openSettingsForPreview(t)
	r.mu.Lock()
	defer r.mu.Unlock()
	cfg, err := config.Parse([]byte(`{"outputs":[{"connector":"DP-1","bar":{"enabled":false,"edge":"left","gap":4,"font":{"size":20},"style":"solid","items":{"left":["clock"]}}}]}`))
	if err != nil {
		t.Fatal(err)
	}
	h.draft = cfg
	h.set = r.settingsForLocked(cfg)
	h.section, h.settingsPage = "Bar", "Displays"
	r.rebuildPanel(h)
	h.focus = []*ui.Node{{Kind: ui.KindButton, Action: "bar-display-reset:DP-1", Focusable: true}}
	h.roving = ui.Roving{Count: 1}
	h.activate(r)
	bar := h.draft.ForConnector("DP-1")
	if !bar.Enabled || bar.Edge != cfg.Bar.Edge || bar.Gap != cfg.Bar.Gap {
		t.Fatalf("reset retained basic fields: %+v", bar)
	}
	if bar.Style != "solid" || len(bar.Left) != 1 || bar.Left[0].ID != "clock" {
		t.Fatal("reset destroyed advanced fields or layout")
	}
	h.focus = []*ui.Node{{Kind: ui.KindButton, Action: "bar-display-layout:DP-1", Focusable: true}}
	h.roving = ui.Roving{Count: 1}
	h.activate(r)
	if h.barOutput != "DP-1" || h.settingsPage != "Layout" {
		t.Fatalf("shortcut lost target: %q/%q", h.barOutput, h.settingsPage)
	}
}

func TestOutputSettingsReplayCreatesAndResetsAnAbsentConnector(t *testing.T) {
	cfg := config.Default()
	if err := applySettingsWriteChanges(&cfg, []settingsWriteChange{{path: "outputs.DP-2.height", value: "64"}}, nil); err != nil {
		t.Fatal(err)
	}
	if cfg.ForConnector("DP-2").Height != 64 {
		t.Fatal("new override not replayed")
	}
	if err := applySettingsWriteChanges(&cfg, []settingsWriteChange{{path: "outputs.DP-2.height", reset: true}}, nil); err != nil {
		t.Fatal(err)
	}
	if cfg.OutputBarOverridden("DP-2", "height") {
		t.Fatal("reset replay pinned value")
	}
}

func TestSettingsSurvivesDisablingItsHostBar(t *testing.T) {
	r, h := openSettingsForPreview(t)
	cfg := r.cfg.Clone()
	cfg.Bar.Enabled = false
	prepared, err := r.PrepareConfig(cfg, []wayland.HostIdentity{{Global: 7, Connector: "DP-1"}})
	if err != nil {
		t.Fatal(err)
	}
	prepared.Commit()
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.panelHosts[PanelSettings] != h {
		t.Fatal("disabling the host bar closed Settings")
	}
}

func TestSettingsCanOpenAfterAllBarsAreHidden(t *testing.T) {
	r := newPanelRegistry(t)
	r.UpdateDisplayOutputs([]wayland.OutputInfo{{Global: 7, Connector: "DP-1", Width: 1536, Height: 864}})
	r.mu.Lock()
	r.focused = "DP-1"
	r.cfg.Bar.Enabled = false
	global, trig := r.focusedTriggerLocked()
	r.mu.Unlock()
	if global != 7 || trig.OutW != 1536 || trig.OutH != 864 || trig.BarZone != 0 {
		t.Fatalf("hidden-bar settings target: %d %+v", global, trig)
	}
	if err := r.OpenPanel(PanelSettings, global, trig); err != nil {
		t.Fatal(err)
	}
}
