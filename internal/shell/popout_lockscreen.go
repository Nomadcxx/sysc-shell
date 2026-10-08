package shell

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image/png"
	"io"
	"log"
	"os/exec"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/Nomadcxx/sysc-shell/internal/icons"
	"github.com/Nomadcxx/sysc-shell/internal/lockconfig"
	"github.com/Nomadcxx/sysc-shell/internal/theme"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
	"github.com/Nomadcxx/sysc-terminal/renderer"
)

// Registry.mu owns the draft and completion of file/preview work.
type lockScreenUI struct {
	loaded, saving, previewing bool
	loadingHeaders             bool
	headers                    []string
	headerMessage              string
	loadErr                    bool
	config                     lockconfig.Config
	menu, message              string
	image                      *ui.Image
	previewSequence            uint64
	previewCancel              context.CancelFunc
}

// lockToggle is a labelled switch row, disabled while another save is in flight.
func lockToggle(label, action string, on, disabled bool) *ui.Node {
	node := &ui.Node{Kind: ui.KindToggle, Action: action, Focusable: true, Name: label, Role: "switch"}
	if on {
		node.Value = 1
	}
	if disabled {
		node.State |= ui.StateDisabled
		node.AriaDisabled, node.Focusable = true, false
	}
	return &ui.Node{Kind: ui.KindRow, Gap: theme.MarginM, Children: []*ui.Node{{Kind: ui.KindText, Text: label}, node}}
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
				s.loadErr = true
			} else {
				s.config = cfg
				s.loadErr = false
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
		{"effect", "Background", s.config.Effect, lockconfig.Effects()},
		{"header", "Header", s.config.Header, s.headers},
		{"texteffect", "Text effect", s.config.TextEffect, lockconfig.TextEffects()},
		{"palette", "Palette", s.config.WithShellTheme().Palette, renderer.Palettes()},
		{"clockstyle", "Clock style", s.config.ClockStyle, lockconfig.ClockStyles()},
		{"backend", "Effect engine", s.config.Backend(), []string{"auto", "cpu", "gpu"}},
		{"fps", "Effect FPS", strconv.Itoa(s.config.EffectFPS), []string{"10", "20", "30", "60"}},
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
			if choice.key == "header" && s.headerMessage != "" {
				rows = append(rows, &ui.Node{Kind: ui.KindText, Role: "status", Text: s.headerMessage})
			}
			opts := make([]wallpaperOption, 0, len(choice.values))
			for _, value := range choice.values {
				opts = append(opts, wallpaperOption{action: "lockscreen-" + choice.key + ":" + value, label: value, selected: value == choice.value})
			}
			rows = append(rows, wallpaperOptionList(h, opts))
		}
	}
	rows = append(rows,
		lockToggle("Follow shell theme", "lockscreen-follow-shell", s.config.FollowsShell(), saving),
		lockToggle("Reduced motion", "lockscreen-reduced", s.config.ReducedMotion, saving),
		lockToggle("24-hour clock", "lockscreen-clock24", s.config.Clock24h, saving),
		lockToggle("Blur backdrop", "lockscreen-blur", s.config.BlurBackdrop(), saving),
		lockToggle("GPU power save", "lockscreen-gpu-save", s.config.GpuPowerSave(), saving),
	)
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
	if saving || s.loadErr {
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
		rows = append(rows, &ui.Node{Kind: ui.KindText, Text: "Still preview"}, &ui.Node{Kind: ui.KindImage, Image: s.image, ImageW: w, ImageH: w * s.image.Height / s.image.Width})
	}
	return settingsBody(h, theme.MarginM, rows...)
}

const (
	lockPreviewWidth    = 480
	lockPreviewHeight   = 270
	lockPreviewPNGBytes = 4 << 20
	lockPreviewTimeout  = 4 * time.Second
)

// lockPreview renders an ordinary sample through the installed locker; no
// credentials, session bus or lock request crosses this command boundary.
func lockPreview(ctx context.Context, c lockconfig.Config) (*ui.Image, error) {
	if err := c.Validate(); err != nil {
		return nil, err
	}
	// Invalid decoration uses the native shipped catalogue, just as locking does.
	headers, _ := lockconfig.ReadHeaders(lockconfig.HeadersPath())
	c = c.WithShellTheme()
	request, err := json.Marshal(struct {
		Headers string            `json:"headers"`
		Config  lockconfig.Config `json:"config"`
		Width   int               `json:"width"`
		Height  int               `json:"height"`
	}{headers, c, 2 * lockPreviewWidth, 2 * lockPreviewHeight})
	if err != nil {
		return nil, err
	}
	data, err := lockCommand(ctx, "--preview", request, lockPreviewPNGBytes)
	if err != nil {
		return nil, err
	}
	header, err := png.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	if header.Width != 2*lockPreviewWidth || header.Height != 2*lockPreviewHeight {
		return nil, fmt.Errorf("lock preview dimensions differ")
	}
	// Reuse the shell's bounded raster decoder, scaling the matching aspect
	// ratio to the ordinary settings image and converting to premultiplied BGRA.
	img := icons.DecodeRaster(data, lockPreviewWidth, lockPreviewHeight)
	if img == nil {
		return nil, fmt.Errorf("cannot decode lock preview")
	}
	return img, nil
}

// lockCommand bounds native output and closes inherited pipes on cancellation.
func lockCommand(ctx context.Context, argument string, input []byte, budget int64) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "sysc-lock", argument)
	cmd.Stdin = bytes.NewReader(input)
	out, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	defer out.Close()
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	// Closing the read side also bounds a child that leaves an inherited pipe
	// open after the preview process has been cancelled.
	stopClose := context.AfterFunc(ctx, func() { _ = out.Close() })
	defer stopClose()
	data, readErr := io.ReadAll(io.LimitReader(out, budget+1))
	if readErr != nil || int64(len(data)) > budget {
		_ = cmd.Process.Kill()
	}
	waitErr := cmd.Wait()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if readErr != nil {
		return nil, readErr
	}
	if int64(len(data)) > budget {
		return nil, fmt.Errorf("lock command exceeds output budget")
	}
	if waitErr != nil {
		return nil, waitErr
	}
	return data, nil
}

func lockHeaderIDs(ctx context.Context) ([]string, error) {
	data, err := lockCommand(ctx, "--describe", nil, lockconfig.MaxBytes)
	if err != nil {
		return nil, err
	}
	var description struct {
		Headers []string `json:"headers"`
	}
	if err := json.Unmarshal(data, &description); err != nil {
		return nil, err
	}
	if len(description.Headers) == 0 || len(description.Headers) > 16 {
		return nil, fmt.Errorf("invalid header count")
	}
	seen := make(map[string]bool)
	for _, id := range description.Headers {
		if !strings.HasPrefix(id, "ascii_") || len(id) <= 6 || len(id) > 32 || seen[id] {
			return nil, fmt.Errorf("invalid header ID")
		}
		for _, c := range id {
			if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_') {
				return nil, fmt.Errorf("invalid header ID")
			}
		}
		seen[id] = true
	}
	return description.Headers, nil
}

// loadLockHeaders queries only when the menu opens. Registry.mu owns publication.
func (r *Registry) loadLockHeaders(h *PanelHost) {
	s := &h.lockScreen
	if s.loadingHeaders {
		return
	}
	s.loadingHeaders, s.headerMessage = true, "Loading headers…"
	s.headers = nil
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), lockPreviewTimeout)
		defer cancel()
		headers, err := lockHeaderIDs(ctx)
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
		s.loadingHeaders = false
		if err != nil {
			s.headerMessage = "Headers unavailable. Install or update sysc-lock."
		} else {
			s.headers, s.headerMessage = headers, ""
		}
		r.rebuildPanel(h)
		r.publishSurface(h.output, panelSurfaceID(h.id))
	}()
}

// finishLockPreview only publishes the current request into its owning panel.
func (r *Registry) finishLockPreview(h *PanelHost, sequence uint64, img *ui.Image, err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	select {
	case <-r.closed:
		return
	default:
	}
	s := &h.lockScreen
	if r.panelHosts[PanelSettings] != h || sequence != s.previewSequence {
		return
	}
	s.previewing, s.previewCancel = false, nil
	if err != nil {
		log.Printf("shell: lock preview: %v", err)
		s.message = "Preview unavailable. Install or update sysc-lock."
	} else {
		s.image = img
	}
	r.rebuildPanel(h)
	r.publishSurface(h.output, panelSurfaceID(h.id))
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
		switch menu {
		case "effect", "header", "texteffect", "palette", "clockstyle", "backend", "fps":
		default:
			return true
		}
		if s.menu == menu {
			s.menu = ""
		} else {
			s.menu = menu
			if menu == "header" {
				r.loadLockHeaders(h)
			}
		}
	case n.Action == "lockscreen-follow-shell":
		follow := !s.config.FollowsShell()
		s.config.FollowShell = &follow
		s.draftChanged()
	case n.Action == "lockscreen-reduced":
		s.config.ReducedMotion = !s.config.ReducedMotion
		s.draftChanged()
	case n.Action == "lockscreen-clock24":
		s.config.Clock24h = !s.config.Clock24h
		s.draftChanged()
	case n.Action == "lockscreen-blur":
		blur := !s.config.BlurBackdrop()
		s.config.Blur = &blur
		s.draftChanged()
	case n.Action == "lockscreen-gpu-save":
		save := !s.config.GpuPowerSave()
		s.config.EffectGpuPowerSave = &save
		s.draftChanged()
	case n.Action == "lockscreen-preview":
		if s.previewing {
			return true
		}
		s.previewing = true
		s.message = ""
		s.previewSequence++
		sequence, cfg := s.previewSequence, s.config
		ctx, cancel := context.WithTimeout(context.Background(), lockPreviewTimeout)
		s.previewCancel = cancel
		go func() {
			defer cancel()
			go func() {
				select {
				case <-h.stopAnim:
					cancel()
				case <-r.closed:
					cancel()
				case <-ctx.Done():
				}
			}()
			img, err := lockPreview(ctx, cfg)
			r.finishLockPreview(h, sequence, img, err)
		}()
	case n.Action == "lockscreen-apply":
		// Never write a draft seeded from defaults over a failed read.
		if s.loadErr {
			break
		}
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
		} else if value, ok := strings.CutPrefix(n.Action, "lockscreen-header:"); ok {
			if !slices.Contains(s.headers, value) {
				s.message = "That presentation choice is unavailable."
				break
			}
			cfg.Header = value
		} else if value, ok := strings.CutPrefix(n.Action, "lockscreen-texteffect:"); ok {
			cfg.TextEffect = value
		} else if value, ok := strings.CutPrefix(n.Action, "lockscreen-palette:"); ok {
			cfg.Palette = value
			follow := false
			cfg.FollowShell = &follow
		} else if value, ok := strings.CutPrefix(n.Action, "lockscreen-clockstyle:"); ok {
			if !slices.Contains(lockconfig.ClockStyles(), value) {
				s.message = "That presentation choice is unavailable."
				break
			}
			cfg.ClockStyle = value
		} else if value, ok := strings.CutPrefix(n.Action, "lockscreen-backend:"); ok {
			if value != "auto" && value != "cpu" && value != "gpu" {
				s.message = "That presentation choice is unavailable."
				break
			}
			backend := value
			cfg.EffectBackend = &backend
		} else if value, ok := strings.CutPrefix(n.Action, "lockscreen-fps:"); ok {
			fps, err := strconv.Atoi(value)
			if err != nil || fps < 10 || fps > 120 {
				s.message = "That presentation choice is unavailable."
				break
			}
			cfg.EffectFPS = fps
		} else {
			return false
		}
		if err := cfg.Validate(); err != nil {
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

// stopPreview cancels the native sample request. Callers hold r.mu.
func (s *lockScreenUI) stopPreview() {
	if s.previewCancel != nil {
		s.previewCancel()
		s.previewCancel = nil
	}
	s.previewing = false
}

// draftChanged drops the native request and its frame after a draft edit.
// Callers hold r.mu.
func (s *lockScreenUI) draftChanged() {
	s.stopPreview()
	s.previewSequence++
	s.image = nil
	s.message = ""
}
