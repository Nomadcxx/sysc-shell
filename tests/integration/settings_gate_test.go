package integration

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Nomadcxx/sysc-shell/internal/config"
	"github.com/Nomadcxx/sysc-shell/internal/shell"
	"github.com/Nomadcxx/sysc-shell/internal/theme"
	"github.com/Nomadcxx/sysc-shell/internal/theming"
)

func TestAcceptSettingsPanelOpens(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.Accessibility.ReducedMotion = true
	reg := shell.NewRegistry(cfg)
	t.Cleanup(reg.Close)
	if err := reg.OpenPanel(shell.PanelSettings, 7, shell.Trigger{Align: "center"}); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		req := <-reg.AuxRequests()
		if req.Open != nil && req.Open.ExclusiveZone != -1 {
			t.Fatalf("%s exclusive zone = %d", req.Open.ID, req.Open.ExclusiveZone)
		}
	}
}

func TestAcceptStockThemesGenerate(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	script := "#!/bin/sh\ncat > colors.json <<'EOF'\n" + stubColors + "\nEOF\n"
	path := filepath.Join(dir, "matugen")
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	g := theme.Generator{CacheDir: dir, Matugen: path}
	for _, name := range theme.StockNames() {
		tok, err := g.Generate(theme.Source{Kind: "stock", Seed: name}, theme.Options{Mode: "dark"})
		if err != nil || tok.Primary == "" {
			t.Fatalf("stock %s: tok=%+v err=%v", name, tok, err)
		}
	}
}

func TestAcceptNiriTemplateLiveApply(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	cfg := filepath.Join(dir, "config.kdl")
	gen := filepath.Join(dir, "sysc-shell.kdl")
	if err := os.WriteFile(cfg, []byte("keybinds { }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	rendered := theming.Render(theming.Catalog().Template("niri"), theme.Fallback)
	if err := theming.ApplyNiri(cfg, gen, rendered); err != nil {
		t.Fatal(err)
	}
	if err := theming.ApplyNiri(cfg, gen, strings.ReplaceAll(rendered, theme.Fallback.Primary, "#abcdef")); err != nil {
		t.Fatal(err)
	}
	body, _ := os.ReadFile(cfg)
	if strings.Count(string(body), `include "sysc-shell.kdl"`) != 1 {
		t.Fatalf("include rewritten: %s", body)
	}
	out, _ := os.ReadFile(gen)
	if !strings.Contains(string(out), "#abcdef") {
		t.Fatal("palette change did not rewrite generated kdl")
	}
}

const stubColors = `{"dark":{"surface":"#1a1c1e","on_surface":"#e2e2e6","primary":"#a8c7fa","on_primary":"#062e6f","on_surface_variant":"#c3c6cf","error":"#ffb4ab","surface_container":"#1d1f21","outline":"#8d9199","primary_container":"#003a75","on_primary_container":"#d6e3ff","on_error":"#ffffff"},"light":{"surface":"#faf9fd","on_surface":"#1a1c1e","primary":"#3b5ba9","on_primary":"#ffffff","on_surface_variant":"#43474e","error":"#ba1a1a","surface_container":"#eeedf1","outline":"#73777f","primary_container":"#d6e3ff","on_primary_container":"#001b3d","on_error":"#ffffff"}}`
