package shell

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/Nomadcxx/sysc-shell/internal/services"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

// Panel configure, render and handle all run with Registry.mu held. A write
// that took a bus round trip on that path would stall every bar on the
// machine, so every network write goes through scheduleControl, which runs it
// on its own goroutine before re-taking the lock to publish.
func TestNetworkWriteNeverRunsUnderRegistryLock(t *testing.T) {
	r := &Registry{}
	h := &PanelHost{id: PanelNetwork, networkTab: "wifi"}
	done := make(chan struct{})

	r.mu.Lock() // held exactly as the panel paths hold it
	r.scheduleControl(h, func() error { close(done); return nil })
	select {
	case <-done:
	case <-time.After(time.Second):
		r.mu.Unlock()
		t.Fatal("the control never ran while the lock was held; it is not off-owner")
	}
	r.mu.Unlock()
}

// A bare PanelHost carries the zero Metrics, so a tree test must build against
// the package's standardMetrics() rather than h.metrics(), which would assert
// against zero padding and prove nothing.

// collectText flattens every text node in a tree, for order-independent
// assertions about what a card renders.
func collectText(n *ui.Node) []string {
	if n == nil {
		return nil
	}
	var out []string
	if n.Kind == ui.KindText && n.Text != "" {
		out = append(out, n.Text)
	}
	for _, c := range n.Children {
		out = append(out, collectText(c)...)
	}
	return out
}

func findRowIfPresent(n *ui.Node, ssid string) *ui.Node {
	if n == nil {
		return nil
	}
	if n.Kind == ui.KindButton && slices.Contains(collectText(n), ssid) {
		return n
	}
	for _, c := range n.Children {
		if got := findRowIfPresent(c, ssid); got != nil {
			return got
		}
	}
	return nil
}

// findRowBySSID returns the access-point row whose label matches, failing the
// test if there is none.
func findRowBySSID(t *testing.T, n *ui.Node, ssid string) *ui.Node {
	t.Helper()
	if got := findRowIfPresent(n, ssid); got != nil {
		return got
	}
	t.Fatalf("no row for %q", ssid)
	return nil
}

func hasIcon(n *ui.Node, name string) bool {
	if n == nil {
		return false
	}
	if n.Kind == ui.KindIcon && n.Icon == name {
		return true
	}
	for _, c := range n.Children {
		if hasIcon(c, name) {
			return true
		}
	}
	return false
}

func firstOfKind(n *ui.Node, k ui.Kind) *ui.Node {
	if n == nil {
		return nil
	}
	if n.Kind == k {
		return n
	}
	for _, c := range n.Children {
		if got := firstOfKind(c, k); got != nil {
			return got
		}
	}
	return nil
}

func sampleAPs() []services.AccessPoint {
	return []services.AccessPoint{
		{SSID: "LukeAP", Strength: 92, Secured: true, Saved: true, Active: true},
		{SSID: "Orac 15A", Strength: 74, Secured: true, Saved: true},
		{SSID: "NETGEAR-Guest", Strength: 41},
	}
}

func TestPanelNetworkTargetSizeAndDefaultTab(t *testing.T) {
	if got := panelTargetSize(PanelNetwork); got.W != 460 || got.H != 560 {
		t.Fatalf("panelTargetSize = %dx%d, want 460x560", got.W, got.H)
	}
	h := &PanelHost{id: PanelNetwork}
	_ = networkTree(&Registry{}, h)
	if h.networkTab != "wifi" {
		t.Fatalf("default tab = %q, want wifi", h.networkTab)
	}
}

// The panel must survive a registry with no network service at all: that is
// the state on a machine without NetworkManager, and a nil dereference here
// paints nothing while the service still reads active.
func TestNetworkTreeToleratesAnAbsentService(t *testing.T) {
	h := &PanelHost{id: PanelNetwork}
	if got := networkTree(&Registry{}, h); got == nil {
		t.Fatal("networkTree returned nil with no service; it must still paint")
	}
}

func TestHeaderShowsDashForAbsentFiguresNotZero(t *testing.T) {
	st := services.NetworkState{WirelessEnabled: false}
	card := networkHeaderCard(&PanelHost{networkTab: "wifi"}, st, standardMetrics())
	texts := collectText(card)
	for _, want := range []string{"IPv4", "Down", "Up", "—"} {
		if !slices.Contains(texts, want) {
			t.Fatalf("header missing %q; got %v", want, texts)
		}
	}
	if slices.Contains(texts, "0") {
		t.Error("absent figures must render as a dash, never zero")
	}
}

func TestHeaderShowsTheActiveConnection(t *testing.T) {
	st := services.NetworkState{
		WirelessEnabled: true,
		Kind:            services.ConnWireless,
		Connected:       true,
		SSID:            "LukeAP",
		IPv4:            "192.168.1.37",
		Interface:       "wlan0",
		Strength:        92,
	}
	card := networkHeaderCard(&PanelHost{networkTab: "wifi"}, st, standardMetrics())
	texts := collectText(card)
	for _, want := range []string{"LukeAP", "192.168.1.37"} {
		if !slices.Contains(texts, want) {
			t.Errorf("header missing %q; got %v", want, texts)
		}
	}
	if !hasIcon(card, "signal_wifi_4_bar") {
		t.Error("header must carry the band glyph for the active connection")
	}
}

// The Ethernet tab has no radio to switch, so the toggle must not appear on it.
func TestHeaderOmitsTheRadioToggleOnEthernet(t *testing.T) {
	st := services.NetworkState{Kind: services.ConnWired, Connected: true, Interface: "enp7s0"}
	card := networkHeaderCard(&PanelHost{networkTab: "ethernet"}, st, standardMetrics())
	var toggles int
	var walk func(*ui.Node)
	walk = func(n *ui.Node) {
		if n == nil {
			return
		}
		if n.Kind == ui.KindToggle {
			toggles++
		}
		for _, c := range n.Children {
			walk(c)
		}
	}
	walk(card)
	if toggles != 0 {
		t.Errorf("ethernet header carries %d toggle(s); the wired link has no radio", toggles)
	}
}

func TestNetworkTabsAreTwoSegmentsWithWifiSelected(t *testing.T) {
	tabs := networkTabs(&PanelHost{networkTab: "wifi"}, standardMetrics())
	seg := firstOfKind(tabs, ui.KindSegmented)
	if seg == nil {
		t.Fatal("no segmented control in the tabs row")
	}
	if len(seg.Children) != 2 {
		t.Fatalf("segments = %d, want 2", len(seg.Children))
	}
	if seg.Children[0].State&ui.StateSelected == 0 {
		t.Error("Wi-Fi must be the selected segment by default")
	}
	if seg.Children[1].State&ui.StateSelected != 0 {
		t.Error("Ethernet must not be selected while the tab is wifi")
	}
}

// D9: the status block owns the filled highlight. The connected row is marked
// with a trailing check alone, so the panel has one focal point rather than
// stating the same fact twice with equal weight.
func TestConnectedRowCarriesCheckWithoutFilledHighlight(t *testing.T) {
	tree := networkWifiTree(sampleAPs(), services.NetworkState{WirelessEnabled: true}, &PanelHost{networkTab: "wifi"}, standardMetrics())
	row := findRowBySSID(t, tree, "LukeAP")
	if !hasIcon(row, "check") {
		t.Error("the active row must carry a trailing check")
	}
	if row.Fill == ui.FillAccent {
		t.Error("the filled highlight belongs to the status block, not the row")
	}
}

// A secured network must be visibly secured before it is tapped: that is what
// tells the user a password prompt is coming.
func TestSecuredRowCarriesTheLockGlyph(t *testing.T) {
	tree := networkWifiTree(sampleAPs(), services.NetworkState{WirelessEnabled: true}, &PanelHost{networkTab: "wifi"}, standardMetrics())
	if !hasIcon(findRowBySSID(t, tree, "Orac 15A"), "lock") {
		t.Error("a secured network must show the lock glyph")
	}
	if hasIcon(findRowBySSID(t, tree, "NETGEAR-Guest"), "lock") {
		t.Error("an open network must not show the lock glyph")
	}
}

// Ordering is the service's job. The panel must not re-sort, or the two would
// drift and the list would reorder under the pointer.
func TestWifiListPreservesServiceOrder(t *testing.T) {
	tree := networkWifiTree(sampleAPs(), services.NetworkState{WirelessEnabled: true}, &PanelHost{networkTab: "wifi"}, standardMetrics())
	texts := collectText(tree)
	iLuke := slices.Index(texts, "LukeAP")
	iOrac := slices.Index(texts, "Orac 15A")
	iGuest := slices.Index(texts, "NETGEAR-Guest")
	if iLuke < 0 || iOrac < 0 || iGuest < 0 {
		t.Fatalf("a row is missing: %v", texts)
	}
	if !(iLuke < iOrac && iOrac < iGuest) {
		t.Errorf("rows reordered: LukeAP=%d Orac=%d Guest=%d", iLuke, iOrac, iGuest)
	}
}

// Radio off is not an empty list: the list would say "no networks here", which
// is a different and wrong claim.
func TestRadioOffShowsAnOffStateNotAnEmptyList(t *testing.T) {
	tree := networkWifiTree(nil, services.NetworkState{WirelessEnabled: false}, &PanelHost{networkTab: "wifi"}, standardMetrics())
	texts := collectText(tree)
	joined := strings.Join(texts, "|")
	if !strings.Contains(strings.ToLower(joined), "off") {
		t.Errorf("radio-off state must say so; got %v", texts)
	}
}

func TestEthernetTabShowsTheWiredInterface(t *testing.T) {
	st := services.NetworkState{Kind: services.ConnWired, Connected: true, Interface: "enp7s0", IPv4: "192.168.1.24"}
	tree := networkEthernetTree(st, standardMetrics())
	texts := collectText(tree)
	if !slices.Contains(texts, "enp7s0") {
		t.Errorf("ethernet tab must name the interface; got %v", texts)
	}
	if !hasIcon(tree, "lan") {
		t.Error("ethernet tab must carry the lan glyph")
	}
}
