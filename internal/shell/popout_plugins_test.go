package shell

import (
	"strings"
	"testing"

	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

func TestPluginsSectionShowsDirectoryAndCards(t *testing.T) {
	reg := bindTestPlugin(t, "ok")
	h := &PanelHost{id: PanelSettings, section: "Plugins", search: ui.NewField("")}
	root := settingsTree(reg, h)
	text := treeText(root)
	if !strings.Contains(text, "Timer") {
		t.Fatalf("missing plugin name in %q", text)
	}
	if !strings.Contains(text, "1.0.0") || !strings.Contains(text, "user") {
		t.Fatalf("missing metadata in %q", text)
	}
	if !strings.Contains(text, "notifications") {
		t.Fatalf("missing capabilities in %q", text)
	}
	if !strings.Contains(text, "Rescan") {
		t.Fatal("missing rescan")
	}
	if !strings.Contains(pluginDirectoryLabel(reg), "org.sysc.timer") &&
		!strings.Contains(pluginDirectoryLabel(reg), reg.plugins.opts.Roots[0].Path) {
		t.Fatalf("directory label = %q", pluginDirectoryLabel(reg))
	}
}

func TestPluginManagerEnableDisableAndRetry(t *testing.T) {
	reg := bindTestPlugin(t, "ok")
	h := &PanelHost{id: PanelSettings, section: "Plugins", search: ui.NewField("")}
	if !reg.handlePluginManager(h, "plugin-enable:org.sysc.timer") {
		t.Fatal("disable toggle not handled")
	}
	for _, id := range reg.cfg.Plugins.Enabled {
		if id == "org.sysc.timer" {
			t.Fatal("plugin still enabled after toggle")
		}
	}
	if !reg.handlePluginManager(h, "plugin-enable:org.sysc.timer") {
		t.Fatal("enable not handled")
	}
	found := false
	for _, id := range reg.cfg.Plugins.Enabled {
		if id == "org.sysc.timer" {
			found = true
		}
	}
	if !found {
		t.Fatal("plugin was not enabled")
	}
	if !reg.handlePluginManager(h, "plugin-retry:org.sysc.timer") {
		t.Fatal("retry not handled")
	}
	if !reg.handlePluginManager(h, "plugin-rescan") {
		t.Fatal("rescan not handled")
	}
}

func TestRejectedPluginSettingLeavesConfigUnchanged(t *testing.T) {
	reg := bindTestPlugin(t, "ok")
	before := append([]string(nil), reg.cfg.Plugins.Enabled...)
	if err := reg.plugins.applySetting("org.sysc.timer", "ghost", true); err == nil {
		t.Fatal("unknown setting must be rejected")
	}
	if len(reg.cfg.Plugins.Settings["org.sysc.timer"]) != 0 {
		t.Fatalf("settings changed: %+v", reg.cfg.Plugins.Settings)
	}
	if len(reg.cfg.Plugins.Enabled) != len(before) {
		t.Fatal("enabled list changed")
	}
}

func treeText(n *ui.Node) string {
	var b strings.Builder
	dumpText(n, &b)
	return b.String()
}
