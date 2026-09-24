package store

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func newInstaller(t *testing.T) (*Installer, *assetServer) {
	t.Helper()
	return &Installer{Root: filepath.Join(t.TempDir(), "plugins"), Client: NewHTTPClient(), Arch: runtime.GOARCH}, newAssetServer(t)
}

func installedVersion(t *testing.T, root string) string {
	t.Helper()
	in, err := LoadInstalled(root)
	if err != nil {
		t.Fatal(err)
	}
	return in[fixtureID].Version
}

func plan(r Release) Plan {
	return Plan{Source: "test", CatalogCommit: "c0ffee", ID: fixtureID, Release: r}
}

func TestInstallPlacesThePluginAndRecordsIt(t *testing.T) {
	t.Parallel()
	in, srv := newInstaller(t)
	r := release(t, srv, runtime.GOARCH, "1.4.0", defaultCaps, nil)
	if err := in.Install(context.Background(), plan(r)); err != nil {
		t.Fatalf("Install: %v", err)
	}
	recs, _ := LoadInstalled(in.Root)
	rec := recs[fixtureID]
	if rec.Source != "test" || rec.Version != "1.4.0" || rec.CatalogCommit != "c0ffee" || rec.SHA256 == "" || !sameSet(rec.Capabilities, defaultCaps) {
		t.Errorf("record = %+v", rec)
	}
	if entries, _ := os.ReadDir(filepath.Join(in.Root, ".staging")); len(entries) != 0 {
		t.Errorf("staging left %d entries behind", len(entries))
	}
}

func TestInstallRejectsAManifestThatDisagreesWithTheCatalog(t *testing.T) {
	t.Parallel()
	cases := map[string]func(*Release){
		"version":      func(r *Release) { r.Version = "9.9.9" },
		"capabilities": func(r *Release) { r.Capabilities = []string{"panels"} },
		"requires":     func(r *Release) { r.Requires.Commands = []string{"gh"} },
		"protocol":     func(r *Release) { r.Protocol = v1Version(1, 2) },
	}
	for name, mutate := range cases {
		in, srv := newInstaller(t)
		r := release(t, srv, runtime.GOARCH, "1.4.0", defaultCaps, nil)
		mutate(&r)
		err := in.Install(context.Background(), plan(r))
		if KindOf(err) != KindManifest {
			t.Errorf("%s: err = %v, want %s", name, err, KindManifest)
		}
		if _, err := os.Stat(filepath.Join(in.Root, fixtureID)); err == nil {
			t.Errorf("%s: a mismatched plugin was installed", name)
		}
	}
}

func TestInstallRejectsAChecksumMismatch(t *testing.T) {
	t.Parallel()
	in, srv := newInstaller(t)
	r := release(t, srv, runtime.GOARCH, "1.4.0", defaultCaps, nil)
	a := r.Assets["linux-"+runtime.GOARCH]
	a.SHA256 = sum([]byte("tampered"))
	r.Assets["linux-"+runtime.GOARCH] = a
	if err := in.Install(context.Background(), plan(r)); KindOf(err) != KindChecksum {
		t.Fatalf("err = %v", err)
	}
}

func TestInstallWithoutAnAssetForThisMachine(t *testing.T) {
	t.Parallel()
	in, srv := newInstaller(t)
	r := release(t, srv, "sysc-other-arch", "1.4.0", defaultCaps, nil)
	if err := in.Install(context.Background(), plan(r)); KindOf(err) != KindNoAsset {
		t.Fatalf("err = %v", err)
	}
}

func TestUpdateKeepsThePreviousVersionAndRollbackRestoresIt(t *testing.T) {
	t.Parallel()
	in, srv := newInstaller(t)
	ctx := context.Background()
	if err := in.Install(ctx, plan(release(t, srv, runtime.GOARCH, "1.4.0", defaultCaps, nil))); err != nil {
		t.Fatal(err)
	}
	if err := in.Install(ctx, plan(release(t, srv, runtime.GOARCH, "1.5.0", defaultCaps, nil))); err != nil {
		t.Fatal(err)
	}
	if got := installedVersion(t, in.Root); got != "1.5.0" {
		t.Fatalf("after update: %s", got)
	}
	if err := in.Rollback(fixtureID); err != nil {
		t.Fatalf("Rollback: %v", err)
	}
	if got := installedVersion(t, in.Root); got != "1.4.0" {
		t.Fatalf("after rollback: %s", got)
	}
	if err := in.Rollback(fixtureID); err != nil {
		t.Fatalf("second Rollback: %v", err)
	}
	if got := installedVersion(t, in.Root); got != "1.5.0" {
		t.Fatalf("rolling back twice should return to 1.5.0, got %s", got)
	}
}

func TestAFailedSwapKeepsThePreviousVersion(t *testing.T) {
	t.Parallel()
	for _, failAt := range []int{1, 2} {
		in, srv := newInstaller(t)
		ctx := context.Background()
		if err := in.Install(ctx, plan(release(t, srv, runtime.GOARCH, "1.4.0", defaultCaps, nil))); err != nil {
			t.Fatal(err)
		}
		calls := 0
		in.rename = func(a, b string) error {
			if calls++; calls == failAt {
				return errors.New("injected")
			}
			return os.Rename(a, b)
		}
		err := in.Install(ctx, plan(release(t, srv, runtime.GOARCH, "1.5.0", defaultCaps, nil)))
		if KindOf(err) != KindDisk {
			t.Errorf("rename %d failing: err = %v", failAt, err)
		}
		in.rename = nil
		if got := installedVersion(t, in.Root); got != "1.4.0" {
			t.Errorf("rename %d failing: installed %s, want 1.4.0 kept", failAt, got)
		}
	}
}

func TestRemoveDeletesEveryVersion(t *testing.T) {
	t.Parallel()
	in, srv := newInstaller(t)
	ctx := context.Background()
	if err := in.Install(ctx, plan(release(t, srv, runtime.GOARCH, "1.4.0", defaultCaps, nil))); err != nil {
		t.Fatal(err)
	}
	if err := in.Install(ctx, plan(release(t, srv, runtime.GOARCH, "1.5.0", defaultCaps, nil))); err != nil {
		t.Fatal(err)
	}
	if err := in.Remove(fixtureID); err != nil {
		t.Fatal(err)
	}
	for _, dir := range []string{filepath.Join(in.Root, fixtureID), filepath.Join(in.Root, ".prev", fixtureID)} {
		if _, err := os.Stat(dir); err == nil {
			t.Errorf("%s survived Remove", dir)
		}
	}
	if err := in.Rollback(fixtureID); KindOf(err) != KindNoPrevious {
		t.Errorf("Rollback after Remove: %v", err)
	}
}

// TestRecoverInterruptedRestoresAStrandedPrev proves F5: a crash between the
// two renames in swapIn leaves only .prev/<id>, with the active directory
// gone. RecoverInterrupted must put it back so LoadInstalled sees it again.
func TestRecoverInterruptedRestoresAStrandedPrev(t *testing.T) {
	t.Parallel()
	in, srv := newInstaller(t)
	ctx := context.Background()
	if err := in.Install(ctx, plan(release(t, srv, runtime.GOARCH, "1.4.0", defaultCaps, nil))); err != nil {
		t.Fatal(err)
	}
	if err := in.Install(ctx, plan(release(t, srv, runtime.GOARCH, "1.5.0", defaultCaps, nil))); err != nil {
		t.Fatal(err)
	}
	active, _ := in.paths(fixtureID)
	if err := os.RemoveAll(active); err != nil {
		t.Fatal(err)
	}
	if err := in.RecoverInterrupted(); err != nil {
		t.Fatalf("RecoverInterrupted: %v", err)
	}
	if _, err := os.Stat(active); err != nil {
		t.Fatalf("active directory not restored: %v", err)
	}
	if got := installedVersion(t, in.Root); got != "1.4.0" {
		t.Fatalf("after recovery: %s, want 1.4.0", got)
	}
}

// TestRecoverInterruptedIsANoOpWhenNothingIsStranded covers the ordinary
// case: no .prev directory, or a .prev entry whose active counterpart is
// already there.
func TestRecoverInterruptedIsANoOpWhenNothingIsStranded(t *testing.T) {
	t.Parallel()
	in, _ := newInstaller(t)
	if err := in.RecoverInterrupted(); err != nil {
		t.Fatalf("no .prev at all: %v", err)
	}
	srv := newAssetServer(t)
	ctx := context.Background()
	if err := in.Install(ctx, plan(release(t, srv, runtime.GOARCH, "1.4.0", defaultCaps, nil))); err != nil {
		t.Fatal(err)
	}
	if err := in.Install(ctx, plan(release(t, srv, runtime.GOARCH, "1.5.0", defaultCaps, nil))); err != nil {
		t.Fatal(err)
	}
	if err := in.RecoverInterrupted(); err != nil {
		t.Fatalf("active present alongside .prev: %v", err)
	}
	if got := installedVersion(t, in.Root); got != "1.5.0" {
		t.Fatalf("RecoverInterrupted touched a healthy install: %s", got)
	}
}

func TestEveryTreeChangeGoesThroughReplace(t *testing.T) {
	t.Parallel()
	in, srv := newInstaller(t)
	var ids []string
	in.Replace = func(id string, swap func() error) error {
		ids = append(ids, id)
		return swap()
	}
	ctx := context.Background()
	if err := in.Install(ctx, plan(release(t, srv, runtime.GOARCH, "1.4.0", defaultCaps, nil))); err != nil {
		t.Fatal(err)
	}
	if err := in.Install(ctx, plan(release(t, srv, runtime.GOARCH, "1.5.0", defaultCaps, nil))); err != nil {
		t.Fatal(err)
	}
	if err := in.Rollback(fixtureID); err != nil {
		t.Fatal(err)
	}
	_ = in.Remove(fixtureID)
	if len(ids) != 4 {
		t.Fatalf("Replace saw %v, want four calls", ids)
	}
}

func TestInstallerRefusesAnInvalidID(t *testing.T) {
	t.Parallel()
	in, _ := newInstaller(t)
	for _, id := range []string{"../escape", "org/sysc", ""} {
		if err := in.Remove(id); KindOf(err) != KindNotListed {
			t.Errorf("Remove(%q) = %v", id, err)
		}
		if err := in.Rollback(id); KindOf(err) != KindNotListed {
			t.Errorf("Rollback(%q) = %v", id, err)
		}
	}
}
