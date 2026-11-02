package theming

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Nomadcxx/sysc-shell/internal/theme"
)

func TestNiriThemingTargetsSyscCompositorDir(t *testing.T) {
	testCompositorTheming(t, false, false)
}

func TestNiriUnthemingTargetsSyscCompositorDir(t *testing.T) {
	testCompositorTheming(t, true, false)
}

func TestNiriThemingTargetsXDGCompositorDir(t *testing.T) {
	testCompositorTheming(t, false, true)
}

func testCompositorTheming(t *testing.T, unapply, xdg bool) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("SYSC_SOCKET", "/run/control.sock")
	t.Setenv("NIRI_SOCKET", "/run/sysc-compositor.wayland-1.7.sock")
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	root := filepath.Join(home, ".config")
	if xdg {
		root = t.TempDir()
		t.Setenv("XDG_CONFIG_HOME", root)
	}
	dir := filepath.Join(root, "sysc-compositor")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	cfg, gen := filepath.Join(dir, "config.kdl"), filepath.Join(dir, "sysc-shell.kdl")
	const original = "input {}\n"
	if err := os.WriteFile(cfg, []byte(original), 0o600); err != nil {
		t.Fatal(err)
	}
	if unapply {
		if err := ApplyNiri(cfg, gen, Render(Catalog().Template("niri"), theme.Fallback)); err != nil {
			t.Fatal(err)
		}
	}
	enabled := func(name string) bool { return name == "niri" && !unapply }
	if _, _, err := ApplyEnabledAndWait(home, enabled, theme.Fallback, 100); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if unapply {
		if string(content) != original {
			t.Fatalf("unthemed config = %q", content)
		}
		if _, err := os.Stat(gen); !os.IsNotExist(err) {
			t.Fatalf("generated file remains: %v", err)
		}
	} else {
		if !strings.Contains(string(content), niriInclude) {
			t.Fatalf("config missing include: %s", content)
		}
		if _, err := os.Stat(gen); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := os.Stat(filepath.Join(root, "niri")); !os.IsNotExist(err) {
		t.Fatalf("Niri directory touched: %v", err)
	}
}

func TestNiriThemingKeepsInputIncludeLast(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	dir := t.TempDir()
	cfg, gen := filepath.Join(dir, "config.kdl"), filepath.Join(dir, "sysc-shell.kdl")
	const inputInclude = `include optional=true "sysc-shell-input.kdl"`
	if err := os.WriteFile(cfg, []byte("input {}\n"+inputInclude+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := ApplyNiri(cfg, gen, Render(Catalog().Template("niri"), theme.Fallback)); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(strings.TrimSpace(string(got)), inputInclude) {
		t.Fatalf("input include is not last:\n%s", got)
	}
	if strings.Count(string(got), niriInclude) != 1 {
		t.Fatalf("theme include missing or repeated:\n%s", got)
	}
}
