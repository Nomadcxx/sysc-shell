package store

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/Nomadcxx/sysc-shell/plugin/catalog"
)

func TestListingStatus(t *testing.T) {
	t.Parallel()
	asset := map[string]catalog.Asset{"linux-amd64": {URL: "https://e.com/a", SHA256: sum(nil), Size: 1}}
	tip := entryFor(catalog.Release{Version: "1.5.0", Protocol: v1Version(1, 0), Assets: asset})
	tooNew := entryFor(catalog.Release{Version: "2.0.0", Protocol: v1Version(2, 0), Assets: asset})
	cases := []struct {
		name      string
		entry     catalog.Entry
		installed *Record
		local     string
		want      Status
	}{
		{"available", tip, nil, "", StatusAvailable},
		{"installed current", tip, &Record{Source: "s", Version: "1.5.0"}, "", StatusInstalled},
		{"installed older", tip, &Record{Source: "s", Version: "1.4.0"}, "", StatusUpdateAvailable},
		{"installed older from another source", tip, &Record{Source: "other", Version: "1.4.0"}, "", StatusInstalled},
		{"incompatible", tooNew, nil, "", StatusIncompatible},
		{"local only", tip, nil, "/home/u/p", StatusLocalOnly},
		{"shadowed", tip, &Record{Source: "s", Version: "1.4.0"}, "/home/u/p", StatusShadowed},
	}
	for _, c := range cases {
		installed := Installed{}
		if c.installed != nil {
			installed[fixtureID] = *c.installed
		}
		local := map[string]string{}
		if c.local != "" {
			local[fixtureID] = c.local
		}
		got := buildListings([]SourceState{{Name: "s"}}, map[string]catalog.Catalog{"s": {Entries: []catalog.Entry{c.entry}}}, installed, local, nil, "amd64")
		if len(got) != 1 || got[0].Status != c.want {
			t.Errorf("%s: %+v, want %s", c.name, got, c.want)
		}
	}
}

// TestBuildListingsAddsOrphanManagedInstalls proves F4: a managed install
// whose id no catalog row covers (a disabled/removed source, an unknown
// source, or a source that stopped publishing that id) still gets a row, so
// an error on it (a failed rollback or remove) has somewhere to show.
func TestBuildListingsAddsOrphanManagedInstalls(t *testing.T) {
	t.Parallel()
	installed := Installed{
		"org.sysc.orphan":          {Source: "gone", Version: "1.0.0"},
		"org.sysc.shadowed-orphan": {Source: SourceUnknown, Version: "2.0.0"},
	}
	local := map[string]string{"org.sysc.shadowed-orphan": "/home/u/p"}
	errs := map[string]error{"org.sysc.orphan": errors.New("boom")}

	got := buildListings(nil, nil, installed, local, errs, "amd64")
	if len(got) != 2 {
		t.Fatalf("listings = %+v, want 2", got)
	}
	byID := map[string]Listing{}
	for _, l := range got {
		byID[l.Entry.ID] = l
	}
	o, ok := byID["org.sysc.orphan"]
	if !ok || o.Status != StatusUnlisted || o.Source != "gone" || o.Err == nil ||
		o.Installed == nil || o.Installed.Version != "1.0.0" {
		t.Errorf("orphan = %+v, %v", o, ok)
	}
	s, ok := byID["org.sysc.shadowed-orphan"]
	if !ok || s.Status != StatusShadowed || s.LocalDir != "/home/u/p" {
		t.Errorf("shadowed orphan = %+v, %v", s, ok)
	}
}

// storeFixture is a source repo, an asset server and a store over them.
type storeFixture struct {
	t        *testing.T
	repo     *gitRepo
	srv      *assetServer
	st       *Store
	root     string
	mediaDir string
}

func newStoreFixture(t *testing.T, local map[string]string) *storeFixture {
	return newStoreFixtureWithInterval(t, local, 0)
}

func newStoreFixtureWithInterval(t *testing.T, local map[string]string, checkInterval time.Duration) *storeFixture {
	t.Helper()
	f := &storeFixture{t: t, repo: newGitRepo(t), srv: newAssetServer(t)}
	f.root = filepath.Join(t.TempDir(), "plugins")
	f.mediaDir = filepath.Join(t.TempDir(), "media")
	f.st = New(Options{
		CacheDir:      filepath.Join(t.TempDir(), "sources"),
		MediaDir:      f.mediaDir,
		CheckInterval: checkInterval,
		Installer:     &Installer{Root: f.root, Client: NewHTTPClient(), Arch: runtime.GOARCH},
		Sources:       func() []Source { return []Source{{Name: "test", URL: f.repo.url()}} },
		Local:         func() map[string]string { return local },
	})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		f.st.Run(ctx)
	}()
	// Cancel and wait for Run before the t.TempDir cleanups fire (they were
	// registered first, so LIFO runs this one first). Without the wait the
	// refresher can recreate cache files while TempDir removes them, failing
	// cleanup with "directory not empty".
	t.Cleanup(func() {
		cancel()
		<-done
	})
	return f
}

func TestStoreUpdatesPublishLatestSnapshot(t *testing.T) {
	t.Parallel()
	f := newStoreFixture(t, nil)
	f.st.setBusy("first")
	f.st.setBusy("latest")

	select {
	case st := <-f.st.Updates():
		if st.Busy != "latest" {
			t.Fatalf("update busy = %q, want latest", st.Busy)
		}
	default:
		t.Fatal("busy change did not publish a store update")
	}
	select {
	case st := <-f.st.Updates():
		t.Fatalf("stale update remained buffered: %+v", st)
	default:
	}
}

func TestStoreRefreshesAtCheckInterval(t *testing.T) {
	t.Parallel()
	const interval = 50 * time.Millisecond
	f := newStoreFixtureWithInterval(t, nil, interval)
	oldRelease := release(t, f.srv, runtime.GOARCH, "1.4.0", defaultCaps, nil)
	f.publish(oldRelease)

	newRelease := release(t, f.srv, runtime.GOARCH, "1.5.0", defaultCaps, nil)
	commit := f.repo.publish(catalogJSON(t, entryFor(newRelease, oldRelease)))
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		st := f.st.State()
		if len(st.Sources) == 1 && st.Sources[0].Commit == commit {
			if got := f.listing().Entry.Version; got != "1.5.0" {
				t.Fatalf("listing version after timed refresh = %s, want 1.5.0", got)
			}
			return
		}
		time.Sleep(interval / 5)
	}
	t.Fatal("the store did not refresh the source on its check interval")
}

func (f *storeFixture) publish(rels ...catalog.Release) {
	f.repo.publish(catalogJSON(f.t, entryFor(rels...)))
	f.await(f.st.Refresh())
}

func (f *storeFixture) await(done <-chan error, err error) {
	f.t.Helper()
	if err != nil {
		f.t.Fatal(err)
	}
	if err := <-done; err != nil {
		f.t.Fatal(err)
	}
}

func (f *storeFixture) listing() Listing {
	f.t.Helper()
	ls := f.st.State().Listings
	if len(ls) != 1 {
		f.t.Fatalf("listings = %+v", ls)
	}
	return ls[0]
}

func TestStoreInstallsUpdatesRollsBackAndRemoves(t *testing.T) {
	t.Parallel()
	f := newStoreFixture(t, nil)
	arch := runtime.GOARCH
	v1rel := release(t, f.srv, arch, "1.4.0", defaultCaps, nil)
	f.publish(v1rel)
	if got := f.listing().Status; got != StatusAvailable {
		t.Fatalf("before install: %s", got)
	}

	f.await(f.st.Install("test", fixtureID, ReleaseRef{}))
	if l := f.listing(); l.Status != StatusInstalled || l.Installed.Version != "1.4.0" {
		t.Fatalf("after install: %+v", l)
	}

	f.publish(release(t, f.srv, arch, "1.5.0", defaultCaps, nil), v1rel)
	if got := f.listing().Status; got != StatusUpdateAvailable {
		t.Fatalf("after publishing 1.5.0: %s", got)
	}
	f.await(f.st.Update(fixtureID, ReleaseRef{}, false))
	if l := f.listing(); l.Installed.Version != "1.5.0" {
		t.Fatalf("after update: %+v", l.Installed)
	}

	f.await(f.st.Rollback(fixtureID))
	if l := f.listing(); l.Installed.Version != "1.4.0" {
		t.Fatalf("after rollback: %+v", l.Installed)
	}

	f.await(f.st.Remove(fixtureID))
	if l := f.listing(); l.Status != StatusAvailable || l.Installed != nil {
		t.Fatalf("after remove: %+v", l)
	}
	if _, err := os.Stat(filepath.Join(f.root, fixtureID)); err == nil {
		t.Fatal("the plugin directory survived Remove")
	}
}

func TestUpdateAsksAgainWhenCapabilitiesChange(t *testing.T) {
	t.Parallel()
	f := newStoreFixture(t, nil)
	arch := runtime.GOARCH
	f.publish(release(t, f.srv, arch, "1.4.0", []string{"panels", "settings"}, nil))
	f.await(f.st.Install("test", fixtureID, ReleaseRef{}))
	f.publish(release(t, f.srv, arch, "1.5.0", defaultCaps, nil))

	if _, err := f.st.Update(fixtureID, ReleaseRef{}, false); KindOf(err) != KindConsent {
		t.Fatalf("unconfirmed update: %v, want %s", err, KindConsent)
	}
	f.await(f.st.Update(fixtureID, ReleaseRef{}, true))
	if l := f.listing(); l.Installed.Version != "1.5.0" {
		t.Fatalf("after confirmed update: %+v", l.Installed)
	}
}

func TestRefreshKeepsTheLastGoodCatalogWhenAFetchFails(t *testing.T) {
	t.Parallel()
	f := newStoreFixture(t, nil)
	f.publish(release(t, f.srv, runtime.GOARCH, "1.4.0", defaultCaps, nil))
	if err := os.RemoveAll(f.repo.dir); err != nil {
		t.Fatal(err)
	}
	done, err := f.st.Refresh()
	if err != nil {
		t.Fatal(err)
	}
	if err := <-done; err == nil {
		t.Fatal("refresh of a deleted source reported success")
	}
	st := f.st.State()
	if st.Sources[0].Err == nil || st.Sources[0].FetchedAt.IsZero() || len(st.Listings) != 1 {
		t.Fatalf("state = %+v; want the source stale and its listing kept", st)
	}
}

// TestRefreshSurfacesAnUnsupportedSchemaAsKindSchema proves the store's
// catalog.Decode error is mapped onto KindSchema, not the generic
// KindCatalog, so the manager can tell "this shell is too old" from an
// ordinary malformed catalog.
func TestRefreshSurfacesAnUnsupportedSchemaAsKindSchema(t *testing.T) {
	t.Parallel()
	f := newStoreFixture(t, nil)
	f.repo.publish([]byte(`{"schema": 2, "plugins": []}`))
	done, err := f.st.Refresh()
	if err != nil {
		t.Fatal(err)
	}
	if err := <-done; KindOf(err) != KindSchema {
		t.Fatalf("refresh err = %v, want %s", err, KindSchema)
	}
	st := f.st.State()
	if len(st.Sources) != 1 || KindOf(st.Sources[0].Err) != KindSchema {
		t.Fatalf("source state = %+v, want KindSchema", st.Sources)
	}
}

func TestALocalCopyShadowsTheManagedListing(t *testing.T) {
	t.Parallel()
	f := newStoreFixture(t, map[string]string{fixtureID: "/home/u/sysc-plugins/plugins/timer"})
	f.publish(release(t, f.srv, runtime.GOARCH, "1.4.0", defaultCaps, nil))
	if got := f.listing().Status; got != StatusLocalOnly {
		t.Fatalf("before install: %s", got)
	}
	f.await(f.st.Install("test", fixtureID, ReleaseRef{}))
	if got := f.listing().Status; got != StatusShadowed {
		t.Fatalf("after install: %s", got)
	}
}

func TestShadowedListingKeepsItsUpdateAvailableFlag(t *testing.T) {
	t.Parallel()
	release := entryFor(
		catalog.Release{Version: "1.5.0", Protocol: v1Version(1, 0), Assets: map[string]catalog.Asset{"linux-amd64": {URL: "https://e.com/new", SHA256: sum([]byte("new")), Size: 3}}},
		catalog.Release{Version: "1.4.0", Protocol: v1Version(1, 0), Assets: map[string]catalog.Asset{"linux-amd64": {URL: "https://e.com/old", SHA256: sum([]byte("old")), Size: 3}}},
	)
	got := buildListings(
		[]SourceState{{Name: "s"}}, map[string]catalog.Catalog{"s": {Entries: []catalog.Entry{release}}},
		Installed{fixtureID: {Source: "s", Version: "1.4.0"}}, map[string]string{fixtureID: "/home/u/plugin"}, nil, "amd64",
	)
	if len(got) != 1 || got[0].Status != StatusShadowed || !got[0].UpdateAvailable {
		t.Fatalf("shadowed listing = %+v; want shadowed with update available", got)
	}
}

func TestInstallRejectsAReleaseDifferentFromDisplayedConsent(t *testing.T) {
	t.Parallel()
	f := newStoreFixture(t, nil)
	f.publish(release(t, f.srv, runtime.GOARCH, "1.4.0", defaultCaps, nil))
	_, err := f.st.Install("test", fixtureID, ReleaseRef{Version: "1.3.0", SHA256: sum([]byte("stale"))})
	if KindOf(err) != KindConsent {
		t.Fatalf("Install with stale displayed release = %v, want %s", err, KindConsent)
	}
}

func TestUpdateRejectsAReleaseDifferentFromDisplayedConsent(t *testing.T) {
	t.Parallel()
	f := newStoreFixture(t, nil)
	arch := runtime.GOARCH
	oldRelease := release(t, f.srv, arch, "1.4.0", defaultCaps, nil)
	f.publish(oldRelease)
	f.await(f.st.Install("test", fixtureID, ReleaseRef{}))
	shown := release(t, f.srv, arch, "1.5.0", defaultCaps, nil)
	f.publish(shown, oldRelease)
	current := release(t, f.srv, arch, "1.6.0", defaultCaps, nil)
	f.repo.publish(catalogJSON(t, entryFor(current, shown, oldRelease)))
	f.await(f.st.Refresh())

	want := ReleaseRef{Version: shown.Version, SHA256: shown.Assets["linux-"+arch].SHA256}
	if _, err := f.st.Update(fixtureID, want, false); KindOf(err) != KindConsent {
		t.Fatalf("Update with stale displayed release = %v, want %s", err, KindConsent)
	}
}

// TestUpdateRefusesIfTheCatalogChangesBeforeItRuns proves F1: Update captures
// the release identity (version + asset sha256) the caller consented to when
// it enqueues, and refuses with KindConsent if a refresh lands first and
// resolves something else -- even when the enqueue-time caps/requires check
// passed, and even for a confirmed update.
func TestUpdateRefusesIfTheCatalogChangesBeforeItRuns(t *testing.T) {
	t.Parallel()
	f := newStoreFixture(t, nil)
	arch := runtime.GOARCH
	v1rel := release(t, f.srv, arch, "1.4.0", defaultCaps, nil)
	f.publish(v1rel)
	f.await(f.st.Install("test", fixtureID, ReleaseRef{}))

	// 1.5.0 has the same capabilities as installed, so a later Update(id,
	// false) would pass the enqueue-time caps/requires check on its own.
	v2rel := release(t, f.srv, arch, "1.5.0", defaultCaps, nil)
	f.publish(v2rel, v1rel)

	// Change the catalog again directly (bypassing Store.Refresh), so the
	// store has not seen it yet: 1.6.0 changes both the version and the
	// asset content, with a smaller capability set too.
	v3rel := release(t, f.srv, arch, "1.6.0", []string{"notifications", "panels"}, nil)
	f.repo.publish(catalogJSON(t, entryFor(v3rel, v2rel, v1rel)))

	// Queue a refresh, then an unconfirmed update, without awaiting the
	// refresh first: the single worker runs its queue in order, so the
	// refresh (which resolves 1.6.0) completes before the update's op runs,
	// whatever the update's own enqueue-time snapshot saw.
	refreshDone, err := f.st.Refresh()
	if err != nil {
		t.Fatal(err)
	}
	updateDone, err := f.st.Update(fixtureID, ReleaseRef{}, false)
	var uerr error
	if err != nil {
		uerr = err
	} else {
		uerr = <-updateDone
	}
	if KindOf(uerr) != KindConsent {
		t.Fatalf("update after catalog changed: %v, want %s", uerr, KindConsent)
	}
	if err := <-refreshDone; err != nil {
		t.Fatal(err)
	}
	if l := f.listing(); l.Installed == nil || l.Installed.Version != "1.4.0" {
		t.Fatalf("installed version changed: %+v", l.Installed)
	}
}

// TestConfirmedUpdateRefusesIfTheCatalogChangesBeforeItRuns is the confirmed
// variant: confirming an old identity's caps/requires question must not
// waive consent for a release that resolves differently by the time the
// update runs.
func TestConfirmedUpdateRefusesIfTheCatalogChangesBeforeItRuns(t *testing.T) {
	t.Parallel()
	f := newStoreFixture(t, nil)
	arch := runtime.GOARCH
	v1rel := release(t, f.srv, arch, "1.4.0", defaultCaps, nil)
	f.publish(v1rel)
	f.await(f.st.Install("test", fixtureID, ReleaseRef{}))

	v2rel := release(t, f.srv, arch, "1.5.0", defaultCaps, nil)
	f.publish(v2rel, v1rel)

	v3rel := release(t, f.srv, arch, "1.6.0", defaultCaps, nil)
	f.repo.publish(catalogJSON(t, entryFor(v3rel, v2rel, v1rel)))

	refreshDone, err := f.st.Refresh()
	if err != nil {
		t.Fatal(err)
	}
	updateDone, err := f.st.Update(fixtureID, ReleaseRef{}, true)
	var uerr error
	if err != nil {
		uerr = err
	} else {
		uerr = <-updateDone
	}
	if KindOf(uerr) != KindConsent {
		t.Fatalf("confirmed update after catalog changed: %v, want %s", uerr, KindConsent)
	}
	if err := <-refreshDone; err != nil {
		t.Fatal(err)
	}
	if l := f.listing(); l.Installed == nil || l.Installed.Version != "1.4.0" {
		t.Fatalf("installed version changed: %+v", l.Installed)
	}
}

// TestInstallRefusesIfTheCatalogChangesBeforeItRuns is F1's Install
// equivalent: nothing to install yet, so there is no caps fallback like
// Update has; the version+sha256 check alone must catch it.
func TestInstallRefusesIfTheCatalogChangesBeforeItRuns(t *testing.T) {
	t.Parallel()
	f := newStoreFixture(t, nil)
	arch := runtime.GOARCH
	v1rel := release(t, f.srv, arch, "1.4.0", defaultCaps, nil)
	f.publish(v1rel)
	if got := f.listing().Status; got != StatusAvailable {
		t.Fatalf("before install: %s", got)
	}

	// Change the catalog directly, without letting the store see it yet.
	v2rel := release(t, f.srv, arch, "1.5.0", defaultCaps, nil)
	f.repo.publish(catalogJSON(t, entryFor(v2rel, v1rel)))

	refreshDone, err := f.st.Refresh()
	if err != nil {
		t.Fatal(err)
	}
	installDone, err := f.st.Install("test", fixtureID, ReleaseRef{})
	var ierr error
	if err != nil {
		ierr = err
	} else {
		ierr = <-installDone
	}
	if KindOf(ierr) != KindConsent {
		t.Fatalf("install after catalog changed: %v, want %s", ierr, KindConsent)
	}
	if err := <-refreshDone; err != nil {
		t.Fatal(err)
	}
	if l := f.listing(); l.Installed != nil {
		t.Fatalf("install ran despite the catalog having changed: %+v", l.Installed)
	}
}

func TestInstallOfSomethingUnlisted(t *testing.T) {
	t.Parallel()
	f := newStoreFixture(t, nil)
	f.publish(release(t, f.srv, runtime.GOARCH, "1.4.0", defaultCaps, nil))
	if _, err := f.st.Install("test", "org.sysc.nope", ReleaseRef{}); KindOf(err) != KindNotListed {
		t.Fatalf("err = %v", err)
	}
	if _, err := f.st.Install("nosuch", fixtureID, ReleaseRef{}); KindOf(err) != KindNotListed {
		t.Fatalf("err = %v", err)
	}
}

// TestInstallOfAnOrphanRowRefusesWithoutMentioningProtocol proves sysc-514:
// find can still match an orphan row by source+id, since an orphan's Source
// field carries the source it was originally installed from. Install must
// refuse it as not listed rather than reach install() with a nil Release and
// report "needs protocol 0.0".
func TestInstallOfAnOrphanRowRefusesWithoutMentioningProtocol(t *testing.T) {
	t.Parallel()
	f := newStoreFixture(t, nil)
	arch := runtime.GOARCH
	f.publish(release(t, f.srv, arch, "1.4.0", defaultCaps, nil))
	f.await(f.st.Install("test", fixtureID, ReleaseRef{}))

	// The source stops publishing this id, orphaning the managed install.
	f.repo.publish(catalogJSON(t))
	f.await(f.st.Refresh())
	if l := f.listing(); l.Status != StatusUnlisted {
		t.Fatalf("status after the source dropped it: %s, want %s", l.Status, StatusUnlisted)
	}

	_, err := f.st.Install("test", fixtureID, ReleaseRef{})
	if KindOf(err) != KindNotListed {
		t.Fatalf("err = %v, want %s", err, KindNotListed)
	}
	if err == nil || strings.Contains(strings.ToLower(err.Error()), "protocol") {
		t.Fatalf("err = %v; must not mention protocol", err)
	}
}
