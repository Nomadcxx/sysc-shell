package shell

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	locksession "github.com/Nomadcxx/sysc-shell/internal/lock"
	"github.com/Nomadcxx/sysc-shell/internal/lockconfig"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

func openLockSettings(t *testing.T) (*Registry, *PanelHost) {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	r, _ := artRegistry(t, artWallpaperEngine{})
	h := openArtSettings(t, r)
	r.mu.Lock()
	h.section = "Lock Screen"
	r.rebuildPanel(h)
	r.mu.Unlock()
	return r, h
}

func TestLockSettingsKeepsIdlePolicySeparate(t *testing.T) {
	r, h := openLockSettings(t)
	r.mu.Lock()
	original := h.draft
	r.configPath = filepath.Join(t.TempDir(), "shell.json")
	old := []byte(`{"idle":{"lock":"1m"}}`)
	if err := os.WriteFile(r.configPath, old, 0600); err != nil {
		t.Fatal(err)
	}
	for _, action := range []string{"lockscreen-palette:dracula", "lockscreen-effect:fire", "lockscreen-reduced"} {
		if !h.lockScreenAction(r, &ui.Node{Action: action}) {
			t.Fatalf("unhandled %s", action)
		}
	}
	if !reflect.DeepEqual(h.draft, original) {
		t.Fatal("presentation wrote shell configuration")
	}
	if !h.lockScreenAction(r, &ui.Node{Action: "lockscreen-apply"}) {
		t.Fatal("unhandled Apply")
	}
	r.mu.Unlock()
	deadline := time.Now().Add(3 * time.Second)
	for {
		r.mu.Lock()
		pending, message := h.lockScreen.saving, h.lockScreen.message
		r.mu.Unlock()
		if !pending {
			if !strings.Contains(message, "next lock") {
				t.Fatalf("Apply: %s", message)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("Apply stalled")
		}
		time.Sleep(time.Millisecond)
	}
	c, err := lockconfig.Load(lockconfig.Path())
	if err != nil || c.Effect != "fire" || c.Palette != "dracula" || !c.ReducedMotion {
		t.Fatalf("locker config %+v: %v", c, err)
	}
	if data, err := os.ReadFile(r.configPath); err != nil || string(data) != string(old) {
		t.Fatal("Apply changed shell config", err)
	}
}

func TestLockSettingsPreviewHasNoCredentialOrLockPath(t *testing.T) {
	r, h := openLockSettings(t)
	r.mu.Lock()
	defer r.mu.Unlock()
	if findAction(h.root, "lockscreen-preview") == nil {
		t.Fatal("missing Preview action")
	}
	if findNode(lockScreenSettingsTree(r, h), func(n *ui.Node) bool {
		return n.Kind == ui.KindTextField || strings.HasPrefix(n.Action, "session-lock")
	}) != nil {
		t.Fatal("preview exposes credential or lock action")
	}
	img, err := lockPreview(lockconfig.Default())
	if err != nil {
		t.Fatal(err)
	}
	if img.Width != 480 || img.Height != 270 || img.Stride != img.Width*4 || len(img.Pix) != img.Stride*img.Height {
		t.Fatalf("preview shape %+v", img)
	}
	for i := 3; i < len(img.Pix); i += 4 {
		if img.Pix[i] != 255 {
			t.Fatalf("nonopaque BGRA at %d", i)
		}
	}
	h.lockScreen.image = img
	r.rebuildPanel(h)
	if findNode(h.root, func(n *ui.Node) bool { return n.Text == "Preview" }) == nil || findNode(h.root, func(n *ui.Node) bool { return n.Kind == ui.KindImage && n.Image == img }) == nil {
		t.Fatal("preview is not labeled ordinary image")
	}
}

func TestLockSettingsSleepProtectionUsesManagedOwner(t *testing.T) {
	r, h := openLockSettings(t)
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, protected := range []bool{false, true} {
		r.managedState = locksession.State{Known: true, Snapshot: locksession.Snapshot{SleepProtected: protected}}
		r.rebuildPanel(h)
		want := "Lock before sleep: Unavailable"
		if protected {
			want = "Lock before sleep: Protected"
		}
		if findNode(h.root, func(n *ui.Node) bool { return n.Text == want }) == nil {
			t.Fatalf("missing %q", want)
		}
	}
	r.managedState.Known = false
	r.rebuildPanel(h)
	if findNode(h.root, func(n *ui.Node) bool { return n.Text == "Lock before sleep: Protected" }) != nil {
		t.Fatal("disconnected owner reported protected")
	}
}

func TestLockSettingsRejectsInvalidChoicesAndStalePreview(t *testing.T) {
	r, h := openLockSettings(t)
	r.mu.Lock()
	before := h.lockScreen.config
	if !h.lockScreenAction(r, &ui.Node{Action: "lockscreen-effect:fire-text"}) || h.lockScreen.config != before {
		t.Fatal("accepted artwork-dependent effect")
	}
	h.lockScreenAction(r, &ui.Node{Action: "lockscreen-preview"})
	// The owner still holds the mutex, so completion cannot publish before this edit.
	h.lockScreenAction(r, &ui.Node{Action: "lockscreen-reduced"})
	r.mu.Unlock()
	deadline := time.Now().Add(3 * time.Second)
	for {
		r.mu.Lock()
		pending, image := h.lockScreen.previewing, h.lockScreen.image
		r.mu.Unlock()
		if !pending {
			if image != nil {
				t.Fatal("published preview from old settings")
			}
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("preview stalled")
		}
		time.Sleep(time.Millisecond)
	}
}

func TestLockSettingsApplyRespectsRegistrySaveGuard(t *testing.T) {
	r, h := openLockSettings(t)
	r.mu.Lock()
	defer r.mu.Unlock()
	r.lockSettingsSaving = true
	before := h.lockScreen.config
	h.lockScreenAction(r, &ui.Node{Action: "lockscreen-reduced"})
	h.lockScreenAction(r, &ui.Node{Action: "lockscreen-apply"})
	if h.lockScreen.config != before || h.lockScreen.saving {
		t.Fatal("overlapped another settings save")
	}
	root := lockScreenSettingsTree(r, h)
	for _, action := range []string{"lockscreen-apply", "lockscreen-preview", "lockscreen-reduced", "lockscreen-menu:effect", "lockscreen-menu:palette"} {
		n := findAction(root, action)
		if n == nil || !n.State.Has(ui.StateDisabled) || !n.AriaDisabled || n.Focusable {
			t.Fatalf("busy control %s lacks disabled accessibility state", action)
		}
	}
}

func TestLockSettingsExplainsHeldBackground(t *testing.T) {
	r, h := openLockSettings(t)
	r.mu.Lock()
	defer r.mu.Unlock()
	r.backgroundError = "the last lock session did not finish"
	r.rebuildPanel(h)
	node := findNode(h.root, func(n *ui.Node) bool {
		return n.Tone == ui.ToneError && n.Text == "Wallpaper and screensaver are held: the last lock session did not finish."
	})
	if node == nil {
		t.Fatal("held background was not explained in the Lock Screen section")
	}
}
