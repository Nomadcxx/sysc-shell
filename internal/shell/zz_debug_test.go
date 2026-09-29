package shell

import (
	"testing"
	"time"

	"github.com/Nomadcxx/sysc-shell/internal/plugin/store"
)

func TestZZSourcesClick(t *testing.T) {
	for _, withPlugin := range []bool{false, true} {
		var reg *Registry
		if withPlugin {
			reg = bindTestPlugin(t, "ok")
		} else {
			reg = newPanelRegistry(t)
		}
		reg.mu.Lock()
		reg.pluginStoreSnapshot = store.State{Sources: []store.SourceState{{Name: "sysc", URL: "https://github.com/Nomadcxx/sysc-plugins", FetchedAt: time.Now(), Plugins: 1}}}
		reg.mu.Unlock()
		if err := reg.HandlePanelByName("open", "settings", "Plugins"); err != nil {
			t.Fatal(err)
		}
		reqs := drainAux(t, reg, 2)
		panel := reqs[1].Open
		w, h := int(panel.Width), int(panel.Height)
		if err := panel.Callbacks.Configure(w, h, 150); err != nil {
			t.Fatalf("configure: %v", err)
		}
		pix := make([]byte, w*h*4*2)
		if err := panel.Callbacks.Render(pix, w, h, w*4); err != nil {
			t.Fatalf("render installed: %v", err)
		}
		reg.mu.Lock()
		host := reg.panelHosts[PanelSettings]
		tab := pluginStoreFindAction(host.root, "plugins-tab:sources")
		reg.mu.Unlock()
		pressAt(reg, host, tab.Bounds.X+tab.Bounds.W/2, tab.Bounds.Y+tab.Bounds.H/2, 0)
		reg.mu.Lock()
		open := reg.panelHosts[PanelSettings] != nil
		tabNow := host.pluginManagerTab
		reg.mu.Unlock()
		cerr := panel.Callbacks.Configure(w, h, 150)
		rerr := panel.Callbacks.Render(pix, w, h, w*4)
		t.Logf("plugin=%v open=%v tab=%q configure=%v render=%v", withPlugin, open, tabNow, cerr, rerr)
	}
}
