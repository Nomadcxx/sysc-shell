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
	"github.com/Nomadcxx/sysc-shell/internal/ui"
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
			want: privacyState{microphone: privacyInactive, camera: privacyActive, screenShare: privacyInactive},
		},
		{
			name: "cast node is not a camera",
			pipe: services.PrivacySnapshot{Known: true,
				VideoPaths: []services.PrivacyVideoPath{{SourceNodeID: 40, Camera: true}}},
			niri: niri.Snapshot{Casts: []niri.Cast{{StreamID: 1, Active: true, Target: niri.CastTarget{Kind: "output", Name: "DP-1"}, TargetSet: true, TargetKnown: true, PWNodeID: 40, HasPWNodeID: true}}},
			want: privacyState{microphone: privacyInactive, camera: privacyInactive, screenShare: privacyActive},
		},
		{
			name: "active unknown cast target stays unknown",
			pipe: services.PrivacySnapshot{Known: true,
				VideoPaths: []services.PrivacyVideoPath{{SourceNodeID: 41}}},
			niri: niri.Snapshot{Casts: []niri.Cast{{StreamID: 2, Active: true, PWNodeID: 41, HasPWNodeID: true}}},
			want: privacyState{microphone: privacyInactive, camera: privacyUnknown, screenShare: privacyUnknown},
		},
		{
			name: "inactive cast does not suppress camera evidence",
			pipe: services.PrivacySnapshot{Known: true,
				VideoPaths: []services.PrivacyVideoPath{{SourceNodeID: 42, Camera: true}}},
			niri: niri.Snapshot{CastsKnown: true, Casts: []niri.Cast{{StreamID: 5, PWNodeID: 42, HasPWNodeID: true}}},
			want: privacyState{microphone: privacyInactive, camera: privacyActive, screenShare: privacyInactive},
		},
		{
			name: "known inactive target does not suppress camera evidence",
			pipe: services.PrivacySnapshot{Known: true,
				VideoPaths: []services.PrivacyVideoPath{{SourceNodeID: 43, Camera: true}}},
			niri: niri.Snapshot{CastsKnown: true, Casts: []niri.Cast{{StreamID: 6, Active: true,
				Target: niri.CastTarget{Kind: "nothing"}, TargetKnown: true,
				PWNodeID: 43, HasPWNodeID: true}}},
			want: privacyState{microphone: privacyInactive, camera: privacyActive, screenShare: privacyInactive},
		},
		{
			name: "valid active cast proves share before full baseline",
			pipe: services.PrivacySnapshot{Known: true},
			niri: niri.Snapshot{Casts: []niri.Cast{{StreamID: 3, Active: true,
				Target: niri.CastTarget{Kind: "window", WindowID: 70}, TargetSet: true, TargetKnown: true}}},
			want: privacyState{microphone: privacyInactive, camera: privacyInactive, screenShare: privacyActive},
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

func TestPrivacyWidgetIsPersistentAccessibleAndShowsUnavailable(t *testing.T) {
	t.Parallel()
	metrics := DefaultTheme().Metrics
	w := buildPrivacyWidget(metrics.CapsulePadding, metrics)
	if w.node == nil || w.node.Action != panelPrivacyAction || w.node.Role != "button" {
		t.Fatalf("privacy node = %+v, want an actionable button", w.node)
	}
	if w.node.Absent {
		t.Fatal("privacy widget is absent before its first update")
	}
	state := privacyState{microphone: privacyUnknown, camera: privacyInactive, screenShare: privacyActive}
	if !w.refresh(barView{Privacy: state}) {
		t.Fatal("refresh did not report the initial privacy state")
	}
	name := strings.ToLower(w.node.Name)
	if w.node.Absent || !strings.Contains(name, "microphone unavailable") || !strings.Contains(name, "screen sharing in use") {
		t.Fatalf("widget state name = %q, absent=%v", w.node.Name, w.node.Absent)
	}
	if len(w.node.Children) != 1 || len(w.node.Children[0].Children) != 3 {
		t.Fatalf("privacy icon row = %+v, want three source glyphs", w.node.Children)
	}
	icons := w.node.Children[0].Children
	if icons[0].Icon != "gpp_bad" || icons[1].Icon != "camera-off" || icons[2].Icon != "desktop_windows" {
		t.Fatalf("privacy icons = %+v, want unknown, inactive and active source glyphs", icons)
	}
	tooltip := w.node.Tooltip
	for _, want := range []string{"Microphone: Unavailable", "Camera: Not in use", "Screen sharing: In use"} {
		if !strings.Contains(tooltip, want) {
			t.Fatalf("tooltip %q does not include %q", tooltip, want)
		}
	}
	inactive := privacyState{microphone: privacyInactive, camera: privacyInactive, screenShare: privacyInactive}
	if !w.refresh(barView{Privacy: inactive}) || w.node.Absent {
		t.Fatal("known inactive state hid the persistent privacy widget")
	}
	if !strings.Contains(w.node.Tooltip, "Microphone: Not in use") || !strings.Contains(w.node.Tooltip, "Screen sharing: Not in use") {
		t.Fatalf("inactive tooltip = %q", w.node.Tooltip)
	}
}

func TestPrivacyPageShowsOnlyPerSourceStatus(t *testing.T) {
	t.Parallel()
	r := newPanelRegistry(t)
	r.privacyPipe = services.PrivacySnapshot{Known: true, MicrophoneActive: true,
		VideoPaths: []services.PrivacyVideoPath{{SourceNodeID: 88, Camera: true}}}
	r.niriSnapshot = niri.Snapshot{CastsKnown: true, Casts: []niri.Cast{{StreamID: 1, Active: true,
		Target: niri.CastTarget{Kind: "window", Name: "private title"}, TargetSet: true, TargetKnown: true}}}
	r.privacy = privacyStateFrom(r.privacyPipe, r.niriSnapshot)
	h := &PanelHost{id: PanelControlCenter, section: "privacy", theme: DefaultTheme()}
	text := strings.Join(collectPrivacyText(ccPrivacy(r, h)), "\n")
	for _, want := range []string{"Microphone", "Camera", "Screen sharing", "In use"} {
		if !strings.Contains(text, want) {
			t.Errorf("Privacy page text %q is missing %q", text, want)
		}
	}
	if strings.Contains(text, "private title") {
		t.Fatalf("Privacy page exposed a captured window title: %q", text)
	}
}

func TestPrivacyRegistryRefreshesBarAndClearsStaleActivityOnFailure(t *testing.T) {
	r := newPanelRegistry(t)
	metrics := DefaultTheme().Metrics
	w := buildPrivacyWidget(metrics.CapsulePadding, metrics)
	bar := &Bar{conn: "DP-1", right: []textWidget{w}}
	r.mu.Lock()
	r.bars = map[uint32]*Bar{7: bar}
	bar.apply(r.viewLocked("DP-1"))
	r.mu.Unlock()
	if err := r.OpenPanel(PanelControlCenter, 7, Trigger{BarEdge: "top", BarZone: 40}); err != nil {
		t.Fatal(err)
	}
	r.mu.Lock()
	if err := r.selectPanelSectionLocked(PanelControlCenter, "privacy"); err != nil {
		r.mu.Unlock()
		t.Fatal(err)
	}
	r.mu.Unlock()
	_ = drainAux(t, r, 2)

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
		pageText := ""
		if h := r.panelHosts[PanelControlCenter]; h != nil && h.section == "privacy" {
			pageText = strings.Join(strings.Fields(renderText(h.root)), " ")
		}
		r.mu.Unlock()
		bar.mu.Lock()
		gotName := bar.right[0].node.Name
		bar.mu.Unlock()
		if got == want && strings.Contains(strings.ToLower(gotName), strings.ToLower(name)) &&
			strings.Contains(strings.ToLower(pageText), strings.ToLower(name)) {
			return
		}
		time.Sleep(time.Millisecond)
	}
	r.mu.Lock()
	got := r.privacy
	pageText := ""
	if h := r.panelHosts[PanelControlCenter]; h != nil && h.section == "privacy" {
		pageText = strings.Join(strings.Fields(renderText(h.root)), " ")
	}
	r.mu.Unlock()
	bar.mu.Lock()
	gotName := bar.right[0].node.Name
	bar.mu.Unlock()
	t.Fatalf("privacy state = %+v, bar name = %q, page = %q; want microphone=%v and %q",
		got, gotName, pageText, want, name)
}

func TestPrivacyIndicatorOpensControlCentreAtPrivacy(t *testing.T) {
	r := newPanelRegistry(t)
	bar := &Bar{conn: "DP-1"}
	r.mu.Lock()
	r.bars = map[uint32]*Bar{7: bar}
	r.bindBarPanelActionsLocked(7, bar)
	r.mu.Unlock()
	if !bar.onAction(panelPrivacyAction, buttonLeft) {
		t.Fatal("left-click did not open Control Centre at Privacy")
	}
	_ = drainAux(t, r, 2)
	r.mu.Lock()
	h := r.panelHosts[PanelControlCenter]
	section := ""
	if h != nil {
		section = h.section
	}
	r.mu.Unlock()
	if h == nil || section != "privacy" {
		t.Fatalf("Control Centre host = %v, section=%q; want the Privacy page", h != nil, section)
	}
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

func collectPrivacyText(n *ui.Node) []string {
	if n == nil {
		return nil
	}
	var out []string
	if strings.TrimSpace(n.Text) != "" {
		out = append(out, strings.TrimSpace(n.Text))
	}
	for _, child := range n.Children {
		out = append(out, collectPrivacyText(child)...)
	}
	return out
}
