package polkit

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNameExistingMatchesExecutableName(t *testing.T) {
	root := t.TempDir()
	proc := filepath.Join(root, "123")
	if err := os.Mkdir(proc, 0o700); err != nil {
		t.Fatal(err)
	}
	cmdline := filepath.Join(proc, "cmdline")
	if err := os.WriteFile(cmdline, []byte("/usr/bin/polkit-gnome-authentication-agent-1\x00--replace\x00"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := nameExisting(root); got != "polkit-gnome-authentication-agent-1" {
		t.Fatalf("nameExisting = %q", got)
	}
	if got := agentName(cmdline); got != "polkit-gnome-authentication-agent-1" {
		t.Fatalf("agentName = %q", got)
	}
}

func TestNameExistingIgnoresPartialAndUnreadableEntries(t *testing.T) {
	root := t.TempDir()
	for pid, argv0 := range map[string]string{
		"1": "/usr/bin/not-polkit-gnome-authentication-agent-1",
		"2": "",
	} {
		proc := filepath.Join(root, pid)
		if err := os.Mkdir(proc, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(proc, "cmdline"), []byte(argv0), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if got := nameExisting(root); got != "" {
		t.Fatalf("nameExisting = %q, want empty", got)
	}
	if got := nameExisting(filepath.Join(root, "missing")); got != "" {
		t.Fatalf("missing proc root = %q, want empty", got)
	}
}
