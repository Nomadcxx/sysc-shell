package wayland

import "testing"

func TestScreencopyIsKnownButNotRequired(t *testing.T) {
	t.Parallel()
	// Known, so addGlobal records it and the manager can bind.
	if _, ok := bindVersion("zwlr_screencopy_manager_v1", 3); !ok {
		t.Fatal("screencopy is not a known interface; addGlobal will drop it")
	}
	// Not required, so a compositor without it still starts. Blur is
	// decoration: its absence must degrade to an opaque panel, never to a
	// refusal to run.
	for _, iface := range requiredSingletons {
		if iface == "zwlr_screencopy_manager_v1" {
			t.Fatal("screencopy is in requiredSingletons; a compositor without it would fail to start")
		}
	}
}

func TestScreencopyBindsAtOurMaximum(t *testing.T) {
	t.Parallel()
	// The server may offer a higher version later; we bind what we generated.
	if got, _ := bindVersion("zwlr_screencopy_manager_v1", 9); got != 3 {
		t.Errorf("bind version = %d, want 3", got)
	}
	if got, _ := bindVersion("zwlr_screencopy_manager_v1", 2); got != 2 {
		t.Errorf("server below our maximum should bind at the server version, got %d", got)
	}
}

func TestBackgroundEffectIsKnownButNotRequired(t *testing.T) {
	t.Parallel()
	if got, ok := bindVersion("ext_background_effect_manager_v1", 4); !ok || got != 1 {
		t.Fatalf("bind version = %d/%v, want 1 and known", got, ok)
	}
	// Frost degrades to a solid bar; a compositor before Niri 26.04 must
	// still start.
	for _, iface := range requiredSingletons {
		if iface == "ext_background_effect_manager_v1" {
			t.Fatal("ext-background-effect is required; a compositor without it would fail to start")
		}
	}
}

// Every global the owner looks up must be in interfaceMaximum, or addGlobal
// never records it and the lookup silently finds nothing. The data device
// manager was missing for this reason: the shell's copy, paste and screenshot
// clipboard never bound on any compositor.
func TestDataDeviceManagerIsKnownButNotRequired(t *testing.T) {
	t.Parallel()
	rs := newRegistryState()
	if got, ok := rs.addGlobal(9, "wl_data_device_manager", 3); !ok || got != 3 {
		t.Fatalf("addGlobal = %d/%v, want 3 and recorded", got, ok)
	}
	if _, ok := rs.singletons["wl_data_device_manager"]; !ok {
		t.Fatal("the data device manager was not recorded, so bindSelection finds nothing")
	}
	for _, iface := range requiredSingletons {
		if iface == "wl_data_device_manager" {
			t.Fatal("a compositor without a clipboard must still start")
		}
	}
}

func TestKeyboardShortcutsInhibitIsKnownButNotRequired(t *testing.T) {
	t.Parallel()
	const iface = "zwp_keyboard_shortcuts_inhibit_manager_v1"
	if got, ok := bindVersion(iface, 9); !ok || got != 1 {
		t.Fatalf("bindVersion = %d/%v, want 1 and known; addGlobal would drop it", got, ok)
	}
	rs := newRegistryState()
	if got, ok := rs.addGlobal(9, iface, 9); !ok || got != 1 {
		t.Fatalf("addGlobal = %d/%v, want 1 and recorded", got, ok)
	}
	if _, ok := rs.singletons[iface]; !ok {
		t.Fatal("the inhibitor manager was not recorded, so the selector cannot inhibit")
	}
	for _, name := range requiredSingletons {
		if name == iface {
			t.Fatal("a compositor without the inhibitor manager must still start")
		}
	}
}

func TestXDGActivationIsKnownButNotRequired(t *testing.T) {
	t.Parallel()
	const iface = "xdg_activation_v1"
	if got, ok := bindVersion(iface, 9); !ok || got != 1 {
		t.Fatalf("bindVersion = %d/%v, want 1 and known", got, ok)
	}
	rs := newRegistryState()
	if got, ok := rs.addGlobal(10, iface, 1); !ok || got != 1 {
		t.Fatalf("addGlobal = %d/%v, want 1 and recorded", got, ok)
	}
	if _, ok := rs.singletons[iface]; !ok {
		t.Fatal("activation manager was not recorded")
	}
	for _, name := range requiredSingletons {
		if name == iface {
			t.Fatal("a compositor without activation tokens must still start")
		}
	}
}

// A compositor that runs a seat for injected input advertises a second
// wl_seat. The shell must keep the user's, which is advertised first, and a
// removal of the other must not forget the bound one.
func TestSingletonKeepsTheFirstAdvertisedGlobal(t *testing.T) {
	t.Parallel()
	type step struct {
		remove bool
		global uint32
	}
	for _, tc := range []struct {
		name  string
		steps []step
		want  uint32 // 0: no wl_seat recorded
	}{
		{"one seat", []step{{global: 3}}, 3},
		{"a second seat does not replace the first", []step{{global: 3}, {global: 40}}, 3},
		{"removing the second keeps the first", []step{{global: 3}, {global: 40}, {remove: true, global: 40}}, 3},
		{"removing the first forgets it", []step{{global: 3}, {remove: true, global: 3}}, 0},
		{"a seat advertised after the first left is recorded", []step{{global: 3}, {remove: true, global: 3}, {global: 41}}, 41},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			rs := newRegistryState()
			for _, s := range tc.steps {
				if s.remove {
					rs.removeGlobal(s.global)
					continue
				}
				if _, ok := rs.addGlobal(s.global, "wl_seat", 7); !ok {
					t.Fatalf("addGlobal(%d) dropped wl_seat", s.global)
				}
			}
			got := rs.singletons["wl_seat"].global
			if _, ok := rs.singletons["wl_seat"]; !ok {
				got = 0
			}
			if got != tc.want {
				t.Fatalf("recorded wl_seat global = %d, want %d", got, tc.want)
			}
		})
	}
}
