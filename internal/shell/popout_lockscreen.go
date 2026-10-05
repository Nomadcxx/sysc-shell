package shell

import (
	"log"
	"strings"
	"time"

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
	previewStop                chan struct{}
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
	motion := &ui.Node{
		Kind: ui.KindToggle, Action: "lockscreen-reduced",
		Focusable: true, Name: "Reduced motion", Role: "switch",
	}
	if s.config.ReducedMotion {
		motion.Value = 1
	}
	if saving {
		motion.State |= ui.StateDisabled
		motion.AriaDisabled, motion.Focusable = true, false
	}
	rows = append(rows, &ui.Node{Kind: ui.KindRow, Gap: theme.MarginM, Children: []*ui.Node{{Kind: ui.KindText, Text: "Reduced motion"}, motion}})
	protected := "Unavailable"
	if r != nil && r.managedState.Known && r.managedState.SleepProtected {
		protected = "Protected"
	}
	rows = append(rows, &ui.Node{Kind: ui.KindText, Role: "status", Text: "Lock before sleep: " + protected})
	if r != nil && r.managedState.Known && r.managedState.SleepError != "" {
		rows = append(rows, &ui.Node{Kind: ui.KindText, Tone: ui.ToneError, Text: "The session owner could not protect sleep."})
	}
	if r != nil && r.backgroundError != "" {
		rows = append(rows, &ui.Node{Kind: ui.KindText, Tone: ui.ToneError, Text: "Wallpaper and screensaver are held: " + r.backgroundError + "."})
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

const (
	lockPreviewWidth  = 480
	lockPreviewHeight = 270
	// A reel, not a slideshow: twenty four frames a second reads as motion
	// at this size, and the run ends after eight seconds so a settings
	// panel never spends a core behind a preview nobody is watching.
	lockPreviewFrames = 192
	lockPreviewEvery  = time.Second / 24
)

func newLockPreviewImage() *ui.Image {
	return &ui.Image{Width: lockPreviewWidth, Height: lockPreviewHeight, Stride: lockPreviewWidth * 4, Pix: make([]byte, lockPreviewWidth*lockPreviewHeight*4)}
}

// lockPreviewLoop steps the effect and hands every frame to publish. publish
// reports whether the panel still wants frames: false ends the run at once, so
// a closed panel or a changed effect costs one frame, not a reel. Reduced
// motion renders one held frame, which is the same answer as before.
func lockPreviewLoop(c lockconfig.Config, frames int, every time.Duration, stop <-chan struct{}, publish func(*ui.Image) bool) error {
	r, err := renderer.New(renderer.Config{Effect: c.Effect, Palette: c.Palette, Width: lockPreviewWidth, Height: lockPreviewHeight})
	if err != nil {
		return err
	}
	// The renderer wants one step before it will draw at all, and pausing
	// first would skip it: step, then pause, then hold.
	if err = r.Step(); err != nil {
		return err
	}
	r.SetPaused(c.ReducedMotion)
	if c.ReducedMotion {
		frames = 1
	}
	img := newLockPreviewImage()
	for i := 0; i < frames; i++ {
		select {
		case <-stop:
			return nil
		default:
		}
		if i > 0 {
			tick := time.NewTimer(every)
			select {
			case <-stop:
				tick.Stop()
				return nil
			case <-tick.C:
			}
			if err = r.Step(); err != nil {
				return err
			}
		}
		if _, err = r.Draw(img.Pix, img.Stride, nil); err != nil {
			return err
		}
		if !publish(img) {
			return nil
		}
	}
	return nil
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
		s.stopPreview()
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
		stop := make(chan struct{})
		s.previewStop = stop
		go func() {
			r.mu.Lock()
			if s.previewSequence != sequence {
				r.mu.Unlock()
				return
			}
			if s.image == nil {
				s.image = newLockPreviewImage()
			}
			live := s.image
			r.mu.Unlock()
			err := lockPreviewLoop(cfg, lockPreviewFrames, lockPreviewEvery, stop, func(frame *ui.Image) bool {
				r.mu.Lock()
				defer r.mu.Unlock()
				select {
				case <-r.closed:
					return false
				default:
				}
				if r.panelHosts[PanelSettings] != h || sequence != s.previewSequence {
					return false
				}
				copy(live.Pix, frame.Pix)
				r.rebuildPanel(h)
				r.publishSurface(h.output, panelSurfaceID(h.id))
				return true
			})
			r.mu.Lock()
			defer r.mu.Unlock()
			s.previewing = false
			if s.previewStop == stop {
				s.previewStop = nil
			}
			if sequence != s.previewSequence {
				return
			}
			if err != nil {
				log.Printf("shell: lock preview: %v", err)
				s.message = "Preview unavailable."
			}
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
			s.stopPreview()
			s.image = nil
			s.previewSequence++
		}
	}
	r.rebuildPanel(h)
	return true
}

// stopPreview ends a running reel. Callers hold r.mu.
func (s *lockScreenUI) stopPreview() {
	if s.previewStop != nil {
		close(s.previewStop)
		s.previewStop = nil
	}
	s.previewing = false
}
