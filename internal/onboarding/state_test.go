package onboarding

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadMissingIsUnseen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	s, err := Load(path)
	if err != nil {
		t.Fatalf("missing file: %v", err)
	}
	if !s.Unseen() {
		t.Fatalf("absent marker must read unseen, got %+v", s)
	}
}

func TestMarkLoadRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "state.json")
	if err := Mark(path, OutcomeCompleted); err != nil {
		t.Fatalf("mark: %v", err)
	}
	s, err := Load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if s.Outcome != OutcomeCompleted || s.Unseen() {
		t.Fatalf("round trip: %+v", s)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("state file mode %o, want 600", info.Mode().Perm())
	}
	if err := Mark(path, OutcomeDismissed); err != nil {
		t.Fatalf("remark: %v", err)
	}
	s, _ = Load(path)
	if s.Outcome != OutcomeDismissed {
		t.Fatalf("remark: %+v", s)
	}
}

func TestMalformedKeptAndUnseen(t *testing.T) {
	for _, body := range []string{"not json", `{"version":1,"outcome":"nonsense"}`} {
		path := filepath.Join(t.TempDir(), "state.json")
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		s, err := Load(path)
		if err == nil {
			t.Fatalf("malformed %q: want error", body)
		}
		if !s.Unseen() {
			t.Fatalf("malformed %q: must read unseen, got %+v", body, s)
		}
		got, err := os.ReadFile(path)
		if err != nil || string(got) != body {
			t.Fatalf("malformed file must be kept for recovery: %q %v", got, err)
		}
	}
}

func TestStateRootRejectsRelativeXDG(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_STATE_HOME", "relative/path")
	root := StateRoot()
	want := filepath.Join(home, ".local", "state", "sysc-shell", "onboarding")
	if root != want {
		t.Fatalf("StateRoot() = %q, want %q", root, want)
	}
}
