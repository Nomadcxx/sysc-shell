package shell

import (
	"strings"
	"testing"
	"time"

	"github.com/Nomadcxx/sysc-shell/internal/plugin"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

func f64(v float64) *float64 { return &v }

func settingRowControl(row *ui.Node) *ui.Node {
	if row == nil || len(row.Children) < 2 {
		return nil
	}
	return row.Children[1]
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
	if row.Kind != ui.KindColumn {
		t.Fatalf("select row kind = %d, want column so the open list can lay out", row.Kind)
	}
}

func TestPluginSettingRowRendersBoolAsCheckbox(t *testing.T) {
	h := &PanelHost{}
	s := plugin.Setting{Key: "show_cursor", Type: plugin.SettingBool, Label: "Show cursor", Default: true}
	row := pluginSettingRow(nil, h, "org.sysc.screen-recorder", s)
	if row == nil || row.Kind != ui.KindRow || len(row.Children) < 2 {
		t.Fatalf("bool row = %+v, want checkbox then label", row)
	}
	box := row.Children[0]
	if box.Kind != ui.KindToggle || box.Role != "checkbox" {
		t.Fatalf("bool control = kind %d role %q, want toggle checkbox", box.Kind, box.Role)
	}
	if row.Children[1].Text != "Show cursor" {
		t.Fatalf("bool label = %q", row.Children[1].Text)
	}
	if !row.Children[1].Focusable || row.Children[1].Action != box.Action {
		t.Fatal("checkbox label must share the toggle action")
	}
}

func TestPluginPanelSettingsWrapsGroupsInCapsules(t *testing.T) {
	h := &PanelHost{}
	schema := []plugin.Setting{
		{Key: "frame_rate", Type: plugin.SettingInt, Label: "Frame rate", Default: 60.0, Min: f64(1), Max: f64(240)},
		{Key: "directory", Type: plugin.SettingFolder, Label: "Output directory", Default: "~/Videos"},
	}
	nodes := pluginPanelSettings(nil, h, "org.sysc.screen-recorder", schema)
	if len(nodes) != 2 {
		t.Fatalf("groups = %d, want one capsule per section", len(nodes))
	}
	for i, title := range []string{"Capture", "File"} {
		n := nodes[i]
		if n.Kind != ui.KindCapsule {
			t.Fatalf("%s kind = %d, want KindCapsule", title, n.Kind)
		}
		if !strings.Contains(treeText(n), title) {
			t.Fatalf("%s missing from %q", title, treeText(n))
		}
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

func TestAIUsagePanelRendersProviderAndPluginSettings(t *testing.T) {
	h := &PanelHost{}
	track := func(key, label string, enabled bool) plugin.Setting {
		return plugin.Setting{Key: key, Type: plugin.SettingBool, Label: label, Default: enabled}
	}
	key := func(name, label, provider string) plugin.Setting {
		return plugin.Setting{
			Key: name, Type: plugin.SettingString, Label: label, Default: "",
			VisibleWhen: &plugin.VisibleWhen{Key: "track_" + provider, Equals: true},
		}
	}
	schema := []plugin.Setting{
		track("track_claude", "Track Claude", true),
		track("track_codex", "Track Codex", true),
		track("track_commandcode", "Track Command Code", true),
		track("track_copilot", "Track Copilot", false),
		track("track_ollama", "Track Ollama", false),
		track("track_minimax", "Track MiniMax", false),
		track("track_opencode_go", "Track OpenCode Go", false),
		track("track_synthetic", "Track Synthetic", false),
		key("commandcode_api_key", "Command Code API key", "commandcode"),
		key("ollama_api_key", "Ollama API key", "ollama"),
		key("minimax_api_key", "MiniMax API key", "minimax"),
		key("opencode_go_api_key", "OpenCode Go API key", "opencode_go"),
		key("synthetic_api_key", "Synthetic API key", "synthetic"),
		{Key: "refresh_interval", Type: plugin.SettingInt, Label: "Refresh interval (s)", Default: 300.0, Min: f64(60), Max: f64(3600)},
		track("alerts_enabled", "Threshold alerts", true),
		{Key: "warn_threshold", Type: plugin.SettingInt, Label: "Warn at %", Default: 85.0,
			VisibleWhen: &plugin.VisibleWhen{Key: "alerts_enabled", Equals: true}},
		{Key: "critical_threshold", Type: plugin.SettingInt, Label: "Critical at %", Default: 95.0,
			VisibleWhen: &plugin.VisibleWhen{Key: "alerts_enabled", Equals: true}},
		{Key: "alert_thresholds", Type: plugin.SettingString, Label: "Per-provider warn overrides", Default: "",
			VisibleWhen: &plugin.VisibleWhen{Key: "alerts_enabled", Equals: true}},
		{Key: "alert_window_scope", Type: plugin.SettingSelect, Label: "Alert windows", Default: "all",
			VisibleWhen: &plugin.VisibleWhen{Key: "alerts_enabled", Equals: true},
			Options:     []plugin.SettingOption{{Value: "all", Label: "All windows"}, {Value: "primary", Label: "Primary window only"}}},
		{Key: "alert_cooldown", Type: plugin.SettingSelect, Label: "Re-alert cooldown", Default: "0",
			VisibleWhen: &plugin.VisibleWhen{Key: "alerts_enabled", Equals: true},
			Options:     []plugin.SettingOption{{Value: "0", Label: "Once per window"}, {Value: "60", Label: "Every hour"}}},
		{Key: "history_retention", Type: plugin.SettingSelect, Label: "History retention (lines)", Default: "2000",
			Options: []plugin.SettingOption{{Value: "500"}, {Value: "2000"}, {Value: "10000"}}},
	}
	reg := &Registry{}
	reg.cfg.Plugins.Settings = map[string]map[string]any{
		"org.sysc.aiusage": {
			"track_ollama": true, "track_minimax": true, "track_opencode_go": true, "track_synthetic": true,
		},
	}
	nodes := pluginPanelSettings(reg, h, "org.sysc.aiusage", schema)
	text := treeText(&ui.Node{Kind: ui.KindColumn, Children: nodes})
	for _, want := range []string{
		"Providers", "Track Claude", "Track Codex", "Track Command Code", "Track Copilot",
		"Track Ollama", "Track MiniMax", "Track OpenCode Go", "Track Synthetic", "Command Code API key",
		"Ollama API key", "MiniMax API key", "OpenCode Go API key", "Synthetic API key", "Refresh interval (s)",
		"Alerts", "Warn at %", "Critical at %", "Per-provider warn overrides",
		"Alert windows", "Re-alert cooldown", "History", "History retention (lines)",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("AI Usage panel setting %q missing from %q", want, text)
		}
	}
}

func TestPluginAPIKeySettingIsMaskedAndCanBeRevealed(t *testing.T) {
	reg := bindTestPlugin(t, "ok")
	h := &PanelHost{id: PanelSettings, section: "Plugins", search: ui.NewField(""), fields: map[string]*ui.Field{}}
	setting := plugin.Setting{Key: "opencode_go_api_key", Type: plugin.SettingString, Label: "OpenCode Go API key"}
	const secret = "opencode-test-key"
	const store = "org.sysc.aiusage.opencode_go_api_key"
	control := pluginSettingControl(h, setting, secret, "plugin-set:org.sysc.aiusage:opencode_go_api_key", store)
	if control.Kind != ui.KindRow || len(control.Children) != 2 {
		t.Fatalf("secret control = %+v, want field plus show button", control)
	}
	field, toggle := control.Children[0], control.Children[1]
	if !field.Masked || ui.DisplayText(field) != strings.Repeat(string(ui.MaskRune), len([]rune(secret))) {
		t.Fatalf("secret field is not masked: %+v", field)
	}
	if toggle.Text != "Show" || !toggle.Focusable || toggle.Name != "Show OpenCode Go API key" {
		t.Fatalf("reveal control = %+v", toggle)
	}

	reg.mu.Lock()
	handled := reg.handlePluginManager(h, toggle)
	reg.mu.Unlock()
	if !handled {
		t.Fatal("show action was not handled")
	}
	control = pluginSettingControl(h, setting, secret, "plugin-set:org.sysc.aiusage:opencode_go_api_key", store)
	field, toggle = control.Children[0], control.Children[1]
	if field.Masked || ui.DisplayText(field) != secret {
		t.Fatalf("revealed field = %+v", field)
	}
	if toggle.Text != "Hide" || toggle.Name != "Hide OpenCode Go API key" {
		t.Fatalf("hide control = %+v", toggle)
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
