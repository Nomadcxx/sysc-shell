package shell

import (
	"testing"

	"github.com/Nomadcxx/sysc-shell/internal/ui"
	v1 "github.com/Nomadcxx/sysc-shell/plugin/v1"
)

// Typing into a host-built settings text field (an API-key bar) buffers in
// the field without persisting per keystroke; Enter or the Save pill commits.
func TestPluginSettingsTypingAccumulates(t *testing.T) {
	const pluginID = "org.sysc.screen-recorder"
	const action = "plugin-set:" + pluginID + ":resolution"
	reg := bindManifestPlugin(t, "ok", pluginID, testRecorderPanelManifest, []string{pluginID})
	newHosts(t, reg, map[uint32]string{7: "DP-1"})
	waitPluginText(t, reg.bars[7], "hello")
	if _, err := reg.plugins.openPanel(pluginID, v1.PanelParams{
		Entry: "panel", Output: "DP-1", Generation: 7, Instance: pluginID + "-1",
	}); err != nil {
		t.Fatal(err)
	}
	_ = drainAux(t, reg, 2)
	waitPluginPanelRoot(t, reg)

	reg.mu.Lock()
	host := reg.panelHosts[PanelPlugin]
	reg.mu.Unlock()
	if host == nil {
		t.Fatal("plugin panel host never created")
	}
	if err := host.configure(640, 720, 120); err != nil {
		t.Fatal(err)
	}
	focusables := ui.Focusables(host.root)
	host.focus = focusables
	host.roving = ui.Roving{Count: len(focusables)}
	idx := -1
	for i, n := range focusables {
		if n != nil && n.Action == action {
			idx = i
		}
	}
	if idx < 0 {
		t.Fatalf("resolution field not focusable in %q", treeText(host.root))
	}
	host.roving.Set(idx)
	for _, code := range []uint32{45, 21, 44} { // x, y, z
		if !host.keyPress(reg, code) {
			t.Fatalf("key %d dropped by the panel", code)
		}
		if n := host.focused(); n == nil || n.Action != action {
			t.Fatalf("focus left the field after key %d", code)
		}
	}
	reg.mu.Lock()
	got, _ := reg.cfg.Plugins.Settings[pluginID]["resolution"].(string)
	reg.mu.Unlock()
	if got != "" {
		t.Fatalf("typing persisted early: cfg resolution = %q", got)
	}
	pillAction := "plugin-set-save:" + pluginID + ":resolution"
	reg.mu.Lock()
	var pill *ui.Node
	for _, n := range host.focus {
		if n != nil && n.Action == pillAction {
			pill = n
		}
	}
	if pill == nil || !reg.handlePluginManager(host, pill) {
		reg.mu.Unlock()
		t.Fatalf("save pill %v", pill)
	}
	got, _ = reg.cfg.Plugins.Settings[pluginID]["resolution"].(string)
	reg.mu.Unlock()
	if got != "originalxyz" {
		t.Fatalf("after Save cfg resolution = %q, want \"originalxyz\"", got)
	}
}
