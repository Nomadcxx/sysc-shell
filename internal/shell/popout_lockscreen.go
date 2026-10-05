package shell

import (
	"log"
	"strings"

	"github.com/Nomadcxx/sysc-shell/internal/lockconfig"
	"github.com/Nomadcxx/sysc-shell/internal/theme"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
	"github.com/Nomadcxx/sysc-terminal/renderer"
)

// Registry.mu owns the draft and completion of file/preview work.
type lockScreenUI struct {
	loaded, saving, previewing bool
	config                     lockconfig.Config
	menu, message              string
	image                      *ui.Image
	previewSequence            uint64
}

func lockScreenSettingsTree(r *Registry, h *PanelHost) *ui.Node {
	s := &h.lockScreen
	if !s.loaded {
		s.config, s.loaded = lockconfig.Default(), true
		if r != nil {
			cfg, err := lockconfig.Load(lockconfig.Path())
			if err != nil {
				log.Printf("shell: read lock presentation: %v", err)
				s.message = "Cannot read lock screen settings."
			} else {
				s.config = cfg
			}
		}
	}
	saving := r != nil && r.lockSettingsSaving
	rows := []*ui.Node{
		{Kind: ui.KindText, Text: "Presentation changes apply to the next lock."},
		{Kind: ui.KindText, Text: "Idle and display power policy are in Session."},
	}
	for _, choice := range []struct {
		key, label, value string
		values            []string
	}{
		{"effect", "Effect", s.config.Effect, renderer.Effects()},
		{"palette", "Palette", s.config.Palette, renderer.Palettes()},
	} {
		combo := wallpaperCombo(h, "lockscreen-"+choice.key, choice.value, min(settingsDropdownFixedWidth, settingsBodyWidth(h)/2))
		combo.Action = "lockscreen-menu:" + choice.key
		if saving {
			combo.State |= ui.StateDisabled
			combo.AriaDisabled, combo.Focusable = true, false
		}
		if s.menu == choice.key {
			combo.State |= ui.StateSelected
		}
		rows = append(rows, &ui.Node{Kind: ui.KindRow, Gap: theme.MarginM, Children: []*ui.Node{{Kind: ui.KindText, Text: choice.label}, combo}})
		if s.menu == choice.key {
			opts := make([]wallpaperOption, 0, len(choice.values))
			for _, value := range choice.values {
				opts = append(opts, wallpaperOption{action: "lockscreen-" + choice.key + ":" + value, label: value, selected: value == choice.value})
			}
			rows = append(rows, wallpaperOptionList(h, opts))
		}
	}
	reduced := "Off"
	if s.config.ReducedMotion {
		reduced = "On"
	}
	motion := wallpaperButton(h, "lockscreen-reduced", "Reduced motion: "+reduced, s.config.ReducedMotion)
	if saving {
		motion.State |= ui.StateDisabled
		motion.AriaDisabled, motion.Focusable = true, false
	}
	rows = append(rows, motion)
	protected := "Unavailable"
	if r != nil && r.managedState.Known && r.managedState.SleepProtected {
		protected = "Protected"
	}
	rows = append(rows, &ui.Node{Kind: ui.KindText, Role: "status", Text: "Lock before sleep: " + protected})
	if r != nil && r.managedState.Known && r.managedState.SleepError != "" {
		rows = append(rows, &ui.Node{Kind: ui.KindText, Tone: ui.ToneError, Text: "The session owner could not protect sleep."})
	}
	apply := wallpaperButton(h, "lockscreen-apply", "Apply", false)
	preview := wallpaperButton(h, "lockscreen-preview", "Preview", false)
	if saving {
		apply.State |= ui.StateDisabled
		apply.AriaDisabled, apply.Focusable = true, false
	}
	if s.previewing || saving {
		preview.State |= ui.StateDisabled
		preview.AriaDisabled, preview.Focusable = true, false
	}
	rows = append(rows, &ui.Node{Kind: ui.KindRow, Gap: theme.MarginM, Children: []*ui.Node{apply, preview}})
	if s.message != "" {
		rows = append(rows, &ui.Node{Kind: ui.KindText, Role: "status", Text: s.message})
	}
	if s.image != nil {
		w := min(s.image.Width, settingsBodyWidth(h))
		rows = append(rows, &ui.Node{Kind: ui.KindText, Text: "Preview"}, &ui.Node{Kind: ui.KindImage, Image: s.image, ImageW: w, ImageH: w * s.image.Height / s.image.Width})
	}
	return settingsBody(h, theme.MarginM, rows...)
}

// ponytail: one ordinary preview frame avoids another animation scheduler; a
// future animated preview can reuse the shell's frame callback scheduling.
func lockPreview(c lockconfig.Config) (*ui.Image, error) {
	const w, h = 480, 270
	r, err := renderer.New(renderer.Config{Effect: c.Effect, Palette: c.Palette, Width: w, Height: h})
	if err != nil {
		return nil, err
	}
	if err = r.Step(); err != nil {
		return nil, err
	}
	r.SetPaused(c.ReducedMotion)
	img := &ui.Image{Width: w, Height: h, Stride: w * 4, Pix: make([]byte, w*h*4)}
	_, err = r.Draw(img.Pix, img.Stride, nil)
	return img, err
}

func (h *PanelHost) lockScreenAction(r *Registry, n *ui.Node) bool {
	if n == nil || h.id != PanelSettings || !strings.HasPrefix(n.Action, "lockscreen-") {
		return false
	}
	s := &h.lockScreen
	if r.lockSettingsSaving {
		return true
	}
	switch {
	case strings.HasPrefix(n.Action, "lockscreen-menu:"):
		menu := strings.TrimPrefix(n.Action, "lockscreen-menu:")
		if menu != "effect" && menu != "palette" {
			return true
		}
		if s.menu == menu {
			s.menu = ""
		} else {
			s.menu = menu
		}
	case n.Action == "lockscreen-reduced":
		s.config.ReducedMotion = !s.config.ReducedMotion
		s.previewSequence++
		s.image = nil
		s.message = ""
	case n.Action == "lockscreen-preview":
		if s.previewing {
			return true
		}
		s.previewing = true
		s.previewSequence++
		sequence, cfg := s.previewSequence, s.config
		go func() {
			img, err := lockPreview(cfg)
			r.mu.Lock()
			defer r.mu.Unlock()
			select {
			case <-r.closed:
				return
			default:
			}
			if r.panelHosts[PanelSettings] != h {
				return
			}
			s.previewing = false
			if sequence == s.previewSequence {
				if err != nil {
					log.Printf("shell: lock preview: %v", err)
					s.message = "Preview unavailable."
				} else {
					s.image, s.message = img, ""
				}
			}
			r.rebuildPanel(h)
			r.publishSurface(h.output, panelSurfaceID(h.id))
		}()
	case n.Action == "lockscreen-apply":
		cfg, path := s.config, lockconfig.Path()
		s.saving, r.lockSettingsSaving = true, true
		s.menu = ""
		s.message = "Saving…"
		go func() {
			err := lockconfig.Save(path, cfg)
			r.mu.Lock()
			defer r.mu.Unlock()
			s.saving, r.lockSettingsSaving = false, false
			select {
			case <-r.closed:
				return
			default:
			}
			if current := r.panelHosts[PanelSettings]; current != h {
				if current != nil {
					current.lockScreen.loaded = false
					r.rebuildPanel(current)
					r.publishSurface(current.output, panelSurfaceID(current.id))
				}
				return
			}
			if err != nil {
				log.Printf("shell: write lock presentation: %v", err)
				s.message = "Could not save lock screen settings."
			} else {
				s.message = "Saved for the next lock."
			}
			r.rebuildPanel(h)
			r.publishSurface(h.output, panelSurfaceID(h.id))
		}()
	default:
		cfg := s.config
		if value, ok := strings.CutPrefix(n.Action, "lockscreen-effect:"); ok {
			cfg.Effect = value
		} else if value, ok := strings.CutPrefix(n.Action, "lockscreen-palette:"); ok {
			cfg.Palette = value
		} else {
			return false
		}
		if err := renderer.Validate(cfg.Effect, cfg.Palette); err != nil {
			s.message = "That presentation choice is unavailable."
		} else {
			s.config = cfg
			s.menu, s.message = "", ""
			s.image = nil
			s.previewSequence++
		}
	}
	r.rebuildPanel(h)
	return true
}
