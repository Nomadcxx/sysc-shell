package shell

import (
	"errors"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	metrics "github.com/Nomadcxx/sysc-metrics"
	"github.com/Nomadcxx/sysc-shell/internal/services"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

type shellNetworkBackend struct {
	mu             sync.Mutex
	state          services.NetworkState
	aps            []services.AccessPoint
	wake           chan<- struct{}
	scan           func() error
	manualRequests []services.ManualNetworkRequest
	manualDone     chan struct{}
	manualErr      error
	vpnProfiles    []services.VPNProfile
	vpnDone        chan struct{}
	vpnCalls       []string
	vpnControlErr  error
	closed         int
}

func (b *shellNetworkBackend) State() (services.NetworkState, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.state, nil
}

func (b *shellNetworkBackend) AccessPoints() ([]services.AccessPoint, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return slices.Clone(b.aps), nil
}

func (b *shellNetworkBackend) Scan() error {
	b.mu.Lock()
	run := b.scan
	b.mu.Unlock()
	if run != nil {
		return run()
	}
	return nil
}
func (b *shellNetworkBackend) SetWirelessEnabled(bool) error       { return nil }
func (b *shellNetworkBackend) Activate(services.AccessPoint) error { return nil }
func (b *shellNetworkBackend) ConnectManual(req services.ManualNetworkRequest) error {
	b.mu.Lock()
	b.manualRequests = append(b.manualRequests, req)
	done, err := b.manualDone, b.manualErr
	b.mu.Unlock()
	if done != nil {
		close(done)
	}
	return err
}
func (b *shellNetworkBackend) VPNProfiles() ([]services.VPNProfile, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return slices.Clone(b.vpnProfiles), nil
}
func (b *shellNetworkBackend) ConnectVPN(uuid string) error {
	b.mu.Lock()
	b.vpnCalls = append(b.vpnCalls, "connect:"+uuid)
	done, err := b.vpnDone, b.vpnControlErr
	b.mu.Unlock()
	if done != nil {
		close(done)
	}
	return err
}
func (b *shellNetworkBackend) DisconnectVPN(uuid string) error {
	b.mu.Lock()
	b.vpnCalls = append(b.vpnCalls, "disconnect:"+uuid)
	done, err := b.vpnDone, b.vpnControlErr
	b.mu.Unlock()
	if done != nil {
		close(done)
	}
	return err
}
func (b *shellNetworkBackend) Forget(string) error { return nil }
func (b *shellNetworkBackend) Close() error {
	b.mu.Lock()
	b.closed++
	b.mu.Unlock()
	return nil
}
func (b *shellNetworkBackend) Watch(wake chan<- struct{}, _ <-chan struct{}) error {
	b.mu.Lock()
	b.wake = wake
	b.mu.Unlock()
	return nil
}

// Panel configure, render and handle all run with Registry.mu held. A write
// that took a bus round trip on that path would stall every bar on the
// machine, so every network write goes through scheduleControl and runs after
// the handler releases the lock.
func TestNetworkWriteRunsAfterRegistryUnlock(t *testing.T) {
	r := &Registry{}
	h := &PanelHost{id: PanelNetwork, networkTab: "wifi"}
	unlocked := make(chan bool, 1)

	r.mu.Lock() // held exactly as the panel paths hold it
	r.scheduleControl(h, func() error {
		ok := r.mu.TryLock()
		if ok {
			r.mu.Unlock()
		}
		unlocked <- ok
		return nil
	})
	select {
	case ok := <-unlocked:
		r.mu.Unlock()
		t.Fatalf("network write ran before the handler released Registry.mu (lock available: %v)", ok)
	case <-time.After(10 * time.Millisecond):
	}
	r.mu.Unlock()
	select {
	case ok := <-unlocked:
		if !ok {
			t.Fatal("network write ran while Registry.mu was held")
		}
	case <-time.After(time.Second):
		t.Fatal("the network write did not run after the handler released Registry.mu")
	}
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

func findActionNode(n *ui.Node, action string) *ui.Node {
	if n == nil {
		return nil
	}
	if n.Action == action {
		return n
	}
	for _, child := range n.Children {
		if got := findActionNode(child, action); got != nil {
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

func TestNetworkPanelConfiguresAtTargetSize(t *testing.T) {
	size := panelTargetSize(PanelNetwork)
	h := &PanelHost{
		id: PanelNetwork, place: Placement{Panel: size}, theme: DefaultTheme(),
	}
	tree := networkTree(&Registry{}, h)
	measure := func(string, ui.TextAttrs) (int, int) { return 10, 16 }
	if err := ui.LayoutColumn(tree, size, measure); err != nil {
		t.Fatalf("network panel does not lay out at %dx%d: %v", size.W, size.H, err)
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
	card := networkHeaderCard(&PanelHost{networkTab: "wifi"}, st, services.Snapshot{}, standardMetrics())
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
	card := networkHeaderCard(&PanelHost{networkTab: "wifi"}, st, services.Snapshot{}, standardMetrics())
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
	card := networkHeaderCard(&PanelHost{networkTab: "ethernet"}, st, services.Snapshot{}, standardMetrics())
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

func TestNetworkTabsHaveWifiEthernetAndVPNWithWifiSelected(t *testing.T) {
	tabs := networkTabs(&PanelHost{networkTab: "wifi"}, standardMetrics())
	seg := firstOfKind(tabs, ui.KindSegmented)
	if seg == nil {
		t.Fatal("no segmented control in the tabs row")
	}
	if len(seg.Children) != 3 {
		t.Fatalf("segments = %d, want 3", len(seg.Children))
	}
	if seg.Children[0].State&ui.StateSelected == 0 {
		t.Error("Wi-Fi must be the selected segment by default")
	}
	if seg.Children[1].State&ui.StateSelected != 0 {
		t.Error("Ethernet must not be selected while the tab is wifi")
	}
	if seg.Children[2].State&ui.StateSelected != 0 {
		t.Error("VPN must not be selected while the tab is wifi")
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

func TestAccessPointRowUsesSmallCornerShape(t *testing.T) {
	row := networkAPRow(services.AccessPoint{SSID: "Test AP", Strength: 50}, standardMetrics(), nil)
	if row.Shape != ui.ShapeSmall {
		t.Fatalf("access-point row shape = %v, want ShapeSmall", row.Shape)
	}
}

func TestAccessPointRowsKeepLeadingAndTrailingIconsAligned(t *testing.T) {
	t.Parallel()
	m := standardMetrics()
	aps := []services.AccessPoint{
		{SSID: "Open", Strength: 41},
		{SSID: "Saved", Strength: 74, Secured: true, Saved: true},
		{SSID: "Active", Strength: 92, Secured: true, Saved: true, Active: true},
	}
	wantLeadingX := -1
	for _, ap := range aps {
		row := networkAPRow(ap, m, nil)
		root := &ui.Node{Kind: ui.KindColumn, Children: []*ui.Node{row}}
		if err := ui.LayoutColumn(root, ui.Rect{X: 8, W: 400, H: 64}, func(s string, _ ui.TextAttrs) (int, int) {
			return len(s) * 8, 16
		}); err != nil {
			t.Fatalf("%s: %v", ap.SSID, err)
		}
		signal := findIconNode(row, wifiBandGlyph(ap.Strength))
		if signal == nil {
			t.Fatalf("%s: no signal icon", ap.SSID)
		}
		if wantLeadingX < 0 {
			wantLeadingX = signal.Bounds.X
		} else if signal.Bounds.X != wantLeadingX {
			t.Errorf("%s: signal X = %d, want stable X %d", ap.SSID, signal.Bounds.X, wantLeadingX)
		}
		if want := row.Bounds.X + m.ButtonPadding; signal.Bounds.X != want {
			t.Errorf("%s: signal X = %d, want row inset X %d", ap.SSID, signal.Bounds.X, want)
		}
		if ap.Secured || ap.Active {
			name := "lock"
			if ap.Active {
				name = "check"
			}
			trailing := findIconNode(row, name)
			if trailing == nil {
				t.Fatalf("%s: no trailing %s icon", ap.SSID, name)
			}
			if got, want := trailing.Bounds.X+trailing.Bounds.W, row.Bounds.X+row.Bounds.W-m.ButtonPadding; got != want {
				t.Errorf("%s: trailing right edge = %d, want %d", ap.SSID, got, want)
			}
		}
	}
}

func findIconNode(n *ui.Node, name string) *ui.Node {
	if n == nil {
		return nil
	}
	if n.Kind == ui.KindIcon && n.Icon == name {
		return n
	}
	for _, child := range n.Children {
		if got := findIconNode(child, name); got != nil {
			return got
		}
	}
	return nil
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

func TestNetworkManualEntryWorksWithoutScannedAccessPoints(t *testing.T) {
	r := newPanelRegistry(t)
	backend := &shellNetworkBackend{manualDone: make(chan struct{}), manualErr: errors.New("activation refused")}
	r.setNetwork(services.NewNetwork(backend))
	h := &PanelHost{id: PanelNetwork, output: 7, place: Placement{Panel: panelTargetSize(PanelNetwork)}, theme: DefaultTheme(), stopAnim: make(chan struct{})}
	r.mu.Lock()
	r.panelHosts[PanelNetwork] = h
	tree := networkWifiTree(nil, services.NetworkState{WirelessEnabled: true, Tier: services.TierNM}, h, h.metrics())
	if findActionNode(tree, "network-manual-open") == nil {
		r.mu.Unlock()
		t.Fatal("manual entry is missing when there are no scanned access points")
	}
	if !h.applyNetworkControl(r, &ui.Node{Action: "network-manual-open"}) {
		r.mu.Unlock()
		t.Fatal("manual entry action was not handled")
	}
	if !h.manualEntry || h.manualSSID == nil {
		r.mu.Unlock()
		t.Fatal("manual entry form did not open")
	}
	if focused := h.focused(); focused == nil || focused.Name != "Network name (SSID)" {
		r.mu.Unlock()
		t.Fatal("manual entry did not focus the SSID field")
	}
	form := networkManualCard(h)
	if findActionNode(form, "network-manual-ssid") == nil ||
		findActionNode(form, "network-manual-security:open") == nil ||
		findActionNode(form, "network-manual-security:personal") == nil {
		r.mu.Unlock()
		t.Fatal("manual entry form is missing SSID or security controls")
	}
	if !h.applyNetworkControl(r, &ui.Node{Action: "network-manual-security:personal"}) || h.manualSecurity != services.ManualSecurityWPAPersonal {
		r.mu.Unlock()
		t.Fatal("WPA-Personal security choice was not applied")
	}
	h.manualSSID.Text = "Not scanned"
	if !h.applyNetworkControl(r, &ui.Node{Action: "network-manual-submit"}) {
		r.mu.Unlock()
		t.Fatal("manual submit action was not handled")
	}
	if h.manualEntry || h.manualSSID.Text != "Not scanned" {
		r.mu.Unlock()
		t.Fatal("manual SSID was not retained for a possible retry")
	}
	r.mu.Unlock()

	select {
	case <-backend.manualDone:
	case <-time.After(time.Second):
		t.Fatal("manual request never reached the backend")
	}
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		r.mu.Lock()
		got := h.errLabel
		r.mu.Unlock()
		if got == "activation refused" {
			break
		}
		time.Sleep(time.Millisecond)
	}
	r.mu.Lock()
	got := h.errLabel
	r.mu.Unlock()
	if got != "activation refused" {
		t.Fatalf("backend result = %q, want actionable error", got)
	}
	backend.mu.Lock()
	defer backend.mu.Unlock()
	if len(backend.manualRequests) != 1 || backend.manualRequests[0] != (services.ManualNetworkRequest{
		SSID: "Not scanned", Security: services.ManualSecurityWPAPersonal,
	}) {
		t.Fatalf("manual requests = %+v", backend.manualRequests)
	}
}

func TestNetworkPasswordSubmitRejectsInvalidPassphrase(t *testing.T) {
	h := &PanelHost{id: PanelNetwork, pendingSSID: "Hidden", password: ui.NewField("short")}
	r := &Registry{network: services.NewNetwork(&shellNetworkBackend{})}
	if !h.applyNetworkControl(r, &ui.Node{Action: "network-password-submit"}) {
		t.Fatal("password submit action was not handled")
	}
	if h.password.Text != "short" || h.pendingSSID != "Hidden" {
		t.Fatal("invalid passphrase should remain editable in the active prompt")
	}
	if h.errLabel == "" {
		t.Fatal("invalid passphrase did not show a validation message")
	}
	h.password.Text = "hunter22"
	if !h.applyNetworkControl(r, &ui.Node{Action: "network-password-submit"}) {
		t.Fatal("corrected password submit action was not handled")
	}
	if h.errLabel != "" {
		t.Fatalf("corrected passphrase retained old error label %q", h.errLabel)
	}
}

func TestNetworkManualSSIDFieldUsesRetainedEditor(t *testing.T) {
	h := &PanelHost{id: PanelNetwork, manualEntry: true, manualSSID: ui.NewField("")}
	h.root = networkManualCard(h)
	h.focus = ui.Focusables(h.root)
	h.roving.Count = len(h.focus)
	h.focusByName("Network name (SSID)")
	if !h.editField(&Registry{}, func(f *ui.Field) { f.Insert("Hidden") }) {
		t.Fatal("manual SSID field did not accept input")
	}
	if h.manualSSID.Text != "Hidden" {
		t.Fatalf("SSID field = %q, want Hidden", h.manualSSID.Text)
	}
}

func TestNetworkManualCancelClearsSSID(t *testing.T) {
	h := &PanelHost{id: PanelNetwork, manualEntry: true, manualSSID: ui.NewField("Not saved")}
	if !h.applyNetworkControl(&Registry{}, &ui.Node{Action: "network-manual-cancel"}) {
		t.Fatal("manual cancel action was not handled")
	}
	if h.manualEntry || h.manualSSID.Text != "" {
		t.Fatalf("cancel retained manual input: open=%v ssid=%q", h.manualEntry, h.manualSSID.Text)
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
	tree := networkEthernetTree(st, &PanelHost{}, standardMetrics())
	texts := collectText(tree)
	if !slices.Contains(texts, "enp7s0") {
		t.Errorf("ethernet tab must name the interface; got %v", texts)
	}
	if !hasIcon(tree, "lan") {
		t.Error("ethernet tab must carry the lan glyph")
	}
}

func TestPasswordCardMasksAndNeverLeaksToTheErrorLabel(t *testing.T) {
	h := &PanelHost{id: PanelNetwork, pendingSSID: "Orac 15A"}
	h.password = ui.NewField("hunter2")
	h.password.Masked = true
	card := networkPasswordCard(h)
	if !slices.Contains(collectText(card), "Join Orac 15A") {
		t.Error("the prompt must name the network being joined")
	}
	h.errLabel = "activation failed"
	for _, s := range collectText(card) {
		if strings.Contains(s, "hunter2") {
			t.Fatal("the passphrase reached the painted tree")
		}
	}
}

func TestPasswordFieldEditsTheCredentialHolder(t *testing.T) {
	h := &PanelHost{id: PanelNetwork, pendingSSID: "Orac 15A", password: ui.NewField("")}
	h.password.Masked = true
	h.root = networkPasswordCard(h)
	h.focus = ui.Focusables(h.root)
	h.roving.Count = len(h.focus)
	h.focusByName("Password")

	if !h.editField(&Registry{}, func(f *ui.Field) { f.Insert("secret") }) {
		t.Fatal("password field did not accept input")
	}
	if got := h.password.Text; got != "secret" {
		t.Fatalf("credential holder = %q, want secret", got)
	}
}

func TestPasswordActionsClearTheCredential(t *testing.T) {
	for _, action := range []string{"network-password-submit", "network-password-cancel"} {
		t.Run(action, func(t *testing.T) {
			h := &PanelHost{id: PanelNetwork, pendingSSID: "Orac 15A", password: ui.NewField("hunter22")}
			r := &Registry{network: services.NewNetwork(&shellNetworkBackend{})}
			if !h.applyNetworkControl(r, &ui.Node{Action: action}) {
				t.Fatalf("%s was not handled", action)
			}
			if h.password.Text != "" || h.pendingSSID != "" {
				t.Fatalf("%s retained credential state: password=%q ssid=%q", action, h.password.Text, h.pendingSSID)
			}
		})
	}
}

func TestPasswordRevealChangesMaskAndAccessibleName(t *testing.T) {
	h := &PanelHost{id: PanelNetwork, pendingSSID: "Orac 15A", password: ui.NewField("hunter2")}
	h.password.Masked = true
	if !h.applyNetworkControl(&Registry{}, &ui.Node{Action: "network-password-reveal"}) {
		t.Fatal("reveal action was not handled")
	}
	if h.password.Masked {
		t.Fatal("reveal action left the password masked")
	}
	card := networkPasswordCard(h)
	if !hasNamedNode(card, "Hide password") {
		t.Fatal("revealed card must expose a Hide password control")
	}
}

func TestNetworkPanelTeardownClearsCredential(t *testing.T) {
	r := newPanelRegistry(t)
	h := &PanelHost{
		id: PanelNetwork, stopAnim: make(chan struct{}), pendingSSID: "Orac 15A",
		password: ui.NewField("hunter22"), manualEntry: true, manualSSID: ui.NewField("Hidden"),
	}
	r.mu.Lock()
	r.panelHosts[PanelNetwork] = h
	r.teardownPanelLocked(PanelNetwork)
	r.mu.Unlock()
	if h.password.Text != "" || h.pendingSSID != "" || h.manualEntry || h.manualSSID.Text != "" {
		t.Fatalf("teardown retained network input: password=%q pending ssid=%q manual=%v/%q", h.password.Text, h.pendingSSID, h.manualEntry, h.manualSSID.Text)
	}
}

func TestNetworkRelayPrimesCachedPanelState(t *testing.T) {
	r := newPanelRegistry(t)
	b := &shellNetworkBackend{
		state: services.NetworkState{WirelessEnabled: true, SSID: "LukeAP"},
		aps:   []services.AccessPoint{{SSID: "LukeAP", Strength: 92}},
	}
	r.setNetwork(services.NewNetwork(b))

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if st := r.network.CachedState(); st.SSID == "LukeAP" && len(r.network.CachedAccessPoints()) == 1 {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("network relay did not prime state: state=%+v aps=%+v", r.network.CachedState(), r.network.CachedAccessPoints())
}

func TestRegistryCloseClosesNetworkService(t *testing.T) {
	r := newPanelRegistry(t)
	b := &shellNetworkBackend{}
	r.setNetwork(services.NewNetwork(b))

	r.Close()

	b.mu.Lock()
	closed := b.closed
	b.mu.Unlock()
	if closed == 0 {
		t.Fatal("registry shutdown left the network service open")
	}
}

func TestNetworkFiguresUseCachedMetricRates(t *testing.T) {
	st := services.NetworkState{Interface: "wlan0"}
	snap := services.Snapshot{Network: &metrics.NetworkSnapshot{Interfaces: []metrics.NetworkInterface{{
		Name: "wlan0",
		Rates: metrics.NetworkRates{
			ReceiveBytesPerSecond:  1_500_000,
			TransmitBytesPerSecond: 250_000,
			Valid:                  true,
		},
	}}}}
	texts := collectText(networkFigures(st, snap, nil, 120))
	for _, want := range []string{"1.5 MB/s", "250.0 kB/s"} {
		if !slices.Contains(texts, want) {
			t.Fatalf("network figures missing %q: %v", want, texts)
		}
	}
}

func TestNetworkPanelLeasesTheExistingMetricSource(t *testing.T) {
	r := newPanelRegistry(t)
	if err := r.OpenPanel(PanelNetwork, 7, Trigger{}); err != nil {
		t.Fatal(err)
	}
	if !r.metrics.SourceLeased(services.SourceNetwork) {
		t.Fatal("network panel did not lease the existing network-rate sampler")
	}
}

func TestNetworkPanelRequestsScanOffRegistryLock(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	defer close(release)
	b := &shellNetworkBackend{scan: func() error {
		close(started)
		<-release
		return nil
	}}
	r := newPanelRegistry(t)
	r.setNetwork(services.NewNetwork(b))
	opened := make(chan error, 1)
	go func() { opened <- r.OpenPanel(PanelNetwork, 7, Trigger{}) }()

	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("opening the network panel did not request a scan")
	}
	select {
	case err := <-opened:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("opening the panel waited for the scan under Registry.mu")
	}
}

func TestSecretPromptReturnsPanelToWifiTab(t *testing.T) {
	r := newPanelRegistry(t)
	network := services.NewNetwork(&shellNetworkBackend{})
	r.mu.Lock()
	r.network = network
	h := &PanelHost{id: PanelNetwork, output: 7, networkTab: "ethernet", stopAnim: make(chan struct{})}
	r.panelHosts[PanelNetwork] = h
	r.mu.Unlock()

	r.presentNetworkSecret(network, services.SecretRequest{SSID: "Orac 15A"})

	r.mu.Lock()
	defer r.mu.Unlock()
	if h.networkTab != "wifi" {
		t.Fatalf("secret prompt stayed on %q tab, want wifi", h.networkTab)
	}
	if h.pendingSSID != "Orac 15A" || h.password == nil || !h.password.Masked {
		t.Fatalf("secret prompt was not installed: ssid=%q password=%+v", h.pendingSSID, h.password)
	}
}

func TestExpiringSecretPromptClearsThePasswordCard(t *testing.T) {
	r := newPanelRegistry(t)
	network := services.NewNetwork(&shellNetworkBackend{})
	r.mu.Lock()
	r.network = network
	h := &PanelHost{id: PanelNetwork, output: 7, stopAnim: make(chan struct{})}
	r.panelHosts[PanelNetwork] = h
	r.mu.Unlock()

	r.presentNetworkSecret(network, services.SecretRequest{SSID: "Orac 15A"})
	r.expireNetworkSecret(network)
	// A second expiry with no card open must be a no-op, not a panic.
	r.expireNetworkSecret(network)

	r.mu.Lock()
	defer r.mu.Unlock()
	if h.pendingSSID != "" {
		t.Fatalf("expired prompt still shown: ssid=%q", h.pendingSSID)
	}
}

func hasNamedNode(n *ui.Node, name string) bool {
	if n == nil {
		return false
	}
	if n.Name == name {
		return true
	}
	for _, child := range n.Children {
		if hasNamedNode(child, name) {
			return true
		}
	}
	return false
}

func countToggles(n *ui.Node) int {
	if n == nil {
		return 0
	}
	count := 0
	if n.Kind == ui.KindToggle {
		count++
	}
	for _, c := range n.Children {
		count += countToggles(c)
	}
	return count
}

func TestTheKernelTierSaysNoManagerAnsweredNotThatTheRadioIsOff(t *testing.T) {
	st := services.NetworkState{Tier: services.TierKernel, ReadOnly: true, WirelessEnabled: true,
		Kind: services.ConnWireless, Connected: true, Interface: "wlan0", Strength: 80}
	h := &PanelHost{networkTab: "wifi"}

	card := networkHeaderCard(h, st, services.Snapshot{}, standardMetrics())
	texts := strings.Join(collectText(card), "|")
	if strings.Contains(strings.ToLower(texts), "off") {
		t.Errorf("a working radio under the kernel tier reads as off: %s", texts)
	}
	if !strings.Contains(texts, "Connected via wlan0") {
		t.Errorf("a connected link with no SSID must say what carries it: %s", texts)
	}
	if countToggles(card) != 0 {
		t.Error("the kernel tier cannot switch the radio, so it must offer no switch")
	}

	body := strings.Join(collectText(networkWifiTree(nil, st, h, standardMetrics())), "|")
	// The hint wraps, so its lines are read back as one sentence.
	if !strings.Contains(body, "NetworkManager is not running") ||
		!strings.Contains(strings.ReplaceAll(body, "|", " "), "Showing what the kernel reports. Networks can't be listed or joined without it.") {
		t.Errorf("the Wi-Fi body must name the tier and why there is no list: %s", body)
	}
	if strings.Contains(body, "No networks found") || strings.Contains(body, "Turn the radio on") {
		t.Errorf("the Wi-Fi body blames the radio or the air: %s", body)
	}
}

func TestNoTierKeepsTodaysRadioNotice(t *testing.T) {
	tree := networkWifiTree(nil, services.NetworkState{Tier: services.TierNone, ReadOnly: true}, &PanelHost{networkTab: "wifi"}, standardMetrics())
	if !strings.Contains(strings.Join(collectText(tree), "|"), "Wi-Fi is off") {
		t.Fatal("TierNone lost today's notice")
	}
}

// TestANoticeHintWrapsInsideThePanel: the kernel tier's hint is the longest a
// notice carries, and its translations are longer still. It must wrap inside
// the card rather than run past it into an ellipsis.
func TestANoticeHintWrapsInsideThePanel(t *testing.T) {
	m := standardMetrics()
	st := services.NetworkState{Tier: services.TierKernel, ReadOnly: true, WirelessEnabled: true}
	notice := networkWifiTree(nil, st, &PanelHost{networkTab: "wifi"}, m)
	measure := func(s string, _ ui.TextAttrs) (int, int) { return 8 * len([]rune(s)), 20 }
	inner := panelTargetSize(PanelNetwork).W - 2*m.PanelPadding
	h, err := ui.ContentHeight(notice, inner, measure)
	if err != nil {
		t.Fatal(err)
	}
	// A title line and a hint that wraps onto at least two more.
	if lines := (h - 2*m.CardPadding) / 20; lines < 3 {
		t.Errorf("the notice is %d high, %d lines; its hint did not wrap", h, lines)
	}
}

func TestNetworkVPNRowsUseUUIDActionsAndShowState(t *testing.T) {
	profiles := []services.VPNProfile{
		{UUID: "11111111-1111-4111-8111-111111111111", Name: "Office", Status: services.VPNConnected},
		{UUID: "22222222-2222-4222-8222-222222222222", Name: "Office", Status: services.VPNDisconnected},
	}
	tree := networkVPNTree(profiles, "", services.NetworkState{Tier: services.TierNM}, &PanelHost{}, standardMetrics())
	for _, action := range []string{
		"network-vpn-disconnect:" + profiles[0].UUID,
		"network-vpn-connect:" + profiles[1].UUID,
	} {
		if findActionNode(tree, action) == nil {
			t.Fatalf("VPN row is missing UUID action %q", action)
		}
	}
	if !slices.Contains(collectText(tree), "Office") {
		t.Fatal("VPN profile names are missing from the panel")
	}
}

func TestNetworkVPNConnectingRowOffersEnabledCancel(t *testing.T) {
	profile := services.VPNProfile{UUID: "11111111-1111-4111-8111-111111111111", Name: "Office", Status: services.VPNConnecting}
	tree := networkVPNTree([]services.VPNProfile{profile}, "", services.NetworkState{Tier: services.TierNM}, &PanelHost{}, standardMetrics())
	control := findActionNode(tree, "network-vpn-disconnect:"+profile.UUID)
	if control == nil {
		t.Fatal("connecting VPN row must offer cancellation through disconnect")
	}
	if control.State&ui.StateDisabled != 0 || control.AriaDisabled {
		t.Fatal("connecting VPN cancel control must remain enabled")
	}
	if !slices.Contains(collectText(control), "Cancel") {
		t.Fatalf("connecting VPN action = %v, want Cancel", collectText(control))
	}
}

func TestNetworkVPNControlDisplaysBackendError(t *testing.T) {
	profile := services.VPNProfile{UUID: "11111111-1111-4111-8111-111111111111", Name: "Office"}
	backend := &shellNetworkBackend{
		vpnProfiles: []services.VPNProfile{profile}, vpnDone: make(chan struct{}), vpnControlErr: errors.New("connection failed"),
	}
	r := newPanelRegistry(t)
	r.setNetwork(services.NewNetwork(backend))
	if err := r.network.RefreshVPNProfiles(); err != nil {
		t.Fatal(err)
	}
	h := &PanelHost{id: PanelNetwork, output: 7, place: Placement{Panel: panelTargetSize(PanelNetwork)}, theme: DefaultTheme(), stopAnim: make(chan struct{})}
	r.mu.Lock()
	r.panelHosts[PanelNetwork] = h
	if !h.applyNetworkControl(r, &ui.Node{Action: "network-vpn-connect:" + profile.UUID}) {
		r.mu.Unlock()
		t.Fatal("VPN connect action was not handled")
	}
	r.mu.Unlock()
	select {
	case <-backend.vpnDone:
	case <-time.After(time.Second):
		t.Fatal("VPN control never reached the backend")
	}
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		r.mu.Lock()
		got := h.errLabel
		r.mu.Unlock()
		if got == "connection failed" {
			break
		}
		time.Sleep(time.Millisecond)
	}
	r.mu.Lock()
	got := h.errLabel
	r.mu.Unlock()
	if got != "connection failed" {
		t.Fatalf("VPN result = %q, want actionable error", got)
	}
}

func TestNetworkVPNTabExplainsUnsupportedBackend(t *testing.T) {
	for _, tier := range []services.Tier{services.TierIWD, services.TierKernel, services.TierNone} {
		tree := networkVPNTree(nil, "", services.NetworkState{Tier: tier, ReadOnly: tier == services.TierKernel}, &PanelHost{}, standardMetrics())
		if findActionNode(tree, "network-vpn-connect:") != nil || findActionNode(tree, "network-vpn-disconnect:") != nil {
			t.Fatalf("tier %v exposed VPN controls", tier)
		}
		if len(collectText(tree)) == 0 {
			t.Fatalf("tier %v did not explain why VPN is unavailable", tier)
		}
	}
}
