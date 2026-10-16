package store

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/Nomadcxx/sysc-shell/internal/plugin"
)

// approvedDigest reads the ledger the store keeps beside installed.json.
func approvedDigest(t *testing.T, root string, source plugin.Source, id string) (string, bool) {
	t.Helper()
	l, err := plugin.LoadConsent(plugin.ConsentPath(root))
	if err != nil {
		t.Fatal(err)
	}
	rec, ok := l.Lookup(source, id)
	return rec.ContentSHA256, ok
}

func currentDigest(t *testing.T, dir string) string {
	t.Helper()
	m, err := plugin.LoadManifest(dir)
	if err != nil {
		t.Fatal(err)
	}
	d, err := plugin.ContentDigest(m)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func TestEveryStoreChangeRecordsTheTreeItInstalled(t *testing.T) {
	t.Parallel()
	f := newStoreFixture(t, nil)
	arch := runtime.GOARCH
	v14 := release(t, f.srv, arch, "1.4.0", defaultCaps, nil)
	f.publish(v14)
	active := filepath.Join(f.root, fixtureID)

	f.await(f.st.Install("test", fixtureID, ReleaseRef{}))
	installed := currentDigest(t, active)
	if got, ok := approvedDigest(t, f.root, plugin.SourceManaged, fixtureID); !ok || got != installed {
		t.Fatalf("after install: approved %q (%v), installed tree is %q", got, ok, installed)
	}

	f.publish(release(t, f.srv, arch, "1.5.0", defaultCaps, nil), v14)
	f.await(f.st.Update(fixtureID, ReleaseRef{}, false))
	updated := currentDigest(t, active)
	if updated == installed {
		t.Fatal("fixture: the update did not change the tree")
	}
	if got, _ := approvedDigest(t, f.root, plugin.SourceManaged, fixtureID); got != updated {
		t.Fatalf("after update: approved %q, tree is %q", got, updated)
	}

	f.await(f.st.Rollback(fixtureID))
	if got, _ := approvedDigest(t, f.root, plugin.SourceManaged, fixtureID); got != installed {
		t.Fatalf("after rollback: approved %q, tree is %q", got, installed)
	}

	f.await(f.st.Remove(fixtureID))
	if _, ok := approvedDigest(t, f.root, plugin.SourceManaged, fixtureID); ok {
		t.Fatal("a removed plugin kept its approval")
	}
}

func TestApproveRecordsAUserCopyAsItIsNow(t *testing.T) {
	t.Parallel()
	local := writePluginDir(t, t.TempDir(), fixtureID, "2.0.0", defaultCaps, nil)
	f := newStoreFixture(t, map[string]string{fixtureID: local})

	f.await(f.st.Approve(plugin.SourceUser, fixtureID))
	want := currentDigest(t, local)
	if got, ok := approvedDigest(t, f.root, plugin.SourceUser, fixtureID); !ok || got != want {
		t.Fatalf("approved %q (%v), copy is %q", got, ok, want)
	}
	if _, ok := approvedDigest(t, f.root, plugin.SourceManaged, fixtureID); ok {
		t.Fatal("approving the user copy approved a managed one")
	}

	if _, err := f.st.Approve(plugin.SourceUser, "org.example.absent"); KindOf(err) != KindNotListed {
		t.Fatalf("approving an absent copy: %v, want %s", err, KindNotListed)
	}
}

func TestStoreAdoptsApprovalsItGaveBeforeTheyWereRecorded(t *testing.T) {
	t.Parallel()
	// A managed install from before approvals were recorded was consented to
	// at install time, so the store records its tree rather than asking again.
	// A directory the store cannot account for is not adopted.
	f := newStoreFixture(t, nil)
	f.publish(release(t, f.srv, runtime.GOARCH, "1.4.0", defaultCaps, nil))
	f.await(f.st.Install("test", fixtureID, ReleaseRef{}))
	if err := os.Remove(plugin.ConsentPath(f.root)); err != nil {
		t.Fatal(err)
	}
	stray := writePluginDir(t, f.root, "org.example.stray", "1.0.0", defaultCaps, nil)
	f.await(f.st.Refresh())

	if got, ok := approvedDigest(t, f.root, plugin.SourceManaged, fixtureID); !ok || got != currentDigest(t, filepath.Join(f.root, fixtureID)) {
		t.Fatalf("store-installed copy not adopted: %q (%v)", got, ok)
	}
	if _, ok := approvedDigest(t, f.root, plugin.SourceManaged, "org.example.stray"); ok {
		t.Fatalf("an unaccounted directory %s was adopted", stray)
	}
}
