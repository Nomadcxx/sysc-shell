package shell

import (
	"reflect"
	"testing"

	"github.com/Nomadcxx/sysc-shell/internal/config"
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
	t.Cleanup(reg.Close)
	if err := reg.OpenPanel(PanelSession, 7, Trigger{}); err != nil {
		t.Fatal(err)
	}
	_ = drainAux(t, reg, 2)
	return reg, reg.panelHosts[PanelSession]
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
