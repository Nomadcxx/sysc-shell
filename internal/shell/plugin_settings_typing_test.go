package shell

import (
	"strings"
	"testing"
	"time"

	"github.com/Nomadcxx/sysc-shell/internal/render"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
	v1 "github.com/Nomadcxx/sysc-shell/plugin/v1"
)

func TestLongMaskedPluginSettingCanSelectDeleteAndSave(t *testing.T) {
	const pluginID = "org.sysc.screen-recorder"
	const key = "test_api_key"
	const action = "plugin-set:" + pluginID + ":" + key
	value := strings.Repeat("a", 4096)
	manifest := strings.ReplaceAll(testRecorderPanelManifest, `"resolution"`, `"`+key+`"`)
	manifest = strings.ReplaceAll(manifest, `"original"`, `"`+value+`"`)
	reg := bindManifestPlugin(t, "ok", pluginID, manifest, []string{pluginID})
	keepInvalidationsDrained(t, reg)
	newHosts(t, reg, map[uint32]string{7: "DP-1"})
	waitPluginText(t, reg.bars[7], "hello")
	if _, err := reg.plugins.openPanel(pluginID, v1.PanelParams{
		Entry: "panel", Output: "DP-1", Generation: 7, Instance: pluginID + "-1",
	}); err != nil {
		t.Fatal(err)
	}
	_ = drainAux(t, reg, 2)
	waitPluginPanelRoot(t, reg)
	host := reg.panelHosts[PanelPlugin]
	if err := host.configure(640, 720, 120); err != nil {
		t.Fatal(err)
	}
	var field, save *ui.Node
	for _, n := range host.focus {
		if n.Action == action {
			field = n
		}
		if n.Action == "plugin-set-save:"+pluginID+":"+key {
			save = n
		}
	}
	if field == nil || !field.Masked || save == nil {
		t.Fatal("masked setting and Save pill must exist")
	}
	box := render.FieldTextRect(field)
	start := time.Now()
	pressAt(reg, host, box.X+1, box.Y+box.H/2, 0)
	pressAt(reg, host, box.X+1, box.Y+box.H/2, 0)
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("two clicks blocked input for %s", elapsed)
	}
	reg.mu.Lock()
	selected := host.fieldFor(host.focused()).SelectedText()
	reg.mu.Unlock()
	if selected != value {
		t.Fatal("double click did not select the complete masked value")
	}
	typeKeys(reg, host, ui.KeyInput{Sym: ui.SymBackSpace}, txt("replacement"))
	reg.mu.Lock()
	defer reg.mu.Unlock()
	if got := host.fieldFor(host.focused()).Text; got != "replacement" {
		t.Fatalf("edited draft = %q, want replacement", got)
	}
	if !reg.handlePluginManager(host, save) || reg.cfg.Plugins.Settings[pluginID][key] != "replacement" {
		t.Fatal("Save did not persist the edited value")
	}
}

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

// A plugin pushing a fresh panel root (AI Usage refreshes its usage cards on
// every round) rebuilds the panel. The settings field must keep focus across
// that rebuild even when the plugin tree grows a focusable ahead of it;
// losing focus is what made every keystroke dead and pushed keys into the
// plugin socket.
func TestPluginPanelKeepsFieldFocusedAcrossRootPush(t *testing.T) {
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

	// Simulate a plugin root push that grows the tree before the settings
	// form: this is exactly what refreshPanel does with the new snapshot.
	reg.plugins.mu.Lock()
	v := reg.plugins.panel
	if v == nil || v.Root == nil {
		reg.plugins.mu.Unlock()
		t.Fatal("no panel root to grow")
	}
	v.Root.Children = append([]*ui.Node{{
		Kind: ui.KindButton, Text: "grew", Action: "plugin:extra-before-form", Focusable: true,
	}}, v.Root.Children...)
	reg.plugins.mu.Unlock()

	reg.mu.Lock()
	reg.rebuildPanel(host)
	reg.mu.Unlock()

	if n := host.focused(); n == nil || n.Action != action {
		t.Fatalf("focus lost across root push: focused = %+v", n)
	}
	if !host.keyPress(reg, 45) { // x
		t.Fatal("key dropped after root push")
	}
	reg.mu.Lock()
	f := host.fields[pluginID+".resolution"]
	reg.mu.Unlock()
	if f == nil || f.Text != "originalx" {
		t.Fatalf("typing did not land in the field after root push: %+v", f)
	}
}
