package shell

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Nomadcxx/sysc-shell/internal/services"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

func ccAudioTestSnapshot() services.AudioSnapshot {
	return services.AudioSnapshot{
		Sinks:   []services.AudioNode{{ID: 46, Description: "Speakers", Default: true}},
		Sources: []services.AudioNode{{ID: 51, Description: "Microphone", Default: true, Level: 70, Muted: true}},
		Streams: []services.AudioNode{{ID: 80}, {ID: 81}, {ID: 82}},
	}
}

func TestControlCentreAudioPageShowsOutputInputAndTheMixer(t *testing.T) {
	h := &PanelHost{id: PanelControlCenter, section: "audio", theme: DefaultTheme()}
	page := ccAudioPage(h, nil, services.AudioState{Level: 40}, true, ccAudioTestSnapshot(), true)
	if page.Height != ccPageH {
		t.Errorf("page height = %d, want the %d px body", page.Height, ccPageH)
	}

	volume := findAction(page, "cc:volume")
	if volume == nil || volume.Value != 40 || volume.Name != "Speakers" || volume.State.Has(ui.StateDisabled) {
		t.Errorf("output slider = %+v, want Speakers at 40", volume)
	}
	if mute := findAction(page, "cc:mute"); mute == nil || mute.Kind != ui.KindButton || mute.State.Has(ui.StateSelected) {
		t.Errorf("output mute = %+v, want an unselected toggle", mute)
	}

	level := findAction(page, "audio-vol:51")
	if level == nil || level.Value != 70 || level.Name != "Microphone" || level.State.Has(ui.StateDisabled) {
		t.Errorf("input slider = %+v, want Microphone at 70", level)
	}
	if mute := findAction(page, "audio-mute:51"); mute == nil || !mute.State.Has(ui.StateSelected) {
		t.Errorf("input mute = %+v, want it selected for a muted source", mute)
	}

	open := findAction(page, ccAudioMixerAction)
	if open == nil || !open.Focusable || !strings.Contains(renderText(open), "Audio mixer · 3 apps") {
		t.Errorf("mixer link = %+v (%q), want Audio mixer · 3 apps", open, renderText(open))
	}
	// The page is the quick surface; per-application rows stay in the panel.
	if got := renderText(page); strings.Contains(got, "Applications") {
		t.Errorf("audio page %q drew the per-application mixer", got)
	}
}

func TestControlCentreAudioInputWaitsForTheMixer(t *testing.T) {
	h := &PanelHost{id: PanelControlCenter, section: "audio", theme: DefaultTheme()}
	page := ccAudioPage(h, nil, services.AudioState{Level: 40}, true, services.AudioSnapshot{}, false)
	if volume := findAction(page, "cc:volume"); volume == nil || volume.Name != "Volume" || volume.State.Has(ui.StateDisabled) {
		t.Errorf("output slider = %+v, want the default-sink poll to drive it before the mixer reports", volume)
	}
	level := findAction(page, "audio-vol:0")
	if level == nil || !level.State.Has(ui.StateDisabled) || level.Focusable {
		t.Errorf("input slider = %+v, want it disabled until the mixer names a source", level)
	}
	if open := findAction(page, ccAudioMixerAction); open == nil || open.Name != "Audio mixer" {
		t.Errorf("mixer link = %+v, want no count before the mixer reports", open)
	}
}

func TestControlCentreAudioInputShowsAPendingLevel(t *testing.T) {
	h := &PanelHost{id: PanelControlCenter, section: "audio", theme: DefaultTheme(),
		pendingVol: map[int]int{51: 20}, pendingMute: map[int]bool{51: false}}
	page := ccAudioPage(h, nil, services.AudioState{}, true, ccAudioTestSnapshot(), true)
	if level := findAction(page, "audio-vol:51"); level == nil || level.Value != 20 {
		t.Errorf("input slider = %+v, want the pending 20", level)
	}
	if mute := findAction(page, "audio-mute:51"); mute == nil || mute.State.Has(ui.StateSelected) {
		t.Errorf("input mute = %+v, want the pending unmute", mute)
	}
}

func TestControlCentreAudioMixerLinkOpensTheAudioPanel(t *testing.T) {
	r := newPanelRegistry(t)
	if err := r.OpenPanel(PanelControlCenter, 7, Trigger{}); err != nil {
		t.Fatal(err)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	h := r.panelHosts[PanelControlCenter]
	if !h.activateControlCentre(r, &ui.Node{Action: ccAudioMixerAction}) {
		t.Fatal("mixer link ignored")
	}
	if r.panelHosts[PanelAudio] == nil || r.panelHosts[PanelControlCenter] != nil {
		t.Fatal("mixer link did not hand the Control Centre over to the Audio panel")
	}
}

// The mixer polls pw-dump every second, so the page holds it only while it is
// shown: the bar's cheap default-sink poll is all the other pages need.
func TestControlCentreAudioPageLeasesTheMixerOnlyWhileShown(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "pw-dump"), []byte("#!/bin/sh\nprintf '[]'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+":"+os.Getenv("PATH"))
	r := newPanelRegistry(t)
	r.audio = services.NewAudio(time.Hour, "/bin/true")
	if err := r.OpenPanel(PanelControlCenter, 7, Trigger{}); err != nil {
		t.Fatal(err)
	}
	r.mu.Lock()
	h := r.panelHosts[PanelControlCenter]
	if h.mixerLease != nil {
		r.mu.Unlock()
		t.Fatal("Home leased the mixer")
	}
	h.selectControlCentreSection(r, "audio")
	if h.mixerLease == nil {
		r.mu.Unlock()
		t.Fatal("the Audio page did not lease the mixer")
	}
	if got := r.mixerHostsLocked(); len(got) != 1 || got[0] != h {
		t.Errorf("mixer hosts = %v, want the Control Centre on its Audio page", got)
	}
	h.selectControlCentreSection(r, "home")
	if h.mixerLease != nil {
		r.mu.Unlock()
		t.Fatal("leaving the Audio page kept the mixer lease")
	}
	if got := r.mixerHostsLocked(); len(got) != 0 {
		t.Errorf("mixer hosts = %v, want none once the page is left", got)
	}
	h.selectControlCentreSection(r, "audio")
	r.mu.Unlock()
	r.ClosePanel(PanelControlCenter)
	if h.mixerLease != nil {
		t.Fatal("closing on the Audio page kept the mixer lease")
	}
}

// Layout sizes text by MaxWidth, not Width: a label given only a Width pushed
// the slider and the percentage off the card for a long device name.
func TestControlCentreAudioRowsKeepALongDeviceNameInside(t *testing.T) {
	h := &PanelHost{id: PanelControlCenter, section: "audio", theme: DefaultTheme()}
	snap := ccAudioTestSnapshot()
	snap.Sinks[0].Description = "Built-in Audio Analog Stereo Headphones Output"
	snap.Sources[0].Description = "Blue Yeti Stereo Microphone Analog Stereo Input"
	page := ccAudioPage(h, nil, services.AudioState{Level: 40}, true, snap, true)
	measure := func(s string, _ ui.TextAttrs) (int, int) { return len(s) * 7, 16 }
	body := ccBodyWidth(h)
	if err := ui.LayoutColumn(page, ui.Rect{W: body, H: ccPageH}, measure); err != nil {
		t.Fatal(err)
	}
	output, input := findAction(page, "cc:volume"), findAction(page, "audio-vol:51")
	if output.Bounds.X != input.Bounds.X {
		t.Errorf("output slider starts at %d, input at %d; want one column", output.Bounds.X, input.Bounds.X)
	}
	walkNodes(page, func(n *ui.Node) {
		if b := n.Bounds; b.X+b.W > body {
			t.Errorf("%q (%s) ends at %d, past the %d px body", n.Text+n.Name, n.Kind, b.X+b.W, body)
		}
	})
}
