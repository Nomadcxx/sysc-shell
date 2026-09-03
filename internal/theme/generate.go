package theme

import (
	"context"
	"encoding/json"
	"fmt"
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
//
// The error is the caller's signal, not a suggestion: a nil error means the
// returned palette is complete and satisfies Valid for the requested mode, and
// a non-nil error means the compiled fallback for that mode came back instead.
// A cold start may paint the fallback and report the error; a reload must keep
// the palette it already has rather than swapping a generated theme for the
// compiled one.
func (g Generator) Generate(src Source, opts Options) (Tokens, error) {
	fallback := FallbackFor(opts.HighContrast)
	if g.Matugen == "" {
		g.Matugen = "matugen"
	}
	dir := g.CacheDir
	if dir == "" {
		base, err := os.UserCacheDir()
		if err != nil {
			return fallback, fmt.Errorf("theme: no cache directory: %w", err)
		}
		dir = filepath.Join(base, "sysc-shell")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fallback, fmt.Errorf("theme: cache directory %s: %w", dir, err)
	}

	cfgPath := filepath.Join(dir, "matugen.toml")
	tplPath := filepath.Join(dir, "matugen-template.json")
	outPath := filepath.Join(dir, "colors.json")
	cfg := strings.ReplaceAll(matugenConfig, "@TPL@", tplPath)
	cfg = strings.ReplaceAll(cfg, "@OUT@", outPath)
	if err := os.WriteFile(cfgPath, []byte(cfg), 0o644); err != nil {
		return fallback, fmt.Errorf("theme: write %s: %w", cfgPath, err)
	}
	if err := os.WriteFile(tplPath, []byte(matugenTemplate), 0o644); err != nil {
		return fallback, fmt.Errorf("theme: write %s: %w", tplPath, err)
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
			return fallback, fmt.Errorf("theme: %q is not a stock seed", src.Seed)
		}
		args = append(args, "color", "hex", hex)
	default:
		return fallback, fmt.Errorf("theme: unknown palette source %q", src.Kind)
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
		return fallback, fmt.Errorf("theme: matugen: %w", err)
	}
	tok, err := parseColors(outPath, opts.Mode)
	if err != nil {
		return fallback, err
	}
	// matugen's contrast level moves the accent backgrounds; the local repair
	// only moves foregrounds. Running it after generation closes the small
	// gaps a seed can still leave without rewriting the palette's hues.
	if err := tok.Valid(opts.HighContrast); err != nil {
		tok = tok.Repair(opts.HighContrast)
		if err := tok.Valid(opts.HighContrast); err != nil {
			return fallback, fmt.Errorf("theme: generated palette is unusable: %w", err)
		}
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

// parseColors reads the generated file for one mode. Every role must be
// present and parseable: a role quietly kept from the compiled fallback is a
// palette that is half generated and half not, which is the mixed state the
// whole-palette rejection exists to prevent.
func parseColors(path, mode string) (Tokens, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Tokens{}, fmt.Errorf("theme: read palette: %w", err)
	}
	var file paletteFile
	if err := json.Unmarshal(data, &file); err != nil {
		return Tokens{}, fmt.Errorf("theme: parse palette: %w", err)
	}
	src := file.Dark
	if strings.EqualFold(mode, "light") {
		src = file.Light
	}
	var tok Tokens
	for _, r := range roles {
		v, ok := src[r.name]
		if !ok {
			return Tokens{}, fmt.Errorf("theme: generated palette omits role %s", r.name)
		}
		if _, err := ParseColor(v); err != nil {
			return Tokens{}, fmt.Errorf("theme: role %s is %q: %w", r.name, v, err)
		}
		*r.get(&tok) = v
	}
	return tok, nil
}
