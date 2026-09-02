package shell

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Nomadcxx/sysc-shell/internal/config"
	"github.com/Nomadcxx/sysc-shell/internal/platform/niri"
	"github.com/Nomadcxx/sysc-shell/internal/platform/wayland"
	"github.com/Nomadcxx/sysc-shell/internal/plugin"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
	v1 "github.com/Nomadcxx/sysc-shell/plugin/v1"
	"github.com/Nomadcxx/sysc-shell/plugins/reference/recorder"
)

func TestMain(m *testing.M) {
	if len(os.Args) > 1 && os.Args[1] == plugin.HelperFlag {
		os.Exit(plugin.HelperServe(os.Args[2:]))
	}
	os.Exit(m.Run())
}

const testTimerManifest = `{
  "schema": 1,
  "id": "org.sysc.timer",
  "name": "Timer",
  "version": "1.0.0",
  "protocol": {"major": 1, "minor": 0},
  "exec": "bin/sysc-plugin-timer",
  "capabilities": ["notifications", "panels", "settings", "state"],
  "requires": {"commands": []},
  "services": [{"id": "timer"}],
  "widgets": [{"id": "bar", "settings": []}],
  "panels": [{"id": "panel", "width": 320, "height": 280, "placement": "attached"}],
  "settings": []
}`

func pluginConfig(root string) config.Config {
	cfg := config.Default()
	cfg.Accessibility.ReducedMotion = true
	cfg.Plugins.Enabled = []string{"org.sysc.timer"}
	cfg.Bar.Left = nil
	cfg.Bar.Center = []config.Item{
		{ID: "clock", Format: "15:04", Boundary: time.Minute},
	}
	cfg.Bar.Right = []config.Item{{
		ID: "plugin", Plugin: "org.sysc.timer", Entry: "bar", Instance: "timer-1",
	}}
	return cfg
}

func bindTestPlugin(t *testing.T, mode string) *Registry {
	t.Helper()
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	dir := filepath.Join(root, "org.sysc.timer")
	if _, err := plugin.WriteHelperPlugin(dir, self, mode, testTimerManifest); err != nil {
		t.Fatal(err)
	}
	reg := NewRegistry(pluginConfig(root))
	t.Cleanup(reg.Close)
	if err := reg.BindPlugins(PluginHostOptions{
		Roots:    []plugin.Root{{Path: root, Source: plugin.SourceUser}},
		StateDir: t.TempDir(),
	}); err != nil {
		t.Fatal(err)
	}
	return reg
}

func waitPluginText(t *testing.T, bar *Bar, want string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if strings.Contains(pluginWidgetText(bar), want) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("plugin text %q never contained %q", pluginWidgetText(bar), want)
}

func pluginWidgetText(bar *Bar) string {
	bar.mu.Lock()
	defer bar.mu.Unlock()
	var b strings.Builder
	for _, sec := range bar.widgets() {
		for _, w := range sec {
			dumpText(w.inner, &b)
		}
	}
	return b.String()
}

func dumpText(n *ui.Node, b *strings.Builder) {
	if n == nil {
		return
	}
	b.WriteString(n.Text)
	for _, c := range n.Children {
		dumpText(c, b)
	}
}

func pluginHitPoint(bar *Bar) (action string, x, y float64) {
	bar.mu.Lock()
	defer bar.mu.Unlock()
	var found *ui.Node
	var walk func(*ui.Node)
	walk = func(n *ui.Node) {
		if n == nil || found != nil {
			return
		}
		if _, ok := parsePluginAction(n.Action); ok {
			found = n
			return
		}
		for _, c := range n.Children {
			walk(c)
		}
	}
	for _, sec := range bar.sections() {
		for _, n := range sec {
			walk(n)
		}
	}
	if found == nil || found.Bounds.W == 0 {
		return "", 0, 0
	}
	return found.Action, float64(found.Bounds.X + found.Bounds.W/2), float64(found.Bounds.Y + found.Bounds.H/2)
}

func pluginWidget(bar *Bar) textWidget {
	bar.mu.Lock()
	defer bar.mu.Unlock()
	for _, sec := range bar.widgets() {
		for _, w := range sec {
			if w.tooltip == "org.sysc.timer" || (w.inner != nil && w.inner.Name == "org.sysc.timer") {
				return w
			}
			if w.inner != nil && w.inner.Width == pluginPlaceholderWidth {
				return w
			}
		}
	}
	return textWidget{}
}

func TestPluginHostOpensOneBarViewPerOutput(t *testing.T) {
	reg := bindTestPlugin(t, "ok")
	newHosts(t, reg, map[uint32]string{1: "DP-1", 2: "HDMI-1"})

	waitPluginText(t, reg.bars[1], "hello")
	waitPluginText(t, reg.bars[2], "hello")

	a := reg.plugins.barViewIDs("DP-1")
	b := reg.plugins.barViewIDs("HDMI-1")
	if len(a) != 1 || len(b) != 1 || a[0] == b[0] {
		t.Fatalf("views = %v %v, want one distinct view per output", a, b)
	}
	pid := reg.plugins.pid("org.sysc.timer")
	if pid == 0 {
		t.Fatal("shared runtime has no pid")
	}
}

func TestPluginHostClosesViewsOnHotUnplug(t *testing.T) {
	reg := bindTestPlugin(t, "ok")
	newHosts(t, reg, map[uint32]string{1: "DP-1"})
	waitPluginText(t, reg.bars[1], "hello")
	if len(reg.plugins.barViewIDs("DP-1")) != 1 {
		t.Fatal("expected one bar view")
	}
	reg.DropHost(1)
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if len(reg.plugins.barViewIDs("DP-1")) == 0 && len(reg.plugins.closedViews()) > 0 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("views = %v closed = %v", reg.plugins.barViewIDs("DP-1"), reg.plugins.closedViews())
}

func TestPluginHostReplugOpensAFreshView(t *testing.T) {
	reg := bindTestPlugin(t, "ok")
	newHosts(t, reg, map[uint32]string{1: "DP-1"})
	waitPluginText(t, reg.bars[1], "hello")
	first := reg.plugins.barViewIDs("DP-1")[0]
	reg.DropHost(1)
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if len(reg.plugins.barViewIDs("DP-1")) == 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	newHosts(t, reg, map[uint32]string{1: "DP-1"})
	waitPluginText(t, reg.bars[1], "hello")
	got := reg.plugins.barViewIDs("DP-1")
	if len(got) != 1 || got[0] == first {
		t.Fatalf("replug views = %v, first was %s", got, first)
	}
	if reg.plugins.pid("org.sysc.timer") == 0 {
		t.Fatal("replug stopped the plugin")
	}
}

func TestPluginHostIgnoresStaleEventsAfterClose(t *testing.T) {
	reg := bindTestPlugin(t, "ok")
	newHosts(t, reg, map[uint32]string{1: "DP-1"})
	waitPluginText(t, reg.bars[1], "hello")
	ids := reg.plugins.barViewIDs("DP-1")
	reg.DropHost(1)
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if len(reg.plugins.barViewIDs("DP-1")) == 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	ok := reg.plugins.deliver(pluginHit{ViewID: ids[0], Node: "go"}, v1.EventActivate, "", "")
	if ok {
		t.Fatal("closed view still accepted input")
	}
}

func TestPluginHostFailurePlaceholderLeavesBuiltinsIntact(t *testing.T) {
	reg := bindTestPlugin(t, "bad-view")
	newHosts(t, reg, map[uint32]string{1: "DP-1"})
	now := time.Date(2026, 9, 1, 15, 4, 0, 0, time.UTC)
	reg.UpdateClock(now)

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		w := pluginWidget(reg.bars[1])
		if w.inner != nil && w.inner.Width == pluginPlaceholderWidth {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	w := pluginWidget(reg.bars[1])
	if w.inner == nil || w.inner.Width != pluginPlaceholderWidth {
		t.Fatalf("placeholder width = %+v", w.inner)
	}
	clock := ""
	reg.bars[1].mu.Lock()
	for _, item := range reg.bars[1].center {
		if item.format != nil && item.inner != nil {
			clock = item.inner.Text
			break
		}
	}
	reg.bars[1].mu.Unlock()
	if clock == "" {
		t.Fatal("built-in clock was cleared by a failed plugin")
	}
}

func TestPluginHostOpensPanelOnTheTriggeringOutput(t *testing.T) {
	reg := bindTestPlugin(t, "call-panel")
	newHosts(t, reg, map[uint32]string{7: "DP-1"})
	waitPluginText(t, reg.bars[7], "hello")
	reqs := drainAux(t, reg, 2)
	if reqs[1].Output != 7 {
		t.Fatalf("panel opened on output %d, want 7", reqs[1].Output)
	}
	if !strings.HasPrefix(reqs[1].Open.ID, "panel:plugin") {
		t.Fatalf("surface = %q", reqs[1].Open.ID)
	}
}

func TestPluginHostTooltipReusesTheBarPath(t *testing.T) {
	reg := bindTestPlugin(t, "ok")
	newHosts(t, reg, map[uint32]string{1: "DP-1"})
	waitPluginText(t, reg.bars[1], "hello")
	if err := reg.bars[1].Configure(800, BarHeight, 120); err != nil {
		t.Fatal(err)
	}
	w := pluginWidget(reg.bars[1])
	if w.node == nil {
		t.Fatal("no plugin widget")
	}
	text, _, _, ok := reg.bars[1].tooltipAt(w.node.Bounds.X+1, w.node.Bounds.Y+1)
	if !ok || text != "org.sysc.timer" {
		t.Fatalf("tooltip = %q ok=%v", text, ok)
	}
}

func TestPluginPrimaryMiddleSecondaryButtons(t *testing.T) {
	reg := bindTestPlugin(t, "ok")
	newHosts(t, reg, map[uint32]string{1: "DP-1"})
	waitPluginText(t, reg.bars[1], "hello")
	bar := reg.bars[1]
	if err := bar.Configure(800, BarHeight, 120); err != nil {
		t.Fatal(err)
	}
	action, x, y := pluginHitPoint(bar)
	if action == "" {
		t.Fatal("plugin button was not arranged")
	}
	bar.Handle(wayland.Event{Kind: wayland.EventPointerEnter, X: x, Y: y})
	for _, button := range []uint32{272, 273, 274} {
		bar.Handle(wayland.Event{Kind: wayland.EventPointerPress, Button: button, X: x, Y: y})
		bar.Handle(wayland.Event{Kind: wayland.EventPointerRelease, Button: button, X: x, Y: y})
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if len(reg.plugins.lastInputs()) >= 3 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	got := reg.plugins.lastInputs()
	if len(got) < 3 {
		t.Fatalf("inputs = %+v", got)
	}
	kinds := map[v1.EventKind]int{}
	var secondary int
	for _, in := range got {
		kinds[in.Event]++
		if in.Node != "go" {
			t.Fatalf("node = %q", in.Node)
		}
		if in.Event == v1.EventPointer && in.Button == v1.ButtonSecondary {
			secondary++
		}
	}
	if kinds[v1.EventPointer] == 0 || kinds[v1.EventActivate] == 0 {
		t.Fatalf("kinds = %v", kinds)
	}
	if secondary != 1 {
		t.Fatalf("secondary pointer events = %d, want 1 (press+release must not both fire)", secondary)
	}
}

func TestPluginInteractiveNodesCarryAccessibleNames(t *testing.T) {
	reg := bindTestPlugin(t, "ok")
	newHosts(t, reg, map[uint32]string{1: "DP-1"})
	waitPluginText(t, reg.bars[1], "hello")
	w := pluginWidget(reg.bars[1])
	var named int
	var walk func(*ui.Node)
	walk = func(n *ui.Node) {
		if n == nil {
			return
		}
		if n.Kind == ui.KindButton {
			if n.Name == "" || n.Role == "" {
				t.Fatalf("button name=%q role=%q", n.Name, n.Role)
			}
			named++
		}
		for _, c := range n.Children {
			walk(c)
		}
	}
	walk(w.inner)
	if named == 0 {
		t.Fatal("no named interactive node")
	}
}

func TestPluginPanelTextChangeSubmitAndClose(t *testing.T) {
	reg := bindTestPlugin(t, "call-panel")
	newHosts(t, reg, map[uint32]string{7: "DP-1"})
	waitPluginText(t, reg.bars[7], "hello")
	_ = drainAux(t, reg, 2)

	deadline := time.Now().Add(5 * time.Second)
	var host *PanelHost
	for time.Now().Before(deadline) {
		reg.mu.Lock()
		host = reg.panelHosts[PanelPlugin]
		reg.mu.Unlock()
		if host != nil && host.root != nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if host == nil {
		t.Fatal("plugin panel never opened")
	}
	if err := host.configure(320, 280, 120); err != nil {
		t.Fatal(err)
	}
	host.roving.Count = len(ui.Focusables(host.root))
	// Focus the text field if present and submit.
	for i, n := range ui.Focusables(host.root) {
		if n.Kind == ui.KindTextField {
			host.roving.Set(i)
			n.Text = "tea"
			reg.deliverPluginText(n.Action, n.Text, v1.EventChange)
			reg.deliverPluginText(n.Action, n.Text, v1.EventSubmit)
			break
		}
	}
	reg.ClosePanel(PanelPlugin)
	deadline = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		inputs := reg.plugins.lastInputs()
		var change, submit bool
		for _, in := range inputs {
			if in.Event == v1.EventChange && in.Text == "tea" {
				change = true
			}
			if in.Event == v1.EventSubmit {
				submit = true
			}
		}
		if change && submit && len(reg.plugins.closedViews()) > 0 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("inputs=%+v closed=%v", reg.plugins.lastInputs(), reg.plugins.closedViews())
}

func TestOutputContextBarEventIncludesOutput(t *testing.T) {
	reg := bindTestPlugin(t, "ok")
	newHosts(t, reg, map[uint32]string{1: "DP-1"})
	waitPluginText(t, reg.bars[1], "hello")
	bar := reg.bars[1]
	if err := bar.Configure(800, BarHeight, 120); err != nil {
		t.Fatal(err)
	}
	action, x, y := pluginHitPoint(bar)
	if action == "" {
		t.Fatal("plugin button was not arranged")
	}
	bar.Handle(wayland.Event{Kind: wayland.EventPointerEnter, X: x, Y: y})
	bar.Handle(wayland.Event{Kind: wayland.EventPointerPress, Button: 272, X: x, Y: y})
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if len(reg.plugins.lastInputs()) > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	got := reg.plugins.lastInputs()
	if len(got) == 0 {
		t.Fatal("no bar event")
	}
	if got[0].Output != "DP-1" || got[0].Generation != 1 {
		t.Fatalf("event output=%q generation=%d", got[0].Output, got[0].Generation)
	}
}

func TestOutputContextGlobalCommandUsesFocusedOutput(t *testing.T) {
	reg := bindTestPlugin(t, "ok")
	newHosts(t, reg, map[uint32]string{1: "DP-1", 2: "HDMI-1"})
	waitPluginText(t, reg.bars[1], "hello")
	reg.UpdateNiri(niri.Snapshot{FocusedOutput: "HDMI-1"})
	got, err := reg.plugins.outputContext(v1.OutputContextParams{})
	if err != nil {
		t.Fatal(err)
	}
	if got.Output != "HDMI-1" || got.Generation != 2 {
		t.Fatalf("focused context = %+v", got)
	}
}

func TestOutputContextStaleGenerationFailsByName(t *testing.T) {
	reg := bindTestPlugin(t, "ok")
	newHosts(t, reg, map[uint32]string{1: "DP-1"})
	waitPluginText(t, reg.bars[1], "hello")
	_, err := reg.plugins.outputContext(v1.OutputContextParams{Output: "DP-1", Generation: 99})
	if err == nil || !strings.Contains(err.Error(), "DP-1") {
		t.Fatalf("stale error = %v", err)
	}
}

func TestOutputContextRejectsUndeclaredConnector(t *testing.T) {
	reg := bindTestPlugin(t, "ok")
	newHosts(t, reg, map[uint32]string{1: "DP-1"})
	waitPluginText(t, reg.bars[1], "hello")
	_, err := reg.plugins.outputContext(v1.OutputContextParams{Output: "HDMI-1"})
	if err == nil || !strings.Contains(err.Error(), "HDMI-1") {
		t.Fatalf("undeclared error = %v", err)
	}
}

const testRecorderPanelManifest = `{
  "schema": 1,
  "id": "org.sysc.screen-recorder",
  "name": "Screen Recorder",
  "version": "1.0.0",
  "protocol": {"major": 1, "minor": 0},
  "exec": "bin/sysc-plugin-timer",
  "capabilities": ["notifications", "panels", "settings", "state"],
  "requires": {"commands": []},
  "services": [{"id": "recorder"}],
  "widgets": [{"id": "bar", "settings": []}],
  "panels": [{"id": "panel", "width": 640, "height": 720, "placement": "attached", "include_settings": true}],
  "settings": [
    {"key": "video_source", "type": "select", "label": "Video source", "default": "portal",
      "options": [{"value": "focused", "label": "Focused output"}, {"value": "portal", "label": "Portal"}]},
    {"key": "directory", "type": "folder", "label": "Output directory", "default": "~/Videos/Recordings"},
    {"key": "filename_pattern", "type": "string", "label": "Filename pattern", "default": "recording_%Y%m%d_%H%M%S"},
    {"key": "frame_rate", "type": "int", "label": "Frame rate", "default": 60, "min": 1, "max": 240},
    {"key": "video_codec", "type": "select", "label": "Video codec", "default": "h264",
      "options": [{"value": "h264", "label": "H.264"}]},
    {"key": "video_qp", "type": "int", "label": "Quality (QP)", "default": 25, "min": 0, "max": 51},
    {"key": "resolution", "type": "string", "label": "Resolution", "default": "original"},
    {"key": "audio_source", "type": "select", "label": "Audio source", "default": "none",
      "options": [{"value": "none", "label": "None"}]},
    {"key": "audio_codec", "type": "select", "label": "Audio codec", "default": "opus",
      "options": [{"value": "opus", "label": "Opus"}]},
    {"key": "audio_bitrate", "type": "int", "label": "Audio bitrate (kbps)", "default": 0, "min": 0, "max": 512},
    {"key": "show_cursor", "type": "bool", "label": "Show cursor", "default": true},
    {"key": "color_range", "type": "select", "label": "Color range", "default": "limited",
      "options": [{"value": "limited", "label": "Limited"}]},
    {"key": "hide_inactive", "type": "bool", "label": "Hide when idle", "default": false},
    {"key": "replay_enabled", "type": "bool", "label": "Replay buffer", "default": false},
    {"key": "replay_duration", "type": "int", "label": "Replay duration (s)", "default": 30, "min": 5, "max": 3600,
      "visible_when": {"key": "replay_enabled", "equals": true}},
    {"key": "replay_filename_pattern", "type": "string", "label": "Replay filename pattern", "default": "replay_%Y%m%d_%H%M%S",
      "visible_when": {"key": "replay_enabled", "equals": true}},
    {"key": "replay_storage", "type": "select", "label": "Replay storage", "default": "ram",
      "options": [{"value": "ram", "label": "RAM"}],
      "visible_when": {"key": "replay_enabled", "equals": true}}
  ]
}`

func bindManifestPlugin(t *testing.T, mode, id, manifest string, enabled []string) *Registry {
	t.Helper()
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	dir := filepath.Join(root, id)
	if _, err := plugin.WriteHelperPlugin(dir, self, mode, manifest); err != nil {
		t.Fatal(err)
	}
	cfg := pluginConfig(root)
	cfg.Plugins.Enabled = enabled
	cfg.Bar.Right = []config.Item{{
		ID: "plugin", Plugin: id, Entry: "bar", Instance: id + "-1",
	}}
	reg := NewRegistry(cfg)
	t.Cleanup(reg.Close)
	if err := reg.BindPlugins(PluginHostOptions{
		Roots:    []plugin.Root{{Path: root, Source: plugin.SourceUser}},
		StateDir: t.TempDir(),
	}); err != nil {
		t.Fatal(err)
	}
	return reg
}

func waitPluginPanelRoot(t *testing.T, reg *Registry) *ui.Node {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		root := reg.plugins.panelTree(nil)
		text := treeText(root)
		if text != "" && !strings.Contains(text, "starting") &&
			(strings.Contains(text, "hello") || strings.Contains(text, "Go")) {
			reg.mu.Lock()
			host := reg.panelHosts[PanelPlugin]
			reg.mu.Unlock()
			if host != nil {
				return root
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("plugin panel tree never became ready")
	return nil
}

func openPanelPluginID(h *pluginHost) string {
	if h == nil {
		return ""
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.panel == nil {
		return ""
	}
	return h.panel.Plugin
}

func TestPluginOpenPanelTogglesThisEntryClosed(t *testing.T) {
	reg := bindTestPlugin(t, "ok")
	newHosts(t, reg, map[uint32]string{7: "DP-1"})
	waitPluginText(t, reg.bars[7], "hello")

	res, err := reg.plugins.openPanel("org.sysc.timer", v1.PanelParams{
		Entry: "panel", Output: "DP-1", Generation: 7, Instance: "timer-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.ViewID == "" {
		t.Fatal("open returned empty view id")
	}
	_ = drainAux(t, reg, 2)
	waitPluginPanelRoot(t, reg)

	res, err = reg.plugins.openPanel("org.sysc.timer", v1.PanelParams{
		Entry: "panel", Output: "DP-1", Generation: 7, Instance: "timer-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		reg.mu.Lock()
		_, open := reg.panels.Output(PanelPlugin)
		host := reg.panelHosts[PanelPlugin]
		reg.mu.Unlock()
		if !open && host == nil {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("second openPanel did not close this plugin panel")
}

// Toggle-close schedules dropPanelViews asynchronously. A quick reopen must
// survive that deferred drop: only the views that existed at close time go away.
func TestDropPanelViewsKeepsReopenedPanel(t *testing.T) {
	reg := bindTestPlugin(t, "ok")
	newHosts(t, reg, map[uint32]string{7: "DP-1"})
	waitPluginText(t, reg.bars[7], "hello")

	first, err := reg.plugins.openPanel("org.sysc.timer", v1.PanelParams{
		Entry: "panel", Output: "DP-1", Generation: 7, Instance: "timer-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if first.ViewID == "" {
		t.Fatal("open returned empty view id")
	}
	_ = drainAux(t, reg, 2)
	waitPluginPanelRoot(t, reg)

	stale := reg.plugins.snapshotPanelViewIDs()
	if len(stale) == 0 {
		t.Fatal("expected panel views after open")
	}

	if err := reg.plugins.closePanel("org.sysc.timer", v1.PanelParams{Entry: "panel"}); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	closed := false
	for time.Now().Before(deadline) {
		reg.mu.Lock()
		_, open := reg.panels.Output(PanelPlugin)
		host := reg.panelHosts[PanelPlugin]
		reg.mu.Unlock()
		if !open && host == nil {
			closed = true
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !closed {
		t.Fatal("closePanel did not close the panel")
	}

	second, err := reg.plugins.openPanel("org.sysc.timer", v1.PanelParams{
		Entry: "panel", Output: "DP-1", Generation: 7, Instance: "timer-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if second.ViewID == "" {
		t.Fatal("reopen returned empty view id")
	}
	for _, id := range stale {
		if second.ViewID == id {
			t.Fatalf("reopen reused closed view id %q", id)
		}
	}
	_ = drainAux(t, reg, 2)
	waitPluginPanelRoot(t, reg)

	// Deferred ClosePanel drop with the close-time snapshot. Must not wipe the
	// reopened view (a wipe-all dropPanelViews would).
	reg.plugins.dropPanelViews(stale)

	reg.plugins.mu.Lock()
	_, alive := reg.plugins.views[second.ViewID]
	panelID := ""
	if reg.plugins.panel != nil {
		panelID = reg.plugins.panel.ID
	}
	reg.plugins.mu.Unlock()
	if !alive || panelID != second.ViewID {
		t.Fatalf("reopened view wiped: alive=%v panel=%q want=%q", alive, panelID, second.ViewID)
	}
	reg.mu.Lock()
	where, open := reg.panels.Output(PanelPlugin)
	owns := open && where == 7 && reg.roots.owns(panelRoot(PanelPlugin))
	reg.mu.Unlock()
	if !owns {
		t.Fatal("PanelPlugin ownership lost after deferred drop")
	}
}

func TestPluginOpenPanelReplacesAnotherPluginPanel(t *testing.T) {
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	if _, err := plugin.WriteHelperPlugin(filepath.Join(root, "org.sysc.timer"), self, "ok", testTimerManifest); err != nil {
		t.Fatal(err)
	}
	if _, err := plugin.WriteHelperPlugin(filepath.Join(root, "org.sysc.screen-recorder"), self, "ok", testRecorderPanelManifest); err != nil {
		t.Fatal(err)
	}
	cfg := pluginConfig(root)
	cfg.Plugins.Enabled = []string{"org.sysc.timer", "org.sysc.screen-recorder"}
	cfg.Bar.Right = []config.Item{
		{ID: "plugin", Plugin: "org.sysc.timer", Entry: "bar", Instance: "timer-1"},
		{ID: "plugin", Plugin: "org.sysc.screen-recorder", Entry: "bar", Instance: "recorder-1"},
	}
	reg := NewRegistry(cfg)
	t.Cleanup(reg.Close)
	if err := reg.BindPlugins(PluginHostOptions{
		Roots:    []plugin.Root{{Path: root, Source: plugin.SourceUser}},
		StateDir: t.TempDir(),
	}); err != nil {
		t.Fatal(err)
	}
	newHosts(t, reg, map[uint32]string{7: "DP-1"})
	waitPluginText(t, reg.bars[7], "hello")

	if _, err := reg.plugins.openPanel("org.sysc.timer", v1.PanelParams{
		Entry: "panel", Output: "DP-1", Generation: 7, Instance: "timer-1",
	}); err != nil {
		t.Fatal(err)
	}
	_ = drainAux(t, reg, 2)
	waitPluginPanelRoot(t, reg)
	if openPanelPluginID(reg.plugins) != "org.sysc.timer" {
		t.Fatalf("timer panel = %q", openPanelPluginID(reg.plugins))
	}

	if _, err := reg.plugins.openPanel("org.sysc.screen-recorder", v1.PanelParams{
		Entry: "panel", Output: "DP-1", Generation: 7, Instance: "recorder-1",
	}); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if openPanelPluginID(reg.plugins) == "org.sysc.screen-recorder" {
			reg.mu.Lock()
			where, open := reg.panels.Output(PanelPlugin)
			reg.mu.Unlock()
			if open && where == 7 {
				return
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("recorder did not replace timer panel: panel=%q", openPanelPluginID(reg.plugins))
}

func TestPluginPanelTreeIncludeSettingsComposesRows(t *testing.T) {
	reg := bindManifestPlugin(t, "ok", "org.sysc.screen-recorder", testRecorderPanelManifest,
		[]string{"org.sysc.screen-recorder"})
	newHosts(t, reg, map[uint32]string{7: "DP-1"})
	waitPluginText(t, reg.bars[7], "hello")

	if _, err := reg.plugins.openPanel("org.sysc.screen-recorder", v1.PanelParams{
		Entry: "panel", Output: "DP-1", Generation: 7, Instance: "org.sysc.screen-recorder-1",
	}); err != nil {
		t.Fatal(err)
	}
	_ = drainAux(t, reg, 2)

	deadline := time.Now().Add(5 * time.Second)
	var text string
	for time.Now().Before(deadline) {
		root := reg.plugins.panelTree(nil)
		text = treeText(root)
		if strings.Contains(text, "Output directory") && strings.Contains(text, "hello") {
			for _, heading := range []string{"Capture", "File", "Video", "Audio", "Replay", "Bar"} {
				if !strings.Contains(text, heading) {
					t.Fatalf("missing group %q in %q", heading, text)
				}
			}
			if strings.Contains(text, "Replay duration") {
				t.Fatalf("hidden replay_duration still shown in %q", text)
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("include_settings tree missing Output directory: %q", text)
}

func TestPluginPanelHostUsesManifestSize(t *testing.T) {
	reg := bindManifestPlugin(t, "ok", "org.sysc.screen-recorder", testRecorderPanelManifest,
		[]string{"org.sysc.screen-recorder"})
	newHosts(t, reg, map[uint32]string{7: "DP-1"})
	waitPluginText(t, reg.bars[7], "hello")

	reg.cfg.Plugins.Settings = map[string]map[string]any{
		"org.sysc.screen-recorder": {
			"directory": "/home/nomadx/scratchpad/recorder-panel-gate/recordings",
		},
	}
	if _, err := reg.plugins.openPanel("org.sysc.screen-recorder", v1.PanelParams{
		Entry: "panel", Output: "DP-1", Generation: 7, Instance: "org.sysc.screen-recorder-1",
	}); err != nil {
		t.Fatal(err)
	}
	_ = drainAux(t, reg, 2)

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		reg.mu.Lock()
		host := reg.panelHosts[PanelPlugin]
		reg.mu.Unlock()
		if host == nil {
			time.Sleep(10 * time.Millisecond)
			continue
		}
		if !strings.Contains(treeText(reg.plugins.panelTree(host)), "Output directory") {
			time.Sleep(10 * time.Millisecond)
			continue
		}
		if host.place.Panel.W != 640 || host.place.Panel.H != 720 {
			t.Fatalf("plugin panel size = %dx%d, want 640x720 from the manifest", host.place.Panel.W, host.place.Panel.H)
		}
		if host.place.Gap != 0 {
			t.Fatalf("plugin panel gap = %d, want 0 (flush to the bar)", host.place.Gap)
		}
		zone, _, _ := DefaultTheme().Geometry()
		if host.place.BarZone != zone {
			t.Fatalf("plugin BarZone = %d, want exclusive zone %d", host.place.BarZone, zone)
		}
		if top := host.place.Margins().Top; top != zone {
			t.Fatalf("plugin top margin = %d, want %d", top, zone)
		}
		if err := host.configure(host.place.Panel.W, host.place.Panel.H, 120); err != nil {
			t.Fatal(err)
		}
		root := host.root
		if root == nil || root.Kind != ui.KindScroll {
			t.Fatalf("root = %+v, want KindScroll", root)
		}
		if len(root.Children) == 0 {
			t.Fatal("plugin panel scroll has no children")
		}
		for i, c := range root.Children {
			if c == nil || c.Kind != ui.KindCapsule {
				t.Fatalf("scroll child %d = %+v, want KindCapsule", i, c)
			}
		}
		return
	}
	t.Fatal("recorder panel never opened")
}

func TestPluginPanelTreeWithoutIncludeSettingsStaysPluginOnly(t *testing.T) {
	reg := bindTestPlugin(t, "ok")
	newHosts(t, reg, map[uint32]string{7: "DP-1"})
	waitPluginText(t, reg.bars[7], "hello")

	if _, err := reg.plugins.openPanel("org.sysc.timer", v1.PanelParams{
		Entry: "panel", Output: "DP-1", Generation: 7, Instance: "timer-1",
	}); err != nil {
		t.Fatal(err)
	}
	_ = drainAux(t, reg, 2)
	root := waitPluginPanelRoot(t, reg)
	text := treeText(root)
	if !strings.Contains(text, "hello") {
		t.Fatalf("plugin root missing: %q", text)
	}
	for _, heading := range []string{"Capture", "File", "Output directory"} {
		if strings.Contains(text, heading) {
			t.Fatalf("include_settings=false still composed %q in %q", heading, text)
		}
	}
}

func TestRecorderBarTreeFitsHostSlot(t *testing.T) {
	wire := recorder.BarTree(recorder.Snapshot{Mode: recorder.Idle}, recorder.Config{})
	root, err := plugin.Convert(wire, v1.ViewBar)
	if err != nil {
		t.Fatal(err)
	}
	if err := ui.Layout(root, ui.Rect{W: pluginBarViewWidth, H: pluginBarViewHeight}, pluginMeasure); err != nil {
		t.Fatal(err)
	}
}
