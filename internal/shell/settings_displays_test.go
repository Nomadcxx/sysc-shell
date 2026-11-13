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

func TestDisplayResolutionRefreshAndVRRPolicies(t *testing.T) {
	r := &Registry{displaysDraft: map[string]map[string]string{"DP-1": {displaysFieldMode: "3440x1440@60030"}}}
	out := displaysTestOutput("DP-1")
	out.Modes = append(out.Modes, displays.Mode{Width: 3440, Height: 1440, RefreshMilliHz: 59940})
	resolution := displaysModeEntry(r, out)
	if got := resolution.Get(config.Config{}); got != "3440x1440" {
		t.Fatalf("resolution = %q", got)
	}
	refresh := displaysRefreshEntry(r, out)
	if len(refresh.Options) != 3 || refresh.Options[1] != "60030" || refresh.Options[2] != "59940" {
		t.Fatalf("rates = %v", refresh.Options)
	}
	for _, input := range []string{"false", "true", displaysVRROnDemand} {
		p, err := displaysVRRFrom(input)
		if err != nil || (p == nil) != (input == "false") {
			t.Fatalf("VRR %q = %+v, %v", input, p, err)
		}
	}
	mode, err := displaysModeFrom("3440x1440")
	if err != nil || mode.RefreshMilliHz != 0 {
		t.Fatalf("automatic refresh = %+v, %v", mode, err)
	}
}

func TestDisplayDraftDirtyComparesSavedPolicies(t *testing.T) {
	r := &Registry{displaysSnapshot: displaysTestSnapshot()}
	syncDisplaysDraftLocked(r, r.displaysSnapshot)
	if displaysDraftDirty(r) {
		t.Fatal("opening page created changes")
	}
	r.displaysDraft["DP-1"][displaysFieldScale] = "1.5"
	if !displaysDraftDirty(r) {
		t.Fatal("changed scale is clean")
	}
}

// fakeDisplays is the service the page talks to. It records every command, so
// a test can tell an edit that reached the compositor from one that did not.
type fakeDisplays struct {
	mu       sync.Mutex
	calls    []string
	updates  chan displays.Snapshot
	preview  map[string]displays.Policy
	refuse   error
	observed displays.Snapshot
	// hold, when set, keeps Preview running until it is closed.
	hold chan struct{}
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
	if f.hold != nil {
		<-f.hold
	}
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
	// Resolution and refresh rate are separate controls; the draft keeps the
	// exact millihertz pairing between them.
	for _, want := range []string{"DP-1", "3440 × 1440 (preferred)", "60.030 Hz", "variable refresh"} {
		if !strings.Contains(text, want) {
			t.Errorf("the page does not show %q:\n%s", want, text)
		}
	}
	entry := displaysEntryByPath(reg, "displays.DP-1."+displaysFieldMode)
	if entry == nil {
		t.Fatal("no resolution entry for DP-1")
	}
	if len(entry.Options) != 3 {
		t.Errorf("resolution options = %v, want automatic plus both advertised sizes", entry.Options)
	}
	if entry.OptionLabels[1] != "3440 × 1440 (preferred)" || entry.OptionLabels[2] != "1920 × 1080" {
		t.Errorf("resolution labels = %q", entry.OptionLabels)
	}
	if got := entry.Get(config.Config{}); got != "3440x1440" {
		t.Errorf("draft resolution = %q, want the size DP-1 is running", got)
	}
	rate := displaysEntryByPath(reg, "displays.DP-1."+displaysFieldRefresh)
	if rate == nil {
		t.Fatal("no refresh entry for DP-1")
	}
	if got := rate.Get(config.Config{}); got != "60030" {
		t.Errorf("draft refresh = %q, want the exact millihertz DP-1 is running", got)
	}
	if len(rate.Options) != 2 || rate.Options[1] != "60030" || rate.OptionLabels[1] != "60.030 Hz" {
		t.Errorf("refresh options = %v %q, want automatic plus the rate advertised for 3440 × 1440", rate.Options, rate.OptionLabels)
	}
	reg.mu.Lock()
	paired := reg.displaysDraft["DP-1"][displaysFieldMode]
	reg.mu.Unlock()
	if paired != "3440x1440@60030" {
		t.Errorf("draft mode = %q, want the exact pairing", paired)
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

// Closing Settings while Preview is still applying must not lose the Revert:
// it runs as soon as Preview returns.
func TestClosingDuringPreviewStillReverts(t *testing.T) {
	t.Parallel()
	reg, host, fake := displaysPage(t, 120, ui.Rect{W: 1536, H: 864})
	fake.hold = make(chan struct{})
	reg.mu.Lock()
	reg.displaysCommandLocked(host, "preview", func(ctx context.Context) error { return fake.Preview(ctx, nil) })
	reg.mu.Unlock()
	displaysWaitFor(t, fake, "preview")
	reg.ClosePanel(PanelSettings)
	if fake.seen("revert") {
		t.Fatal("revert ran while preview still held the service")
	}
	close(fake.hold)
	displaysWaitFor(t, fake, "revert")
}
