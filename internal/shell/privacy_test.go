package shell

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Nomadcxx/sysc-shell/internal/config"
	"github.com/Nomadcxx/sysc-shell/internal/platform/niri"
	"github.com/Nomadcxx/sysc-shell/internal/services"
)

func TestPrivacyStateKeepsAmbiguousCaptureUnknownAndCorrelatesCasts(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		pipe services.PrivacySnapshot
		niri niri.Snapshot
		want privacyState
	}{
		{
			name: "complete empty snapshots",
			pipe: services.PrivacySnapshot{Known: true},
			niri: niri.Snapshot{CastsKnown: true},
			want: privacyState{microphone: privacyInactive, camera: privacyInactive, screenShare: privacyInactive},
		},
		{
			name: "missing pipewire remains unknown",
			pipe: services.PrivacySnapshot{},
			niri: niri.Snapshot{CastsKnown: true},
			want: privacyState{microphone: privacyUnknown, camera: privacyUnknown, screenShare: privacyInactive},
		},
		{
			name: "unclassified paths remain unknown",
			pipe: services.PrivacySnapshot{Known: true, MicrophoneUnknown: true,
				VideoPaths: []services.PrivacyVideoPath{{SourceNodeID: 30}}},
			niri: niri.Snapshot{CastsKnown: true},
			want: privacyState{microphone: privacyUnknown, camera: privacyUnknown, screenShare: privacyInactive},
		},
		{
			name: "known camera evidence wins alongside ambiguity",
			pipe: services.PrivacySnapshot{Known: true, VideoPaths: []services.PrivacyVideoPath{
				{SourceNodeID: 30}, {SourceNodeID: 31, Camera: true},
			}},
			niri: niri.Snapshot{CastsKnown: true},
			want: privacyState{microphone: privacyInactive, camera: privacyActive, cameraIncomplete: true, cameraDetailsMissing: true, screenShare: privacyInactive},
		},
		{
			name: "cast node is not a camera",
			pipe: services.PrivacySnapshot{Known: true,
				VideoPaths: []services.PrivacyVideoPath{{SourceNodeID: 40, Camera: true}}},
			niri: niri.Snapshot{Casts: []niri.Cast{{StreamID: 1, Active: true, Target: niri.CastTarget{Kind: "output", Name: "DP-1"}, TargetSet: true, TargetKnown: true, PWNodeID: 40, HasPWNodeID: true}}},
			want: privacyState{microphone: privacyInactive, camera: privacyInactive, screenShare: privacyActive, screenShareIncomplete: true, screenTargets: "Output DP-1"},
		},
		{
			name: "paused cast node does not mask camera activity",
			pipe: services.PrivacySnapshot{Known: true,
				VideoPaths: []services.PrivacyVideoPath{{SourceNodeID: 40, Camera: true, ApplicationName: "Cheese"}}},
			niri: niri.Snapshot{CastsKnown: true, Casts: []niri.Cast{{StreamID: 1, Active: false,
				Target: niri.CastTarget{Kind: "output", Name: "DP-1"}, TargetSet: true, TargetKnown: true, PWNodeID: 40, HasPWNodeID: true}}},
			want: privacyState{microphone: privacyInactive, camera: privacyActive, screenShare: privacyInactive, cameraApps: "Cheese"},
		},
		{
			name: "unknown active cast target with a camera path stays uncertain",
			pipe: services.PrivacySnapshot{Known: true,
				VideoPaths: []services.PrivacyVideoPath{{SourceNodeID: 41, Camera: true}}},
			niri: niri.Snapshot{Casts: []niri.Cast{{StreamID: 2, Active: true, PWNodeID: 41, HasPWNodeID: true}}},
			want: privacyState{microphone: privacyInactive, camera: privacyUnknown, screenShare: privacyUnknown},
		},
		{
			name: "valid active cast proves share before full baseline",
			pipe: services.PrivacySnapshot{Known: true},
			niri: niri.Snapshot{Casts: []niri.Cast{{StreamID: 3, Active: true,
				Target: niri.CastTarget{Kind: "window", WindowID: 70}, TargetSet: true, TargetKnown: true}}},
			want: privacyState{microphone: privacyInactive, camera: privacyInactive, screenShare: privacyActive, screenShareIncomplete: true, screenTargets: "Window 70"},
		},
		{
			name: "cast baseline absence is unknown",
			pipe: services.PrivacySnapshot{Known: true},
			niri: niri.Snapshot{},
			want: privacyState{microphone: privacyInactive, camera: privacyInactive, screenShare: privacyUnknown},
		},
		{
			name: "known Nothing target is inactive",
			pipe: services.PrivacySnapshot{Known: true},
			niri: niri.Snapshot{CastsKnown: true, Casts: []niri.Cast{{StreamID: 4, Active: true,
				Target: niri.CastTarget{Kind: "nothing"}, TargetKnown: true}}},
			want: privacyState{microphone: privacyInactive, camera: privacyInactive, screenShare: privacyInactive},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := privacyStateFrom(tc.pipe, tc.niri); got != tc.want {
				t.Fatalf("derivePrivacyState() = %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestPrivacyStateCarriesSafeApplicationAndCastTargetDetails(t *testing.T) {
	t.Parallel()
	pipe := services.PrivacySnapshot{
		Known: true, MicrophoneActive: true, MicrophoneApps: []string{"Jitsi Meet"},
		VideoPaths: []services.PrivacyVideoPath{{SourceNodeID: 30, Camera: true, ApplicationName: "Cheese"}},
	}
	snapshot := niri.Snapshot{CastsKnown: true, Casts: []niri.Cast{{
		StreamID: 1, Active: true,
		Target:    niri.CastTarget{Kind: "window", Name: "private title", WindowID: 70},
		TargetSet: true, TargetKnown: true,
	}}}
	state := privacyStateFrom(pipe, snapshot)
	if got, want := state, (privacyState{
		microphone: privacyActive, camera: privacyActive, screenShare: privacyActive,
		microphoneApps: "Jitsi Meet", cameraApps: "Cheese", screenTargets: "Window 70",
	}); got != want {
		t.Fatalf("privacy state = %+v, want %+v", got, want)
	}
	tooltip := privacyTooltip(state)
	for _, want := range []string{"Microphone: Jitsi Meet", "Camera: Cheese", "Screen sharing: Window 70"} {
		if !strings.Contains(tooltip, want) {
			t.Errorf("privacy tooltip %q is missing %q", tooltip, want)
		}
	}
	if strings.Contains(tooltip, "private title") {
		t.Fatalf("privacy tooltip exposed a captured window title: %q", tooltip)
	}
}

func TestPrivacySurfacesKeepPartialActivityUncertaintyVisible(t *testing.T) {
	t.Parallel()
	validCast := niri.Cast{
		StreamID: 1, Active: true,
		Target: niri.CastTarget{Kind: "output", Name: "DP-1"}, TargetSet: true, TargetKnown: true,
	}
	tests := []struct {
		name     string
		pipe     services.PrivacySnapshot
		niri     niri.Snapshot
		activity privacyActivity
		label    string
	}{
		{
			name:     "microphone active with an unclassified source",
			pipe:     services.PrivacySnapshot{Known: true, MicrophoneActive: true, MicrophoneUnknown: true, MicrophoneApps: []string{"Jitsi Meet"}},
			niri:     niri.Snapshot{CastsKnown: true},
			activity: privacyActive, label: "Some microphone sources could not be classified",
		},
		{
			name: "camera active with an unclassified source",
			pipe: services.PrivacySnapshot{Known: true, VideoPaths: []services.PrivacyVideoPath{
				{SourceNodeID: 20, Camera: true, ApplicationName: "Cheese"}, {SourceNodeID: 21},
			}},
			niri:     niri.Snapshot{CastsKnown: true},
			activity: privacyActive, label: "Some camera sources could not be classified",
		},
		{
			name: "screen share active with an unknown target",
			pipe: services.PrivacySnapshot{Known: true},
			niri: niri.Snapshot{CastsKnown: true, Casts: []niri.Cast{
				validCast, {StreamID: 2, Active: true},
			}},
			activity: privacyActive, label: "Some screen-share targets could not be identified",
		},
		{
			name:     "screen share observed before its baseline",
			pipe:     services.PrivacySnapshot{Known: true},
			niri:     niri.Snapshot{Casts: []niri.Cast{validCast}},
			activity: privacyActive, label: "Some screen-share targets could not be identified",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			state := privacyStateFrom(tc.pipe, tc.niri)
			var got privacyActivity
			switch {
			case tc.pipe.MicrophoneActive:
				got = state.microphone
			case len(tc.pipe.VideoPaths) != 0:
				got = state.camera
			default:
				got = state.screenShare
			}
			if got != tc.activity {
				t.Fatalf("activity = %v, want active evidence preserved", got)
			}
			warnings := 0
			for _, icon := range privacyWidgetIcons(state) {
				if icon.name == "gpp_bad" {
					warnings++
				}
			}
			if warnings != 1 {
				t.Fatalf("warning icons = %d, want one when activity is partly unknown", warnings)
			}
			if tooltip := privacyTooltip(state); !strings.Contains(tooltip, tc.label) {
				t.Errorf("privacy tooltip %q is missing %q", tooltip, tc.label)
			}
			page := renderText(ccPrivacy(&Registry{privacy: state}, &PanelHost{theme: DefaultTheme()}))
			if !strings.Contains(page, tc.label) {
				t.Errorf("Privacy page %q is missing %q", page, tc.label)
			}
		})
	}
}

func TestPrivacyDetailsShowUnattributedApplicationsAlongsideKnownNames(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	dump := filepath.Join(dir, "dump.json")
	bin := filepath.Join(dir, "pw-dump")
	data := `[
		{"id":100,"type":"PipeWire:Interface:Device","info":{"props":{"device.form-factor":"microphone"}}},
		{"id":101,"type":"PipeWire:Interface:Device","info":{"props":{"device.api":"v4l2"}}},
		{"id":1,"type":"PipeWire:Interface:Node","info":{"state":"running","props":{"media.class":"Audio/Source","device.id":100}},"props":{}},
		{"id":2,"type":"PipeWire:Interface:Node","info":{"state":"running","props":{"media.class":"Stream/Input/Audio","application.name":"Jitsi Meet"}}},
		{"id":3,"type":"PipeWire:Interface:Node","info":{"state":"running","props":{"media.class":"Stream/Input/Audio"}}},
		{"id":4,"type":"PipeWire:Interface:Node","info":{"state":"running","props":{"media.class":"Video/Source","device.id":101}}},
		{"id":5,"type":"PipeWire:Interface:Node","info":{"state":"running","props":{"media.class":"Stream/Input/Video","application.name":"Cheese"}}},
		{"id":6,"type":"PipeWire:Interface:Node","info":{"state":"running","props":{"media.class":"Stream/Input/Video"}}},
		{"id":7,"type":"PipeWire:Interface:Link","info":{"state":"active","output-node-id":1,"input-node-id":2}},
		{"id":8,"type":"PipeWire:Interface:Link","info":{"state":"active","output-node-id":1,"input-node-id":3}},
		{"id":9,"type":"PipeWire:Interface:Link","info":{"state":"active","output-node-id":4,"input-node-id":5}},
		{"id":10,"type":"PipeWire:Interface:Link","info":{"state":"active","output-node-id":4,"input-node-id":6}}
	]`
	if err := os.WriteFile(dump, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(bin, []byte("#!/bin/sh\ncat '"+dump+"'\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	service := services.NewPrivacy(10*time.Millisecond, bin)
	defer service.Close()
	deadline := time.Now().Add(time.Second)
	var snapshot services.PrivacySnapshot
	for time.Now().Before(deadline) {
		snapshot = service.Snapshot()
		if snapshot.Known {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if !snapshot.Known {
		t.Fatal("PipeWire snapshot did not become known")
	}
	state := privacyStateFrom(snapshot, niri.Snapshot{CastsKnown: true})
	tooltip := privacyTooltip(state)
	for _, want := range []string{
		"Jitsi Meet; Application details unavailable",
		"Cheese; Application details unavailable",
	} {
		if !strings.Contains(tooltip, want) {
			t.Errorf("privacy tooltip %q is missing %q", tooltip, want)
		}
	}
	page := renderText(ccPrivacy(&Registry{privacy: state}, &PanelHost{theme: DefaultTheme()}))
	for _, want := range []string{"Jitsi Meet", "Cheese", "Application details unavailable"} {
		if !strings.Contains(page, want) {
			t.Errorf("Privacy page %q is missing %q", page, want)
		}
	}
}

func TestPrivacyWidgetHidesKnownInactiveAndShowsUnknownOnce(t *testing.T) {
	t.Parallel()
	metrics := DefaultTheme().Metrics
	w := buildPrivacyWidget(metrics.CapsulePadding, metrics)
	if w.node == nil || w.node.Action != panelPrivacyAction || w.node.Role != "button" {
		t.Fatalf("privacy node = %+v, want an accessible Control Centre button", w.node)
	}
	if w.node.Absent || len(w.node.Children) != 1 || len(w.node.Children[0].Children) != 1 || w.node.Children[0].Children[0].Icon != "gpp_bad" {
		t.Fatalf("initial privacy icons = %+v, absent=%v; want one unavailable indicator", w.node.Children, w.node.Absent)
	}
	if w.node.Padding != metrics.CapsulePadding {
		t.Fatalf("privacy padding = %d, want configured bar padding %d", w.node.Padding, metrics.CapsulePadding)
	}
	state := privacyState{microphone: privacyUnknown, camera: privacyInactive, screenShare: privacyActive}
	if !w.refresh(barView{Privacy: state}) {
		t.Fatal("refresh did not report the initial privacy state")
	}
	name := strings.ToLower(w.node.Name)
	if w.node.Absent || !strings.Contains(name, "microphone unavailable") || !strings.Contains(name, "screen sharing in use") {
		t.Fatalf("widget state name = %q, absent=%v", w.node.Name, w.node.Absent)
	}
	if len(w.node.Children) != 1 || len(w.node.Children[0].Children) != 2 {
		t.Fatalf("privacy icon row = %+v, want one warning and one active source", w.node.Children)
	}
	icons := w.node.Children[0].Children
	if icons[0].Icon != "gpp_bad" || icons[1].Icon != "desktop_windows" {
		t.Fatalf("privacy icons = %+v, want one warning and the active screen-share glyph", icons)
	}
	inactive := privacyState{microphone: privacyInactive, camera: privacyInactive, screenShare: privacyInactive}
	if !w.refresh(barView{Privacy: inactive}) {
		t.Fatal("known inactive update did not report a change")
	}
	if !w.node.Absent || !w.hideWhenAbsent {
		t.Fatalf("known inactive widget absent=%v hideWhenAbsent=%v, want no bar space", w.node.Absent, w.hideWhenAbsent)
	}
	if len(w.node.Children[0].Children) != 0 {
		t.Fatalf("known inactive widget retained icons: %+v", w.node.Children)
	}
}

func TestPrivacyWidgetTooltipIncludesActiveDetailsAndUnavailableSignals(t *testing.T) {
	t.Parallel()
	metrics := DefaultTheme().Metrics
	w := buildPrivacyWidget(metrics.CapsulePadding, metrics)
	state := privacyState{
		microphone: privacyActive, microphoneApps: "Jitsi Meet",
		camera:      privacyUnknown,
		screenShare: privacyActive, screenTargets: "Output DP-1",
	}
	w.refresh(barView{Privacy: state})
	for _, want := range []string{"Microphone: Jitsi Meet", "Camera: Unavailable", "Screen sharing: Output DP-1"} {
		if !strings.Contains(w.node.Tooltip, want) {
			t.Errorf("privacy tooltip %q is missing %q", w.node.Tooltip, want)
		}
	}
}

func TestPrivacyControlCentreShowsCurrentDetailsWithoutWindowTitles(t *testing.T) {
	t.Parallel()
	r := &Registry{privacy: privacyState{
		microphone: privacyActive, microphoneApps: "Jitsi Meet, Zoom",
		camera: privacyActive, cameraApps: "Cheese",
		screenShare: privacyActive, screenTargets: "Output DP-1, Window 70",
	}}
	h := &PanelHost{id: PanelControlCenter, section: "privacy", theme: DefaultTheme()}
	page := ccPrivacy(r, h)
	text := renderText(page)
	for _, want := range []string{"Jitsi Meet, Zoom", "Cheese", "Output DP-1", "Window 70", "In use"} {
		if !strings.Contains(text, want) {
			t.Errorf("Privacy page text %q is missing %q", text, want)
		}
	}
	if strings.Contains(text, "private title") {
		t.Fatalf("Privacy page exposed a captured window title: %q", text)
	}
}

func TestPrivacyWidgetOpensControlCentrePrivacyPage(t *testing.T) {
	r := newPanelRegistry(t)
	callbacks, err := r.NewHost(7, "DP-1")
	if err != nil {
		t.Fatal(err)
	}
	if err := callbacks.Configure(1536, 44, 120); err != nil {
		t.Fatal(err)
	}
	bar := r.bars[7]
	if err := bar.Layout(1536, 44); err != nil {
		t.Fatal(err)
	}
	target := bar.actionBounds(panelPrivacyAction)
	if target.W == 0 {
		t.Fatal("privacy button has no action bounds")
	}
	drainAuxQueue(r)
	if !clickButton(bar, target.X+target.W/2, target.Y+target.H/2, buttonLeft) {
		t.Fatal("privacy button did not activate")
	}
	_ = drainAux(t, r, 2)
	r.mu.Lock()
	h := r.panelHosts[PanelControlCenter]
	section := ""
	if h != nil {
		section = h.section
	}
	r.mu.Unlock()
	if section != "privacy" {
		t.Fatalf("Control Centre section = %q, want privacy", section)
	}
}

func TestPrivacyRegistryRefreshesBarAndClearsStaleActivityOnFailure(t *testing.T) {
	r := newPanelRegistry(t)
	w := buildPrivacyWidget(DefaultTheme().Metrics.CapsulePadding, DefaultTheme().Metrics)
	bar := &Bar{conn: "DP-1", right: []textWidget{w}}
	r.mu.Lock()
	r.bars = map[uint32]*Bar{7: bar}
	bar.apply(r.viewLocked("DP-1"))
	r.mu.Unlock()

	dir := t.TempDir()
	bin := filepath.Join(dir, "pw-dump")
	result := filepath.Join(dir, "result")
	fail := filepath.Join(dir, "fail")
	fixture, err := os.ReadFile("../services/testdata/privacy-pw-dump.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(result, fixture, 0o600); err != nil {
		t.Fatal(err)
	}
	script := "#!/bin/sh\nif [ -f '" + fail + "' ]; then exit 1; fi\ncat '" + result + "'\n"
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	r.setPrivacyService(services.NewPrivacy(10*time.Millisecond, bin))
	waitRegistryPrivacy(t, r, bar, privacyActive, "Microphone in use")

	if err := os.WriteFile(fail, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	waitRegistryPrivacy(t, r, bar, privacyUnknown, "Microphone unavailable")

	if err := os.WriteFile(result, []byte(`[]`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(fail); err != nil {
		t.Fatal(err)
	}
	waitRegistryPrivacy(t, r, bar, privacyInactive, "Microphone not in use")
}

func waitRegistryPrivacy(t *testing.T, r *Registry, bar *Bar, want privacyActivity, name string) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		r.mu.Lock()
		got := r.privacy.microphone
		r.mu.Unlock()
		bar.mu.Lock()
		gotName := bar.right[0].node.Name
		bar.mu.Unlock()
		if got == want && strings.Contains(strings.ToLower(gotName), strings.ToLower(name)) {
			return
		}
		time.Sleep(time.Millisecond)
	}
	r.mu.Lock()
	got := r.privacy
	r.mu.Unlock()
	bar.mu.Lock()
	gotName := bar.right[0].node.Name
	bar.mu.Unlock()
	t.Fatalf("privacy state = %+v, bar name = %q; want microphone=%v and %q", got, gotName, want, name)
}

func TestPrivacyItemIsAcceptedAndInDefaultBar(t *testing.T) {
	t.Parallel()
	if !strings.Contains(strings.Join(config.KnownItemIDs(), ","), "privacy") {
		t.Fatal("privacy is not a known bar item")
	}
	found := false
	for _, item := range config.Default().Bar.Right {
		found = found || item.ID == "privacy"
	}
	if !found {
		t.Fatal("privacy is absent from the default right lane")
	}
	if w := buildWidgets([]config.Item{{ID: "privacy"}}, 0, DefaultTheme().Metrics); len(w) != 1 || w[0].node == nil {
		t.Fatalf("privacy widget build = %+v", w)
	}
}
