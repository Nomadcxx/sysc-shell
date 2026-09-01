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
	}, 8)
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

func TestPluginWidgetRefreshAdoptsPreparedTree(t *testing.T) {
	t.Parallel()
	widgets := buildWidgets([]config.Item{
		{ID: "plugin", Plugin: "org.sysc.timer", Entry: "bar", Instance: "t1"},
	}, 8)
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
