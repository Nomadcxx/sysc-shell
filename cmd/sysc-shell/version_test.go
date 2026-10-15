package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Nomadcxx/sysc-shell/internal/buildinfo"
)

func TestVersionFlagIsCheapAndOffline(t *testing.T) {
	dir := t.TempDir()
	runtime := filepath.Join(dir, "run")
	if err := os.Mkdir(runtime, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_RUNTIME_DIR", runtime)
	t.Setenv("NIRI_SOCKET", "")
	t.Setenv("WAYLAND_DISPLAY", "")

	var out, errOut bytes.Buffer
	if code := runVersion([]string{}, &out, &errOut); code != 0 {
		t.Fatalf("code = %d, stderr = %q", code, errOut.String())
	}
	if want := buildinfo.Line() + "\n"; out.String() != want {
		t.Fatalf("stdout = %q, want exactly %q", out.String(), want)
	}
	entries, err := os.ReadDir(runtime)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("runtime dir holds %v, want nothing", entries)
	}
}

func TestVersionFlagRefusesExtraArguments(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := runVersion([]string{"--json"}, &out, &errOut); code != 2 {
		t.Fatalf("code = %d, want 2", code)
	}
	if !strings.Contains(errOut.String(), "usage: sysc-shell") {
		t.Fatalf("stderr = %q", errOut.String())
	}
	if !strings.Contains(errOut.String(), "config check [path]") {
		t.Fatalf("stderr = %q, want the config verb in the usage line", errOut.String())
	}
	if out.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", out.String())
	}
}
