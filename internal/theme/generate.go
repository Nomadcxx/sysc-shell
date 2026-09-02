package theme

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

type Generator struct {
	CacheDir string // defaults to $XDG_CACHE_HOME/sysc-shell
	Matugen  string // defaults to "matugen" (PATH lookup)
}

// Generate renders the palette for src. It is single-flight per process by
// construction: callers (Registry reload path) serialize. One queued rerun is
// the caller's concern, not the generator's.
func (g Generator) Generate(src Source, opts Options) (Tokens, error) {
	if g.Matugen == "" {
		g.Matugen = "matugen"
	}
	dir := g.CacheDir
	if dir == "" {
		base, err := os.UserCacheDir()
		if err != nil {
			return Fallback, nil
		}
		dir = filepath.Join(base, "sysc-shell")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return Fallback, nil // ponytail: cache dir failure degrades to fallback, never blocks startup
	}

	cfgPath := filepath.Join(dir, "matugen.toml")
	tplPath := filepath.Join(dir, "matugen-template.json")
	outPath := filepath.Join(dir, "colors.json")
	cfg := strings.ReplaceAll(matugenConfig, "@TPL@", tplPath)
	cfg = strings.ReplaceAll(cfg, "@OUT@", outPath)
	if err := os.WriteFile(cfgPath, []byte(cfg), 0o644); err != nil {
		return Fallback, nil
	}
	if err := os.WriteFile(tplPath, []byte(matugenTemplate), 0o644); err != nil {
		return Fallback, nil
	}

	args := make([]string, 0, 12)
	switch src.Kind {
	case "wallpaper":
		args = append(args, "image", src.Seed)
	case "hex":
		args = append(args, "color", "hex", src.Seed)
	case "stock":
		hex, ok := StockSeed(src.Seed)
		if !ok {
			return Fallback, nil
		}
		args = append(args, "color", "hex", hex)
	default:
		return Fallback, nil
	}
	args = append(args, "-c", cfgPath, "-t", scheme(opts), "--prefer", "saturation")
	if opts.HighContrast {
		args = append(args, "--contrast", "1")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, g.Matugen, args...)
	cmd.Dir = dir
	if err := cmd.Run(); err != nil {
		return Fallback, nil // ponytail: any matugen failure degrades to fallback
	}
	tok, err := parseColors(outPath, opts.Mode)
	if err != nil {
		return Fallback, nil
	}
	return tok, nil
}

func scheme(opts Options) string {
	if opts.Scheme != "" {
		return opts.Scheme
	}
	return "scheme-tonal-spot"
}

type paletteFile struct {
	Dark  map[string]string `json:"dark"`
	Light map[string]string `json:"light"`
}

func parseColors(path, mode string) (Tokens, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Tokens{}, err
	}
	var file paletteFile
	if err := json.Unmarshal(data, &file); err != nil {
		return Tokens{}, err
	}
	src := file.Dark
	if strings.EqualFold(mode, "light") {
		src = file.Light
	}
	tok := Fallback
	set := func(got string, dest *string) {
		if got != "" {
			*dest = got
		}
	}
	set(src["surface"], &tok.Surface)
	set(src["surface_container"], &tok.SurfaceContainer)
	set(src["surface_container_high"], &tok.SurfaceContainer)
	set(src["on_surface"], &tok.OnSurface)
	set(src["on_surface_variant"], &tok.OnSurfaceVariant)
	set(src["primary"], &tok.Primary)
	set(src["on_primary"], &tok.OnPrimary)
	set(src["primary_container"], &tok.PrimaryContainer)
	set(src["on_primary_container"], &tok.OnPrimaryContainer)
	set(src["outline"], &tok.Outline)
	set(src["error"], &tok.Error)
	set(src["on_error"], &tok.OnError)
	return tok, nil
}
