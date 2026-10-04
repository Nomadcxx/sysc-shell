package displays

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const userRoot = `// my niri config
output "DP-1" {
    scale 1
    mode "1920x1080@60.000"
    layout {
        gaps 4
    }
    focus-at-startup
}
`

// setupDir lays out a config dir and returns the root path and the dir.
func setupDir(t *testing.T, body string) (string, string) {
	t.Helper()
	dir := writeTree(t, map[string]string{"config.kdl": body})
	return filepath.Join(dir, "config.kdl"), dir
}

func TestSetupPrependsOneIncludeLine(t *testing.T) {
	root, dir := setupDir(t, userRoot)
	own, err := Setup(root)
	if err != nil {
		t.Fatalf("Setup: %v", err)
	}

	if own.Root != root {
		t.Errorf("Owned.Root = %q, want %q", own.Root, root)
	}
	if own.Managed != filepath.Join(dir, ManagedFileName) {
		t.Errorf("Owned.Managed = %q, want it beside the real root", own.Managed)
	}
	got, err := os.ReadFile(root)
	if err != nil {
		t.Fatal(err)
	}
	// The recorded root hash describes what the shell last wrote, include
	// line included, so the next check compares like with like.
	if want := Hash(got); own.RootSHA256 != want {
		t.Errorf("Owned.RootSHA256 = %s, want the written bytes %s", own.RootSHA256, want)
	}
	want := string(got)
	line, err := IncludeLine(own.Managed)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(want, string(line)) {
		t.Fatalf("root does not start with the include line:\n%s", want)
	}
	// Every original byte must survive, unchanged, after the include line.
	if string(got[len(line):]) != userRoot {
		t.Fatalf("root bytes after the include line changed:\n%q", got[len(line):])
	}
	if strings.Count(want, "include ") != 1 {
		t.Fatalf("expected exactly one include line, got:\n%s", want)
	}
}

// The bootstrap must be safe to interrupt. The managed file starts as a
// comment-only file, so a shell that dies between the two writes leaves niri
// reading exactly what the user wrote.
func TestSetupBootstrapsWithACommentOnlyManagedFile(t *testing.T) {
	root, dir := setupDir(t, userRoot)
	own, err := Setup(root)
	if err != nil {
		t.Fatalf("Setup: %v", err)
	}
	managed, err := os.ReadFile(own.Managed)
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(string(managed), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if !strings.HasPrefix(trimmed, "//") {
			t.Fatalf("a fresh managed file must hold nothing but comments, found %q", trimmed)
		}
	}
	if len(own.Connectors) != 0 {
		t.Errorf("a fresh managed file owns no outputs, got %v", own.Connectors)
	}
	_ = dir
}

func TestSetupIsIdempotent(t *testing.T) {
	root, _ := setupDir(t, userRoot)
	first, err := Setup(root)
	if err != nil {
		t.Fatalf("Setup: %v", err)
	}
	before, err := os.ReadFile(root)
	if err != nil {
		t.Fatal(err)
	}

	second, err := Setup(root)
	if err != nil {
		t.Fatalf("second Setup: %v", err)
	}
	after, err := os.ReadFile(root)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatal("a second Setup rewrote the root")
	}
	if second.RootSHA256 != first.RootSHA256 || second.ManagedSHA256 != first.ManagedSHA256 {
		t.Error("a second Setup changed the recorded hashes")
	}
	if err := CheckOwnership(root, second); err != nil {
		t.Fatalf("a second Setup left the file unowned: %v", err)
	}
}

// The original root has to be recoverable, and re-running Setup must not
// clobber the first backup.
func TestSetupWritesANonOverwritingBackup(t *testing.T) {
	root, _ := setupDir(t, userRoot)
	first, err := Setup(root)
	if err != nil {
		t.Fatal(err)
	}
	if first.Backup == "" {
		t.Fatal("Setup recorded no backup path")
	}
	body, err := os.ReadFile(first.Backup)
	if err != nil {
		t.Fatalf("backup unreadable: %v", err)
	}
	if string(body) != userRoot {
		t.Fatalf("backup holds %q, want the original bytes", body)
	}

	if _, err := Setup(root); err != nil {
		t.Fatal(err)
	}
	body, err = os.ReadFile(first.Backup)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != userRoot {
		t.Fatal("a second Setup overwrote the first backup")
	}
}

// A file already sitting at the managed path belongs to somebody else. The
// shell must not take it over or destroy it.
func TestSetupRefusesAnUnrelatedManagedFile(t *testing.T) {
	root, dir := setupDir(t, userRoot)
	foreign := filepath.Join(dir, ManagedFileName)
	if err := os.WriteFile(foreign, []byte("output \"HDMI-A-1\" {\n    scale 2\n}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Setup(root); err == nil {
		t.Fatal("Setup took over a file it did not write")
	}
	if body, _ := os.ReadFile(foreign); !strings.Contains(string(body), "HDMI-A-1") {
		t.Fatal("Setup destroyed the existing file")
	}
}

// The shell's own record must be readable back, and its fields are what a
// later session trusts.
func TestOwnershipRoundTrip(t *testing.T) {
	root, dir := setupDir(t, userRoot)
	own, err := Setup(root)
	if err != nil {
		t.Fatal(err)
	}
	own.Connectors = []string{"DP-1"}
	own.ManagedSHA256 = Hash([]byte("candidate"))
	if err := own.Store(root); err != nil {
		t.Fatalf("Store: %v", err)
	}
	loaded, err := LoadOwnership(filepath.Join(dir, OwnedFileName))
	if err != nil {
		t.Fatalf("LoadOwnership: %v", err)
	}
	if len(loaded.Connectors) != 1 || loaded.Connectors[0] != "DP-1" {
		t.Errorf("Connectors = %v, want [DP-1]", loaded.Connectors)
	}
	if loaded.ManagedSHA256 != Hash([]byte("candidate")) {
		t.Error("ManagedSHA256 did not survive the round trip")
	}
	if loaded.Root != root || loaded.Managed != own.Managed {
		t.Error("paths did not survive the round trip")
	}
}

func TestLoadOwnershipRefusals(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, OwnedFileName)

	if _, err := LoadOwnership(path); !errors.Is(err, ErrNotSetUp) {
		t.Fatalf("LoadOwnership on a missing file = %v, want ErrNotSetUp", err)
	}
	if err := os.WriteFile(path, []byte(`{"version":99}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadOwnership(path); err == nil {
		t.Fatal("LoadOwnership accepted a future version")
	}
	if err := os.WriteFile(path, []byte(`{"version":1,"surprise":1}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadOwnership(path); err == nil {
		t.Fatal("LoadOwnership accepted an unknown field")
	}
}

func TestCheckOwnershipAndAdopt(t *testing.T) {
	root, _ := setupDir(t, userRoot)
	own, err := Setup(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := CheckOwnership(root, own); err != nil {
		t.Fatalf("a fresh setup is not owned: %v", err)
	}

	for _, tc := range []struct {
		name    string
		edit    func(root, managed string)
		wantHit string
	}{
		{
			"the managed file was edited",
			func(_, managed string) {
				os.WriteFile(managed, []byte("output \"DP-1\" {\n    scale 3\n}\n"), 0o644)
			},
			"managed",
		},
		{
			"the managed file was removed",
			func(_, managed string) { os.Remove(managed) },
			"managed",
		},
		{
			"the root was edited",
			func(root, _ string) { os.WriteFile(root, []byte(userRoot+"\n// a note\n"), 0o644) },
			"root",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// Rebuild ownership for each case so the cases stay independent.
			dir := t.TempDir()
			r := filepath.Join(dir, "config.kdl")
			if err := os.WriteFile(r, []byte(userRoot), 0o644); err != nil {
				t.Fatal(err)
			}
			own, err := Setup(r)
			if err != nil {
				t.Fatal(err)
			}
			tc.edit(r, own.Managed)
			err = CheckOwnership(r, own)
			if !errors.Is(err, ErrExternalEdit) {
				t.Fatalf("CheckOwnership = %v, want ErrExternalEdit", err)
			}
			if !strings.Contains(err.Error(), tc.wantHit) {
				t.Errorf("error %v does not name %q", err, tc.wantHit)
			}

			// Adoption is the only way past, and it must record the new bytes.
			adopted, err := Adopt(r, own)
			if err != nil {
				t.Fatalf("Adopt: %v", err)
			}
			if err := CheckOwnership(r, adopted); err != nil {
				t.Fatalf("Adopt left the file unowned: %v", err)
			}
		})
	}
}

// A file the user's config depends on is not owned by the shell, so editing
// it must not read as an edit of the managed file -- but it must still stop a
// publication, because the candidate was built against the old graph.
func TestCheckOwnershipSeesUserEdits(t *testing.T) {
	dir := writeTree(t, map[string]string{
		"config.kdl": "include \"./extra.kdl\"\n",
		"extra.kdl":  "output \"DP-1\" {\n    scale 1\n}\n",
	})
	root := filepath.Join(dir, "config.kdl")
	own, err := Setup(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := CheckOwnership(root, own); err != nil {
		t.Fatalf("fresh ownership: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "extra.kdl"), []byte("output \"DP-1\" {\n    scale 2\n}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := CheckOwnership(root, own); !errors.Is(err, ErrExternalEdit) {
		t.Fatalf("CheckOwnership = %v, want ErrExternalEdit after a user edit", err)
	}
}

func TestPublishManagedWritesAndRehashes(t *testing.T) {
	root, _ := setupDir(t, userRoot)
	own, err := Setup(root)
	if err != nil {
		t.Fatal(err)
	}
	g, err := LoadGraph(root)
	if err != nil {
		t.Fatal(err)
	}
	scale := 1.5
	candidate, err := BuildManaged(g, []string{"DP-1"}, map[string]Policy{"DP-1": {Scale: &scale}})
	if err != nil {
		t.Fatal(err)
	}
	if err := PublishManaged(&own, candidate); err != nil {
		t.Fatalf("PublishManaged: %v", err)
	}
	body, err := os.ReadFile(own.Managed)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != string(candidate) {
		t.Error("the published bytes differ from the candidate")
	}
	if own.ManagedSHA256 != Hash(candidate) {
		t.Error("ManagedSHA256 was not updated")
	}
	if len(own.Connectors) != 1 || own.Connectors[0] != "DP-1" {
		t.Errorf("Connectors = %v, want [DP-1]", own.Connectors)
	}
	if err := CheckOwnership(root, own); err != nil {
		t.Errorf("a published file is not owned: %v", err)
	}

	// A second publication of the same bytes must be accepted; publishing
	// something different on top of an unpublished change must not.
	if err := PublishManaged(&own, candidate); err != nil {
		t.Errorf("republishing identical bytes: %v", err)
	}
	if err := os.WriteFile(own.Managed, []byte("// hand edited\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := PublishManaged(&own, candidate); !errors.Is(err, ErrExternalEdit) {
		t.Errorf("PublishManaged = %v, want ErrExternalEdit over a hand-edited file", err)
	}
}

func TestRootTargetRefusals(t *testing.T) {
	t.Run("a directory", func(t *testing.T) {
		if _, err := rootTarget(t.TempDir()); err == nil {
			t.Fatal("rootTarget accepted a directory")
		}
	})

	t.Run("a hard link", func(t *testing.T) {
		dir := writeTree(t, map[string]string{"config.kdl": userRoot})
		other := filepath.Join(dir, "other.kdl")
		if err := os.Link(filepath.Join(dir, "config.kdl"), other); err != nil {
			t.Skipf("hard links unavailable: %v", err)
		}
		if _, err := rootTarget(filepath.Join(dir, "config.kdl")); err == nil {
			t.Fatal("rootTarget accepted a hard-linked file; renaming it would break the other name")
		}
	})

	t.Run("a symlink to a plain file is followed", func(t *testing.T) {
		dir := writeTree(t, map[string]string{"real/config.kdl": userRoot})
		link := filepath.Join(dir, "link.kdl")
		if err := os.Symlink(filepath.Join(dir, "real/config.kdl"), link); err != nil {
			t.Skip("symlinks unavailable")
		}
		got, err := rootTarget(link)
		if err != nil {
			t.Fatalf("rootTarget: %v", err)
		}
		if got != filepath.Join(dir, "real/config.kdl") {
			t.Fatalf("rootTarget = %q, want the target so a user's symlink is preserved", got)
		}
	})
}
