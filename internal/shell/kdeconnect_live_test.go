package shell

import (
	"os"

	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Nomadcxx/sysc-shell/internal/platform/wayland"
	"github.com/Nomadcxx/sysc-shell/internal/plugin"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
	v1 "github.com/Nomadcxx/sysc-shell/plugin/v1"
)

// Opt-in cross-repository gate. Uses the real plugin, assets and paired daemon,
// with in-memory host surfaces: it never opens a window or activates phone actions.
func TestKDEConnectPairedHostLifecycle(t *testing.T) {
	dir := os.Getenv("SYSC_KDECONNECT_TEST_DIR")
	if dir == "" {
		t.Skip("set SYSC_KDECONNECT_TEST_DIR to an installed candidate")
	}
	const id = "org.sysc.kdeconnect"
	root := t.TempDir()
	if err := os.Symlink(dir, filepath.Join(root, "kdeconnect")); err != nil {
		t.Fatal(err)
	}
	cfg := pluginConfig(root)
	cfg.Plugins.Enabled = []string{id}
	cfg.Bar.Right[0].Plugin = id
	reg := NewRegistry(cfg)
	t.Cleanup(reg.Close)
	go func() {
		for {
			select {
			case <-reg.closed:
				return
			case <-reg.invalidations:
			case <-reg.aux:
			}
		}
	}()
	if err := reg.BindPlugins(PluginHostOptions{Roots: []plugin.Root{{Path: root, Source: plugin.SourceUser}}, StateDir: t.TempDir()}); err != nil {
		t.Fatal(err)
	}
	newHosts(t, reg, map[uint32]string{1: "DP-1"})
	wait := func(label string, ready func() bool) {
		t.Helper()
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) {
			if ready() {
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
		reg.plugins.mu.Lock()
		defer reg.plugins.mu.Unlock()
		if slot := reg.plugins.slots[id]; slot != nil {
			t.Logf("runtime: %+v", slot.rt.Status())
		}
		for _, v := range reg.plugins.views {
			var txt strings.Builder
			dumpText(v.Root, &txt)
			t.Logf("view %s kind=%s failed=%v label=%s text=%s", v.ID, v.Kind, v.Failed, v.Label, txt.String())
		}
		t.Fatalf("timed out: %s", label)
	}
	bar := reg.bars[1]
	waitBar := func() {
		wait("bar snapshot", func() bool {
			reg.plugins.mu.Lock()
			defer reg.plugins.mu.Unlock()
			for _, v := range reg.plugins.views {
				if v.Kind == v1.ViewBar && v.Root != nil && !v.Failed {
					return true
				}
			}
			return false
		})
	}
	open := func() {
		waitBar()
		if err := bar.Configure(800, BarHeight, 120); err != nil {
			t.Fatal(err)
		}
		action, x, y := pluginHitPoint(bar)
		if action == "" {
			t.Fatal("no clickable bar icon")
		}
		bar.Handle(wayland.Event{Kind: wayland.EventPointerEnter, X: x, Y: y})
		bar.Handle(wayland.Event{Kind: wayland.EventPointerPress, Button: 272, X: x, Y: y})
		bar.Handle(wayland.Event{Kind: wayland.EventPointerRelease, Button: 272, X: x, Y: y})
	}
	var imageReady func(*ui.Node) bool
	imageReady = func(n *ui.Node) bool {
		if n == nil {
			return false
		}
		if n.Kind == ui.KindImage && n.Image != nil {
			return true
		}
		for _, c := range n.Children {
			if imageReady(c) {
				return true
			}
		}
		return false
	}
	waitPanel := func() {
		wait("paired panel with decoded artwork", func() bool {
			reg.mu.Lock()
			defer reg.mu.Unlock()
			reg.plugins.mu.Lock()
			defer reg.plugins.mu.Unlock()
			v := reg.plugins.panel
			if v == nil || v.Failed || v.Root == nil || reg.panelHosts[PanelPlugin] == nil {
				return false
			}
			var text strings.Builder
			dumpText(v.Root, &text)
			return strings.Contains(text.String(), "Tap to ping") && imageReady(v.Root)
		})
		reg.mu.Lock()
		host := reg.panelHosts[PanelPlugin]
		err := host.configure(400, 520, 120)
		reg.mu.Unlock()
		if err != nil {
			t.Fatal(err)
		}
	}
	open()
	t.Log("opened; waiting for paired panel")
	waitPanel()
	reg.ClosePanel(PanelPlugin)
	open()
	t.Log("opened; waiting for paired panel")
	waitPanel()
	// Exercise Retry via the real failure-panel keyboard handler.
	reg.mu.Lock()
	host := reg.panelHosts[PanelPlugin]
	host.root = pluginPanelError("test retry", true)
	host.focus = ui.Focusables(host.root)
	host.roving.Count = len(host.focus)
	host.roving.Set(1)
	before := reg.plugins.pid(id)
	reg.mu.Unlock()
	host.handle(reg)(wayland.Event{Kind: wayland.EventKeyPress, Key: keyEnter})
	wait("replacement process", func() bool { pid := reg.plugins.pid(id); return pid > 0 && pid != before })
	open()
	t.Log("opened; waiting for paired panel")
	waitPanel()
	reg.ClosePanel(PanelPlugin)
	open()
	t.Log("opened; waiting for paired panel")
	waitPanel()
}
