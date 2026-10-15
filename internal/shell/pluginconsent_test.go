package shell

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/Nomadcxx/sysc-shell/internal/config"
	"github.com/Nomadcxx/sysc-shell/internal/plugin"
	"github.com/Nomadcxx/sysc-shell/internal/plugin/store"
)

// bindConsentPlugin places the helper plugin in a user root beside an empty
// managed root, records an approval for it that its bytes do not match, and
// opens the plugin manager on it.
func bindConsentPlugin(t *testing.T) (*Registry, *PanelHost, string, string) {
	t.Helper()
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	user, managed := t.TempDir(), t.TempDir()
	dir := filepath.Join(user, "org.sysc.timer")
	if _, err := plugin.WriteHelperPlugin(dir, self, "", testTimerManifest); err != nil {
		t.Fatal(err)
	}
	ledger, err := plugin.LoadConsent(plugin.ConsentPath(managed))
	if err != nil {
		t.Fatal(err)
	}
	ledger.Set(plugin.SourceUser, "org.sysc.timer", plugin.ConsentRecord{ContentSHA256: "0000"})
	if err := ledger.Save(plugin.ConsentPath(managed)); err != nil {
		t.Fatal(err)
	}

	reg, host, _ := openPluginManagerTestPanel(t, config.Default(), store.State{})
	reg.configPath = filepath.Join(t.TempDir(), "config.json")
	reg.mu.Lock()
	reg.cfg.Plugins.Enabled = []string{"org.sysc.timer"}
	reg.mu.Unlock()
	if err := reg.BindPlugins(PluginHostOptions{Roots: []plugin.Root{
		{Path: user, Source: plugin.SourceUser}, {Path: managed, Source: plugin.SourceManaged},
	}, StateDir: t.TempDir()}); err != nil {
		t.Fatal(err)
	}
	return reg, host, dir, managed
}

func waitConsent(t *testing.T, reg *Registry, want plugin.ConsentState) plugin.Status {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		st := reg.plugins.status("org.sysc.timer")
		if st.State == plugin.StateRunning && st.Consent.State == want {
			return st
		}
		if time.Now().After(deadline) {
			t.Fatalf("status = %+v, want running with consent %q", st, want)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestAChangedPluginRunsWithoutCapabilitiesUntilApproved(t *testing.T) {
	reg, host, dir, managed := bindConsentPlugin(t)
	st := waitConsent(t, reg, plugin.ConsentChanged)
	reg.plugins.mu.Lock()
	rt := reg.plugins.slots["org.sysc.timer"].rt
	reg.plugins.mu.Unlock()
	if rt.Allows(plugin.CapNotifications) {
		t.Fatal("a changed copy kept its capabilities")
	}

	reg.mu.Lock()
	reg.rebuildPanel(host)
	approve := pluginStoreFindAction(host.root, "plugins-approve:org.sysc.timer")
	changed := pluginStoreHasText(host.root, "Changed since you approved it. It runs without its capabilities until you approve it again.")
	reg.mu.Unlock()
	if approve == nil || !changed {
		t.Fatalf("manager row: approve=%v changed=%v for %+v", approve != nil, changed, st.Consent)
	}

	st2 := store.New(store.Options{
		CacheDir:  t.TempDir(),
		MediaDir:  t.TempDir(),
		Installer: &store.Installer{Root: managed, Arch: runtime.GOARCH},
		Sources:   func() []store.Source { return nil },
		Local:     func() map[string]string { return map[string]string{"org.sysc.timer": dir} },
	})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); st2.Run(ctx) }()
	t.Cleanup(func() { cancel(); <-done })
	reg.mu.Lock()
	reg.pluginStore = st2
	reg.handlePluginManager(host, approve)
	reg.mu.Unlock()

	waitConsent(t, reg, plugin.ConsentApproved)
	deadline := time.Now().Add(10 * time.Second)
	for {
		reg.plugins.mu.Lock()
		slot := reg.plugins.slots["org.sysc.timer"]
		reg.plugins.mu.Unlock()
		if slot != nil && slot.rt.Allows(plugin.CapNotifications) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("approving the copy did not restore its capabilities")
		}
		time.Sleep(10 * time.Millisecond)
	}
	reg.mu.Lock()
	reg.rebuildPanel(host)
	gone := pluginStoreFindAction(host.root, "plugins-approve:org.sysc.timer") == nil
	reg.mu.Unlock()
	if !gone {
		t.Fatal("the approved row still offers Approve")
	}
}

func TestAnUnrecordedPluginKeepsItsGrantAndIsAskedOnce(t *testing.T) {
	reg, host, _, managed := bindConsentPlugin(t)
	waitConsent(t, reg, plugin.ConsentChanged)
	reg.plugins.stopPlugin("org.sysc.timer")
	if err := os.Remove(plugin.ConsentPath(managed)); err != nil {
		t.Fatal(err)
	}
	if err := reg.plugins.syncEnabled(); err != nil {
		t.Fatal(err)
	}
	waitConsent(t, reg, plugin.ConsentUnrecorded)
	reg.plugins.mu.Lock()
	rt := reg.plugins.slots["org.sysc.timer"].rt
	reg.plugins.mu.Unlock()
	if !rt.Allows(plugin.CapNotifications) {
		t.Fatal("a plugin enabled before approvals were recorded lost its grant")
	}
	reg.mu.Lock()
	reg.rebuildPanel(host)
	ask := pluginStoreFindAction(host.root, "plugins-approve:org.sysc.timer") != nil
	reg.mu.Unlock()
	if !ask {
		t.Fatal("an unrecorded copy is not offered approval")
	}
}

func TestTheManagerStatesTheBoundaryAPluginRunsInside(t *testing.T) {
	for b, want := range map[plugin.Boundary]string{
		plugin.BoundaryConfined:   "Confined to its own files and the paths it declared. Network access is not restricted.",
		plugin.BoundaryLimited:    "Unconfined: it runs as your user with full file and network access.",
		plugin.BoundaryNoLandlock: "This kernel provides no filesystem confinement, so it runs with full file and network access.",
		plugin.BoundaryNone:       "",
		"":                        "",
	} {
		if got := pluginBoundaryLine(nil, b); got != want {
			t.Errorf("%q: %q, want %q", b, got, want)
		}
	}
}
