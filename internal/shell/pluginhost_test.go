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
	for _, in := range got {
		kinds[in.Event]++
		if in.Node != "go" {
			t.Fatalf("node = %q", in.Node)
		}
	}
	if kinds[v1.EventPointer] == 0 || kinds[v1.EventActivate] == 0 {
		t.Fatalf("kinds = %v", kinds)
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
