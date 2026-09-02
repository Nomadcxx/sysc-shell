package shell

import (
	"errors"
	"github.com/Nomadcxx/sysc-shell/internal/render"
	"os/exec"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	metrics "github.com/Nomadcxx/sysc-metrics"

	"github.com/Nomadcxx/sysc-shell/internal/config"
	"github.com/Nomadcxx/sysc-shell/internal/services"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

func TestSessionActionsList(t *testing.T) {
	t.Parallel()
	_, h := newSessionHost(t, "swaylock")
	got := focusableNames(h.root)
	want := []string{"Lock", "Log out", "Suspend", "Reboot", "Power off"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("actions = %v, want %v", got, want)
	}
}

func TestLockHiddenWithoutLocker(t *testing.T) {
	t.Parallel()
	_, h := newSessionHost(t, "")
	got := focusableNames(h.root)
	want := []string{"Log out", "Suspend", "Reboot", "Power off"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("actions = %v, want %v", got, want)
	}
	for _, name := range got {
		if name == "Lock" {
			t.Fatal("Lock shown without a locker")
		}
	}
}

func TestSessionPanelTargetSizeIs420(t *testing.T) {
	t.Parallel()
	got := panelTargetSize(PanelSession)
	if got.W != 420 {
		t.Fatalf("width = %d, want 420", got.W)
	}
}

func TestSessionTreeOmitsBatteryWithoutAPresentPack(t *testing.T) {
	t.Parallel()
	_, h := newSessionHost(t, "swaylock")
	var meters []*ui.Node
	collectByKind(h.root, ui.KindMeter, &meters)
	if len(meters) != 0 {
		t.Fatal("KindMeter shown without a present battery")
	}
	if headingNamed(h.root, "Battery") {
		t.Fatal("Battery heading shown without a present pack")
	}
}

func TestSessionTreeShowsBatteryWhenPresent(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.Session.Locker = "swaylock"
	cfg.Accessibility.ReducedMotion = true
	reg := NewRegistry(cfg)
	reg.lookPath = func(string) (string, error) { return "", exec.ErrNotFound }
	t.Cleanup(reg.Close)
	reg.UpdateMetrics(services.Snapshot{Battery: &metrics.BatterySnapshot{
		Present: true, Charge: 0.84, ChargeValid: true, State: metrics.BatteryDischarging,
	}})
	if err := reg.OpenPanel(PanelSession, 7, Trigger{}); err != nil {
		t.Fatal(err)
	}
	_ = drainAux(t, reg, 2)
	h := reg.panelHosts[PanelSession]
	if !headingNamed(h.root, "Battery") {
		t.Fatal("present pack has no Battery heading")
	}
	var meters []*ui.Node
	collectByKind(h.root, ui.KindMeter, &meters)
	if len(meters) != 1 {
		t.Fatalf("meters = %d, want 1", len(meters))
	}
	if !strings.Contains(strings.Join(texts(h.root), " "), "84%") {
		t.Fatalf("tree %v has no percent text", texts(h.root))
	}
}

func TestSessionTreeOmitsProfilesWhenUnavailable(t *testing.T) {
	t.Parallel()
	reg, h := newSessionHost(t, "swaylock")
	reg.lookPath = func(string) (string, error) { return "", exec.ErrNotFound }
	reg.rebuildPanel(h)
	for _, n := range ui.Focusables(h.root) {
		if n.Role == "tab" {
			t.Fatalf("profile tab %q shown without powerprofilesctl", n.Name)
		}
	}
}

// starredPowerProfilesList is the fixture from TestParsePowerProfilesMarksTheStarredNameActive.
const starredPowerProfilesList = "" +
	"  performance:\n" +
	"    Driver:     amd_pstate\n" +
	"\n" +
	"* balanced:\n" +
	"    Driver:     amd_pstate\n" +
	"\n" +
	"  power-saver:\n" +
	"    Driver:     amd_pstate\n"

func TestOpenPanelLoadsProfileTabsFromList(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.Session.Locker = "swaylock"
	cfg.Accessibility.ReducedMotion = true
	reg := NewRegistry(cfg)
	listed := make(chan struct{})
	release := make(chan struct{})
	reg.lookPath = func(string) (string, error) { return "/usr/bin/powerprofilesctl", nil }
	reg.runArgvOutput = func([]string) (string, error) {
		close(listed)
		<-release
		return starredPowerProfilesList, nil
	}
	t.Cleanup(func() {
		select {
		case <-release:
		default:
			close(release)
		}
		reg.Close()
	})

	errc := make(chan error, 1)
	go func() { errc <- reg.OpenPanel(PanelSession, 7, Trigger{}) }()
	select {
	case <-listed:
	case <-time.After(time.Second):
		t.Fatal("runArgvOutput was not called")
	}
	select {
	case err := <-errc:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("OpenPanel held the lock across list")
	}
	_ = drainAux(t, reg, 2)
	close(release)
	waitProfileTabNames(t, reg, "Power saver", "Balanced", "Performance")
}

func TestSessionActionsRemain(t *testing.T) {
	t.Parallel()
	_, h := newSessionHost(t, "swaylock")
	got := focusableNames(h.root)
	for _, name := range []string{"Lock", "Log out", "Suspend", "Reboot", "Power off"} {
		if !slices.Contains(got, name) {
			t.Fatalf("missing %s in %v", name, got)
		}
	}
}

func TestSessionAlignsToTheTrailingEdge(t *testing.T) {
	t.Parallel()
	reg := newPanelRegistry(t)
	cb, err := reg.NewHost(7, "eDP-1")
	if err != nil {
		t.Fatal(err)
	}
	if err := cb.Configure(1536, 44, 120); err != nil {
		t.Fatal(err)
	}
	drainAuxQueue(reg)
	if err := reg.TogglePanelByName("session"); err != nil {
		t.Fatal(err)
	}
	reqs := drainAux(t, reg, 2)
	got := reqs[1].Open
	if got.MarginLeft == (1536-got.Width)/2 {
		t.Fatal("session still centred")
	}
	if got.MarginLeft == (1920-got.Width)/2 {
		t.Fatal("session still centred")
	}
}

func TestSelectingAListedProfileRunsSet(t *testing.T) {
	t.Parallel()
	reg, h := newSessionHost(t, "swaylock")
	reg.lookPath = func(string) (string, error) { return "/usr/bin/powerprofilesctl", nil }
	reg.runArgvOutput = func([]string) (string, error) { return starredPowerProfilesList, nil }
	h.profilesOK = true
	h.profiles = []string{"power-saver", "balanced", "performance"}
	h.profileActive = "balanced"
	reg.rebuildPanel(h)
	var got [][]string
	reg.runArgv = func(argv []string) error {
		got = append(got, append([]string(nil), argv...))
		return nil
	}
	activateNamed(h, reg, "Performance")
	if len(got) != 1 || !reflect.DeepEqual(got[0], []string{"powerprofilesctl", "set", "performance"}) {
		t.Fatalf("argv = %v", got)
	}
	waitProfileTabNames(t, reg, "Power saver", "Balanced", "Performance")
}

func TestASuccessfulProfileSetClearsAPriorError(t *testing.T) {
	t.Parallel()
	reg, h := newSessionHost(t, "swaylock")
	h.profilesOK = true
	h.profiles = []string{"power-saver", "balanced", "performance"}
	h.profileActive = "balanced"
	reg.rebuildPanel(h)
	reg.runArgv = func([]string) error { return errors.New("set failed") }
	activateNamed(h, reg, "Performance")
	if h.errLabel == "" {
		t.Fatal("failed set left errLabel empty")
	}
	reg.runArgv = func([]string) error { return nil }
	activateNamed(h, reg, "Performance")
	if h.errLabel != "" {
		t.Fatalf("errLabel = %q after a successful set", h.errLabel)
	}
	if hasToneError(h.root) {
		t.Fatal("ToneError text still in the tree after a successful set")
	}
}

func TestSessionExecMapping(t *testing.T) {
	t.Parallel()
	reg, h := newSessionHost(t, "swaylock")
	var got [][]string
	reg.runArgv = func(argv []string) error {
		got = append(got, append([]string(nil), argv...))
		return nil
	}
	cases := []struct {
		name string
		want []string
	}{
		{"Log out", []string{"loginctl", "terminate-session", "self"}},
		{"Suspend", []string{"loginctl", "suspend"}},
		{"Reboot", []string{"loginctl", "reboot"}},
		{"Power off", []string{"loginctl", "poweroff"}},
		{"Lock", []string{"swaylock"}},
	}
	for i, tc := range cases {
		if i > 0 {
			if err := reg.OpenPanel(PanelSession, 7, Trigger{}); err != nil {
				t.Fatal(err)
			}
			_ = drainAux(t, reg, 2)
			h = reg.panelHosts[PanelSession]
		}
		got = nil
		activateNamed(h, reg, tc.name)
		if len(got) != 1 || !reflect.DeepEqual(got[0], tc.want) {
			t.Fatalf("%s: argv = %v, want %v", tc.name, got, tc.want)
		}
		_ = drainAux(t, reg, 2) // close requests after a successful action
	}
}

func newSessionHost(t *testing.T, locker string) (*Registry, *PanelHost) {
	t.Helper()
	cfg := config.Default()
	cfg.Session.Locker = locker
	cfg.Accessibility.ReducedMotion = true
	reg := NewRegistry(cfg)
	reg.lookPath = func(string) (string, error) { return "", exec.ErrNotFound }
	t.Cleanup(reg.Close)
	if err := reg.OpenPanel(PanelSession, 7, Trigger{}); err != nil {
		t.Fatal(err)
	}
	_ = drainAux(t, reg, 2)
	return reg, reg.panelHosts[PanelSession]
}

func headingNamed(n *ui.Node, name string) bool {
	if n.Name == name && n.Role == "heading" {
		return true
	}
	for _, c := range n.Children {
		if headingNamed(c, name) {
			return true
		}
	}
	return false
}

// hasToneError reports whether an error *message* is in the tree. Reboot and
// Power off are permanently error-toned outlines, so the tone alone no longer
// distinguishes a failure; only a text node carrying it does.
func hasToneError(n *ui.Node) bool {
	if n.Kind == ui.KindText && n.Tone == ui.ToneError {
		return true
	}
	for _, c := range n.Children {
		if hasToneError(c) {
			return true
		}
	}
	return false
}

func focusableNames(root *ui.Node) []string {
	var names []string
	for _, n := range ui.Focusables(root) {
		names = append(names, n.Name)
	}
	return names
}

func activateNamed(h *PanelHost, r *Registry, name string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for i, n := range h.focus {
		if n.Name == name {
			h.roving.Set(i)
			h.activate(r)
			return
		}
	}
}

func waitProfileTabNames(t *testing.T, reg *Registry, want ...string) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	var names []string
	for {
		reg.mu.Lock()
		h := reg.panelHosts[PanelSession]
		names = nil
		if h != nil {
			names = focusableNames(h.root)
		}
		reg.mu.Unlock()
		ok := true
		for _, w := range want {
			if !slices.Contains(names, w) {
				ok = false
				break
			}
		}
		if ok && len(want) > 0 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("tabs = %v, want %v", names, want)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// --- Catalogue composition at the real panel width --------------------------

// layoutSession lays the session tree out at the real 420 px panel width, so
// every assertion below is about the panel the user actually sees.
func layoutSession(t *testing.T, reg *Registry, h *PanelHost) {
	t.Helper()
	reg.mu.Lock()
	defer reg.mu.Unlock()
	reg.rebuildPanel(h)
	size := panelTargetSize(PanelSession)
	if err := h.configure(size.W, size.H, int(ui.ScaleUnit)); err != nil {
		t.Fatal(err)
	}
	if got := h.place.Panel.W; got != 420 {
		t.Fatalf("session panel width = %d, want 420", got)
	}
}

// findNode returns the first node matching a predicate, depth first.
func findNode(n *ui.Node, match func(*ui.Node) bool) *ui.Node {
	if n == nil {
		return nil
	}
	if match(n) {
		return n
	}
	for _, c := range n.Children {
		if got := findNode(c, match); got != nil {
			return got
		}
	}
	return nil
}

func findByName(n *ui.Node, name string) *ui.Node {
	return findNode(n, func(c *ui.Node) bool { return c.Name == name })
}

func sessionWithProfiles(t *testing.T) (*Registry, *PanelHost) {
	t.Helper()
	reg, h := newSessionHost(t, "swaylock")
	reg.mu.Lock()
	h.profilesOK = true
	h.profiles = []string{"power-saver", "balanced", "performance"}
	h.profileActive = "balanced"
	reg.mu.Unlock()
	layoutSession(t, reg, h)
	return reg, h
}

func TestSessionKeepsThreeDistinctHighContainerCards(t *testing.T) {
	t.Parallel()
	reg, h := sessionWithProfiles(t)
	reg.UpdateMetrics(services.Snapshot{Battery: &metrics.BatterySnapshot{
		Present: true, ChargeValid: true, Charge: 0.42, State: metrics.BatteryDischarging,
	}})
	layoutSession(t, reg, h)

	var cards []*ui.Node
	var walk func(*ui.Node)
	walk = func(n *ui.Node) {
		if n == nil {
			return
		}
		if n.Kind == ui.KindCapsule && n.Fill == ui.FillContainerHigh {
			cards = append(cards, n)
		}
		for _, c := range n.Children {
			walk(c)
		}
	}
	walk(h.root)
	if len(cards) != 3 {
		t.Fatalf("session shows %d high-container cards, want Battery, Power profile and Session", len(cards))
	}
	for _, name := range []string{"Battery", "Power profile", "Session"} {
		if !headingNamed(h.root, name) {
			t.Errorf("card %q is missing", name)
		}
	}
}

func TestBatteryPercentageReservesRoomForFullCharge(t *testing.T) {
	t.Parallel()
	reg, h := sessionWithProfiles(t)
	reg.UpdateMetrics(services.Snapshot{Battery: &metrics.BatterySnapshot{
		Present: true, ChargeValid: true, Charge: 0.42, State: metrics.BatteryDischarging,
	}})
	layoutSession(t, reg, h)
	low := findNode(h.root, func(n *ui.Node) bool { return n.Text == "42%" })
	if low == nil {
		t.Fatal("no battery percentage in the tree")
	}
	if low.MinWidthText != "100%" {
		t.Errorf("percentage reserves %q, want 100%%", low.MinWidthText)
	}
	lowWidth := low.Bounds.W

	reg.UpdateMetrics(services.Snapshot{Battery: &metrics.BatterySnapshot{
		Present: true, ChargeValid: true, Charge: 1, State: metrics.BatteryFull,
	}})
	layoutSession(t, reg, h)
	full := findNode(h.root, func(n *ui.Node) bool { return n.Text == "100%" })
	if full == nil {
		t.Fatal("no full-charge percentage in the tree")
	}
	// Reaching three digits must not widen the figure, or the rows beside it
	// shift as the battery drains.
	if full.Bounds.W != lowWidth {
		t.Errorf("percentage width changed from %d to %d between 42%% and 100%%", lowWidth, full.Bounds.W)
	}
}

func TestProfileRowIsOneSegmentedControlThatFitsEveryLabel(t *testing.T) {
	t.Parallel()
	_, h := sessionWithProfiles(t)

	row := findNode(h.root, func(n *ui.Node) bool { return n.Kind == ui.KindSegmented })
	if row == nil {
		t.Fatal("the power profile row is not a segmented control")
	}
	if len(row.Children) != 3 {
		t.Fatalf("segmented row has %d children, want 3", len(row.Children))
	}

	selected := 0
	widths := map[int]bool{}
	for _, seg := range row.Children {
		widths[seg.Bounds.W] = true
		if seg.State.Has(ui.StateSelected) {
			selected++
			if seg.Name != "Balanced" {
				t.Errorf("selected segment is %q, want the active Balanced", seg.Name)
			}
		}
		// Every label has to fit at 420 px, Balanced included.
		label := findNode(seg, func(n *ui.Node) bool { return n.Kind == ui.KindText })
		if label == nil {
			t.Fatalf("segment %q lost its label", seg.Name)
		}
		if label.Bounds.W <= 0 || label.Bounds.X < seg.Bounds.X ||
			label.Bounds.X+label.Bounds.W > seg.Bounds.X+seg.Bounds.W {
			t.Errorf("segment %q label clips: label %+v in %+v", seg.Name, label.Bounds, seg.Bounds)
		}
	}
	if selected != 1 {
		t.Errorf("%d segments are selected, want exactly the active one", selected)
	}
	// Equal allocation: at most a one-pixel remainder separates the widths.
	if len(widths) > 2 {
		t.Errorf("segment widths are not equal: %v", widths)
	}
}

func TestSelectedProfileSwapsItsIconForACheckAndKeepsItsLabel(t *testing.T) {
	t.Parallel()
	_, h := sessionWithProfiles(t)
	row := findNode(h.root, func(n *ui.Node) bool { return n.Kind == ui.KindSegmented })

	for _, seg := range row.Children {
		icon := findNode(seg, func(n *ui.Node) bool { return n.Kind == ui.KindIcon })
		if icon == nil {
			t.Fatalf("segment %q has no icon", seg.Name)
		}
		if got := icon.IconSize; got != sessionProfileIconSize {
			t.Errorf("segment %q icon is %d px, want %d", seg.Name, got, sessionProfileIconSize)
		}
		want := sessionProfileIcon(strings.ToLower(strings.ReplaceAll(seg.Name, " ", "-")))
		if seg.State.Has(ui.StateSelected) {
			want = "check"
		}
		if icon.Icon != want {
			t.Errorf("segment %q icon = %q, want %q", seg.Name, icon.Icon, want)
		}
		// The check replaces the glyph, never the label.
		if findNode(seg, func(n *ui.Node) bool { return n.Text == seg.Name }) == nil {
			t.Errorf("segment %q lost its label", seg.Name)
		}
	}
}

func TestProfileCardIsAbsentWithoutPowerProfilesctl(t *testing.T) {
	t.Parallel()
	reg, h := newSessionHost(t, "swaylock")
	layoutSession(t, reg, h)

	if headingNamed(h.root, "Power profile") {
		t.Error("the power profile card is present without powerprofilesctl")
	}
	if findNode(h.root, func(n *ui.Node) bool { return n.Kind == ui.KindSegmented }) != nil {
		t.Error("a segmented control survived without any profiles")
	}
}

func TestSessionActionsAreFullWidthIconStadiums(t *testing.T) {
	t.Parallel()
	_, h := sessionWithProfiles(t)

	icons := map[string]string{
		"Lock": "lock", "Log out": "logout", "Suspend": "bedtime",
		"Reboot": "restart_alt", "Power off": "power_settings_new",
	}
	var width int
	for name, want := range icons {
		node := findByName(h.root, name)
		if node == nil {
			t.Fatalf("action %q is missing", name)
		}
		if node.Bounds.H != 40 {
			t.Errorf("action %q is %d px high, want 40", name, node.Bounds.H)
		}
		icon := findNode(node, func(n *ui.Node) bool { return n.Kind == ui.KindIcon })
		if icon == nil || icon.Icon != want {
			t.Errorf("action %q icon = %v, want %q", name, icon, want)
		} else if icon.IconSize != sessionActionIconSize {
			t.Errorf("action %q icon is %d px, want %d", name, icon.IconSize, sessionActionIconSize)
		}
		if !render.ValidMaterialIcon(want) {
			t.Errorf("action %q names %q, which is outside the embedded subset", name, want)
		}
		// Every action spans the same full card width.
		if width == 0 {
			width = node.Bounds.W
		} else if node.Bounds.W != width {
			t.Errorf("action %q is %d px wide, want the full %d", name, node.Bounds.W, width)
		}
	}
}

func TestLockIsAbsentWithoutALocker(t *testing.T) {
	t.Parallel()
	reg, h := newSessionHost(t, "")
	layoutSession(t, reg, h)
	if findByName(h.root, "Lock") != nil {
		t.Error("Lock is offered with no configured locker")
	}
	for _, name := range []string{"Log out", "Suspend", "Reboot", "Power off"} {
		if findByName(h.root, name) == nil {
			t.Errorf("action %q disappeared with the locker", name)
		}
	}
}

func TestRebootAndPowerOffAreErrorTonedOutlines(t *testing.T) {
	t.Parallel()
	_, h := sessionWithProfiles(t)

	for _, name := range []string{"Reboot", "Power off"} {
		node := findByName(h.root, name)
		if node == nil {
			t.Fatalf("action %q is missing", name)
		}
		if node.Fill != ui.FillOutline {
			t.Errorf("%q uses fill %d, want the outline treatment", name, node.Fill)
		}
		if node.Tone != ui.ToneError {
			t.Errorf("%q is not error-toned", name)
		}
		if node.Fill == ui.FillError {
			t.Errorf("%q is a solid red block", name)
		}
	}
	// The ordinary actions stay ordinary.
	for _, name := range []string{"Lock", "Log out", "Suspend"} {
		node := findByName(h.root, name)
		if node == nil {
			t.Fatalf("action %q is missing", name)
		}
		if node.Fill != ui.FillNone || node.Tone == ui.ToneError {
			t.Errorf("%q picked up destructive treatment: fill %d tone %d", name, node.Fill, node.Tone)
		}
	}
}

func TestSessionActionIdentityIsUnchanged(t *testing.T) {
	t.Parallel()
	_, h := sessionWithProfiles(t)

	// The action IDs and the argv they map to are contracts with the IPC alias
	// and the key binding. Recomposing the panel must not touch either.
	want := map[string][]string{
		"session-lock":     {"swaylock"},
		"session-logout":   {"loginctl", "terminate-session", "self"},
		"session-suspend":  {"loginctl", "suspend"},
		"session-reboot":   {"loginctl", "reboot"},
		"session-poweroff": {"loginctl", "poweroff"},
	}
	for id, argv := range want {
		node := findNode(h.root, func(n *ui.Node) bool { return n.Action == id })
		if node == nil {
			t.Errorf("action id %q is no longer in the tree", id)
			continue
		}
		got := sessionArgv(id, "swaylock")
		if len(got) != len(argv) {
			t.Errorf("%s argv = %v, want %v", id, got, argv)
			continue
		}
		for i := range argv {
			if got[i] != argv[i] {
				t.Errorf("%s argv = %v, want %v", id, got, argv)
				break
			}
		}
	}
}
