package shell

import (
	"errors"
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

func hasToneError(n *ui.Node) bool {
	if n.Tone == ui.ToneError {
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
