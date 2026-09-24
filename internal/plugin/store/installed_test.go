package store

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Nomadcxx/sysc-shell/plugin/catalog"
)

func TestLoadInstalledOfAnEmptyRoot(t *testing.T) {
	t.Parallel()
	in, err := LoadInstalled(filepath.Join(t.TempDir(), "absent"))
	if err != nil || len(in) != 0 {
		t.Fatalf("LoadInstalled = %v, %v", in, err)
	}
}

func TestInstalledRoundTrips(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writePluginDir(t, root, fixtureID, "1.4.0", defaultCaps, nil)
	want := Installed{fixtureID: {Source: "sysc", Version: "1.4.0", SHA256: "ab",
		Capabilities: defaultCaps, Previous: &Record{Source: "sysc", Version: "1.3.0"}}}
	if err := want.Save(root); err != nil {
		t.Fatal(err)
	}
	got, err := LoadInstalled(root)
	if err != nil || got[fixtureID].Source != "sysc" || got[fixtureID].Previous == nil {
		t.Fatalf("LoadInstalled = %+v, %v", got, err)
	}
}

func TestLoadInstalledReconcilesWithTheTree(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writePluginDir(t, root, fixtureID, "1.4.0", defaultCaps, []string{"gh"})
	writePluginDir(t, root, "org.sysc.misnamed", "1.0.0", defaultCaps, nil)
	if err := os.Rename(filepath.Join(root, "org.sysc.misnamed"), filepath.Join(root, "org.sysc.renamed")); err != nil {
		t.Fatal(err)
	}
	stale := Installed{"org.sysc.gone": {Source: "sysc", Version: "1.0.0"}}
	if err := stale.Save(root); err != nil {
		t.Fatal(err)
	}
	got, err := LoadInstalled(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := got["org.sysc.gone"]; ok {
		t.Error("a record whose directory is gone survived")
	}
	if _, ok := got["org.sysc.misnamed"]; ok {
		t.Error("a directory whose name is not its manifest id was adopted")
	}
	rec, ok := got[fixtureID]
	if !ok || rec.Source != SourceUnknown || rec.Version != "1.4.0" || !catalog.SameSet(rec.Requires, []string{"gh"}) {
		t.Errorf("unrecorded directory = %+v, %v; want an unknown-source record from its manifest", rec, ok)
	}
}

func TestLoadInstalledRebuildsACorruptFile(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writePluginDir(t, root, fixtureID, "1.4.0", defaultCaps, nil)
	if err := os.WriteFile(filepath.Join(root, InstalledName), []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := LoadInstalled(root)
	if err != nil || got[fixtureID].Source != SourceUnknown {
		t.Fatalf("LoadInstalled = %+v, %v", got, err)
	}
	if _, err := os.Stat(filepath.Join(root, fixtureID)); err != nil {
		t.Fatal("rebuilding deleted a plugin")
	}
}
