package shell

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/Nomadcxx/sysc-shell/internal/platform/wayland"
	"github.com/Nomadcxx/sysc-shell/internal/plugin/store"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
	"github.com/Nomadcxx/sysc-shell/plugin/catalog"
)

func TestPluginStorePanelHasDedicatedNameAndTarget(t *testing.T) {
	id, err := parsePanelName("plugin-store")
	if err != nil {
		t.Fatalf("parsePanelName(plugin-store): %v", err)
	}
	if got := id.String(); got != "plugin-store" {
		t.Fatalf("PanelID.String() = %q, want plugin-store", got)
	}
	if got, want := panelTargetSize(id), (ui.Rect{W: 1280, H: 820}); got != want {
		t.Fatalf("panel target = %+v, want %+v", got, want)
	}
}

func TestPluginStorePanelBuildsAndHeadlessRendersEmptyState(t *testing.T) {
	id, err := parsePanelName("plugin-store")
	if err != nil {
		t.Fatal(err)
	}
	for _, output := range []ui.Rect{{W: 1600, H: 1000}, {W: 1229, H: 691}} {
		reg := newPanelRegistry(t)
		if err := reg.OpenPanel(id, 7, Trigger{BarEdge: "top", OutW: output.W, OutH: output.H}); err != nil {
			t.Fatalf("open on %dx%d: %v", output.W, output.H, err)
		}
		reqs := drainAux(t, reg, 2)
		panel := reqs[1].Open
		width, height := int(panel.Width), int(panel.Height)
		if err := panel.Callbacks.Configure(width, height, 120); err != nil {
			t.Fatalf("configure on %dx%d: %v", output.W, output.H, err)
		}
		pixels := make([]byte, width*height*4)
		if err := panel.Callbacks.Render(pixels, width, height, width*4); err != nil {
			t.Fatalf("render on %dx%d: %v", output.W, output.H, err)
		}
		h := reg.panelHosts[id]
		if !pluginStoreHasText(h.root, "Plugin store") || !pluginStoreHasText(h.root, "No plugins match") {
			t.Fatalf("empty store panel on %dx%d lacks its title or empty state", output.W, output.H)
		}
		if !pluginStoreHasKind(h.root, ui.KindTextField) {
			t.Fatalf("empty store panel on %dx%d has no search field", output.W, output.H)
		}
	}
}

func pluginStoreHasText(n *ui.Node, text string) bool {
	if n == nil {
		return false
	}
	if n.Text == text {
		return true
	}
	for _, child := range n.Children {
		if pluginStoreHasText(child, text) {
			return true
		}
	}
	return false
}

func pluginStoreHasKind(n *ui.Node, kind ui.Kind) bool {
	if n == nil {
		return false
	}
	if n.Kind == kind {
		return true
	}
	for _, child := range n.Children {
		if pluginStoreHasKind(child, kind) {
			return true
		}
	}
	return false
}

func TestPluginStorePanelHeadlessRendersEveryGridStateAtBothSizes(t *testing.T) {
	withScreenshot := modelListing("org.sysc.timer", "Timer", "sysc", "productivity", "1.4.0")
	withScreenshot.Entry.Screenshot = &catalog.Screenshot{URL: "https://example.com/timer.png", SHA256: strings.Repeat("c", 64)}
	withoutScreenshot := modelListing("org.sysc.clock", "Clock", "community", "utilities", "1.0.0")
	states := []struct {
		name  string
		state store.State
		want  string
	}{
		{name: "loading", state: store.State{Busy: "refresh"}, want: "Loading plugins…"},
		{name: "results and no screenshot", state: store.State{
			Sources: []store.SourceState{{Name: "sysc"}}, Listings: []store.Listing{withScreenshot, withoutScreenshot},
		}, want: "Clock"},
		{name: "empty", state: store.State{}, want: "No plugins match"},
		{name: "all sources failed", state: store.State{
			Sources: []store.SourceState{{Name: "sysc", Err: errors.New("network unreachable")}},
		}, want: "All sources failed"},
		{name: "stale", state: store.State{
			Sources:  []store.SourceState{{Name: "sysc", FetchedAt: time.Now(), Err: errors.New("offline")}},
			Listings: []store.Listing{withScreenshot},
		}, want: "Stale sources"},
	}
	for _, size := range []struct {
		name string
		out  ui.Rect
	}{{"target", ui.Rect{W: 1600, H: 1000}}, {"laptop", ui.Rect{W: 1229, H: 691}}} {
		for _, tc := range states {
			t.Run(size.name+"/"+tc.name, func(t *testing.T) {
				reg, host, panel := openPluginStoreTestPanel(t, tc.state, size.out)
				width, height := int(panel.Width), int(panel.Height)
				pixels := make([]byte, width*height*4)
				if err := panel.Callbacks.Render(pixels, width, height, width*4); err != nil {
					t.Fatalf("render %dx%d: %v", width, height, err)
				}
				if !pluginStoreHasText(host.root, tc.want) {
					t.Fatalf("panel lacks %q at %dx%d", tc.want, width, height)
				}
				if !pluginStoreHasKind(host.root, ui.KindTextField) {
					t.Fatal("panel has no search field")
				}
				if size.name == "target" && tc.name == "results and no screenshot" && host.pluginStoreColumns != 4 {
					t.Fatalf("target grid has %d columns, want 4", host.pluginStoreColumns)
				}
				if size.name == "laptop" && tc.name == "results and no screenshot" && host.pluginStoreColumns != 3 {
					t.Fatalf("laptop grid has %d columns, want 3", host.pluginStoreColumns)
				}
				_ = reg
			})
		}
	}
}

func TestPluginStoreKeyboardSelectionClampsAtGridEdges(t *testing.T) {
	state := store.State{}
	for i := range 6 {
		state.Listings = append(state.Listings, modelListing(fmt.Sprintf("org.sysc.p%d", i), fmt.Sprintf("P%d", i), "sysc", "utilities", "1.0.0"))
	}
	reg, host, _ := openPluginStoreTestPanel(t, state, ui.Rect{W: 1600, H: 1000})
	reg.mu.Lock()
	defer reg.mu.Unlock()
	first := pluginStoreKey(state.Listings[0])
	last := pluginStoreKey(state.Listings[5])
	if host.pluginStoreSelected != first {
		t.Fatalf("initial selection = %q, want %q", host.pluginStoreSelected, first)
	}
	for _, key := range []uint32{keyLeft, keyUp} {
		host.keyPress(reg, key)
		if host.pluginStoreSelected != first {
			t.Fatalf("selection wrapped from first card on key %d: %q", key, host.pluginStoreSelected)
		}
	}
	host.keyPress(reg, keyDown)
	if host.pluginStoreSelected != pluginStoreKey(state.Listings[4]) {
		t.Fatalf("down selection = %q, want fifth card", host.pluginStoreSelected)
	}
	host.keyPress(reg, keyRight)
	if host.pluginStoreSelected != last {
		t.Fatalf("right selection = %q, want last card", host.pluginStoreSelected)
	}
	for _, key := range []uint32{keyRight, keyDown} {
		host.keyPress(reg, key)
		if host.pluginStoreSelected != last {
			t.Fatalf("selection exceeded last card on key %d: %q", key, host.pluginStoreSelected)
		}
	}
}

func TestPluginStoreSelectionSurvivesAFilterThatKeepsTheCard(t *testing.T) {
	timer := modelListing("org.sysc.timer", "Timer", "sysc", "productivity", "1.4.0")
	clock := modelListing("org.sysc.clock", "Clock", "sysc", "utilities", "1.0.0")
	reg, host, _ := openPluginStoreTestPanel(t, store.State{Listings: []store.Listing{timer, clock}}, ui.Rect{W: 1600, H: 1000})
	reg.mu.Lock()
	host.pluginStoreSelected = pluginStoreKey(timer)
	host.search = ui.NewField("Timer")
	host.focusByName("Search")
	reg.rebuildPanel(host)
	got := host.pluginStoreSelected
	reg.mu.Unlock()
	if got != pluginStoreKey(timer) {
		t.Fatalf("selection after filter = %q, want %q", got, pluginStoreKey(timer))
	}
	if !pluginStoreHasText(host.root, "Timer") {
		t.Fatal("filtered tree does not show the selected card")
	}
}

func TestPluginStoreVisibleMediaIncludesOnlyVisibleRowsAndTheNextRow(t *testing.T) {
	listings := make([]store.Listing, 12)
	for i := range listings {
		listings[i] = modelListing(fmt.Sprintf("org.sysc.p%d", i), fmt.Sprintf("P%d", i), "sysc", "utilities", "1.0.0")
		listings[i].Entry.Screenshot = &catalog.Screenshot{URL: fmt.Sprintf("https://example.com/%d.png", i), SHA256: fmt.Sprintf("%064x", i+1)}
	}
	got := pluginStoreVisibleMedia(listings, nil, 4, 1, 1)
	if len(got) != 8 {
		t.Fatalf("visible media count = %d, want two four-card rows", len(got))
	}
	if got[0].SHA256 != listings[4].Entry.Screenshot.SHA256 || got[7].SHA256 != listings[11].Entry.Screenshot.SHA256 {
		t.Fatalf("visible media rows start/end at %q/%q, want row 2 through row 3", got[0].SHA256, got[7].SHA256)
	}
}

func openPluginStoreTestPanel(t *testing.T, state store.State, output ui.Rect) (*Registry, *PanelHost, *wayland.AuxSpec) {
	t.Helper()
	reg := newPanelRegistry(t)
	reg.mu.Lock()
	reg.pluginStoreSnapshot = state
	reg.mu.Unlock()
	id, err := parsePanelName("plugin-store")
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.OpenPanel(id, 7, Trigger{BarEdge: "top", OutW: output.W, OutH: output.H}); err != nil {
		t.Fatal(err)
	}
	reqs := drainAux(t, reg, 2)
	panel := reqs[1].Open
	width, height := int(panel.Width), int(panel.Height)
	if err := panel.Callbacks.Configure(width, height, 120); err != nil {
		t.Fatal(err)
	}
	reg.mu.Lock()
	host := reg.panelHosts[id]
	reg.mu.Unlock()
	return reg, host, panel
}

func TestPluginStoreFilterAndSortControlsUpdateTheGrid(t *testing.T) {
	timer := modelListing("org.sysc.timer", "Timer", "sysc", "utilities", "1.4.0")
	clock := modelListing("org.community.clock", "Clock", "community", "utilities", "1.0.0")
	clock.Installed = &store.Record{Source: "community", Version: "1.0.0"}
	appearance := modelListing("org.sysc.editor", "Editor", "sysc", "appearance", "1.0.0")
	state := store.State{
		Sources:  []store.SourceState{{Name: "sysc"}, {Name: "community"}},
		Listings: []store.Listing{timer, clock, appearance},
	}
	reg, host, _ := openPluginStoreTestPanel(t, state, ui.Rect{W: 1600, H: 1000})
	reg.mu.Lock()
	defer reg.mu.Unlock()

	pluginStoreActivateAction(t, reg, host, "store-source:community")
	if host.pluginStoreQuery.Source != "community" || !pluginStoreHasText(host.root, "Clock") {
		t.Fatalf("source chip did not filter to community: query=%+v", host.pluginStoreQuery)
	}
	pluginStoreActivateAction(t, reg, host, "store-hide-installed")
	if !host.pluginStoreQuery.HideInstalled || !pluginStoreHasText(host.root, "No plugins match") {
		t.Fatal("Hide installed did not remove the installed community listing")
	}
	pluginStoreActivateAction(t, reg, host, "store-clear")
	// Categories is an expander: its chips join the keyboard order and a
	// choice closes the row again.
	if pluginStoreFindAction(host.root, "store-category:appearance") != nil {
		t.Fatal("category chips showed before Categories was opened")
	}
	pluginStoreActivateAction(t, reg, host, "store-categories")
	pluginStoreActivateAction(t, reg, host, "store-category:appearance")
	if host.pluginStoreQuery.Category != "appearance" || !pluginStoreHasText(host.root, "Editor") || pluginStoreHasText(host.root, "Timer") {
		t.Fatalf("category choice = %q, want appearance", host.pluginStoreQuery.Category)
	}
	if host.pluginStoreCategoriesOpen || !pluginStoreHasText(host.root, "Appearance") {
		t.Fatal("choosing a category did not close the row and label the chip")
	}
	pluginStoreActivateAction(t, reg, host, "store-sort")
	if host.pluginStoreQuery.Sort != SortUpdated {
		t.Fatalf("sort chip selected %v, want SortUpdated", host.pluginStoreQuery.Sort)
	}
}

func TestPluginStoreKeyboardOpensDetailSearchesAndEscapesInOrder(t *testing.T) {
	listing := modelListing("org.sysc.timer", "Timer", "sysc", "productivity", "1.4.0")
	reg, host, _ := openPluginStoreTestPanel(t, store.State{Listings: []store.Listing{listing}}, ui.Rect{W: 1600, H: 1000})
	reg.mu.Lock()
	defer reg.mu.Unlock()
	if focused := host.focused(); focused == nil || focused.Key != "store-card:"+pluginStoreKey(listing) {
		t.Fatalf("initial focus = %+v, want selected card", focused)
	}
	host.keyPress(reg, keyEnter)
	if host.pluginStoreDetail != pluginStoreKey(listing) || pluginStoreFindAction(host.root, "store-back") == nil {
		t.Fatal("Enter did not open the selected plugin detail")
	}
	host.keyPress(reg, keyEsc)
	if host.pluginStoreDetail != "" || !pluginStoreHasKind(host.root, ui.KindVirtualList) {
		t.Fatal("Escape did not return from the detail to the grid")
	}
	if got, ok := ui.EvdevText(53, false); !ok || got != "/" {
		t.Fatalf("evdev slash key = %q, %v", got, ok)
	}
	host.keyPress(reg, 53)
	if focused := host.focused(); focused == nil || focused.Kind != ui.KindTextField {
		t.Fatalf("slash focused %+v, want search field", focused)
	}
	for _, key := range []uint32{20, 23, 50, 18, 19} { // t i m e r
		host.keyPress(reg, key)
	}
	if host.pluginStoreQuery.Text != "timer" {
		t.Fatalf("typed query = %q, want timer", host.pluginStoreQuery.Text)
	}
	host.keyPress(reg, keyEsc)
	if host.search.Text != "" || reg.panelHosts[PanelPluginStore] == nil {
		t.Fatal("Escape did not clear search before keeping the panel open")
	}
	host.keyPress(reg, keyEsc)
	if reg.panelHosts[PanelPluginStore] != nil {
		t.Fatal("Escape did not close the panel after search was clear")
	}
}

func pluginStoreActivateAction(t *testing.T, reg *Registry, host *PanelHost, action string) {
	t.Helper()
	index := pluginStoreFocusIndex(host, action)
	if index < 0 {
		t.Fatalf("action %q is not focusable", action)
	}
	host.roving.Set(index)
	if !host.activate(reg) {
		t.Fatalf("action %q did not activate", action)
	}
}

func pluginStoreFocusIndex(host *PanelHost, action string) int {
	for i, node := range host.focus {
		if node != nil && node.Action == action {
			return i
		}
	}
	return -1
}

func pluginStoreFindAction(node *ui.Node, action string) *ui.Node {
	if node == nil {
		return nil
	}
	if node.Action == action {
		return node
	}
	for _, child := range node.Children {
		if found := pluginStoreFindAction(child, action); found != nil {
			return found
		}
	}
	return nil
}

// A pointer click on a card opens its detail, and one on Back returns to the
// grid. The keyboard path was covered; the live laptop run found the click
// path doing nothing.
func TestPluginStoreClickOpensDetailAndBack(t *testing.T) {
	listing := modelListing("org.sysc.timer", "Timer", "sysc", "productivity", "1.4.0")
	reg, host, panel := openPluginStoreTestPanel(t, store.State{Listings: []store.Listing{listing}}, ui.Rect{W: 1536, H: 864})
	renderPluginStorePanel(t, panel)
	reg.mu.Lock()
	card := pluginStoreFindAction(host.root, "store-open:"+pluginStoreKey(listing))
	reg.mu.Unlock()
	if card == nil {
		t.Fatal("no card")
	}
	pressAt(reg, host, card.Bounds.X+card.Bounds.W/2, card.Bounds.Y+card.Bounds.H/3, 0)
	reg.mu.Lock()
	detail := host.pluginStoreDetail
	back := pluginStoreFindAction(host.root, "store-back")
	reg.mu.Unlock()
	if detail != pluginStoreKey(listing) || back == nil {
		t.Fatalf("click on the card left detail = %q", detail)
	}
	renderPluginStorePanel(t, panel)
	reg.mu.Lock()
	back = pluginStoreFindAction(host.root, "store-back")
	reg.mu.Unlock()
	pressAt(reg, host, back.Bounds.X+back.Bounds.W/2, back.Bounds.Y+back.Bounds.H/2, 0)
	reg.mu.Lock()
	defer reg.mu.Unlock()
	if host.pluginStoreDetail != "" {
		t.Fatal("click on Back did not return to the grid")
	}
}

// Enter activates the focused control. The grid only takes Enter and the
// arrows while a card has focus; before, Enter on a chip or on Install
// reopened the selected card instead.
func TestPluginStoreEnterActivatesTheFocusedControl(t *testing.T) {
	listing := modelListing("org.sysc.timer", "Timer", "sysc", "productivity", "1.4.0")
	installed := modelListing("org.sysc.clock", "Clock", "sysc", "utilities", "1.0.0")
	installed.Installed = &store.Record{Source: "sysc", Version: "1.0.0"}
	installed.Status = store.StatusInstalled
	reg, host, _ := openPluginStoreTestPanel(t, store.State{Listings: []store.Listing{listing, installed}}, ui.Rect{W: 1600, H: 1000})
	reg.mu.Lock()
	defer reg.mu.Unlock()
	host.roving.Set(pluginStoreFocusIndex(host, "store-hide-installed"))
	host.keyPress(reg, keyEnter)
	if !host.pluginStoreQuery.HideInstalled || host.pluginStoreDetail != "" {
		t.Fatalf("Enter on Hide installed: hide=%v detail=%q", host.pluginStoreQuery.HideInstalled, host.pluginStoreDetail)
	}
	host.pluginStoreDetail = pluginStoreKey(listing)
	reg.rebuildPanel(host)
	if f := host.focused(); f == nil || f.Action != "store-primary" {
		t.Fatalf("detail opened with focus on %+v, want the primary action", f)
	}
	host.keyPress(reg, keyEnter)
	if host.pluginStoreConsent == nil || pluginStoreFindAction(host.root, "store-confirm") == nil {
		t.Fatal("Enter on Install did not open the consent step")
	}
}

// A wheel scroll on the grid survives the next rebuild, which a worker
// snapshot triggers at any moment.
func TestPluginStoreGridScrollSurvivesARebuild(t *testing.T) {
	var listings []store.Listing
	for i := 0; i < 24; i++ {
		listings = append(listings, modelListing(fmt.Sprintf("org.sysc.p%02d", i), fmt.Sprintf("P%02d", i), "sysc", "utilities", "1.0.0"))
	}
	reg, host, panel := openPluginStoreTestPanel(t, store.State{Listings: listings}, ui.Rect{W: 1600, H: 1000})
	renderPluginStorePanel(t, panel)
	reg.mu.Lock()
	defer reg.mu.Unlock()
	grid := pluginStoreFindKey(host.root, "plugin-store-grid")
	host.hoverX, host.hoverY = grid.Bounds.X+grid.Bounds.W/2, grid.Bounds.Y+grid.Bounds.H/2
	if !host.scrollAxis(reg, wayland.Event{Kind: wayland.EventPointerAxis, AxisValue120: 240}) {
		t.Fatal("wheel over the grid did not scroll")
	}
	if host.pluginStoreScroll == 0 {
		t.Fatal("the host did not record the grid's scroll")
	}
	want := host.pluginStoreScroll
	reg.rebuildPanel(host)
	if got := pluginStoreFindKey(host.root, "plugin-store-grid").ScrollOffset; got != want {
		t.Fatalf("grid offset after rebuild = %d, want %d", got, want)
	}
}

func pluginStoreFindKey(node *ui.Node, key string) *ui.Node {
	if node == nil {
		return nil
	}
	if node.Key == key {
		return node
	}
	for _, child := range node.Children {
		if found := pluginStoreFindKey(child, key); found != nil {
			return found
		}
	}
	return nil
}
