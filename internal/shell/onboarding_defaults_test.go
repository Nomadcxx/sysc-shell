package shell

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/Nomadcxx/sysc-shell/internal/config"
	"github.com/Nomadcxx/sysc-shell/internal/plugin"
	"github.com/Nomadcxx/sysc-shell/internal/plugin/store"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
	"github.com/Nomadcxx/sysc-shell/plugin/catalog"
)

func onbTestListing(id string, commands ...string) store.Listing {
	l := modelListing(id, id, onbSource, "utilities", "1.0.0")
	l.Entry.Requires = catalog.Requires{Commands: commands}
	l.Resolution.Release = &l.Entry.Release
	return l
}

func onbTestCommands(cmds ...string) func(string) bool {
	return func(cmd string) bool { return slices.Contains(cmds, cmd) }
}

func onbRecKeys(recs []onbRec) []string {
	var keys []string
	for _, rec := range recs {
		keys = append(keys, rec.key)
	}
	return keys
}

// The owner's default set (2026-11-01): sound for everyone, battery and Wi-Fi
// by hardware, Calendar and Pomodoro Timer for everyone, ThinkPad by
// hardware, and any plugin whose required tool is installed -- except tools
// on nearly every install, which say nothing about the user.
func TestOnboardingRecommendationsFollowTheDefaultSet(t *testing.T) {
	listings := []store.Listing{
		onbTestListing("org.sysc.calendar"),
		onbTestListing("org.sysc.timer"),
		onbTestListing("org.sysc.cat"),
		onbTestListing("org.sysc.thinkpad"),
		onbTestListing("org.sysc.kdeconnect", "kdeconnect-cli"),
		onbTestListing("org.sysc.mini-docker", "docker"),
		onbTestListing("org.sysc.moonbit", "moonbit"),
		onbTestListing("org.sysc.updates", "pacman"),
		onbTestListing("org.sysc.wallpaper-depth", "python3"),
	}
	community := onbTestListing("org.other.docker", "docker")
	community.Source = "community"
	listings = append(listings, community)

	noVolume := config.Default()
	noVolume.Bar.Right = slices.DeleteFunc(slices.Clone(noVolume.Bar.Right), func(it config.Item) bool { return it.ID == "volume" })

	installed := onbTestListing("org.sysc.timer")
	installed.Installed = &store.Record{Source: onbSource, Version: "1.0.0"}

	cases := []struct {
		name     string
		machine  onbMachine
		cfg      config.Config
		listings []store.Listing
		local    []plugin.Candidate
		queued   map[string]onbInstall
		want     []string
	}{{
		name:     "desktop with nothing extra",
		machine:  onbMachine{has: onbTestCommands("pacman", "python3")},
		cfg:      config.Default(),
		listings: listings,
		want:     []string{"org.sysc.calendar", "org.sysc.timer"},
	}, {
		name:     "laptop with tools",
		machine:  onbMachine{battery: true, wifi: true, thinkpad: true, has: onbTestCommands("kdeconnect-cli", "docker", "moonbit", "pacman", "python3")},
		cfg:      noVolume,
		listings: listings,
		want: []string{"item:volume", "item:battery", "item:wifi", "org.sysc.calendar", "org.sysc.timer",
			"org.sysc.thinkpad", "org.sysc.kdeconnect", "org.sysc.mini-docker", "org.sysc.moonbit"},
	}, {
		name:     "nothing already there is offered again",
		machine:  onbMachine{battery: true, has: onbTestCommands("docker", "moonbit")},
		cfg:      withRightItem(config.Default(), "battery"),
		listings: append(slices.Clone(listings[2:]), installed),
		local:    []plugin.Candidate{{Manifest: plugin.Manifest{ID: "org.sysc.mini-docker"}}},
		queued:   map[string]onbInstall{"org.sysc.moonbit": {}},
		want:     nil,
	}, {
		name:     "a failed install is offered again",
		machine:  onbMachine{has: onbTestCommands("moonbit")},
		cfg:      config.Default(),
		listings: listings[6:7],
		queued:   map[string]onbInstall{"org.sysc.moonbit": {failed: true}},
		want:     []string{"org.sysc.moonbit"},
	}, {
		name:    "no catalog yet",
		machine: onbMachine{battery: true},
		cfg:     config.Default(),
		want:    []string{"item:battery"},
	}}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := onbRecKeys(onbRecommendations(tc.machine, tc.cfg, tc.listings, tc.local, tc.queued))
			if !slices.Equal(got, tc.want) {
				t.Fatalf("recommendations = %v, want %v", got, tc.want)
			}
		})
	}
}

func withRightItem(cfg config.Config, id string) config.Config {
	cfg.Bar.Right = append(slices.Clone(cfg.Bar.Right), config.Item{ID: id})
	return cfg
}

func TestHasWirelessInterfaceReadsTheKernelList(t *testing.T) {
	root := t.TempDir()
	for _, dir := range []string{"lo", "eth0"} {
		if err := os.MkdirAll(filepath.Join(root, dir), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if hasWirelessInterface(root) {
		t.Fatal("wired interfaces reported as wireless")
	}
	if err := os.MkdirAll(filepath.Join(root, "wlan0", "wireless"), 0o755); err != nil {
		t.Fatal(err)
	}
	if !hasWirelessInterface(root) {
		t.Fatal("a wireless interface was missed")
	}
	if hasWirelessInterface(filepath.Join(root, "missing")) {
		t.Fatal("an unreadable list reported wireless")
	}
}

// The Plugins step stages built-in items like plugins: checked by default,
// untickable, and written only at Finish.
func TestOnboardingFinishPlacesCheckedBuiltInItems(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	reg := newPanelRegistry(t)
	reg.onboardingHardware = func() onbMachine { return onbMachine{battery: true, wifi: true} }
	configPath := setOnboardingTestConfig(t, reg, reg.cfg.Clone())
	if err := reg.OpenPanel(PanelOnboarding, 7, Trigger{OutW: 1536, OutH: 864}); err != nil {
		t.Fatal(err)
	}
	reg.mu.Lock()
	h := reg.panelHosts[PanelOnboarding]
	h.onbPage = onbPlugins
	reg.rebuildPanel(h)
	wifi := byAction(h.root, "onb-rec:item:wifi")
	if wifi == nil || byAction(h.root, "onb-rec:item:battery") == nil {
		reg.mu.Unlock()
		t.Fatal("the Plugins step does not offer battery and Wi-Fi on a laptop")
	}
	if wifi.Value != 1 {
		reg.mu.Unlock()
		t.Fatal("Wi-Fi does not start checked")
	}
	if !h.activateOnboarding(reg, wifi) {
		reg.mu.Unlock()
		t.Fatal("the Wi-Fi tile did not toggle")
	}
	if got := byAction(h.root, "onb-rec:item:wifi"); got == nil || got.Value != 0 {
		reg.mu.Unlock()
		t.Fatal("Wi-Fi stayed checked after it was unticked")
	}
	if onbConfigHasItem(reg.cfg, "battery") {
		reg.mu.Unlock()
		t.Fatal("battery was written before Finish")
	}
	if err := reg.finishOnboardingLocked(h); err != nil {
		reg.mu.Unlock()
		t.Fatal(err)
	}
	reg.mu.Unlock()
	saved, err := config.LoadRequired(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if !onbConfigHasItem(saved, "battery") {
		t.Fatal("Finish did not place the checked battery item")
	}
	if onbConfigHasItem(saved, "wifi") {
		t.Fatal("Finish placed the unticked Wi-Fi item")
	}
}

// Finish waits for the installs "Install and continue" queued, and an
// install that failed is reported and skipped rather than holding setup.
func TestOnboardingFinishWaitsForQueuedInstalls(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	reg := newPanelRegistry(t)
	setOnboardingTestConfig(t, reg, reg.cfg.Clone())
	if err := reg.OpenPanel(PanelOnboarding, 7, Trigger{OutW: 1536, OutH: 864}); err != nil {
		t.Fatal(err)
	}
	reg.mu.Lock()
	defer reg.mu.Unlock()
	h := reg.panelHosts[PanelOnboarding]
	listing := onbTestListing("org.sysc.calendar")
	reg.pluginStoreSnapshot = store.State{Listings: []store.Listing{listing}}
	reg.onboardingInstalls = map[string]onbInstall{"org.sysc.calendar": {name: "Calendar"}}
	h.onbPage = onbReady
	reg.rebuildPanel(h)
	if finish := byAction(h.root, "onb-finish"); finish == nil || !finish.AriaDisabled {
		t.Fatal("Finish is enabled while a plugin is installing")
	}
	if names := onbSelectedPlugins(reg, h); !slices.Contains(names, "Calendar (installing)") {
		t.Fatalf("Review plugins = %v, want the install in progress", names)
	}
	if err := reg.finishOnboardingLocked(h); err == nil {
		t.Fatal("Finish completed while a plugin was installing")
	}

	listing.Err = errors.New("download failed")
	reg.pluginStoreSnapshot = store.State{Listings: []store.Listing{listing}}
	reg.rebuildPanel(h)
	if names := onbSelectedPlugins(reg, h); !slices.Contains(names, "Calendar (install failed)") {
		t.Fatalf("Review plugins = %v, want the failed install", names)
	}
	if err := reg.finishOnboardingLocked(h); err != nil {
		t.Fatalf("a failed install held Finish: %v", err)
	}
}

// With checked catalog plugins the step shows the store's consent lines and
// the primary action names the install; without a store it reports the
// catalog unavailable rather than offering what it cannot install.
func TestOnboardingPluginsStepShowsConsentForCheckedPlugins(t *testing.T) {
	reg := newPanelRegistry(t)
	if err := reg.OpenPanel(PanelOnboarding, 7, Trigger{OutW: 1536, OutH: 864}); err != nil {
		t.Fatal(err)
	}
	reg.mu.Lock()
	defer reg.mu.Unlock()
	h := reg.panelHosts[PanelOnboarding]
	h.onbPage = onbPlugins
	reg.rebuildPanel(h)
	if onboardingNodeByKey(h.root, "onb-consent") != nil {
		t.Fatal("consent shown with nothing to install")
	}
	if !onbTreeHasText(h.root, "The plugin catalog is unavailable. You can install plugins later from the plugin store.") {
		t.Fatal("no store: the step does not say the catalog is unavailable")
	}

	reg.pluginStoreSnapshot = store.State{Listings: []store.Listing{onbTestListing("org.sysc.calendar")}}
	reg.rebuildPanel(h)
	if onboardingNodeByKey(h.root, "onb-consent") == nil {
		t.Fatal("a checked catalog plugin shows no consent")
	}
	if byAction(h.root, "onb-next") == nil || !onbTreeHasText(h.root, "Install and continue") {
		t.Fatal("the primary action does not name the install")
	}
	if err := reg.queueOnboardingInstallsLocked(h); err == nil {
		t.Fatal("installing without a plugin store reported success")
	}

	calendar := byAction(h.root, "onb-rec:org.sysc.calendar")
	h.activateOnboarding(reg, calendar)
	if onboardingNodeByKey(h.root, "onb-consent") != nil || onbTreeHasText(h.root, "Install and continue") {
		t.Fatal("consent stayed after the only plugin was unticked")
	}
}

func onbTreeHasText(root *ui.Node, text string) bool {
	for _, n := range walk(root) {
		if n.Text == text {
			return true
		}
	}
	return false
}

// A fresh install has no catalog until something refreshes the store, so the
// wizard asks for it once and says it is loading meanwhile.
func TestOnboardingRequestsTheCatalogOnce(t *testing.T) {
	reg := newPanelRegistry(t)
	reg.pluginStore = store.New(store.Options{})
	if err := reg.OpenPanel(PanelOnboarding, 7, Trigger{OutW: 1536, OutH: 864}); err != nil {
		t.Fatal(err)
	}
	reg.mu.Lock()
	defer reg.mu.Unlock()
	if !reg.onboardingCatalogRequested || reg.onboardingCatalogErr != nil {
		t.Fatalf("catalog requested = %t, err = %v; want one queued refresh", reg.onboardingCatalogRequested, reg.onboardingCatalogErr)
	}
	if got := onbCatalogStatus(reg); got != onbCatalogLoading {
		t.Fatalf("catalog status = %d, want loading", got)
	}
	reg.onboardingCatalogRequested = false
	reg.pluginStoreSnapshot = store.State{Listings: []store.Listing{onbTestListing("org.sysc.calendar")}}
	reg.requestOnboardingCatalogLocked()
	if reg.onboardingCatalogRequested {
		t.Fatal("refreshed a catalog the store already has")
	}
	if got := onbCatalogStatus(reg); got != onbCatalogReady {
		t.Fatalf("catalog status = %d, want ready", got)
	}
}
