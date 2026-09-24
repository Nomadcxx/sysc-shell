package shell

import (
	"image"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Nomadcxx/sysc-shell/internal/config"
	"github.com/Nomadcxx/sysc-shell/internal/plugin"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
	v1 "github.com/Nomadcxx/sysc-shell/plugin/v1"
)

// Opt-in current-source gate. It runs a disposable AI Usage build through the
// real plugin Supervisor and shell layout, with credential discovery fenced
// off and every provider except the no-credential setup case disabled.
func TestAIUsageStagedPanelThroughShell(t *testing.T) {
	dir := os.Getenv("SYSC_AIUSAGE_TEST_DIR")
	if dir == "" {
		t.Skip("set SYSC_AIUSAGE_TEST_DIR to a staged AI Usage plugin")
	}
	if _, err := os.Stat(filepath.Join(dir, "manifest.json")); err != nil {
		t.Fatalf("staged plugin manifest: %v", err)
	}

	// The Go setup row must be visible, but no local OpenCode credential may
	// turn this layout-only test into a network request.
	t.Setenv("OPENCODE_GO_API_KEY", "")
	t.Setenv("OPENCODE_AUTH", filepath.Join(t.TempDir(), "no-auth.json"))
	const id = "org.sysc.aiusage"
	root := t.TempDir()
	if err := os.Symlink(dir, filepath.Join(root, "aiusage")); err != nil {
		t.Fatal(err)
	}
	cfg := pluginConfig(root)
	cfg.Plugins.Enabled = []string{id}
	cfg.Plugins.Settings = map[string]map[string]any{id: {
		"track_claude": false, "track_codex": false, "track_commandcode": false,
		"track_copilot": false, "track_ollama": false, "track_minimax": false,
		"track_opencode_go": true, "track_synthetic": false,
	}}
	cfg.Bar.Right = []config.Item{{ID: "plugin", Plugin: id, Entry: "bar", Instance: id + "-1"}}
	reg := NewRegistry(cfg)
	t.Cleanup(reg.Close)
	if err := reg.BindPlugins(PluginHostOptions{
		Roots: []plugin.Root{{Path: root, Source: plugin.SourceUser}}, StateDir: t.TempDir(),
	}); err != nil {
		t.Fatal(err)
	}
	newHosts(t, reg, map[uint32]string{1: "DP-1"})
	res, err := reg.plugins.openPanel(id, v1.PanelParams{
		Entry: "panel", Output: "DP-1", Generation: 1, Instance: id + "-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.ViewID == "" {
		t.Fatal("panel open returned an empty view id")
	}
	_ = drainAux(t, reg, 2)
	waitPluginPanelRoot(t, reg)

	reg.mu.Lock()
	host := reg.panelHosts[PanelPlugin]
	var rootNode *ui.Node
	var layoutErr error
	if host == nil {
		layoutErr = os.ErrNotExist
	} else {
		layoutErr = host.configure(750, 430, 120)
		rootNode = host.root
	}
	var rendered strings.Builder
	if rootNode != nil {
		dumpText(rootNode, &rendered)
	}
	reg.mu.Unlock()
	if layoutErr != nil {
		t.Fatalf("AI Usage panel layout: %v", layoutErr)
	}
	for _, want := range []string{"AI Usage", "OpenCode Go", "Track OpenCode Go", "OpenCode Go API key"} {
		if !strings.Contains(rendered.String(), want) {
			t.Errorf("rendered sysc-shell panel omits %q", want)
		}
	}

	var keyField, revealButton *ui.Node
	var find func(*ui.Node)
	find = func(n *ui.Node) {
		if n == nil {
			return
		}
		if n.Kind == ui.KindTextField && n.Action == "plugin-set:"+id+":opencode_go_api_key" {
			keyField = n
		}
		if n.Kind == ui.KindButton && n.Action == "plugin-secret:"+id+".opencode_go_api_key" {
			revealButton = n
		}
		for _, child := range n.Children {
			find(child)
		}
	}
	find(rootNode)
	if keyField == nil || !keyField.Masked || !keyField.Focusable {
		t.Fatalf("OpenCode Go API-key field is missing, visible, or not keyboard-focusable: %+v", keyField)
	}
	if revealButton == nil || !revealButton.Focusable || revealButton.Text != "Show" || revealButton.Name != "Show OpenCode Go API key" {
		t.Fatalf("OpenCode Go API-key reveal control is absent or inaccessible: %+v", revealButton)
	}
	if len(ui.Focusables(rootNode)) == 0 {
		t.Fatal("staged AI Usage panel has no keyboard focus targets")
	}
	if path := os.Getenv("SYSC_AIUSAGE_TEST_PNG"); path != "" {
		const width, height = 750, 430
		pixels := make([]byte, width*height*4)
		reg.mu.Lock()
		host = reg.panelHosts[PanelPlugin]
		if host == nil {
			reg.mu.Unlock()
			t.Fatal("AI Usage panel host disappeared before render")
		}
		err := host.render(pixels, width, height, width*4)
		reg.mu.Unlock()
		if err != nil {
			t.Fatalf("render staged AI Usage panel: %v", err)
		}

		// ponytail: keep visual capture opt-in and refuse to overwrite prior output.
		f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if err != nil {
			t.Fatalf("create rendered panel PNG: %v", err)
		}
		if err := png.Encode(f, &image.RGBA{Pix: pixels, Stride: width * 4, Rect: image.Rect(0, 0, width, height)}); err != nil {
			_ = f.Close()
			_ = os.Remove(path)
			t.Fatalf("encode rendered panel PNG: %v", err)
		}
		if err := f.Close(); err != nil {
			_ = os.Remove(path)
			t.Fatalf("close rendered panel PNG: %v", err)
		}

		f, err = os.Open(path)
		if err != nil {
			t.Fatalf("open rendered panel PNG: %v", err)
		}
		img, err := png.Decode(f)
		_ = f.Close()
		if err != nil {
			t.Fatalf("decode rendered panel PNG: %v", err)
		}
		if got := img.Bounds(); got.Dx() != 750 || got.Dy() != 430 {
			t.Fatalf("rendered panel dimensions = %v, want 750x430", got)
		}
	}
}
