package shell

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Nomadcxx/sysc-shell/internal/config"
	"github.com/Nomadcxx/sysc-shell/internal/inputconfig"
	"github.com/Nomadcxx/sysc-shell/internal/platform/compositor"
	"github.com/Nomadcxx/sysc-shell/internal/platform/niri"
	"github.com/Nomadcxx/sysc-shell/internal/settings"
)

type inputSettingsSnapshot struct {
	dir                           string
	effective                     config.Input
	touchpad, ready               bool
	blocked                       string
	touchpadSection, mouseSection inputconfig.Section
}

func (s inputSettingsSnapshot) option() settings.Option {
	blocked := s.blocked
	if !s.ready {
		blocked = "Loading input settings…"
	}
	return settings.WithInput(s.effective, s.touchpad, blocked)
}

func loadInputSettings(dir string, kind compositor.Kind, version, udevRoot string) inputSettingsSnapshot {
	s := inputSettingsSnapshot{dir: dir, ready: true, touchpad: inputconfig.HasTouchpad(udevRoot)}
	if kind == compositor.Niri && !inputconfig.SupportedVersion(version) {
		s.blocked = "Needs Niri 26.04 or sysc-compositor"
		return s
	}
	var err error
	s.touchpadSection, s.mouseSection, err = inputconfig.ReadSections(filepath.Join(dir, "config.kdl"))
	if err == nil {
		s.effective, err = inputconfig.Effective(s.touchpadSection, s.mouseSection)
	}
	if err != nil {
		s.blocked = err.Error()
	}
	return s
}

func readInputSettings() inputSettingsSnapshot {
	kind := compositor.Detect(os.Getenv)
	version := ""
	if kind == compositor.Niri {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		var err error
		version, err = niri.Version(ctx, os.Getenv("NIRI_SOCKET"))
		if err != nil {
			return inputSettingsSnapshot{ready: true, blocked: "Could not read compositor version: " + err.Error()}
		}
	}
	return loadInputSettings(compositor.ConfigDir(os.Getenv("HOME"), kind), kind, version, "/run/udev/data")
}

func (r *Registry) refreshInputSettingsAsync(h *PanelHost) {
	go func() {
		state := readInputSettings()
		r.mu.Lock()
		r.inputSettings = state
		if r.panelHosts[PanelSettings] != h {
			r.mu.Unlock()
			return
		}
		h.set = r.settingsForLocked(h.draft)
		r.rebuildPanel(h)
		output := h.output
		r.mu.Unlock()
		r.publishSurface(output, panelSurfaceID(PanelSettings))
	}()
}

func hasInputChanges(changes []settingsWriteChange) bool {
	for _, change := range changes {
		if strings.HasPrefix(change.path, "input.") {
			return true
		}
	}
	return false
}

// applyInputSettings runs on the settings writer, outside Registry.mu, and
// refuses the whole transaction before changing files if the config is unreadable.
func applyInputSettings(cfg *config.Config, changes []settingsWriteChange, state inputSettingsSnapshot) error {
	if state.blocked != "" {
		return fmt.Errorf("%s", state.blocked)
	}
	registry := settings.DefaultFor(*cfg, settings.WithInput(state.effective, true, ""))
	candidate := cfg.Clone()
	for _, change := range changes {
		if !strings.HasPrefix(change.path, "input.") {
			continue
		}
		entry := registry.ByPath(change.path)
		if entry == nil || entry.Set == nil {
			return fmt.Errorf("unknown input setting %q", change.path)
		}
		if err := entry.Set(&candidate, change.value); err != nil {
			return err
		}
	}
	if err := inputconfig.Write(state.dir, candidate.Input, state.touchpadSection, state.mouseSection); err != nil {
		return err
	}
	cfg.Input = candidate.Input
	return nil
}
