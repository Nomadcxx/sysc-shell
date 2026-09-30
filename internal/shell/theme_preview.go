package shell

import (
	"errors"

	"github.com/Nomadcxx/sysc-shell/internal/config"
	"github.com/Nomadcxx/sysc-shell/internal/platform/wayland"
	"github.com/Nomadcxx/sysc-shell/internal/theme"
)

type themePreviewState struct {
	cfg    config.Config
	tokens theme.Tokens
}

// themePreviewShow paints a candidate palette across every surface without
// writing the config file and without touching the application templates
// (sysc-780, D5). The candidate comes from isolated generation only; committed
// palette stays authoritative, so themePreviewShow -> themePreviewHide leaves
// the shell exactly where it was. A candidate that will not generate is
// refused -- the committed palette keeps painting, the error goes to the
// caller, and nothing half-generated reaches the screen.
func (r *Registry) themePreviewShow(cfg config.Config) (map[string]any, error) {
	r.mu.Lock()
	r.previewRequest++
	request := r.previewRequest
	if !r.previewing {
		r.previewing = true
		r.previewPrevErr = r.themeErr
	}
	r.mu.Unlock()

	tokens, err := r.generatePreviewOnly(cfg)
	if err != nil {
		r.mu.Lock()
		if request == r.previewRequest && r.previewTheme == nil {
			r.previewing = false
			r.previewPrevErr = ""
		}
		r.mu.Unlock()
		return nil, err
	}

	r.mu.Lock()
	if request != r.previewRequest || !r.previewing {
		r.mu.Unlock()
		return nil, errors.New("theme preview superseded")
	}
	outputs, surfacePubs := r.paintThemeLocked(cfg, tokens, "", false)
	r.mu.Unlock()
	r.publishTheme(outputs, surfacePubs)
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
	r.previewRequest++
	visible := r.previewTheme != nil
	r.previewing = false
	prevErr := r.previewPrevErr
	r.previewPrevErr = ""
	var outputs map[string]uint32
	var surfacePubs []wayland.Invalidation
	if visible {
		outputs, surfacePubs = r.paintThemeLocked(r.cfg, r.tokens, prevErr, true)
	} else {
		r.previewTheme = nil
	}
	r.mu.Unlock()

	r.publishTheme(outputs, surfacePubs)
	return map[string]any{"previewing": false}
}
