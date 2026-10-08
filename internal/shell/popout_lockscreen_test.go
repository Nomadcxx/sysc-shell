package shell

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"image"
	"image/png"
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
	installLockPreviewHelper(t, "")
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
	ctx, cancel := context.WithTimeout(context.Background(), lockPreviewTimeout)
	defer cancel()
	img, err := lockPreview(ctx, lockconfig.Default())
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
	if findNode(h.root, func(n *ui.Node) bool { return n.Text == "Still preview" }) == nil || findNode(h.root, func(n *ui.Node) bool { return n.Kind == ui.KindImage && n.Image == img }) == nil {
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

func TestLockSettingsReenablesPreviewWhenTheNativeRequestEnds(t *testing.T) {
	installLockPreviewHelper(t, "")
	r, h := openLockSettings(t)
	r.mu.Lock()
	h.lockScreen.config.ReducedMotion = true
	h.lockScreenAction(r, &ui.Node{Action: "lockscreen-preview"})
	if n := findAction(h.root, "lockscreen-preview"); n == nil || !n.State.Has(ui.StateDisabled) {
		r.mu.Unlock()
		t.Fatal("Preview is not disabled while the preview runs")
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
		t.Fatal("Preview row vanished when the preview ended")
	}
	if preview.State.Has(ui.StateDisabled) || preview.AriaDisabled || !preview.Focusable {
		t.Fatalf("Preview still disabled after the preview ended: %+v", preview)
	}
	// The roving index walks the focusable list, so a stale count keeps
	// keyboard focus off Preview.
	if n := slices.IndexFunc(h.focus, func(n *ui.Node) bool { return n.Action == "lockscreen-preview" }); n < 0 {
		t.Fatal("Preview is not in the roving focus list after the preview ended")
	}
}

func TestLockSettingsRejectsInvalidChoicesAndStalePreview(t *testing.T) {
	installLockPreviewHelper(t, "")
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

// This subprocess replaces only the installed command during the native PNG
// contract tests. It never connects to a session bus or Wayland display.
func TestLockPreviewCommandHelper(t *testing.T) {
	if os.Getenv("SYSC_SHELL_PREVIEW_HELPER") != "1" {
		return
	}
	if os.Args[len(os.Args)-1] != "--preview" {
		os.Exit(2)
	}
	data, err := os.ReadFile("/dev/stdin")
	if err != nil {
		os.Exit(2)
	}
	var request struct {
		Config        lockconfig.Config `json:"config"`
		Width, Height int
	}
	if json.Unmarshal(data, &request) != nil {
		os.Exit(2)
	}
	if path := os.Getenv("SYSC_SHELL_PREVIEW_REQUEST"); path != "" {
		if os.WriteFile(path, data, 0600) != nil {
			os.Exit(2)
		}
	}
	switch os.Getenv("SYSC_SHELL_PREVIEW_MODE") {
	case "fail":
		os.Exit(2)
	case "wait":
		time.Sleep(time.Minute)
		os.Exit(2)
	case "overflow":
		os.Stdout.Write(bytes.Repeat([]byte{'x'}, 5<<20))
		os.Exit(0)
	case "malformed":
		os.Stdout.Write([]byte("not a PNG"))
		os.Exit(0)
	case "dimensions":
		request.Width, request.Height = 2, 2
	}
	img := image.NewRGBA(image.Rect(0, 0, request.Width, request.Height))
	for i := 0; i < len(img.Pix); i += 4 {
		img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3] = 30, 20, 9, 255
	}
	if png.Encode(os.Stdout, img) != nil {
		os.Exit(2)
	}
	os.Exit(0)
}

func installLockPreviewHelper(t *testing.T, mode string) string {
	t.Helper()
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	script := "#!/bin/sh\nexec '" + strings.ReplaceAll(binary, "'", "'\"'\"'") + "' -test.run=^TestLockPreviewCommandHelper$ -- \"$@\"\n"
	if err := os.WriteFile(filepath.Join(dir, "sysc-lock"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	t.Setenv("SYSC_SHELL_PREVIEW_HELPER", "1")
	t.Setenv("SYSC_SHELL_PREVIEW_MODE", mode)
	request := filepath.Join(dir, "request.json")
	t.Setenv("SYSC_SHELL_PREVIEW_REQUEST", request)
	return request
}

func waitLockPreview(t *testing.T, r *Registry, h *PanelHost) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		r.mu.Lock()
		pending := h.lockScreen.previewing
		r.mu.Unlock()
		if !pending {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("preview did not finish")
		}
		time.Sleep(time.Millisecond)
	}
}

func TestLockSettingsNativeStillPreview(t *testing.T) {
	requestPath := installLockPreviewHelper(t, "")
	r, h := openLockSettings(t)
	r.mu.Lock()
	h.lockScreen.config = lockconfig.Config{Effect: "fire", Palette: "eldritch", ClockStyle: "plain", Clock24h: true, ReducedMotion: true, EffectFPS: 30}
	h.lockScreenAction(r, &ui.Node{Action: "lockscreen-preview"})
	r.mu.Unlock()
	waitLockPreview(t, r, h)
	data, err := os.ReadFile(requestPath)
	if err != nil {
		t.Fatalf("preview did not invoke native --preview command: %v", err)
	}
	var request struct {
		Config        lockconfig.Config `json:"config"`
		Width, Height int
	}
	if err := json.Unmarshal(data, &request); err != nil {
		t.Fatal(err)
	}
	if request.Width != 960 || request.Height != 540 || request.Config != h.lockScreen.config {
		t.Fatalf("preview request = %+v", request)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	img := h.lockScreen.image
	if img == nil || img.Width != 480 || img.Height != 270 || !bytes.Equal(img.Pix[:4], []byte{9, 20, 30, 255}) {
		t.Fatal("native PNG was not fitted to a 480x270 BGRA preview")
	}
	if findNode(h.root, func(n *ui.Node) bool { return n.Text == "Still preview" }) == nil {
		t.Fatal("native preview is not explicitly labeled still")
	}
	if n := findAction(h.root, "lockscreen-preview"); n == nil || !n.Focusable || n.AriaDisabled || n.State.Has(ui.StateDisabled) {
		t.Fatal("Preview did not reenable")
	}
}

func TestLockSettingsSelectsNone(t *testing.T) {
	r, h := openLockSettings(t)
	r.mu.Lock()
	defer r.mu.Unlock()
	h.lockScreenAction(r, &ui.Node{Action: "lockscreen-menu:effect"})
	list := findNode(h.root, func(n *ui.Node) bool { return n.Kind == ui.KindVirtualList })
	if list == nil || findAction(list.Item(0), "lockscreen-effect:none") == nil {
		t.Fatal("none is missing from effect picker")
	}
	h.lockScreenAction(r, &ui.Node{Action: "lockscreen-effect:none"})
	if h.lockScreen.config.Effect != "none" || h.lockScreen.message != "" {
		t.Fatal("none was rejected")
	}
}

func TestLockPreviewRejectsUntrustedCommandOutput(t *testing.T) {
	for _, mode := range []string{"fail", "overflow", "malformed", "dimensions"} {
		t.Run(mode, func(t *testing.T) {
			installLockPreviewHelper(t, mode)
			ctx, cancel := context.WithTimeout(context.Background(), lockPreviewTimeout)
			defer cancel()
			if img, err := lockPreview(ctx, lockconfig.Default()); err == nil || img != nil {
				t.Fatal("accepted invalid preview output")
			}
		})
	}
}

func TestLockPreviewCancellationStopsTheCommand(t *testing.T) {
	path := installLockPreviewHelper(t, "wait")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { _, err := lockPreview(ctx, lockconfig.Default()); done <- err }()
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := os.Stat(path); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("preview command did not start")
		}
		time.Sleep(time.Millisecond)
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("cancelled preview: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("cancelled command was not reaped")
	}
}

func TestLockSettingsPreviewFailureReenablesTheControl(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	r, h := openLockSettings(t)
	r.mu.Lock()
	h.lockScreenAction(r, &ui.Node{Action: "lockscreen-preview"})
	r.mu.Unlock()
	waitLockPreview(t, r, h)
	r.mu.Lock()
	defer r.mu.Unlock()
	if h.lockScreen.message == "" {
		t.Fatal("missing preview failure status")
	}
	if n := findAction(h.root, "lockscreen-preview"); n == nil || n.AriaDisabled || n.State.Has(ui.StateDisabled) || !n.Focusable {
		t.Fatal("failed preview left control disabled")
	}
}

func TestLockSettingsStalePreviewCannotCompleteANewerRequest(t *testing.T) {
	r, h := openLockSettings(t)
	r.mu.Lock()
	h.lockScreen.previewSequence = 2
	h.lockScreen.previewing = true
	cancelled := false
	h.lockScreen.previewCancel = func() { cancelled = true }
	h.lockScreen.message = "new request"
	r.mu.Unlock()
	r.finishLockPreview(h, 1, &ui.Image{}, errors.New("old request failed"))
	r.mu.Lock()
	defer r.mu.Unlock()
	if !h.lockScreen.previewing || h.lockScreen.previewCancel == nil || h.lockScreen.message != "new request" || h.lockScreen.image != nil || cancelled {
		t.Fatal("stale completion mutated newer request")
	}
	h.lockScreen.stopPreview()
	if !cancelled {
		t.Fatal("draft change did not cancel current request")
	}
}

func TestLockSettingsClosedPreviewDoesNotPublish(t *testing.T) {
	r, h := openLockSettings(t)
	r.mu.Lock()
	h.lockScreen.previewSequence = 1
	h.lockScreen.previewing = true
	delete(r.panelHosts, PanelSettings)
	r.mu.Unlock()
	r.finishLockPreview(h, 1, &ui.Image{}, nil)
	r.mu.Lock()
	defer r.mu.Unlock()
	if h.lockScreen.image != nil {
		t.Fatal("closed panel received preview")
	}
}

func TestLockPreviewDeadline(t *testing.T) {
	installLockPreviewHelper(t, "wait")
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if img, err := lockPreview(ctx, lockconfig.Default()); !errors.Is(err, context.DeadlineExceeded) || img != nil {
		t.Fatalf("deadline: image=%v error=%v", img, err)
	}
}

func TestLockPreviewInstalledComposition(t *testing.T) {
	binary := os.Getenv("SYSC_LOCK_CONTRACT_BINARY")
	if binary == "" {
		t.Skip("set SYSC_LOCK_CONTRACT_BINARY to check native PNG presentation")
	}
	dir := t.TempDir()
	if err := os.Symlink(binary, filepath.Join(dir, "sysc-lock")); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	c := lockconfig.Default()
	ctx, cancel := context.WithTimeout(context.Background(), lockPreviewTimeout)
	defer cancel()
	first, err := lockPreview(ctx, c)
	if err != nil {
		t.Fatal(err)
	}
	c.ClockStyle = "plain"
	second, err := lockPreview(ctx, c)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(first.Pix, second.Pix) {
		t.Fatal("clock style did not change the native composition")
	}
}
