package shell

import (
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os"

	"github.com/Nomadcxx/sysc-shell/internal/config"
	"github.com/Nomadcxx/sysc-shell/internal/settings"
	"github.com/Nomadcxx/sysc-shell/internal/theming"
)

// ThemeCall answers the theme.* IPC methods (sysc-715). Writes go through the
// same settings entries the pane uses, so the entry's own Setter is the only
// validation, and persist through writeConfig: the file write plus a reload
// poke is the sanctioned way to change the theme from outside the owner
// goroutine, the same way the settings pane commits a draft.
func (r *Registry) ThemeCall(method string, params json.RawMessage) (map[string]any, error) {
	var p struct {
		Mode   string `json:"mode"`
		Source string `json:"source"`
		Seed   string `json:"seed"`
		Name   string `json:"name"`
		On     *bool  `json:"on"`
	}
	if len(params) > 0 {
		if err := json.Unmarshal(params, &p); err != nil {
			return nil, errors.New("malformed params")
		}
	}
	switch method {
	case "theme.mode.get":
		return map[string]any{"mode": r.themeSnapshot().ThemeGen.Mode}, nil
	case "theme.palette.get":
		cfg := r.themeSnapshot()
		return map[string]any{
			"source": cfg.ThemeGen.Source, "seed": cfg.ThemeGen.Seed,
			"scheme": cfg.ThemeGen.Scheme, "mode": cfg.ThemeGen.Mode,
		}, nil
	case "theme.mode.set":
		return r.themeSet("appearance.mode", p.Mode)
	case "theme.mode.toggle":
		next := "light"
		if r.themeSnapshot().ThemeGen.Mode == "light" {
			next = "dark"
		}
		return r.themeSet("appearance.mode", next)
	case "theme.palette.set":
		return r.themePaletteSet(p.Source, p.Seed)
	case "theme.preview.show":
		cfg := r.themeSnapshot()
		if err := themeApplyOverrides(r.settingsFor, &cfg, p.Mode, p.Source, p.Seed); err != nil {
			return nil, err
		}
		return r.themePreviewShow(cfg)
	case "theme.preview.hide":
		return r.themePreviewHide(), nil
	case "theme.templates.apply":
		return r.themeTemplatesApply(p.Name, p.On)
	default:
		return nil, fmt.Errorf("unknown method %s", method)
	}
}

// themeSnapshot copies the config an out-of-band writer starts from. It reads
// the file the writer itself writes, because the live view lags the reload
// poke that writeConfig sends and a stale view routes a seed to the wrong
// setting and drops a persisted one. With no readable file (a registry that
// skips persisting) it falls back to the live config. The map is cloned
// because a template toggle writes through it; the owner goroutine may still
// be reading the original.
func (r *Registry) themeSnapshot() config.Config {
	r.mu.Lock()
	path, live := r.configPath, r.cfg
	r.mu.Unlock()
	cfg := live
	if path != "" {
		if loaded, err := config.Load(path); err == nil {
			cfg = loaded
		}
	}
	cfg.Templates = maps.Clone(cfg.Templates)
	return cfg
}

func (r *Registry) themeSet(path, value string) (map[string]any, error) {
	cfg := r.themeSnapshot()
	if err := themeEntrySet(r.settingsFor(cfg), &cfg, path, value); err != nil {
		return nil, err
	}
	if err := r.writeConfig(cfg); err != nil {
		return nil, err
	}
	return map[string]any{"path": path, "value": value}, nil
}

func (r *Registry) themePaletteSet(source, seed string) (map[string]any, error) {
	if source == "" && seed == "" {
		return nil, errors.New("theme.palette.set needs source or seed")
	}
	cfg := r.themeSnapshot()
	if err := themeApplyOverrides(r.settingsFor, &cfg, "", source, seed); err != nil {
		return nil, err
	}
	if err := r.writeConfig(cfg); err != nil {
		return nil, err
	}
	return map[string]any{"source": cfg.ThemeGen.Source, "seed": cfg.ThemeGen.Seed}, nil
}

// themeApplyOverrides sets the appearance fields a theme verb names through
// the same settings entries the pane uses, so an out-of-band caller is
// validated by the entry's own Setter. An empty value leaves its field alone.
// Order matters: what a seed means follows the source, so the seed entry is
// chosen from the registry rebuilt after the source is applied.
func themeApplyOverrides(reg func(config.Config) *settings.Registry, cfg *config.Config, mode, source, seed string) error {
	if mode != "" {
		if err := themeEntrySet(reg(*cfg), cfg, "appearance.mode", mode); err != nil {
			return err
		}
	}
	if source != "" {
		if err := themeEntrySet(reg(*cfg), cfg, "appearance.source", source); err != nil {
			return err
		}
	}
	if seed != "" {
		path := "appearance.seed"
		if cfg.ThemeGen.Source == "palette" {
			path = "appearance.palette"
		}
		if err := themeEntrySet(reg(*cfg), cfg, path, seed); err != nil {
			return err
		}
	}
	return nil
}

// themeEntrySet runs one settings entry's Setter, the same validation the
// pane applies.
func themeEntrySet(reg *settings.Registry, cfg *config.Config, path, value string) error {
	e, ok := reg.Lookup(path)
	if !ok {
		return fmt.Errorf("unknown setting %s", path)
	}
	return e.Set(cfg, value)
}

func (r *Registry) themeTemplatesApply(name string, on *bool) (map[string]any, error) {
	resultNames := theming.Catalog().Names()
	var cfg config.Config
	if name == "" {
		// No target: rewrite every template from the committed tokens and poke
		// the normal reload so the live shell state catches up too.
		cfg = r.themeSnapshot()
		if err := r.writeConfig(cfg); err != nil {
			return nil, err
		}
	} else {
		cfg = r.themeSnapshot()
		e, ok := r.settingsFor(cfg).Lookup("theme.templates." + name)
		if !ok {
			return nil, fmt.Errorf("unknown template %s", name)
		}
		value := "true"
		if on != nil && !*on {
			value = "false"
		}
		if err := e.Set(&cfg, value); err != nil {
			return nil, err
		}
		if err := r.writeConfig(cfg); err != nil {
			return nil, err
		}
	}

	var outcomes map[string]error
	var applyErr error
	home := os.Getenv("HOME")
	if home == "" {
		applyErr = errors.New("HOME is unset")
	} else {
		outcomes, applyErr = theming.ApplyEnabledAndWait(home, cfg.TemplateEnabled, r.Tokens(), nil)
	}
	if outcomes != nil {
		r.recordTemplateOutcomes(outcomes, false)
	}
	if outcomes == nil && applyErr == nil {
		applyErr = errors.New("template apply completed without per-template outcomes")
	}

	results := make(map[string]any, len(resultNames))
	for _, template := range resultNames {
		var templateErr error
		if outcomes == nil {
			templateErr = applyErr
		} else {
			var attempted bool
			templateErr, attempted = outcomes[template]
			if !attempted {
				continue
			}
		}
		result := map[string]any{"status": "applied"}
		if templateErr != nil {
			status := "error"
			if errors.Is(templateErr, theming.ErrUserModified) {
				status = "refused"
			}
			result = map[string]any{"status": status, "error": templateErr.Error()}
		}
		results[template] = result
	}
	body := map[string]any{"results": results}
	if name == "" {
		body["applied"] = "all"
		return body, nil
	}
	body["applied"] = name
	body["enabled"] = cfg.TemplateEnabled(name)
	return body, nil
}
