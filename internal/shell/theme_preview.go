package shell

import (
	"github.com/Nomadcxx/sysc-shell/internal/config"
)

// themePreviewShow paints a candidate palette across every surface without
// writing the config file and without touching the application templates
// (sysc-780, D5). The candidate comes from generateOnly only; the committed
// palette stays authoritative, so themePreviewShow -> themePreviewHide leaves
// the shell exactly where it was. A candidate that will not generate is
// refused -- the committed palette keeps painting, the error goes to the
// caller, and nothing half-generated reaches the screen.
func (r *Registry) themePreviewShow(cfg config.Config) (map[string]any, error) {
	tokens, err := r.generateOnly(cfg)
	if err != nil {
		return nil, err
	}
	r.paintTheme(cfg, tokens, "", false)
	return map[string]any{
		"previewing": true,
		"mode":       cfg.ThemeGen.Mode,
		"source":     cfg.ThemeGen.Source,
		"seed":       cfg.ThemeGen.Seed,
	}, nil
}

// themePreviewHide repaints the palette of record and clears the preview
// flag. It is a no-op when nothing is being previewed, so a caller may hide
// defensively.
func (r *Registry) themePreviewHide() map[string]any {
	r.mu.Lock()
	if !r.previewing {
		r.mu.Unlock()
		return map[string]any{"previewing": false}
	}
	r.previewing = false
	prevErr := r.previewPrevErr
	r.previewPrevErr = ""
	cfg := r.cfg
	tokens := r.tokens
	r.mu.Unlock()

	r.paintTheme(cfg, tokens, prevErr, true)
	return map[string]any{"previewing": false}
}
