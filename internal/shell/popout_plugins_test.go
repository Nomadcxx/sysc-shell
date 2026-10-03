package shell

import (
	"strings"
	"testing"
	"time"

	"github.com/Nomadcxx/sysc-shell/internal/plugin"
	"github.com/Nomadcxx/sysc-shell/internal/theme"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

func f64(v float64) *float64 { return &v }

func settingRowControl(row *ui.Node) *ui.Node {
	if row == nil || len(row.Children) < 2 {
		return nil
	}
	trailing := row.Children[1]
	if trailing.Kind == ui.KindRow && len(trailing.Children) > 0 {
		return trailing.Children[0]
	}
	return trailing
}

func TestPluginSettingRowRendersSelectAsMenu(t *testing.T) {
	h := &PanelHost{}
	s := plugin.Setting{
		Key: "video_codec", Type: plugin.SettingSelect, Label: "Video codec", Default: "h264",
		Options: []plugin.SettingOption{{Value: "h264", Label: "H.264"}, {Value: "hevc", Label: "HEVC"}},
	}
	row := pluginSettingRow(nil, h, "org.sysc.screen-recorder", s)
	ctrl := settingRowControl(row)
	if ctrl == nil || ctrl.Kind != ui.KindMenu {
		t.Fatalf("select control = %+v, want KindMenu", ctrl)
	}
	if ctrl.Text != "H.264" {
		t.Fatalf("menu label = %q, want H.264", ctrl.Text)
	}
	if row.Kind != ui.KindRow {
		t.Fatalf("select row kind = %d, want settings row", row.Kind)
	}
}

func TestPluginSettingRowRendersBoolAsSwitch(t *testing.T) {
	h := &PanelHost{}
	s := plugin.Setting{Key: "show_cursor", Type: plugin.SettingBool, Label: "Show cursor", Description: "Include the pointer in captures.", Default: true}
	row := pluginSettingRow(nil, h, "org.sysc.screen-recorder", s)
	if row == nil || row.Kind != ui.KindRow || len(row.Children) < 2 {
		t.Fatalf("bool row = %+v, want switch beside its label", row)
	}
	switchNode := settingRowControl(row)
	if switchNode.Kind != ui.KindToggle || switchNode.Role != "switch" || !switchNode.Focusable || switchNode.Name != s.Label {
		t.Fatalf("bool control = %+v, want accessible switch", switchNode)
	}
	label := row.Children[0].Children[0]
	if label.Text != "Show cursor" {
		t.Fatalf("bool label = %q", label.Text)
	}
	if !label.Focusable || label.Role != "switch" || label.Value != switchNode.Value || label.Action != switchNode.Action {
		t.Fatal("switch label must retain pointer and keyboard activation with the same accessible state")
	}
	if row.Children[0].Kind != ui.KindColumn || row.Children[0].Children[1].Text != s.Description || !row.Children[1].PinEnd {
		t.Fatal("plugin setting does not use the Settings label, caption, and right-pinned control anatomy")
	}
}

func TestPluginSettingRowShowsIntegerValue(t *testing.T) {
	r := &Registry{}
	r.cfg.Plugins.Settings = map[string]map[string]any{
		"org.sysc.aiusage": {"refresh_interval": 600},
	}
	s := plugin.Setting{Key: "refresh_interval", Type: plugin.SettingInt, Label: "Refresh interval", Default: 300, Min: f64(60), Max: f64(3600)}
	row := pluginSettingRow(r, &PanelHost{}, "org.sysc.aiusage", s)
	trailing := row.Children[1]
	if len(trailing.Children) != 2 {
		t.Fatalf("integer control row has %d children, want slider and current value", len(trailing.Children))
	}
	if trailing.Children[0].Kind != ui.KindSlider {
		t.Fatalf("integer control = %+v, want slider", trailing.Children[0])
	}
	value := trailing.Children[1]
	if value.Kind != ui.KindText || value.Text != "600" || !value.Tabular {
		t.Fatalf("integer value = %+v, want visible tabular value 600", value)
	}
}

func TestPluginSettingControlFitsCardContent(t *testing.T) {
	m, _ := theme.MetricsFor(theme.DensityDefault)
	h := &PanelHost{id: PanelPlugin}
	h.theme.Metrics = m
	h.place = Placement{
		Panel: ui.Rect{W: 758}, Output: ui.Rect{W: 1000}, BarEdge: "top",
		BarShape: "attached", Fillet: 12,
	}
	s := plugin.Setting{Key: "provider_api_key", Type: plugin.SettingString, Label: "API key"}
	row := pluginSettingRow(nil, h, "org.sysc.aiusage", s)
	field := row.Children[1].Children[0]
	if field.Kind != ui.KindTextField || field.Padding != m.ButtonPadding {
		t.Fatalf("plugin setting field = %+v, want a padded text field", field)
	}
	joints := h.place.Joints()
	contentWidth := h.place.Panel.W - joints.Left - joints.Right - 2*m.PanelPadding - 2*m.CardPadding
	measure := func(string, ui.TextAttrs) (int, int) { return 100, 20 }
	_, fieldHeight, err := ui.Measure(field, measure)
	if err != nil {
		t.Fatal(err)
	}
	rowHeight, err := ui.ContentHeight(row, contentWidth, measure)
	if err != nil {
		t.Fatal(err)
	}
	if rowHeight < fieldHeight {
		t.Fatalf("setting row height = %d, smaller than field height %d", rowHeight, fieldHeight)
	}
	used := row.Children[0].Width + theme.MarginL + row.Children[1].Width
	if used > contentWidth {
		t.Fatalf("setting row uses %dpx inside %dpx card content", used, contentWidth)
	}
}

func TestPluginPanelSettingsWrapsGroupsInCapsules(t *testing.T) {
	h := &PanelHost{}
	schema := []plugin.Setting{
		{Key: "frame_rate", Type: plugin.SettingInt, Label: "Frame rate", Default: 60.0, Min: f64(1), Max: f64(240)},
		{Key: "directory", Type: plugin.SettingFolder, Label: "Output directory", Default: "~/Videos"},
		{Key: "auto_generate", Type: plugin.SettingBool, Label: "Generate on wallpaper change", Default: true},
		{Key: "details", Type: plugin.SettingString, Label: "Advanced details", Default: "hidden",
			VisibleWhen: &plugin.VisibleWhen{Key: "auto_generate", Equals: false}},
	}
	nodes := pluginPanelSettings(nil, h, "org.sysc.screen-recorder", schema)
	if len(nodes) != 3 {
		t.Fatalf("groups = %d, want Capture, File, and generic Settings capsules", len(nodes))
	}
	for i, title := range []string{"Capture", "File", "Settings"} {
		n := nodes[i]
		if n.Kind != ui.KindCapsule {
			t.Fatalf("%s kind = %d, want KindCapsule", title, n.Kind)
		}
		if !strings.Contains(treeText(n), title) {
			t.Fatalf("%s missing from %q", title, treeText(n))
		}
	}
	settings := treeText(nodes[2])
	if !strings.Contains(settings, "Generate on wallpaper change") || strings.Contains(settings, "Advanced details") {
		t.Fatalf("generic settings card does not honor schema visibility: %q", settings)
	}
}

func TestAIUsageSettingsUseThreeCardsAndSwitches(t *testing.T) {
	h := &PanelHost{}
	schema := []plugin.Setting{
		{Key: "track_copilot", Type: plugin.SettingBool, Label: "Copilot", Default: false},
		{Key: "track_minimax", Type: plugin.SettingBool, Label: "MiniMax", Default: false},
		{Key: "minimax_api_key", Type: plugin.SettingString, Label: "MiniMax API key", Default: "",
			VisibleWhen: &plugin.VisibleWhen{Key: "track_minimax", Equals: true}},
		{Key: "refresh_interval", Type: plugin.SettingInt, Label: "Refresh interval (seconds)", Default: 300,
			Min: f64(60), Max: f64(3600)},
		{Key: "history_retention", Type: plugin.SettingSelect, Label: "History retained", Default: "2000",
			Options: []plugin.SettingOption{{Value: "2000", Label: "2,000 entries"}}},
		{Key: "alerts_enabled", Type: plugin.SettingBool, Label: "Threshold alerts", Default: true},
		{Key: "warn_threshold", Type: plugin.SettingInt, Label: "Warning threshold (%)", Default: 85,
			Min: f64(50), Max: f64(99), VisibleWhen: &plugin.VisibleWhen{Key: "alerts_enabled", Equals: true}},
	}
	cards := pluginPanelSettings(nil, h, "org.sysc.aiusage", schema)
	if len(cards) != 3 {
		t.Fatalf("cards = %d, want Providers, Usage, and Alerts", len(cards))
	}
	for i, title := range []string{"Providers", "Usage", "Alerts"} {
		if cards[i].Kind != ui.KindCapsule || cards[i].Fill != ui.FillContainerHigh || !strings.Contains(treeText(cards[i]), title) {
			t.Fatalf("card %d = %+v, want themed %s surface", i, cards[i], title)
		}
	}
	root := &ui.Node{Kind: ui.KindColumn, Children: cards}
	if strings.Contains(treeText(root), "MiniMax API key") {
		t.Fatal("disabled provider credential is visible")
	}

	var copilot, minimax *ui.Node
	var visit func(*ui.Node)
	visit = func(n *ui.Node) {
		if n == nil {
			return
		}
		if n.Kind == ui.KindToggle {
			switch n.Action {
			case "plugin-set:org.sysc.aiusage:track_copilot":
				copilot = n
			case "plugin-set:org.sysc.aiusage:track_minimax":
				minimax = n
			}
		}
		for _, child := range n.Children {
			visit(child)
		}
	}
	visit(root)
	for name, toggle := range map[string]*ui.Node{"Copilot": copilot, "MiniMax": minimax} {
		if toggle == nil || toggle.Role != "switch" || toggle.Value != 0 || !toggle.Focusable {
			t.Errorf("%s toggle = %+v, want an accessible opt-in switch", name, toggle)
		}
	}

	reg := &Registry{}
	reg.cfg.Plugins.Settings = map[string]map[string]any{
		"org.sysc.aiusage": {"track_minimax": true, "minimax_api_key": "saved-key"},
	}
	visible := pluginPanelSettings(reg, h, "org.sysc.aiusage", schema)
	var credential *ui.Node
	visit = func(n *ui.Node) {
		if n == nil {
			return
		}
		if n.Kind == ui.KindTextField && n.Action == "plugin-set:org.sysc.aiusage:minimax_api_key" {
			credential = n
		}
		for _, child := range n.Children {
			visit(child)
		}
	}
	visit(&ui.Node{Kind: ui.KindColumn, Children: visible})
	if credential == nil || !credential.Masked || credential.Text != "saved-key" {
		t.Fatalf("saved provider credential = %+v, want unchanged and masked", credential)
	}
	if value, ok := pluginSettingValueFromNode(h, credential); !ok || value != "saved-key" {
		t.Fatalf("masked credential value = %v, %t; want saved-key", value, ok)
	}
}

func TestPluginSettingRowRendersIntAsSlider(t *testing.T) {
	h := &PanelHost{}
	s := plugin.Setting{
		Key: "frame_rate", Type: plugin.SettingInt, Label: "Frame rate", Default: 60.0,
		Min: f64(1), Max: f64(240),
	}
	ctrl := settingRowControl(pluginSettingRow(nil, h, "org.sysc.screen-recorder", s))
	if ctrl == nil || ctrl.Kind != ui.KindSlider {
		t.Fatalf("int control = %+v, want KindSlider", ctrl)
	}
	if ctrl.Min != 1 || ctrl.Max != 240 {
		t.Fatalf("slider bounds = [%v,%v], want [1,240]", ctrl.Min, ctrl.Max)
	}
}

func TestPluginSettingRowRendersFolderAsTextField(t *testing.T) {
	h := &PanelHost{}
	s := plugin.Setting{
		Key: "directory", Type: plugin.SettingFolder, Label: "Output directory",
		Default: "~/Videos/Recordings",
	}
	ctrl := settingRowControl(pluginSettingRow(nil, h, "org.sysc.screen-recorder", s))
	if ctrl == nil || ctrl.Kind != ui.KindTextField {
		t.Fatalf("folder control = %+v, want KindTextField", ctrl)
	}
}

func TestPluginPanelSettingsVisibleWhenReplayDuration(t *testing.T) {
	h := &PanelHost{}
	schema := []plugin.Setting{
		{Key: "replay_enabled", Type: plugin.SettingBool, Label: "Replay buffer", Default: false},
		{Key: "replay_duration", Type: plugin.SettingInt, Label: "Replay duration (s)", Default: 30.0,
			Min: f64(5), Max: f64(3600),
			VisibleWhen: &plugin.VisibleWhen{Key: "replay_enabled", Equals: true}},
		{Key: "hide_inactive", Type: plugin.SettingBool, Label: "Hide when idle", Default: false},
	}
	reg := &Registry{}
	hidden := pluginPanelSettings(reg, h, "org.sysc.screen-recorder", schema)
	if strings.Contains(treeText(&ui.Node{Kind: ui.KindColumn, Children: hidden}), "Replay duration") {
		t.Fatal("replay_duration shown while replay_enabled is false")
	}
	reg.cfg.Plugins.Settings = map[string]map[string]any{
		"org.sysc.screen-recorder": {"replay_enabled": true},
	}
	shown := pluginPanelSettings(reg, h, "org.sysc.screen-recorder", schema)
	if !strings.Contains(treeText(&ui.Node{Kind: ui.KindColumn, Children: shown}), "Replay duration") {
		t.Fatal("replay_duration absent while replay_enabled is true")
	}
}

func TestPluginSettingApplyDecodedControlValue(t *testing.T) {
	reg := bindManifestPlugin(t, "ok", "org.sysc.screen-recorder", testRecorderPanelManifest,
		[]string{"org.sysc.screen-recorder"})
	h := &PanelHost{id: PanelSettings, section: "Plugins", search: ui.NewField("")}

	reg.mu.Lock()
	dir := &ui.Node{
		Kind: ui.KindTextField, Text: "/tmp/recordings",
		Action: "plugin-set:org.sysc.screen-recorder:directory",
		Name:   "Output directory",
	}
	if !reg.handlePluginManager(h, dir) {
		reg.mu.Unlock()
		t.Fatal("directory plugin-set not handled")
	}
	got := reg.cfg.Plugins.Settings["org.sysc.screen-recorder"]["directory"]
	if got != "/tmp/recordings" {
		reg.mu.Unlock()
		t.Fatalf("directory = %#v, want /tmp/recordings", got)
	}

	codec := &ui.Node{
		Kind: ui.KindMenu, Text: "focused",
		Action: "plugin-set:org.sysc.screen-recorder:video_source",
		Name:   "Video source",
	}
	if !reg.handlePluginManager(h, codec) {
		reg.mu.Unlock()
		t.Fatal("select plugin-set not handled")
	}
	got = reg.cfg.Plugins.Settings["org.sysc.screen-recorder"]["video_source"]
	if got != "focused" {
		reg.mu.Unlock()
		t.Fatalf("video_source = %#v, want focused", got)
	}

	before := reg.cfg.Plugins.Settings["org.sysc.screen-recorder"]["directory"]
	bad := &ui.Node{
		Kind: ui.KindSlider, Value: 9999, Min: 1, Max: 240,
		Action: "plugin-set:org.sysc.screen-recorder:frame_rate",
		Name:   "Frame rate",
	}
	if !reg.handlePluginManager(h, bad) {
		reg.mu.Unlock()
		t.Fatal("rejected slider should still be handled")
	}
	if reg.cfg.Plugins.Settings["org.sysc.screen-recorder"]["frame_rate"] != nil {
		reg.mu.Unlock()
		t.Fatalf("rejected frame_rate wrote %#v", reg.cfg.Plugins.Settings["org.sysc.screen-recorder"]["frame_rate"])
	}
	if reg.cfg.Plugins.Settings["org.sysc.screen-recorder"]["directory"] != before {
		reg.mu.Unlock()
		t.Fatal("rejected apply changed sibling settings")
	}
	reg.mu.Unlock()
}

// PanelHost.handle holds Registry.mu; apply must not re-lock it.
func TestPluginSettingApplyUnderRegistryLock(t *testing.T) {
	reg := bindManifestPlugin(t, "ok", "org.sysc.screen-recorder", testRecorderPanelManifest,
		[]string{"org.sysc.screen-recorder"})
	h := &PanelHost{id: PanelSettings, section: "Plugins", search: ui.NewField("")}

	done := make(chan bool, 1)
	go func() {
		reg.mu.Lock()
		defer reg.mu.Unlock()
		done <- reg.handlePluginManager(h, &ui.Node{
			Kind: ui.KindTextField, Text: "/tmp/locked-apply",
			Action: "plugin-set:org.sysc.screen-recorder:directory",
			Name:   "Output directory",
		})
	}()
	select {
	case ok := <-done:
		if !ok {
			t.Fatal("plugin-set under Registry.mu not handled")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("handlePluginManager deadlocked re-locking Registry.mu")
	}
	got := reg.cfg.Plugins.Settings["org.sysc.screen-recorder"]["directory"]
	if got != "/tmp/locked-apply" {
		t.Fatalf("directory = %#v, want /tmp/locked-apply", got)
	}
}

func TestPluginsSectionShowsInstalledRowAndSourcesDirectory(t *testing.T) {
	reg := bindTestPlugin(t, "ok")
	h := &PanelHost{id: PanelSettings, section: "Plugins", search: ui.NewField("")}
	root := settingsTree(reg, h)
	text := treeText(root)
	if !strings.Contains(text, "Timer") {
		t.Fatalf("missing plugin name in %q", text)
	}
	// A plugin in the user root is labelled Local, the store's name for it.
	if !strings.Contains(text, "1.0.0") || !strings.Contains(text, "Local") {
		t.Fatalf("missing metadata in %q", text)
	}
	// Capabilities are shown in the store detail and the consent step, not on
	// an Installed row (U4). The directory and Rescan live on Sources.
	h.pluginManagerTab = "sources"
	if text := treeText(settingsTree(reg, h)); !strings.Contains(text, "Rescan") {
		t.Fatalf("missing rescan on Sources in %q", text)
	}
	if !strings.Contains(pluginDirectoryLabel(reg), "org.sysc.timer") &&
		!strings.Contains(pluginDirectoryLabel(reg), reg.plugins.opts.Roots[0].Path) {
		t.Fatalf("directory label = %q", pluginDirectoryLabel(reg))
	}
}

func TestPluginManagerEnableDisableAndRetry(t *testing.T) {
	reg := bindTestPlugin(t, "ok")
	h := &PanelHost{id: PanelSettings, section: "Plugins", search: ui.NewField("")}
	reg.mu.Lock()
	if !reg.handlePluginManager(h, &ui.Node{Action: "plugin-enable:org.sysc.timer"}) {
		reg.mu.Unlock()
		t.Fatal("disable toggle not handled")
	}
	for _, id := range reg.cfg.Plugins.Enabled {
		if id == "org.sysc.timer" {
			reg.mu.Unlock()
			t.Fatal("plugin still enabled after toggle")
		}
	}
	if !reg.handlePluginManager(h, &ui.Node{Action: "plugin-enable:org.sysc.timer"}) {
		reg.mu.Unlock()
		t.Fatal("enable not handled")
	}
	found := false
	for _, id := range reg.cfg.Plugins.Enabled {
		if id == "org.sysc.timer" {
			found = true
		}
	}
	if !found {
		reg.mu.Unlock()
		t.Fatal("plugin was not enabled")
	}
	if !reg.handlePluginManager(h, &ui.Node{Action: "plugin-retry:org.sysc.timer"}) {
		reg.mu.Unlock()
		t.Fatal("retry not handled")
	}
	if !reg.handlePluginManager(h, &ui.Node{Action: "plugin-rescan"}) {
		reg.mu.Unlock()
		t.Fatal("rescan not handled")
	}
	reg.mu.Unlock()
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
