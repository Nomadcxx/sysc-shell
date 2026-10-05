package shell

import (
	"errors"
	"io"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	locksession "github.com/Nomadcxx/sysc-shell/internal/lock"
	"github.com/Nomadcxx/sysc-shell/internal/settings"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
	"github.com/Nomadcxx/sysc-shell/internal/walls"
)

type fakeWallsService struct {
	mu sync.Mutex

	snapshot walls.Snapshot
	updates  chan walls.Snapshot

	refreshes  int
	patches    [][]walls.Setting
	enables    []bool
	runtime    []bool
	previews   int
	stops      int
	closes     int
	accept     bool
	idleResult <-chan error
}

func newFakeWallsService(snapshot walls.Snapshot) *fakeWallsService {
	return &fakeWallsService{snapshot: snapshot, updates: make(chan walls.Snapshot, 8), accept: true}
}

func (f *fakeWallsService) Snapshot() walls.Snapshot {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.snapshot
}

func (f *fakeWallsService) Updates() <-chan walls.Snapshot { return f.updates }

func (f *fakeWallsService) Refresh() {
	f.mu.Lock()
	f.refreshes++
	f.mu.Unlock()
}

func (f *fakeWallsService) Apply(patch []walls.Setting) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.patches = append(f.patches, append([]walls.Setting(nil), patch...))
	return f.accept
}

func (f *fakeWallsService) SetEnabled(value bool) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.enables = append(f.enables, value)
	return f.accept
}

func (f *fakeWallsService) ConfigureIdle(enabled bool, timeout string) error {
	f.mu.Lock()
	f.enables = append(f.enables, enabled)
	if enabled {
		f.patches = append(f.patches, []walls.Setting{{Key: "timeout", Value: timeout}})
	}
	accept, result := f.accept, f.idleResult
	f.mu.Unlock()
	if !accept {
		return errors.New("unit command rejected")
	}
	if result != nil {
		if err := <-result; err != nil {
			return err
		}
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.snapshot.UnitFileState = "disabled"
	if enabled {
		f.snapshot.UnitFileState = "enabled"
		f.snapshot.Timeout = timeout
	}
	return nil
}

func (f *fakeWallsService) SetRuntimeRunning(value bool) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.runtime = append(f.runtime, value)
	return f.accept
}

func (f *fakeWallsService) Preview() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.previews++
	return f.accept
}

func (f *fakeWallsService) StopPreview() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.stops++
	return f.accept
}

func (f *fakeWallsService) Close() error {
	f.mu.Lock()
	f.closes++
	f.mu.Unlock()
	return nil
}

func (f *fakeWallsService) Publish(snapshot walls.Snapshot) {
	f.mu.Lock()
	f.snapshot = snapshot
	f.mu.Unlock()
	select {
	case f.updates <- snapshot:
	default:
		select {
		case <-f.updates:
		default:
		}
		f.updates <- snapshot
	}
}

func (f *fakeWallsService) counts() (refreshes, previews, stops, closes int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.refreshes, f.previews, f.stops, f.closes
}

func readyWallsSnapshot() walls.Snapshot {
	return walls.Snapshot{
		UnitKnown: true, LoadState: "loaded", UnitFileState: "disabled",
		ActiveState: "inactive", SubState: "dead", ServiceAvailable: true,
		ConfigKnown: true, ConfigExists: true, ConfigReadable: true,
		ConfigSourceMatches: true, CanApply: true, CanPreview: true,
		Effect: "matrix-art", Theme: "rama", Timeout: "5m", DateTime: "false",
		DateTimePosition: "bottom",
	}
}

func newOpenWallsSettings(t *testing.T, snapshot walls.Snapshot) (*Registry, *PanelHost, *fakeWallsService) {
	t.Helper()
	r := newPanelRegistry(t)
	withTestBar(t, r, 7, r.cfg)
	service := newFakeWallsService(snapshot)
	r.attachWallsService(service)
	if err := r.OpenPanel(PanelSettings, 7, Trigger{OutW: 1536, OutH: 864}); err != nil {
		t.Fatal(err)
	}
	r.mu.Lock()
	if err := r.selectPanelSectionLocked(PanelSettings, "Screensaver"); err != nil {
		r.mu.Unlock()
		t.Fatal(err)
	}
	h := r.panelHosts[PanelSettings]
	r.mu.Unlock()
	return r, h, service
}

func TestScreensaverSettingsOpeningRefreshesWithoutStartingPreview(t *testing.T) {
	r, _, service := newOpenWallsSettings(t, readyWallsSnapshot())
	refreshes, previews, _, _ := service.counts()
	if refreshes == 0 {
		t.Fatal("opening Screensaver did not queue a state refresh")
	}
	if previews != 0 {
		t.Fatalf("opening Settings started %d previews", previews)
	}
	r.mu.Lock()
	root := r.panelHosts[PanelSettings].root
	r.mu.Unlock()
	for _, action := range []string{"walls:preview", "walls:reset", "walls:apply"} {
		if findNode(root, func(n *ui.Node) bool { return n.Action == action }) == nil {
			t.Errorf("Settings tree has no %q action", action)
		}
	}
}

func TestScreensaverDraftPreservesDirtyValueAcrossRefresh(t *testing.T) {
	r, h, service := newOpenWallsSettings(t, readyWallsSnapshot())
	r.mu.Lock()
	setWallsDraft(r, h, "effect", "fire")
	r.mu.Unlock()
	next := readyWallsSnapshot()
	next.Effect, next.Theme = "rain", "nord"
	service.Publish(next)
	waitFor(t, func() bool {
		r.mu.Lock()
		defer r.mu.Unlock()
		return r.wallsSnapshot.Theme == "nord"
	})
	r.mu.Lock()
	defer r.mu.Unlock()
	if h.wallsDraft["effect"] != "fire" || !h.wallsDirty["effect"] {
		t.Fatalf("dirty effect was replaced by refresh: draft=%v dirty=%v", h.wallsDraft, h.wallsDirty)
	}
	if h.wallsDraft["theme"] != "nord" || h.wallsDirty["theme"] {
		t.Fatalf("clean theme did not follow refresh: draft=%v dirty=%v", h.wallsDraft, h.wallsDirty)
	}
}

func TestScreensaverResetIsDraftOnlyAndEmptyApplySendsNothing(t *testing.T) {
	r, h, service := newOpenWallsSettings(t, readyWallsSnapshot())
	r.mu.Lock()
	if applyWallsDraft(r, h) {
		r.mu.Unlock()
		t.Fatal("empty Apply was queued")
	}
	setWallsDraft(r, h, "effect", "fire")
	if !resetWallsDraft(r, h) {
		r.mu.Unlock()
		t.Fatal("Reset was rejected")
	}
	if h.wallsDraft["effect"] != "matrix-art" || wallsDraftIsDirty(h) {
		r.mu.Unlock()
		t.Fatalf("Reset draft = %v, dirty=%v", h.wallsDraft, h.wallsDirty)
	}
	r.mu.Unlock()
	service.mu.Lock()
	defer service.mu.Unlock()
	if len(service.patches) != 0 {
		t.Fatalf("Reset or empty Apply persisted %v", service.patches)
	}
}

func TestScreensaverApplySendsOneDirtyBatchAndPreviewNamesSavedValues(t *testing.T) {
	r, h, service := newOpenWallsSettings(t, readyWallsSnapshot())
	r.mu.Lock()
	setWallsDraft(r, h, "effect", "fire")
	setWallsDraft(r, h, "theme", "nord")
	if !applyWallsDraft(r, h) {
		r.mu.Unlock()
		t.Fatal("dirty Apply was not queued")
	}
	actions := screensaverActions(h, r.wallsSnapshot, false)
	preview := findNode(actions, func(n *ui.Node) bool { return n.Action == "walls:preview" })
	r.mu.Unlock()
	if preview == nil || preview.Name != "Preview saved settings" {
		t.Fatalf("dirty draft Preview = %+v", preview)
	}
	service.mu.Lock()
	defer service.mu.Unlock()
	want := []walls.Setting{{Key: "effect", Value: "fire"}, {Key: "theme", Value: "nord"}}
	if len(service.patches) != 1 || !slices.Equal(service.patches[0], want) {
		t.Fatalf("Apply batches = %v, want one patch %v", service.patches, want)
	}
}

func TestScreensaverSettingsShowsRawInvalidValuesAndKeepsUnrelatedApplyAvailable(t *testing.T) {
	snapshot := readyWallsSnapshot()
	snapshot.Effect = "legacy-effect"
	snapshot.Theme = "legacy-theme"
	snapshot.DateTime = "sometimes"
	snapshot.DateTimePosition = "left-edge"
	r, h, service := newOpenWallsSettings(t, snapshot)
	r.mu.Lock()
	for key, raw := range map[string]string{
		"effect": "legacy-effect", "theme": "legacy-theme",
		"datetime": "sometimes", "datetime-position": "left-edge",
	} {
		node := findNode(h.root, func(n *ui.Node) bool { return n.Action == "set:walls."+key })
		if node == nil || node.Kind != ui.KindTextField || node.Text != raw {
			r.mu.Unlock()
			t.Fatalf("invalid saved %s was hidden by a default: node=%+v", key, node)
		}
	}
	text := renderText(h.root)
	for _, raw := range []string{"legacy-effect", "legacy-theme", "sometimes", "left-edge", "Enter one of"} {
		if !strings.Contains(text, raw) {
			r.mu.Unlock()
			t.Fatalf("Settings did not show %q with a correction path: %s", raw, text)
		}
	}
	if !setWallsDraft(r, h, "timeout", "10m") {
		r.mu.Unlock()
		t.Fatal("unrelated timeout draft was rejected")
	}
	apply := findNode(h.root, func(n *ui.Node) bool { return n.Action == "walls:apply" })
	if apply == nil || apply.State.Has(ui.StateDisabled) {
		r.mu.Unlock()
		t.Fatalf("invalid untouched values disabled unrelated Apply: %+v", apply)
	}
	if !applyWallsDraft(r, h) {
		r.mu.Unlock()
		t.Fatal("unrelated Apply did not queue")
	}
	r.mu.Unlock()
	service.mu.Lock()
	defer service.mu.Unlock()
	want := []walls.Setting{{Key: "timeout", Value: "10m"}}
	if len(service.patches) != 1 || !slices.Equal(service.patches[0], want) {
		t.Fatalf("unrelated Apply batch = %v, want %v", service.patches, want)
	}
}

func TestScreensaverUnitEnablementWaitsUntilPreviewStops(t *testing.T) {
	snapshot := readyWallsSnapshot()
	snapshot.UnitFileState, snapshot.ActiveState, snapshot.SubState = "disabled", "inactive", "dead"
	snapshot.Previewing, snapshot.PreviewReady = true, true
	r, h, service := newOpenWallsSettings(t, snapshot)
	r.mu.Lock()
	toggle := findNode(h.root, func(n *ui.Node) bool { return n.Action == "walls:enable" })
	if toggle == nil || !toggle.State.Has(ui.StateDisabled) || !toggle.AriaDisabled {
		r.mu.Unlock()
		t.Fatalf("service enablement remains interactive during Preview: %+v", toggle)
	}
	if !strings.Contains(renderText(h.root), "Stop Preview before changing service enablement") {
		r.mu.Unlock()
		t.Fatalf("disabled enablement lacks a correction path: %s", renderText(h.root))
	}
	activateWallsAction(r, h, &ui.Node{Action: "walls:enable", Kind: ui.KindToggle, Value: 1})
	r.mu.Unlock()
	service.mu.Lock()
	defer service.mu.Unlock()
	if len(service.enables) != 0 {
		t.Fatalf("attempt to enable during Preview queued service changes: %v", service.enables)
	}
}

func TestScreensaverApplyWaitsUntilPreviewStops(t *testing.T) {
	snapshot := readyWallsSnapshot()
	snapshot.Previewing, snapshot.PreviewReady = true, true
	r, h, service := newOpenWallsSettings(t, snapshot)
	r.mu.Lock()
	setWallsDraft(r, h, "effect", "fire")
	apply := findNode(h.root, func(n *ui.Node) bool { return n.Action == "walls:apply" })
	if apply == nil || !apply.State.Has(ui.StateDisabled) || !apply.AriaDisabled {
		r.mu.Unlock()
		t.Fatalf("Apply remains interactive during Preview: %+v", apply)
	}
	if !strings.Contains(renderText(h.root), "Stop Preview before applying settings") {
		r.mu.Unlock()
		t.Fatalf("disabled Apply lacks a correction path: %s", renderText(h.root))
	}
	if applyWallsDraft(r, h) {
		r.mu.Unlock()
		t.Fatal("Apply queued while Preview owns a child")
	}
	r.mu.Unlock()
	service.mu.Lock()
	defer service.mu.Unlock()
	if len(service.patches) != 0 {
		t.Fatalf("Apply during Preview queued config writes: %v", service.patches)
	}
}

func TestScreensaverLockMakesSettingsAndPreviewUnavailable(t *testing.T) {
	r, h, service := newOpenWallsSettings(t, readyWallsSnapshot())
	r.mu.Lock()
	setWallsDraft(r, h, "effect", "fire")
	r.lockerAcquired = true
	r.rebuildPanel(h)
	root := h.root
	preview := findNode(root, func(n *ui.Node) bool { return n.Action == "walls:preview" })
	if preview == nil || !preview.State.Has(ui.StateDisabled) || !preview.AriaDisabled {
		r.mu.Unlock()
		t.Fatalf("Settings Preview is interactive while locked: %+v", preview)
	}
	reset := findNode(root, func(n *ui.Node) bool { return n.Action == "walls:reset" })
	if reset == nil || !reset.State.Has(ui.StateDisabled) || !reset.AriaDisabled {
		r.mu.Unlock()
		t.Fatalf("Settings Reset is interactive while locked: %+v", reset)
	}
	for _, action := range []string{
		"set:walls.effect", "set:walls.theme", "set:walls.file", "set:walls.datetime",
		"pick:walls.datetime-position=top", "pick:walls.datetime-position=center",
		"pick:walls.datetime-position=bottom",
	} {
		node := findNode(root, func(n *ui.Node) bool { return n.Action == action })
		if node == nil || !node.State.Has(ui.StateDisabled) || !node.AriaDisabled {
			r.mu.Unlock()
			t.Fatalf("Screensaver draft control %q remains interactive while locked: %+v", action, node)
		}
	}
	activateWallsAction(r, h, preview)
	ccPreview := &ui.Node{Action: "cc:walls-preview"}
	hcc := &PanelHost{id: PanelControlCenter, section: "home"}
	r.wallsSnapshot.CanPreview = true
	hcc.activateControlCentre(r, ccPreview)
	r.mu.Unlock()
	_, previews, _, _ := service.counts()
	if previews != 0 {
		t.Fatalf("locked session started %d previews", previews)
	}
}

func TestManagedSnapshotRefreshesScreensaverPane(t *testing.T) {
	r, h, _ := newOpenWallsSettings(t, readyWallsSnapshot())
	r.applyManagedSnapshot(locksession.State{Known: true, Snapshot: locksession.Snapshot{Phase: "sealed"}})
	r.mu.Lock()
	if !r.lockerAcquired || !strings.Contains(renderText(h.root), "unavailable while locked") {
		t.Fatal("sealed snapshot did not disable screensaver")
	}
	r.mu.Unlock()
	r.applyManagedSnapshot(locksession.State{Known: true, Snapshot: locksession.Snapshot{Phase: "idle", ConfirmedUnlock: 5}})
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.lockerAcquired || strings.Contains(renderText(h.root), "unavailable while locked") {
		t.Fatal("confirmed unlock did not refresh screensaver")
	}
}

func TestMissingUnitDoesNotDisableStandalonePreview(t *testing.T) {
	snapshot := readyWallsSnapshot()
	snapshot.LoadState, snapshot.UnitFileState = "not-found", "not-found"
	snapshot.ServiceAvailable = false
	snapshot.CanApply = false
	snapshot.CanPreview = true
	row := ccWallsRow(240, snapshot, false, false)
	preview := findNode(row, func(n *ui.Node) bool { return n.Action == "cc:walls-preview" })
	settings := findNode(row, func(n *ui.Node) bool { return n.Action == "settings-section:Screensaver" })
	if preview == nil || preview.State.Has(ui.StateDisabled) || settings == nil {
		t.Fatalf("missing unit removed standalone Preview or Settings navigation: %s", renderText(row))
	}
	if !strings.Contains(renderText(row), "Screensaver service is not installed") {
		t.Fatalf("missing unit state is hidden: %s", renderText(row))
	}
}

func TestWallsRelayUpdatesBothPanelsAndClosingOneKeepsService(t *testing.T) {
	r, settingsHost, service := newOpenWallsSettings(t, readyWallsSnapshot())
	if err := r.OpenPanel(PanelControlCenter, 7, Trigger{OutW: 1536, OutH: 864}); err != nil {
		t.Fatal(err)
	}
	next := readyWallsSnapshot()
	next.UnitFileState, next.ActiveState, next.SubState = "enabled", "active", "running"
	next.Effect, next.Theme = "fire", "nord"
	service.Publish(next)
	waitFor(t, func() bool {
		r.mu.Lock()
		defer r.mu.Unlock()
		return r.wallsSnapshot.Theme == "nord"
	})
	r.mu.Lock()
	if !strings.Contains(renderText(settingsHost.root), "nord") || !strings.Contains(renderText(r.panelHosts[PanelControlCenter].root), "Enabled at login") || !strings.Contains(renderText(r.panelHosts[PanelControlCenter].root), "Running") {
		r.mu.Unlock()
		t.Fatalf("panels do not share the new snapshot: settings=%q control=%q", renderText(settingsHost.root), renderText(r.panelHosts[PanelControlCenter].root))
	}
	r.closePanelLocked(PanelSettings)
	if _, _, _, closes := service.counts(); closes != 0 {
		r.mu.Unlock()
		t.Fatal("closing Settings stopped the shared service")
	}
	failed := next
	failed.ActiveState, failed.SubState = "failed", "failed"
	r.mu.Unlock()
	service.Publish(failed)
	waitFor(t, func() bool {
		r.mu.Lock()
		defer r.mu.Unlock()
		return r.wallsSnapshot.ActiveState == "failed"
	})
	r.mu.Lock()
	defer r.mu.Unlock()
	if !strings.Contains(renderText(r.panelHosts[PanelControlCenter].root), "Failed") {
		t.Fatalf("remaining Control Centre did not update: %q", renderText(r.panelHosts[PanelControlCenter].root))
	}
}

func TestCaffeineCaptionDoesNotSubmitWallsCommands(t *testing.T) {
	snapshot := readyWallsSnapshot()
	snapshot.UnitFileState, snapshot.ActiveState, snapshot.SubState = "enabled", "active", "running"
	r := newPanelRegistry(t)
	service := newFakeWallsService(snapshot)
	r.attachWallsService(service)
	r.mu.Lock()
	h := &PanelHost{id: PanelControlCenter, section: "home", theme: DefaultTheme()}
	offRoot := ccHome(r, h)
	off := renderText(offRoot)
	r.inhibitWanted = true
	onRoot := ccHome(r, h)
	on := renderText(onRoot)
	r.mu.Unlock()
	if strings.Contains(off, "Caffeine does not pause the screensaver") || !strings.Contains(on, "Caffeine does not pause the screensaver") {
		t.Fatalf("Caffeine caption off/on = %q / %q", off, on)
	}
	if !strings.Contains(on, "Running") {
		t.Fatalf("Caffeine caption hid the visible service state: %q", on)
	}
	if strings.Contains(on, "Showing") || strings.Contains(on, "Visible") {
		t.Fatalf("Home claims unknown display coverage: %q", on)
	}
	var actions func(*ui.Node) []string
	actions = func(node *ui.Node) []string {
		var out []string
		if node == nil {
			return out
		}
		if strings.HasPrefix(node.Action, "cc:walls-") {
			out = append(out, node.Action)
		}
		for _, child := range node.Children {
			out = append(out, actions(child)...)
		}
		return out
	}
	if !slices.Equal(actions(offRoot), actions(onRoot)) {
		t.Fatalf("Caffeine changed screensaver actions: off=%v on=%v", actions(offRoot), actions(onRoot))
	}
	service.mu.Lock()
	defer service.mu.Unlock()
	if service.refreshes != 0 || len(service.patches) != 0 || len(service.enables) != 0 || len(service.runtime) != 0 || service.previews != 0 || service.stops != 0 {
		t.Fatalf("Caffeine submitted walls commands: %+v", service)
	}
}

func TestCaffeineToggleUpdatesOpenScreensaverSettings(t *testing.T) {
	snapshot := readyWallsSnapshot()
	snapshot.UnitFileState, snapshot.ActiveState, snapshot.SubState = "enabled", "active", "running"
	r, settingsHost, service := newOpenWallsSettings(t, snapshot)
	if err := r.OpenPanel(PanelControlCenter, 7, Trigger{OutW: 1536, OutH: 864}); err != nil {
		t.Fatal(err)
	}
	beforeRefreshes, beforePreviews, beforeStops, _ := service.counts()
	r.mu.Lock()
	defer r.mu.Unlock()
	controlHost := r.panelHosts[PanelControlCenter]
	if strings.Contains(renderText(settingsHost.root), "Caffeine does not pause the screensaver") {
		t.Fatal("Caffeine caption is visible before the toggle")
	}
	// An existing hold keeps the action synchronous without starting a real
	// session-bus inhibitor in this UI test.
	r.inhibit = closerFunc(func() error { return nil })
	if !controlHost.activateControlCentre(r, &ui.Node{Action: "cc:caffeine"}) {
		t.Fatal("Caffeine control was not handled")
	}
	if got := renderText(settingsHost.root); !strings.Contains(got, "Caffeine does not pause the screensaver") {
		t.Fatalf("open Screensaver Settings did not update after Caffeine: %q", got)
	}
	service.mu.Lock()
	defer service.mu.Unlock()
	if service.refreshes != beforeRefreshes || service.previews != beforePreviews || service.stops != beforeStops || len(service.patches) != 0 || len(service.enables) != 0 || len(service.runtime) != 0 {
		t.Fatalf("Caffeine toggle sent walls work: %+v", service)
	}
}

func TestCaffeineFailureUpdatesOpenScreensaverSettings(t *testing.T) {
	snapshot := readyWallsSnapshot()
	snapshot.UnitFileState, snapshot.ActiveState, snapshot.SubState = "enabled", "active", "running"
	r, settingsHost, service := newOpenWallsSettings(t, snapshot)
	if err := r.OpenPanel(PanelControlCenter, 7, Trigger{OutW: 1536, OutH: 864}); err != nil {
		t.Fatal(err)
	}
	r.mu.Lock()
	r.startInhibit = func() (io.Closer, error) { return nil, errors.New("caffeine denied") }
	controlHost := r.panelHosts[PanelControlCenter]
	if !controlHost.activateControlCentre(r, &ui.Node{Action: "cc:caffeine"}) {
		r.mu.Unlock()
		t.Fatal("Caffeine control was not handled")
	}
	r.mu.Unlock()
	waitFor(t, func() bool {
		r.mu.Lock()
		defer r.mu.Unlock()
		return controlHost.errLabel == "caffeine denied"
	})
	r.mu.Lock()
	if r.inhibitWanted {
		r.mu.Unlock()
		t.Fatal("failed Caffeine start remained enabled")
	}
	if got := renderText(settingsHost.root); strings.Contains(got, "Caffeine does not pause the screensaver") {
		r.mu.Unlock()
		t.Fatalf("Screensaver Settings kept the failed Caffeine caption: %q", got)
	}
	r.mu.Unlock()
	service.mu.Lock()
	defer service.mu.Unlock()
	if len(service.patches) != 0 || len(service.enables) != 0 || len(service.runtime) != 0 || service.previews != 0 || service.stops != 0 {
		t.Fatalf("Caffeine failure sent walls work: %+v", service)
	}
}

func TestScreensaverSettingsFocusOrderAndAccessibleNames(t *testing.T) {
	r, h, _ := newOpenWallsSettings(t, readyWallsSnapshot())
	r.mu.Lock()
	defer r.mu.Unlock()
	focus := ui.Focusables(h.root)
	positions := map[string]int{}
	for i, node := range focus {
		if node != nil && node.Name != "" {
			positions[node.Name] = i
		}
	}
	want := []string{"Enable screensaver", "Effect", "Theme", "Artwork", "Show date and time", "Top", "Bottom", "Preview", "Reset", "Apply"}
	previous := -1
	for _, name := range want {
		position, ok := positions[name]
		if !ok || position <= previous {
			t.Fatalf("focus order does not include %q after index %d: %v", name, previous, positions)
		}
		previous = position
	}
	if !strings.Contains(renderText(h.root), "Display coverage is unknown") {
		t.Fatal("Settings did not describe unknown screensaver coverage")
	}
	if findNode(h.root, func(n *ui.Node) bool { return n.Name == "Date and time position" && n.Role == "radiogroup" }) == nil {
		t.Fatal("date and time position picker has no accessible group name")
	}
	if h.section != "Screensaver" || !slices.Contains(settings.SectionNames(), "Screensaver") {
		t.Fatal("Screensaver section is unreachable from the Settings information architecture")
	}
}

func TestScreensaverSettingsOmitsTimeout(t *testing.T) {
	r, h, _ := newOpenWallsSettings(t, readyWallsSnapshot())
	r.mu.Lock()
	defer r.mu.Unlock()
	body := screensaverSettingsBody(r, h)
	timeout := findNode(body, func(n *ui.Node) bool {
		return n.Action == "set:walls.timeout" || n.Action == "pick:walls.timeout" ||
			strings.HasPrefix(n.Action, "set:walls.timeout=") || strings.HasPrefix(n.Action, "pick:walls.timeout=")
	})
	if timeout != nil {
		t.Fatalf("Screensaver body still has walls.timeout control %+v", timeout)
	}
	for _, e := range screensaverEntries(h) {
		if e.Path == "walls.timeout" {
			t.Fatal("screensaverEntries still registers walls.timeout")
		}
	}
	text := renderText(body)
	if !strings.Contains(text, "After idle") || !strings.Contains(text, "Session") {
		t.Fatalf("caption must mention After idle on Session: %s", text)
	}
	if strings.Contains(text, "own idle timeout") {
		t.Fatalf("caption still treats Screensaver as the idle timeout place: %s", text)
	}
}

func waitFor(t *testing.T, ready func() bool) {
	t.Helper()
	deadline := time.NewTimer(2 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	for !ready() {
		select {
		case <-deadline.C:
			t.Fatal("condition did not settle before deadline")
		case <-ticker.C:
		}
	}
}
