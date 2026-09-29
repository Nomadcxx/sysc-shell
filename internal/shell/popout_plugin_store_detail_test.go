package shell

import (
	"errors"
	"strings"
	"testing"

	"github.com/Nomadcxx/sysc-shell/internal/platform/wayland"
	"github.com/Nomadcxx/sysc-shell/internal/plugin/store"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
	"github.com/Nomadcxx/sysc-shell/plugin/catalog"
)

func TestPluginStoreDetailHeadlessRendersREADMEAndFallback(t *testing.T) {
	listing := modelListing("org.sysc.timer", "Timer", "sysc", "productivity", "1.4.0")
	listing.Entry.Author = "Ada Lovelace"
	listing.Entry.License = "MIT"
	listing.Entry.Homepage = "https://example.com/timer"
	listing.Entry.ReleaseNotes = "https://example.com/releases/1.4.0"
	listing.Resolution.Release = &listing.Entry.Release
	listing.Entry.Readme = &catalog.Screenshot{URL: "https://example.com/README.md", SHA256: strings.Repeat("d", 64)}
	listing.Entry.LongDescription = ""
	state := store.State{Listings: []store.Listing{listing}, Media: map[string]store.MediaState{}}
	reg, host, panel := openPluginStoreTestPanel(t, state, ui.Rect{W: 1229, H: 691})
	reg.mu.Lock()
	host.pluginStoreDetail = pluginStoreKey(listing)
	reg.pluginStoreReadmes = map[string]string{listing.Entry.Readme.SHA256: "# Timer\n\nREADME body text."}
	reg.rebuildPanel(host)
	reg.mu.Unlock()
	renderPluginStorePanel(t, panel)
	for _, want := range []string{"Timer", "by Ada Lovelace · v1.4.0 · MIT", "Official", "README body text.", "Capabilities", "Requires", "Install"} {
		if !pluginStoreHasText(host.root, want) {
			t.Errorf("detail view lacks %q", want)
		}
	}
	if pluginStoreFindAction(host.root, "store-open-homepage") == nil || pluginStoreFindAction(host.root, "store-open-release-notes") == nil {
		t.Fatal("detail view is missing its HTTPS links")
	}

	listing.Entry.Readme = nil
	state = store.State{Listings: []store.Listing{listing}}
	reg2, host2, panel2 := openPluginStoreTestPanel(t, state, ui.Rect{W: 1229, H: 691})
	reg2.mu.Lock()
	host2.pluginStoreDetail = pluginStoreKey(listing)
	reg2.rebuildPanel(host2)
	reg2.mu.Unlock()
	renderPluginStorePanel(t, panel2)
	if !pluginStoreHasText(host2.root, listing.Entry.Description) {
		t.Fatal("detail view without a README did not fall back to its description")
	}
}

func TestPluginStoreDetailConsentPinsDisplayedReleaseAndShowsErrors(t *testing.T) {
	listing := modelListing("org.sysc.timer", "Timer", "sysc", "productivity", "1.4.0")
	sha := strings.Repeat("a", 64)
	listing.Entry.Release.Assets["linux-amd64"] = catalog.Asset{URL: "https://example.com/timer.tar.gz", SHA256: sha, Size: 1}
	listing.Resolution.Release = &listing.Entry.Release
	listing.Resolution.AssetKey = "linux-amd64"
	reg, host, panel := openPluginStoreTestPanel(t, store.State{Listings: []store.Listing{listing}}, ui.Rect{W: 1229, H: 691})
	reg.mu.Lock()
	reg.cfg.Plugins.Enabled = append(reg.cfg.Plugins.Enabled, listing.Entry.ID)
	host.pluginStoreDetail = pluginStoreKey(listing)
	reg.rebuildPanel(host)
	pluginStoreActivateAction(t, reg, host, "store-primary")
	var got store.ReleaseRef
	if host.pluginStoreConsent != nil {
		got = host.pluginStoreConsent.ref
	}
	if want := (store.ReleaseRef{Version: "1.4.0", SHA256: sha}); got != want {
		t.Fatalf("pinned consent release = %+v, want %+v", got, want)
	}
	reg.mu.Unlock()
	for _, want := range []string{"It runs as your user with full file and network access.", "It is still enabled, so it will start immediately.", "SHA-256: " + sha, "Confirm Install"} {
		if !pluginStoreHasText(host.root, want) {
			t.Errorf("consent view lacks %q", want)
		}
	}

	changed := modelListing("org.sysc.timer", "Timer", "sysc", "productivity", "1.5.0")
	changed.Entry.Release.Assets["linux-amd64"] = catalog.Asset{URL: "https://example.com/timer-v1.5.tar.gz", SHA256: strings.Repeat("b", 64), Size: 1}
	changed.Resolution.Release = &changed.Entry.Release
	reg.mu.Lock()
	reg.pluginStoreSnapshot.Listings = []store.Listing{changed}
	reg.rebuildPanel(host)
	reg.mu.Unlock()
	if !pluginStoreHasText(host.root, "Version: 1.4.0") || !pluginStoreHasText(host.root, "SHA-256: "+sha) {
		t.Fatal("refresh changed the release shown in the open consent block")
	}
	reg.mu.Lock()
	pluginStoreActivateAction(t, reg, host, "store-confirm")
	reg.mu.Unlock()
	if !pluginStoreHasText(host.root, "plugin store unavailable") {
		t.Fatal("confirmation error was not rendered on the detail view")
	}
	renderPluginStorePanel(t, panel)
}

func TestPluginStoreRemoveNeedsInlineConfirmation(t *testing.T) {
	listing := modelListing("org.sysc.timer", "Timer", "sysc", "productivity", "1.4.0")
	listing.Installed = &store.Record{Source: "sysc", Version: "1.3.0"}
	listing.Status = store.StatusInstalled
	reg, host, panel := openPluginStoreTestPanel(t, store.State{Listings: []store.Listing{listing}}, ui.Rect{W: 1229, H: 691})
	reg.mu.Lock()
	host.pluginStoreDetail = pluginStoreKey(listing)
	reg.rebuildPanel(host)
	pluginStoreActivateAction(t, reg, host, "store-primary")
	reg.mu.Unlock()
	if !pluginStoreHasText(host.root, "Remove Timer?") || pluginStoreFindAction(host.root, "store-confirm") == nil {
		t.Fatal("Remove did not expand an inline confirmation")
	}
	renderPluginStorePanel(t, panel)
}

func TestOpenURLRejectsHTTP(t *testing.T) {
	if err := openURL("http://example.com"); err == nil {
		t.Fatalf("openURL(http) error = %v, want insecure URL error", err)
	}
}

func renderPluginStorePanel(t *testing.T, panel *wayland.AuxSpec) {
	t.Helper()
	width, height := int(panel.Width), int(panel.Height)
	pixels := make([]byte, width*height*4)
	if err := panel.Callbacks.Render(pixels, width, height, width*4); err != nil {
		t.Fatalf("render plugin store panel: %v", err)
	}
}

// The line under a queued install or removal follows the worker: in
// progress until the listing shows the outcome, silent once a new error
// arrives (the detail shows that), never claiming success at enqueue.
func TestPluginStorePendingOpReadsTheOutcomeFromTheListing(t *testing.T) {
	old := errors.New("earlier failure")
	fresh := errors.New("download failed")
	install := &pluginStorePendingOp{name: "Timer", version: "1.4.0", enabled: true, prevErr: old}
	remove := &pluginStorePendingOp{name: "Timer", remove: true}
	for _, tc := range []struct {
		name string
		op   *pluginStorePendingOp
		l    store.Listing
		want string
	}{
		{"install queued", install, store.Listing{Err: old}, "Installing Timer 1.4.0…"},
		{"install done", install, store.Listing{Installed: &store.Record{Version: "1.4.0"}}, "Installed Timer 1.4.0. It is enabled and starts now."},
		{"install failed", install, store.Listing{Err: fresh}, ""},
		{"remove queued", remove, store.Listing{Installed: &store.Record{Version: "1.4.0"}}, "Removing Timer…"},
		{"remove done", remove, store.Listing{}, "Removed Timer. Its settings are kept."},
	} {
		if got := tc.op.status(tc.l); got != tc.want {
			t.Errorf("%s: status = %q, want %q", tc.name, got, tc.want)
		}
	}
}
