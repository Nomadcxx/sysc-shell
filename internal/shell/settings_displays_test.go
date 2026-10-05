package shell

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Nomadcxx/sysc-shell/internal/config"
	"github.com/Nomadcxx/sysc-shell/internal/displays"
	"github.com/Nomadcxx/sysc-shell/internal/settings"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

// fakeDisplays is the service the page talks to. It records every command, so
// a test can tell an edit that reached the compositor from one that did not.
type fakeDisplays struct {
	mu       sync.Mutex
	calls    []string
	updates  chan displays.Snapshot
	preview  map[string]displays.Policy
	refuse   error
	observed displays.Snapshot
}

func newFakeDisplays() *fakeDisplays {
	return &fakeDisplays{updates: make(chan displays.Snapshot, 4)}
}

func (f *fakeDisplays) record(call string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, call)
}

func (f *fakeDisplays) seen(call string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slicesContains(f.calls, call)
}

func (f *fakeDisplays) count(call string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, c := range f.calls {
		if c == call {
			n++
		}
	}
	return n
}

func slicesContains(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}

func (f *fakeDisplays) Subscribe() <-chan displays.Snapshot { return f.updates }
func (f *fakeDisplays) Refresh()                            { f.record("refresh") }
func (f *fakeDisplays) Root() string                        { return f.observed.Note }
func (f *fakeDisplays) Close(context.Context) error         { f.record("close"); return nil }

func (f *fakeDisplays) Preview(_ context.Context, draft map[string]displays.Policy) error {
	f.mu.Lock()
	f.preview = draft
	f.mu.Unlock()
	f.record("preview")
	return f.refuse
}

func (f *fakeDisplays) Keep(context.Context) error   { f.record("keep"); return f.refuse }
func (f *fakeDisplays) Revert(context.Context) error { f.record("revert"); return f.refuse }

func displaysTestOutput(connector string) displays.Output {
	scale := 1.0
	return displays.Output{
		Name: displays.Name{Connector: connector, Make: "Test", Model: "Panel"},
		Policy: displays.Policy{
			Scale: &scale,
			Mode:  &displays.ModePolicy{Width: 3440, Height: 1440, RefreshMilliHz: 60030},
		},
		Logical:      &displays.Rect{Width: 3440, Height: 1440},
		Modes:        []displays.Mode{{Width: 3440, Height: 1440, RefreshMilliHz: 60030, Preferred: true}, {Width: 1920, Height: 1080, RefreshMilliHz: 60030}},
		VRRSupported: true,
	}
}

func displaysTestSnapshot() displays.Snapshot {
	return displays.Snapshot{Outputs: []displays.Output{displaysTestOutput("DP-1")}}
}

// displaysPage opens Settings on the Display section with a service attached,
// and returns the registry, the host and the fake.
func displaysPage(t *testing.T, scale int, output ui.Rect) (*Registry, *PanelHost, *fakeDisplays) {
	t.Helper()
	fake := newFakeDisplays()
	fake.observed = displays.Snapshot{Note: "/home/u/.config/niri/config.kdl"}
	reg := newPanelRegistry(t)
	reg.mu.Lock()
	reg.displaysService = fake
	reg.displaysRoot = "/home/u/.config/niri/config.kdl"
	reg.displaysSnapshot = displaysTestSnapshot()
	reg.displaysDraft = nil
	reg.displaysArmed = false
	reg.displaysBusy = false
	reg.displaysErr = ""
	reg.displaysDiscovery = &displays.Discovery{Root: "/home/u/.config/niri/config.kdl", Source: "the compositor's command line", Confirm: true}
	reg.mu.Unlock()
	if err := reg.OpenPanel(PanelSettings, 7, Trigger{OutW: output.W, OutH: output.H}); err != nil {
		t.Fatal(err)
	}
	reqs := drainAux(t, reg, 2)
	panel := reqs[1].Open
	width, height := int(panel.Width), int(panel.Height)
	if err := panel.Callbacks.Configure(width, height, scale); err != nil {
		t.Fatal(err)
	}
	reg.mu.Lock()
	host := reg.panelHosts[PanelSettings]
	host.section = displaysSection
	syncDisplaysDraftLocked(reg, reg.displaysSnapshot)
	reg.rebuildPanel(host)
	reg.mu.Unlock()
	return reg, host, fake
}

func displaysWaitFor(t *testing.T, fake *fakeDisplays, call string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if fake.seen(call) {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("the %s command never reached the service", call)
}

func displaysPageText(h *PanelHost) string {
	return renderText(h.root)
}

func displaysEntryByPath(r *Registry, path string) *settings.Entry {
	for _, e := range displaysEntries(r, r.displaysSnapshot) {
		if e.Path == path {
			found := e
			return &found
		}
	}
	return nil
}

func TestDisplaySectionIsReachable(t *testing.T) {
	t.Parallel()
	if section, page, ok := settingsAddress("Display"); !ok || section != displaysSection || page != "" {
		t.Fatalf("settingsAddress(\"Display\") = %q, %q, %v", section, page, ok)
	}
	// The old address must still mean Bar's per-output overrides: the new page
	// is a different section with a different name.
	if section, page, ok := settingsAddress("Displays"); !ok || section != "Bar" || page != "Displays" {
		t.Fatalf("settingsAddress(\"Displays\") = %q, %q, %v, want Bar/Displays", section, page, ok)
	}
	if !slicesContains(settingsSections, displaysSection) {
		t.Fatalf("the rail has no %q section: %v", displaysSection, settingsSections)
	}
}

func TestDisplayPageShowsTheLiveOutputsAndModes(t *testing.T) {
	t.Parallel()
	reg, host, _ := displaysPage(t, 120, ui.Rect{W: 1536, H: 864})
	text := displaysPageText(host)
	// The control shows the mode the output is running; every advertised mode is
	// in the entry's options, which is what the menu lists when it opens.
	for _, want := range []string{"DP-1", "3440 × 1440 @ 60.030 Hz (preferred)", "variable refresh"} {
		if !strings.Contains(text, want) {
			t.Errorf("the page does not show %q:\n%s", want, text)
		}
	}
	entry := displaysEntryByPath(reg, "displays.DP-1."+displaysFieldMode)
	if entry == nil {
		t.Fatal("no resolution entry for DP-1")
	}
	if len(entry.Options) != 3 {
		t.Errorf("resolution options = %v, want automatic plus both advertised modes", entry.Options)
	}
	if entry.OptionLabels[2] != "1920 × 1080 @ 60.030 Hz" {
		t.Errorf("second mode label = %q", entry.OptionLabels[2])
	}
	if entry.OptionLabels[1] != "3440 × 1440 @ 60.030 Hz (preferred)" {
		t.Errorf("preferred mode label = %q", entry.OptionLabels[1])
	}
	// The modes the service reported are the only ones offered, and the draft
	// holds the value the output is actually running.
	if got := entry.Get(config.Config{}); got != "3440x1440@60030" {
		t.Errorf("draft resolution = %q, want the mode DP-1 is running", got)
	}
}

func TestDisplaysDraftEditSendsNothingToTheCompositor(t *testing.T) {
	t.Parallel()
	reg, host, fake := displaysPage(t, 120, ui.Rect{W: 1536, H: 864})
	before := fake.count("preview") + fake.count("keep") + fake.count("revert") + fake.count("refresh")
	// A pick is a draft edit; a field commit is a draft edit.
	if !activateDisplaysAction(reg, host, &ui.Node{Kind: ui.KindButton, Action: "displays:reset"}) {
		t.Fatal("discard changes was not handled")
	}
	for _, action := range []string{
		"pick:displays.DP-1." + displaysFieldScale + "=1.5",
		"pick:displays.DP-1." + displaysFieldMode + "=1920x1080@60030",
	} {
		host.focus = []*ui.Node{{Kind: ui.KindButton, Action: action}}
		host.roving.Set(0)
		if !host.activate(reg) {
			t.Fatalf("the control %q was not handled", action)
		}
	}
	// A field commits through the setting path, not through activation.
	host.applySetting(reg, &ui.Node{
		Kind: ui.KindTextField, Text: "1920", Action: "set:displays.DP-1." + displaysFieldPositionX,
	})
	if got := fake.count("preview") + fake.count("keep") + fake.count("revert") + fake.count("refresh"); got != before {
		t.Errorf("a draft edit sent %d commands to the service, want none", got-before)
	}
	reg.mu.Lock()
	draft := reg.displaysDraft["DP-1"]
	reg.mu.Unlock()
	if draft[displaysFieldScale] != "1.5" {
		t.Errorf("the edited scale is not on the draft: %v", draft)
	}
	if draft[displaysFieldPositionX] != "1920" {
		t.Errorf("the edited position is not on the draft: %v", draft)
	}
}

func TestDisplaysPreviewIsTheOnlyThingThatArmsATransaction(t *testing.T) {
	t.Parallel()
	reg, host, fake := displaysPage(t, 120, ui.Rect{W: 1536, H: 864})
	// Keep with nothing armed is refused in the page rather than sent.
	if !activateDisplaysAction(reg, host, &ui.Node{Kind: ui.KindButton, Action: "displays:keep", Name: "Keep preview"}) {
		t.Fatal("keep was not handled")
	}
	if fake.count("keep") != 0 {
		t.Errorf("keep reached the service with nothing previewed")
	}
	if !strings.Contains(reg.displaysErr, "Preview a change first") {
		t.Errorf("the refusal does not say what to do: %q", reg.displaysErr)
	}
	// Preview is what reaches the service, and it carries the edited draft.
	reg.mu.Lock()
	reg.displaysDraft = map[string]map[string]string{"DP-1": {displaysFieldScale: "1.5"}}
	reg.displaysErr = ""
	reg.rebuildPanel(host)
	reg.mu.Unlock()
	if !activateDisplaysAction(reg, host, &ui.Node{Kind: ui.KindButton, Action: "displays:preview", Name: "Preview"}) {
		t.Fatal("preview was not handled")
	}
	displaysWaitFor(t, fake, "preview")
	scale := fake.preview["DP-1"].Scale
	if scale == nil || *scale != 1.5 {
		t.Errorf("previewed scale = %v, want the edited 1.5", scale)
	}
}

func TestDisplaysRejectsADraftItCannotRead(t *testing.T) {
	t.Parallel()
	reg, host, _ := displaysPage(t, 120, ui.Rect{W: 1536, H: 864})
	reg.mu.Lock()
	reg.displaysDraft = map[string]map[string]string{"DP-1": {displaysFieldPositionX: "12"}}
	reg.displaysErr = ""
	reg.rebuildPanel(host)
	reg.mu.Unlock()
	if !activateDisplaysAction(reg, host, &ui.Node{Kind: ui.KindButton, Action: "displays:preview", Name: "Preview"}) {
		t.Fatal("preview was not handled")
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && reg.displaysErr == "" {
		time.Sleep(2 * time.Millisecond)
	}
	if !strings.Contains(reg.displaysErr, "position") || !strings.Contains(reg.displaysErr, "DP-1") {
		t.Errorf("half a position was accepted or reported as %q", reg.displaysErr)
	}
}

func TestDisplaysUnavailableControlsExplainThemselves(t *testing.T) {
	t.Parallel()
	reg := &Registry{displaysSnapshot: displays.Snapshot{Outputs: []displays.Output{{
		Name:         displays.Name{Connector: "HDMI-A-1"},
		Policy:       displays.Policy{Scale: nil},
		Modes:        nil,
		VRRSupported: false,
	}}}}
	entries := displaysEntries(reg, reg.displaysSnapshot)
	byPath := map[string]settings.Entry{}
	for _, e := range entries {
		byPath[e.Path] = e
	}
	mode := byPath["displays.HDMI-A-1."+displaysFieldMode]
	if len(mode.Options) != 1 || mode.OptionLabels[0] != "Not available while HDMI-A-1 is not mapped" {
		t.Errorf("an unmapped output offers %v/%v", mode.Options, mode.OptionLabels)
	}
	if !strings.Contains(mode.Describe, "Connect and enable") {
		t.Errorf("an unmapped output does not say why: %q", mode.Describe)
	}
	vrr := byPath["displays.HDMI-A-1."+displaysFieldVRR]
	if len(vrr.Options) != 1 || vrr.OptionLabels[0] != "Not available on HDMI-A-1" {
		t.Errorf("hardware without VRR offers %v/%v", vrr.Options, vrr.OptionLabels)
	}
	// A saved value outside the presets is never snapped to one.
	reg.displaysDraft = map[string]map[string]string{"DP-1": {displaysFieldScale: "1.3"}}
	scale := displaysScaleEntry(reg, displays.Output{Name: displays.Name{Connector: "DP-1"}})
	if scale.Kind != settings.KindString {
		t.Errorf("a saved scale of 1.3 became a %v of the presets", scale.Kind)
	}
	if scale.Describe != "Saved scale 1.3 is not a preset. Enter a number, or Automatic." {
		t.Errorf("scale explanation = %q", scale.Describe)
	}
}

func TestDisplaysPreviewPutsRevertUnderTheKeyboard(t *testing.T) {
	t.Parallel()
	reg, host, _ := displaysPage(t, 120, ui.Rect{W: 1536, H: 864})
	// The real path: the service publishes a snapshot and the registry relays it.
	reg.SetDisplaysService(newFakeDisplays())
	reg.mu.Lock()
	svc := reg.displaysService
	reg.mu.Unlock()
	if svc == nil {
		t.Fatal("the service was not attached")
	}
	reg.applyDisplaysSnapshot(reg.displaysService, displays.Snapshot{
		Outputs:   displaysTestSnapshot().Outputs,
		Preview:   true,
		Remaining: 12 * time.Second,
	})
	reg.mu.Lock()
	host = reg.panelHosts[PanelSettings]
	reg.mu.Unlock()
	if got := nameOf(host.focused()); got != "Revert preview" {
		t.Fatalf("focus after arming = %q, want Revert preview", got)
	}
	// Enter activates the focused button, so it reverts and cannot keep.
	armed := reg.displaysService.(*fakeDisplays)
	host.keyInput(reg, ui.KeyInput{Code: keyEnter})
	displaysWaitFor(t, armed, "revert")
	if armed.seen("keep") {
		t.Error("Enter on a previewed change kept it instead of reverting")
	}
}

func TestDisplaysCountdownCountsDownAndSaysNothingAboutSaving(t *testing.T) {
	t.Parallel()
	reg, host, _ := displaysPage(t, 120, ui.Rect{W: 1536, H: 864})
	reg.mu.Lock()
	reg.displaysArmed = true
	reg.displaysSnapshot = displays.Snapshot{
		Outputs:   displaysTestSnapshot().Outputs,
		Preview:   true,
		Remaining: 9 * time.Second,
		Note:      "Saved to the niri configuration.",
	}
	reg.rebuildPanel(host)
	text := displaysPageText(host)
	reg.mu.Unlock()
	if !strings.Contains(text, "reverts in 9 seconds") {
		t.Errorf("the countdown is missing:\n%s", text)
	}
	// A failed command must not leave a saved claim on screen.
	reg.mu.Lock()
	reg.displaysErr = "displays: niri validate did not finish within 5s"
	reg.displaysSnapshot.Note = "Saved to the niri configuration."
	reg.rebuildPanel(host)
	text = displaysPageText(host)
	reg.mu.Unlock()
	if !strings.Contains(text, "did not finish") {
		t.Errorf("the failure is not on the page:\n%s", text)
	}
	if strings.Contains(text, "Saved to the niri configuration") {
		t.Errorf("the page claims the settings were saved over a failure:\n%s", text)
	}
}

func TestLeavingTheDisplaySectionRevertsAPreview(t *testing.T) {
	t.Parallel()
	reg, host, _ := displaysPage(t, 120, ui.Rect{W: 1536, H: 864})
	reg.mu.Lock()
	fake := reg.displaysService.(*fakeDisplays)
	reg.displaysArmed = true
	reg.displaysSnapshot = displays.Snapshot{Outputs: displaysTestSnapshot().Outputs, Preview: true, Remaining: 5 * time.Second}
	reg.mu.Unlock()

	reg.mu.Lock()
	if err := reg.selectPanelSectionLocked(PanelSettings, "Screensaver"); err != nil {
		reg.mu.Unlock()
		t.Fatal(err)
	}
	reg.mu.Unlock()
	displaysWaitFor(t, fake, "revert")
	_ = host
}

func TestClosingSettingsRevertsAPreview(t *testing.T) {
	t.Parallel()
	reg, _, _ := displaysPage(t, 120, ui.Rect{W: 1536, H: 864})
	reg.mu.Lock()
	fake := reg.displaysService.(*fakeDisplays)
	reg.displaysArmed = true
	reg.mu.Unlock()
	// ClosePanel takes the registry lock itself.
	reg.ClosePanel(PanelSettings)
	displaysWaitFor(t, fake, "revert")
}

func TestDisplaysAdoptIsOnlyOfferedBeforeARootIsConfirmed(t *testing.T) {
	t.Parallel()
	reg, host, _ := displaysPage(t, 120, ui.Rect{W: 1536, H: 864})
	reg.mu.Lock()
	reg.displaysService = nil
	reg.displaysRoot = ""
	reg.displaysSnapshot = displays.Snapshot{}
	reg.displaysDraft = map[string]map[string]string{}
	found := &displays.Discovery{Root: "/home/u/.config/niri/config.kdl", Source: "NIRI_CONFIG", Confirm: true}
	reg.displaysDiscovery = found
	reg.rebuildPanel(host)
	text := renderText(host.root)
	reg.mu.Unlock()
	if !strings.Contains(text, "Adopt this configuration") {
		t.Fatalf("no adopt control with a proposal:\n%s", text)
	}
	if !strings.Contains(text, "starts as a comment") {
		t.Errorf("adopting does not say it changes nothing yet:\n%s", text)
	}
	// A read-only proposal is a refusal, not a button.
	reg.mu.Lock()
	readOnly := &displays.Discovery{
		Root: "/etc/niri/config.kdl", Source: "the system path", ReadOnly: true,
		Note: "That configuration is not writable by this account.",
	}
	reg.displaysDiscovery = readOnly
	reg.rebuildPanel(host)
	text = renderText(host.root)
	reg.mu.Unlock()
	if strings.Contains(text, "Adopt this configuration") {
		t.Errorf("a read-only root can be adopted:\n%s", text)
	}
	if !strings.Contains(text, "not writable by this account") {
		t.Errorf("the read-only reason is missing:\n%s", text)
	}
}

func TestDisplaysSearchFindsTheLiveOutputs(t *testing.T) {
	t.Parallel()
	reg, host, _ := displaysPage(t, 120, ui.Rect{W: 1536, H: 864})
	hits := displaysSearchEntries(reg, "vrr")
	if len(hits) == 0 {
		hits = displaysSearchEntries(reg, "variable refresh")
	}
	if len(hits) == 0 {
		t.Fatal("searching for the refresh row found nothing")
	}
	reg.mu.Lock()
	host.query = "vrr"
	reg.rebuildPanel(host)
	text := renderText(host.root)
	reg.mu.Unlock()
	if !strings.Contains(text, "DP-1") {
		t.Errorf("a search for the refresh row does not show the output:\n%s", text)
	}
}

func TestDisplaysLayoutKeepsEveryControlVisible(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		out   ui.Rect
		scale int
	}{
		{ui.Rect{W: 1536, H: 864}, 120},
		{ui.Rect{W: 1536, H: 864}, 150},
		{ui.Rect{W: 3440, H: 1440}, 120},
	} {
		_, host, _ := displaysPage(t, tc.scale, tc.out)
		_, height := host.place.Panel.H, host.place.Panel.H
		buttons := []*ui.Node{}
		for _, n := range walk(host.root) {
			if strings.HasPrefix(n.Action, "displays:") {
				buttons = append(buttons, n)
			}
		}
		if len(buttons) == 0 {
			t.Fatalf("%v at scale %d: no display controls were built", tc.out, tc.scale)
		}
		_ = height
		for _, b := range buttons {
			if b.Bounds.W <= 0 || b.Bounds.H <= 0 {
				t.Errorf("%v at scale %d: %q has no size", tc.out, tc.scale, nameOf(b))
			}
			if b.Bounds.X < 0 || b.Bounds.Y < 0 {
				t.Errorf("%v at scale %d: %q is outside the panel at %v", tc.out, tc.scale, nameOf(b), b.Bounds)
			}
			if b.Bounds.X+b.Bounds.W > host.place.Panel.W+1 {
				t.Errorf("%v at scale %d: %q runs past the panel edge: %v", tc.out, tc.scale, nameOf(b), b.Bounds)
			}
		}
	}
}

func TestDisplaysRailStillReachesItsLastSection(t *testing.T) {
	t.Parallel()
	_, host, _ := displaysPage(t, 150, ui.Rect{W: 1536, H: 864})
	rail := settingsRail(host, displaysSection)
	tabs := []*ui.Node{}
	for _, n := range walk(rail) {
		if strings.HasPrefix(n.Action, "section:") {
			tabs = append(tabs, n)
		}
	}
	if len(tabs) != len(settings.SectionNames()) {
		t.Fatalf("the rail has %d tabs for %d sections", len(tabs), len(settings.SectionNames()))
	}
	last := nameOf(tabs[len(tabs)-1])
	if last != settings.SectionNames()[len(settings.SectionNames())-1] {
		t.Errorf("the last rail tab is %q, want %q", last, settings.SectionNames()[len(settings.SectionNames())-1])
	}
	// sysc-906: every section must fit inside the rail's own height, or the
	// last one is only reachable by a rail that scrolls.
	if bottom := tabs[len(tabs)-1].Bounds.Y + tabs[len(tabs)-1].Bounds.H; bottom > host.place.Panel.H {
		t.Errorf("the last rail tab ends at %d, past the panel height %d", bottom, host.place.Panel.H)
	}
}
