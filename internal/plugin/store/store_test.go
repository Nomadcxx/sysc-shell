package store

import (
	"context"
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
