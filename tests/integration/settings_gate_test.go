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

// stubColors is complete matugen output for one seed. The generator
// rejects a partial palette, so a fixture naming a handful of roles
// would only ever exercise the rejection path.
const stubColors = `{"dark":{"primary":"#b3c5ff","on_primary":"#192e60","primary_container":"#324578","on_primary_container":"#dbe1ff","secondary":"#c1c6dd","on_secondary":"#2a3042","secondary_container":"#414659","on_secondary_container":"#dde1f9","tertiary":"#e1bbdc","on_tertiary":"#422741","tertiary_container":"#5a3d58","on_tertiary_container":"#ffd6f8","error":"#ffb4ab","on_error":"#690005","error_container":"#93000a","on_error_container":"#ffdad6","surface":"#121318","on_surface":"#e3e2e9","surface_variant":"#45464f","on_surface_variant":"#c5c6d0","surface_dim":"#121318","surface_bright":"#38393f","surface_container_lowest":"#0d0e13","surface_container_low":"#1a1b21","surface_container":"#1e1f25","surface_container_high":"#292a2f","surface_container_highest":"#33343a","background":"#121318","on_background":"#e3e2e9","outline":"#8f909a","outline_variant":"#45464f","inverse_surface":"#e3e2e9","inverse_on_surface":"#2f3036","inverse_primary":"#4a5c92","shadow":"#000000","scrim":"#000000","surface_tint":"#b3c5ff","primary_fixed":"#dbe1ff","primary_fixed_dim":"#b3c5ff","on_primary_fixed":"#00184a","on_primary_fixed_variant":"#324578","secondary_fixed":"#dde1f9","secondary_fixed_dim":"#c1c6dd","on_secondary_fixed":"#151b2c","on_secondary_fixed_variant":"#414659","tertiary_fixed":"#ffd6f8","tertiary_fixed_dim":"#e1bbdc","on_tertiary_fixed":"#2b122b","on_tertiary_fixed_variant":"#5a3d58"},"light":{"primary":"#4a5c92","on_primary":"#ffffff","primary_container":"#dbe1ff","on_primary_container":"#00184a","secondary":"#585e72","on_secondary":"#ffffff","secondary_container":"#dde1f9","on_secondary_container":"#151b2c","tertiary":"#735471","on_tertiary":"#ffffff","tertiary_container":"#ffd6f8","on_tertiary_container":"#2b122b","error":"#ba1a1a","on_error":"#ffffff","error_container":"#ffdad6","on_error_container":"#410002","surface":"#faf8ff","on_surface":"#1a1b21","surface_variant":"#e2e2ec","on_surface_variant":"#45464f","surface_dim":"#dad9e0","surface_bright":"#faf8ff","surface_container_lowest":"#ffffff","surface_container_low":"#f4f3fa","surface_container":"#eeedf4","surface_container_high":"#e8e7ef","surface_container_highest":"#e3e2e9","background":"#faf8ff","on_background":"#1a1b21","outline":"#757680","outline_variant":"#c5c6d0","inverse_surface":"#2f3036","inverse_on_surface":"#f1f0f7","inverse_primary":"#b3c5ff","shadow":"#000000","scrim":"#000000","surface_tint":"#4a5c92","primary_fixed":"#dbe1ff","primary_fixed_dim":"#b3c5ff","on_primary_fixed":"#00184a","on_primary_fixed_variant":"#324578","secondary_fixed":"#dde1f9","secondary_fixed_dim":"#c1c6dd","on_secondary_fixed":"#151b2c","on_secondary_fixed_variant":"#414659","tertiary_fixed":"#ffd6f8","tertiary_fixed_dim":"#e1bbdc","on_tertiary_fixed":"#2b122b","on_tertiary_fixed_variant":"#5a3d58"}}`
