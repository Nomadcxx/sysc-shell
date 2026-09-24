package store

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestListingStatus(t *testing.T) {
	t.Parallel()
	asset := map[string]Asset{"linux-amd64": {URL: "https://e.com/a", SHA256: sum(nil), Size: 1}}
	tip := entryFor(Release{Version: "1.5.0", Protocol: v1Version(1, 0), Assets: asset})
	tooNew := entryFor(Release{Version: "2.0.0", Protocol: v1Version(2, 0), Assets: asset})
	cases := []struct {
		name      string
		entry     Entry
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
		got := buildListings([]SourceState{{Name: "s"}}, map[string]Catalog{"s": {Entries: []Entry{c.entry}}}, installed, local, nil, "amd64")
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
	t    *testing.T
	repo *gitRepo
	srv  *assetServer
	st   *Store
	root string
}

func newStoreFixture(t *testing.T, local map[string]string) *storeFixture {
	t.Helper()
	f := &storeFixture{t: t, repo: newGitRepo(t), srv: newAssetServer(t)}
	f.root = filepath.Join(t.TempDir(), "plugins")
	f.st = New(Options{
		CacheDir:  filepath.Join(t.TempDir(), "sources"),
		Installer: &Installer{Root: f.root, Client: NewHTTPClient(), Arch: runtime.GOARCH},
		Sources:   func() []Source { return []Source{{Name: "test", URL: f.repo.url()}} },
		Local:     func() map[string]string { return local },
	})
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go f.st.Run(ctx)
	return f
}

func (f *storeFixture) publish(rels ...Release) {
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

	f.await(f.st.Install("test", fixtureID))
	if l := f.listing(); l.Status != StatusInstalled || l.Installed.Version != "1.4.0" {
		t.Fatalf("after install: %+v", l)
	}

	f.publish(release(t, f.srv, arch, "1.5.0", defaultCaps, nil), v1rel)
	if got := f.listing().Status; got != StatusUpdateAvailable {
		t.Fatalf("after publishing 1.5.0: %s", got)
	}
	f.await(f.st.Update(fixtureID, false))
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
	f.await(f.st.Install("test", fixtureID))
	f.publish(release(t, f.srv, arch, "1.5.0", defaultCaps, nil))

	if _, err := f.st.Update(fixtureID, false); KindOf(err) != KindConsent {
		t.Fatalf("unconfirmed update: %v, want %s", err, KindConsent)
	}
	f.await(f.st.Update(fixtureID, true))
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

func TestALocalCopyShadowsTheManagedListing(t *testing.T) {
	t.Parallel()
	f := newStoreFixture(t, map[string]string{fixtureID: "/home/u/sysc-plugins/plugins/timer"})
	f.publish(release(t, f.srv, runtime.GOARCH, "1.4.0", defaultCaps, nil))
	if got := f.listing().Status; got != StatusLocalOnly {
		t.Fatalf("before install: %s", got)
	}
	f.await(f.st.Install("test", fixtureID))
	if got := f.listing().Status; got != StatusShadowed {
		t.Fatalf("after install: %s", got)
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
	f.await(f.st.Install("test", fixtureID))

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
	updateDone, err := f.st.Update(fixtureID, false)
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
	f.await(f.st.Install("test", fixtureID))

	v2rel := release(t, f.srv, arch, "1.5.0", defaultCaps, nil)
	f.publish(v2rel, v1rel)

	v3rel := release(t, f.srv, arch, "1.6.0", defaultCaps, nil)
	f.repo.publish(catalogJSON(t, entryFor(v3rel, v2rel, v1rel)))

	refreshDone, err := f.st.Refresh()
	if err != nil {
		t.Fatal(err)
	}
	updateDone, err := f.st.Update(fixtureID, true)
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
	installDone, err := f.st.Install("test", fixtureID)
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
	if _, err := f.st.Install("test", "org.sysc.nope"); KindOf(err) != KindNotListed {
		t.Fatalf("err = %v", err)
	}
	if _, err := f.st.Install("nosuch", fixtureID); KindOf(err) != KindNotListed {
		t.Fatalf("err = %v", err)
	}
}
