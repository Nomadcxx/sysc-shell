package shell

import (
	"os"
	"path/filepath"
	"reflect"
	"slices"
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
	img := newLockPreviewImage()
	stop := make(chan struct{})
	defer close(stop)
	if err := lockPreviewLoop(lockconfig.Default(), 1, time.Millisecond, stop, func(frame *ui.Image) bool {
		copy(img.Pix, frame.Pix)
		return true
	}); err != nil {
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

func TestLockSettingsReenablesPreviewWhenTheReelEnds(t *testing.T) {
	r, h := openLockSettings(t)
	r.mu.Lock()
	// One held frame, so the reel ends in milliseconds instead of eight
	// seconds; the completion path is the same either way.
	h.lockScreen.config.ReducedMotion = true
	h.lockScreenAction(r, &ui.Node{Action: "lockscreen-preview"})
	if n := findAction(h.root, "lockscreen-preview"); n == nil || !n.State.Has(ui.StateDisabled) {
		r.mu.Unlock()
		t.Fatal("Preview is not disabled while the reel runs")
	}
	r.mu.Unlock()
	deadline := time.Now().Add(3 * time.Second)
	for {
		r.mu.Lock()
		pending := h.lockScreen.previewing
		r.mu.Unlock()
		if !pending {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("preview stalled")
		}
		time.Sleep(time.Millisecond)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	preview := findAction(h.root, "lockscreen-preview")
	if preview == nil {
		t.Fatal("Preview row vanished when the reel ended")
	}
	if preview.State.Has(ui.StateDisabled) || preview.AriaDisabled || !preview.Focusable {
		t.Fatalf("Preview still disabled after the reel ended: %+v", preview)
	}
	// The roving index walks the focusable list, so a stale count keeps
	// keyboard focus off Preview.
	if n := slices.IndexFunc(h.focus, func(n *ui.Node) bool { return n.Action == "lockscreen-preview" }); n < 0 {
		t.Fatal("Preview is not in the roving focus list after the reel ended")
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
	for _, action := range []string{"lockscreen-apply", "lockscreen-preview", "lockscreen-reduced", "lockscreen-menu:effect", "lockscreen-menu:palette", "lockscreen-menu:backend", "lockscreen-menu:fps", "lockscreen-clock24", "lockscreen-blur", "lockscreen-gpu-save"} {
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

func TestLockSettingsReducedMotionIsASplitRowToggle(t *testing.T) {
	r, h := openLockSettings(t)
	r.mu.Lock()
	row := findNode(h.root, func(n *ui.Node) bool {
		if n.Kind != ui.KindRow {
			return false
		}
		for _, c := range n.Children {
			if c.Kind == ui.KindToggle && c.Action == "lockscreen-reduced" {
				return true
			}
		}
		return false
	})
	r.mu.Unlock()
	if row == nil {
		t.Fatal("reduced motion is a full-width button, not a toggle in a split row")
	}
	labelled := false
	for _, c := range row.Children {
		if c.Kind == ui.KindText && c.Text == "Reduced motion" {
			labelled = true
		}
	}
	if !labelled {
		t.Fatal("split row has no Reduced motion label beside the toggle")
	}
	if findNode(h.root, func(n *ui.Node) bool { return n.Text == "Reduced motion: Off" }) != nil {
		t.Fatal("baked value text still present")
	}
}

func TestLockPreviewLoopAnimatesUnlessReducedMotion(t *testing.T) {
	frames := make(chan *ui.Image, 4)
	stop := make(chan struct{})
	defer close(stop)
	done := make(chan struct{})
	go func() {
		defer close(done)
		if err := lockPreviewLoop(lockconfig.Config{Effect: "rain", Palette: "nord"}, 3, time.Millisecond, stop, func(img *ui.Image) bool {
			// The loop owns one buffer, so keep our own copy of each frame.
			keep := &ui.Image{Width: img.Width, Height: img.Height, Stride: img.Stride, Pix: append([]byte(nil), img.Pix...)}
			frames <- keep
			return true
		}); err != nil {
			t.Errorf("loop: %v", err)
		}
	}()
	var got []*ui.Image
	for len(got) < 3 {
		select {
		case img := <-frames:
			got = append(got, img)
		case <-done:
			t.Fatalf("loop ended after %d frames", len(got))
		case <-time.After(5 * time.Second):
			t.Fatalf("only %d frames arrived", len(got))
		}
	}
	if string(got[0].Pix) == string(got[1].Pix) {
		t.Fatal("preview never advanced: two frames are identical")
	}

	held := 0
	quietStop := make(chan struct{})
	if err := lockPreviewLoop(lockconfig.Config{Effect: "rain", Palette: "nord", ReducedMotion: true}, 4, time.Millisecond, quietStop, func(*ui.Image) bool {
		held++
		return true
	}); err != nil {
		t.Fatalf("reduced loop: %v", err)
	}
	if held != 1 {
		t.Fatalf("reduced motion published %d frames, want one held still", held)
	}
}

func TestLockPreviewLoopStopsWhenTold(t *testing.T) {
	closed := make(chan struct{})
	close(closed)
	published := 0
	if err := lockPreviewLoop(lockconfig.Config{Effect: "rain", Palette: "nord"}, 100, time.Millisecond, closed, func(*ui.Image) bool {
		published++
		return true
	}); err != nil {
		t.Fatalf("stopped loop: %v", err)
	}
	if published != 0 {
		t.Fatalf("published %d frames after stop", published)
	}

	stop := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = lockPreviewLoop(lockconfig.Config{Effect: "rain", Palette: "nord"}, 100, time.Millisecond, stop, func(*ui.Image) bool { return false })
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("loop kept running after the panel said it was stale")
	}
}

func TestLockSettingsShowsNewPresentationRows(t *testing.T) {
	r, h := openLockSettings(t)
	r.mu.Lock()
	defer r.mu.Unlock()
	for action, label := range map[string]string{
		"lockscreen-clock24":  "24-hour clock",
		"lockscreen-blur":     "Blur backdrop",
		"lockscreen-gpu-save": "GPU power save",
	} {
		row := findNode(h.root, func(n *ui.Node) bool {
			if n.Kind != ui.KindRow {
				return false
			}
			for _, c := range n.Children {
				if c.Kind == ui.KindToggle && c.Action == action {
					return true
				}
			}
			return false
		})
		if row == nil {
			t.Fatalf("missing toggle row %s", action)
		}
		labelled := false
		for _, c := range row.Children {
			if c.Kind == ui.KindText && c.Text == label {
				labelled = true
			}
		}
		if !labelled {
			t.Fatalf("row for %s lacks label %q", action, label)
		}
	}
	for _, action := range []string{"lockscreen-menu:backend", "lockscreen-menu:fps", "lockscreen-menu:clockstyle"} {
		if findAction(h.root, action) == nil {
			t.Fatalf("missing %s dropdown", action)
		}
	}
}

func TestLockSettingsEditsGpuAndPresentationKeys(t *testing.T) {
	r, h := openLockSettings(t)
	r.mu.Lock()
	defer r.mu.Unlock()
	if !h.lockScreenAction(r, &ui.Node{Action: "lockscreen-menu:backend"}) || h.lockScreen.menu != "backend" {
		t.Fatal("backend menu did not open")
	}
	if !h.lockScreenAction(r, &ui.Node{Action: "lockscreen-backend:gpu"}) || h.lockScreen.config.Backend() != "gpu" {
		t.Fatalf("backend not drafted: %+v", h.lockScreen.config)
	}
	h.lockScreenAction(r, &ui.Node{Action: "lockscreen-menu:fps"})
	if !h.lockScreenAction(r, &ui.Node{Action: "lockscreen-fps:30"}) || h.lockScreen.config.EffectFPS != 30 {
		t.Fatal("fps choice not drafted")
	}
	before := h.lockScreen.config
	if !h.lockScreenAction(r, &ui.Node{Action: "lockscreen-fps:nope"}) || h.lockScreen.config != before {
		t.Fatal("invalid fps accepted")
	}
	if h.lockScreen.message == "" {
		t.Fatal("invalid fps left no message")
	}
	if !h.lockScreenAction(r, &ui.Node{Action: "lockscreen-backend:text"}) || h.lockScreen.config.Backend() != "gpu" {
		t.Fatal("invalid backend accepted")
	}
	for _, action := range []string{"lockscreen-clock24", "lockscreen-blur", "lockscreen-gpu-save"} {
		before := h.lockScreen.config
		if !h.lockScreenAction(r, &ui.Node{Action: action}) || h.lockScreen.config == before {
			t.Fatalf("%s did not change the draft", action)
		}
	}
	if !h.lockScreen.config.Clock24h || h.lockScreen.config.BlurBackdrop() || h.lockScreen.config.GpuPowerSave() {
		t.Fatalf("toggle values wrong: %+v", h.lockScreen.config)
	}
	if h.lockScreenAction(r, &ui.Node{Action: "lockscreen-zzz"}) {
		t.Fatal("unknown action handled")
	}
}

func TestLockSettingsEditsClockStyle(t *testing.T) {
	r, h := openLockSettings(t)
	r.mu.Lock()
	defer r.mu.Unlock()
	if h.lockScreen.config.ClockStyle != lockconfig.DefaultClockStyle {
		t.Fatalf("draft style = %q", h.lockScreen.config.ClockStyle)
	}
	if !h.lockScreenAction(r, &ui.Node{Action: "lockscreen-menu:clockstyle"}) || h.lockScreen.menu != "clockstyle" {
		t.Fatal("clock style menu did not open")
	}
	if !h.lockScreenAction(r, &ui.Node{Action: "lockscreen-clockstyle:phm_slanted"}) || h.lockScreen.config.ClockStyle != "phm_slanted" {
		t.Fatalf("clock style not drafted: %+v", h.lockScreen.config)
	}
	before := h.lockScreen.config
	if !h.lockScreenAction(r, &ui.Node{Action: "lockscreen-clockstyle:nope"}) || h.lockScreen.config != before {
		t.Fatal("invalid clock style accepted")
	}
	if h.lockScreen.message == "" {
		t.Fatal("invalid clock style left no message")
	}
}
