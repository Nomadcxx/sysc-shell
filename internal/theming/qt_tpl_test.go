package theming

import (
	"os"
	"strings"
	"testing"

	"github.com/Nomadcxx/sysc-shell/internal/theme"
)

func TestQtTemplateRendersCompleteOutput(t *testing.T) {
	t.Parallel()
	out := renderComplete(t, "qt")
	assertHexAssignments(t, out)
	for _, want := range []string{"active_colors=", "disabled_colors=", "inactive_colors="} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q", want)
		}
	}
	if n := strings.Count(strings.SplitN(out, "\n", 4)[2], ","); n != 21 {
		t.Errorf("colour list length: %d commas", n)
	}
}

func TestQtAppliesSidecarsAndSchemePaths(t *testing.T) {
	home := t.TempDir()
	markTemplatesComplete(t, "qt")
	for _, ct := range []string{"qt5ct", "qt6ct"} {
		dir := joined(home, ".config", ct)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		user := "[General]\nstyle=Fusion\n"
		if err := os.WriteFile(dir+"/ct.conf", []byte(user), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(dir+"/"+ct+".conf", []byte(user), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := ApplyEnabled(home, func(string) bool { return true }, theme.Fallback); err != nil {
		t.Fatal(err)
	}
	for _, p := range templateTargets["qt"].sidecar(home) {
		b, err := os.ReadFile(p)
		if err != nil || !strings.Contains(string(b), "[ColorScheme]") {
			t.Fatalf("sidecar %s: %v", p, err)
		}
	}
	cfg, err := os.ReadFile(joined(home, ".config", "qt5ct", "qt5ct.conf"))
	if err != nil {
		t.Fatal(err)
	}
	s := string(cfg)
	if !strings.Contains(s, "style=Fusion") || strings.Count(s, "color_scheme_path=") != 1 {
		t.Fatalf("directive not managed: %s", s)
	}
	if err := ApplyEnabled(home, func(string) bool { return false }, theme.Fallback); err != nil {
		t.Fatal(err)
	}
	for _, p := range templateTargets["qt"].sidecar(home) {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Fatalf("sidecar %s survived disable", p)
		}
	}
	cfg, _ = os.ReadFile(joined(home, ".config", "qt5ct", "qt5ct.conf"))
	if strings.TrimSpace(string(cfg)) != "[General]\nstyle=Fusion" {
		t.Fatalf("disable did not restore: %q", cfg)
	}
}
