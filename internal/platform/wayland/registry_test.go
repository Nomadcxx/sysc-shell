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
