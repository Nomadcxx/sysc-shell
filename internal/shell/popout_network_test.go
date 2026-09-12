package shell

import (
	"slices"
	"testing"

	"github.com/Nomadcxx/sysc-shell/internal/services"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

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
