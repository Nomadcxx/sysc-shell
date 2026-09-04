package wallpaper

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPersistRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "wallpaper", "assignments.json")
	want := map[string]Assignment{
		"DP-1": {Kind: KindImage, Path: "/w/a.png", DesiredPlayback: StateStatic},
		"DP-3": {Kind: KindVideo, Path: "/w/b.mp4", PreviewPath: "/c/b.jpg", DesiredPlayback: StatePaused},
	}
	if err := SaveAssignments(path, want); err != nil {
		t.Fatalf("save: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("mode = %v, want 0600", perm)
	}

	got, err := LoadAssignments(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(got) != len(want) {
		t.Fatalf("loaded %d assignments, want %d", len(got), len(want))
	}
	for connector, a := range want {
		if got[connector] != a {
			t.Errorf("%s = %+v, want %+v", connector, got[connector], a)
		}
	}
}

func TestPersistMissingFileIsEmpty(t *testing.T) {
	got, err := LoadAssignments(filepath.Join(t.TempDir(), "absent.json"))
	if err != nil {
		t.Fatalf("a first run has no file and that is not an error: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("got %d assignments from a missing file", len(got))
	}
}

func TestPersistRejectsNewlinePath(t *testing.T) {
	path := filepath.Join(t.TempDir(), "assignments.json")
	bad := map[string]Assignment{
		"DP-1": {Kind: KindImage, Path: "/w/a.png\nquit", DesiredPlayback: StateStatic},
	}
	if err := SaveAssignments(path, bad); err == nil {
		t.Fatal("a path with a newline must not be written")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("a refused save must leave no file behind")
	}
}

func TestPersistRejectsNewlineOnLoad(t *testing.T) {
	// The file is on disk and can be edited, so the check runs on the way in
	// as well: a newline would smuggle a second command onto the engine socket.
	path := filepath.Join(t.TempDir(), "assignments.json")
	body := `{"DP-1":{"kind":"image","path":"/w/a.png\nquit","desired_playback":"static"}}`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if _, err := LoadAssignments(path); err == nil {
		t.Fatal("a newline in a stored path must fail the load")
	}
}

func TestPersistRejectsUnknownKind(t *testing.T) {
	path := filepath.Join(t.TempDir(), "assignments.json")
	body := `{"DP-1":{"kind":"hologram","path":"/w/a.png","desired_playback":"static"}}`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if _, err := LoadAssignments(path); err == nil {
		t.Fatal("an unknown kind must fail the load rather than default to image")
	}
}
