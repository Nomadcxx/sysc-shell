package shell

import (
	"testing"

	"github.com/Nomadcxx/sysc-shell/internal/config"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

func TestPluginWidgetPlaceholderHasFixedWidth(t *testing.T) {
	t.Parallel()
	widgets := buildWidgets([]config.Item{
		{ID: "clock", Format: "15:04"},
		{ID: "plugin", Plugin: "org.sysc.timer", Entry: "bar", Instance: "t1"},
	}, 8, standardMetrics())
	if len(widgets) != 2 {
		t.Fatalf("widgets = %d, want 2", len(widgets))
	}
	plugin := widgets[1]
	if plugin.inner == nil || plugin.inner.Width != pluginPlaceholderWidth {
		t.Fatalf("placeholder width = %+v", plugin.inner)
	}
	if plugin.tooltip != "org.sysc.timer" {
		t.Fatalf("tooltip = %q", plugin.tooltip)
	}
}

func TestPluginWidgetFailedPlaceholderOpensCamera(t *testing.T) {
	widgets := buildWidgets([]config.Item{
		{ID: "plugin", Plugin: "org.sysc.screen-recorder", Entry: "bar", Instance: "rec-1"},
	}, 8, standardMetrics())
	w := widgets[0]
	w.refresh(barView{Plugins: map[string]pluginFrame{
		"rec-1": {Failed: true, Label: "does not fit", ViewID: "v1", Revision: 1},
	}})
	mark := w.inner.Children[0]
	if mark.Text != "!" || mark.Action != "plugin:v1:camera" {
		t.Fatalf("placeholder = %+v", mark)
	}
}

func TestPluginWidgetRefreshAdoptsPreparedTree(t *testing.T) {
	t.Parallel()
	widgets := buildWidgets([]config.Item{
		{ID: "plugin", Plugin: "org.sysc.timer", Entry: "bar", Instance: "t1"},
	}, 8, standardMetrics())
	w := widgets[0]
	tree := &ui.Node{Kind: ui.KindRow, Children: []*ui.Node{
		{Kind: ui.KindButton, Text: "hello", Name: "Start", Role: "button", Action: "plugin:v1:go"},
	}}
	changed := w.refresh(barView{Plugins: map[string]pluginFrame{
		"t1": {Root: tree, Revision: 1},
	}})
	if !changed {
		t.Fatal("refresh reported no change")
	}
	if w.inner.Width == pluginPlaceholderWidth {
		t.Fatal("adopted tree kept the placeholder width")
	}
	if len(w.inner.Children) != 1 || w.inner.Children[0].Text != "hello" {
		t.Fatalf("children = %+v", w.inner.Children)
	}
	if w.refresh(barView{Plugins: map[string]pluginFrame{
		"t1": {Root: tree, Revision: 1},
	}}) {
		t.Fatal("identical revision refreshed again")
	}
}

// A disabled plugin has no runtime and so no frame, the same as one still
// starting. Without the off set it fell through to the "!" placeholder and
// stayed in the bar after the user switched it off.
func TestPluginWidgetHidesWhileItsPluginIsDisabled(t *testing.T) {
	t.Parallel()
	widgets := buildWidgets([]config.Item{
		{ID: "plugin", Plugin: "org.sysc.timer", Entry: "bar", Instance: "t1"},
	}, 8, standardMetrics())
	w := widgets[0]
	w.refresh(barView{PluginsOff: map[string]bool{"org.sysc.timer": true}})
	if visibleWidgetNode(w) != nil {
		t.Fatal("a disabled plugin's widget is still in the bar")
	}
	if !w.refresh(barView{}) {
		t.Fatal("re-enabling reported no change")
	}
	if visibleWidgetNode(w) == nil {
		t.Fatal("a re-enabled plugin's widget stayed hidden")
	}
}

func TestBarViewNamesDisabledPluginItems(t *testing.T) {
	t.Parallel()
	reg := newPanelRegistry(t)
	reg.mu.Lock()
	defer reg.mu.Unlock()
	reg.cfg.Bar.Right = append(reg.cfg.Bar.Right,
		config.Item{ID: "plugin", Plugin: "org.sysc.on", Entry: "bar", Instance: "a"},
		config.Item{ID: "group", Items: []config.Item{
			{ID: "plugin", Plugin: "org.sysc.off", Entry: "bar", Instance: "b"},
		}})
	reg.cfg.Plugins.Enabled = []string{"org.sysc.on"}
	off := reg.viewLocked("DP-1").PluginsOff
	if off["org.sysc.on"] || !off["org.sysc.off"] {
		t.Fatalf("PluginsOff = %v, want only org.sysc.off", off)
	}
}
