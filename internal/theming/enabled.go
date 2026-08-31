package theming

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"

	"github.com/Nomadcxx/sysc-shell/internal/theme"
)

var procRoot = "/proc"

var (
	applyMu   sync.Mutex
	applyBusy bool
	applyPend bool
)

func ApplyEnabled(home string, enabled func(string) bool, tok theme.Tokens) error {
	if home == "" || enabled == nil {
		return nil
	}
	applyMu.Lock()
	if applyBusy {
		applyPend = true
		applyMu.Unlock()
		return nil
	}
	applyBusy = true
	applyMu.Unlock()

	var err error
	for {
		err = applyOnce(home, enabled, tok)
		applyMu.Lock()
		if !applyPend {
			applyBusy = false
			applyMu.Unlock()
			return err
		}
		applyPend = false
		applyMu.Unlock()
	}
}

func applyOnce(home string, enabled func(string) bool, tok theme.Tokens) error {
	cat := Catalog()
	var first error
	keep := func(err error) {
		if first == nil && err != nil {
			first = err
		}
	}
	for _, name := range cat.Names() {
		rendered := Render(cat.Template(name), tok)
		on := enabled(name)
		switch name {
		case "niri":
			cfg := filepath.Join(home, ".config", "niri", "config.kdl")
			gen := filepath.Join(home, ".config", "niri", "sysc-shell.kdl")
			if !on {
				keep(UnapplyNiri(cfg, gen))
				continue
			}
			if _, err := os.Stat(cfg); err != nil {
				continue
			}
			keep(ApplyNiri(cfg, gen, rendered))
		case "gtk3", "gtk4":
			ini := filepath.Join(home, ".config", "gtk-3.0", "settings.ini")
			css := filepath.Join(home, ".themes", "sysc-shell-Dark", "gtk-3.0", "gtk.css")
			if name == "gtk4" {
				ini = filepath.Join(home, ".config", "gtk-4.0", "settings.ini")
				css = filepath.Join(home, ".themes", "sysc-shell-Dark", "gtk-4.0", "gtk.css")
			}
			if !on {
				keep(UnapplyWrite(css))
				keep(UnapplyGtkThemeName(ini))
				continue
			}
			keep(ApplyWrite(css, rendered))
			keep(ApplyGtkThemeName(ini, gtkOurs))
		default:
			target := writeTarget(home, name)
			if target == "" {
				continue
			}
			if !on {
				keep(UnapplyWrite(target))
				continue
			}
			if err := ApplyWrite(target, rendered); err != nil {
				keep(err)
				continue
			}
			if name == "kitty" {
				keep(signalKitty(procRoot))
			}
		}
	}
	return first
}

func writeTarget(home, name string) string {
	switch name {
	case "alacritty":
		return filepath.Join(home, ".config", "alacritty", "alacritty.toml")
	case "foot":
		return filepath.Join(home, ".config", "foot", "foot.ini")
	case "ghostty":
		return filepath.Join(home, ".config", "ghostty", "config")
	case "kitty":
		return filepath.Join(home, ".config", "kitty", "kitty.conf")
	case "wezterm":
		return filepath.Join(home, ".config", "wezterm", "wezterm.lua")
	case "qt":
		return filepath.Join(home, ".config", "qt5ct", "colors", "sysc-shell.conf")
	case "kcolorscheme":
		return filepath.Join(home, ".local", "share", "color-schemes", "sysc-shell.colors")
	case "emacs":
		return filepath.Join(home, ".emacs.d", "sysc-shell-theme.el")
	case "helix":
		return filepath.Join(home, ".config", "helix", "themes", "sysc-shell.toml")
	case "btop":
		return filepath.Join(home, ".config", "btop", "themes", "sysc-shell.theme")
	case "cava":
		return filepath.Join(home, ".config", "cava", "config")
	case "starship":
		return filepath.Join(home, ".config", "starship.toml")
	case "scroll":
		return filepath.Join(home, ".config", "scroll", "config")
	default:
		return ""
	}
}

func kittyPIDs(root string) []int {
	ents, err := os.ReadDir(root)
	if err != nil {
		return nil
	}
	var pids []int
	for _, e := range ents {
		if !e.IsDir() {
			continue
		}
		pid, err := strconv.Atoi(e.Name())
		if err != nil {
			continue
		}
		b, err := os.ReadFile(filepath.Join(root, e.Name(), "comm"))
		if err != nil {
			continue
		}
		if strings.TrimSpace(string(b)) == "kitty" {
			pids = append(pids, pid)
		}
	}
	return pids
}

func signalKitty(root string) error {
	var first error
	for _, pid := range kittyPIDs(root) {
		p, err := os.FindProcess(pid)
		if err != nil {
			if first == nil {
				first = err
			}
			continue
		}
		if err := p.Signal(syscall.SIGUSR1); err != nil && first == nil {
			first = err
		}
	}
	return first
}
