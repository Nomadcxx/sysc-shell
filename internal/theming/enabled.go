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

type applyJob struct {
	home    string
	enabled func(string) bool
	tok     theme.Tokens
	force   func(string) bool
}

var (
	applyMu     sync.Mutex
	applyBusy   bool
	applyQueued *applyJob
)

// ApplyEnabled renders every enabled template for home. It returns the
// per-template outcomes -- a refusal or write failure keyed by template name
// -- plus the first error overall. force names templates the user explicitly
// overrode a refusal for; those are overwritten with a backup.
func ApplyEnabled(home string, enabled func(string) bool, tok theme.Tokens, force func(string) bool) (map[string]error, error) {
	if home == "" || enabled == nil {
		return nil, nil
	}
	job := applyJob{home: home, enabled: enabled, tok: tok, force: force}
	applyMu.Lock()
	if applyBusy {
		applyQueued = &job
		applyMu.Unlock()
		return nil, nil
	}
	applyBusy = true
	applyMu.Unlock()

	var err error
	var outcomes map[string]error
	current := job
	for {
		outcomes, err = applyOnce(current.home, current.enabled, current.tok, current.force)
		applyMu.Lock()
		if applyQueued == nil {
			applyBusy = false
			applyMu.Unlock()
			return outcomes, err
		}
		current = *applyQueued
		applyQueued = nil
		applyMu.Unlock()
	}
}

func applyOnce(home string, enabled func(string) bool, tok theme.Tokens, force func(string) bool) (map[string]error, error) {
	cat := Catalog()
	outcomes := map[string]error{}
	var first error
	record := func(name string, err error) {
		if err == nil {
			return
		}
		outcomes[name] = err
		if first == nil {
			first = err
		}
	}
	forceOn := func(name string) bool { return force != nil && force(name) }
	for _, name := range cat.Names() {
		rendered := Render(cat.Template(name), tok)
		// GH #7: a stub template rendered onto a live app config neuters the
		// app. An incomplete template behaves as off everywhere, which also
		// removes a stub written by an older release.
		on := enabled(name) && Complete(name)
		switch name {
		case "niri":
			cfg := filepath.Join(home, ".config", "niri", "config.kdl")
			gen := filepath.Join(home, ".config", "niri", "sysc-shell.kdl")
			if !on {
				record(name, UnapplyNiri(cfg, gen))
				continue
			}
			if _, err := os.Stat(cfg); err != nil {
				continue
			}
			record(name, applyNiri(cfg, gen, rendered, forceOn(name)))
		case "gtk3", "gtk4":
			ini := filepath.Join(home, ".config", "gtk-3.0", "settings.ini")
			css := filepath.Join(home, ".themes", "sysc-shell-Dark", "gtk-3.0", "gtk.css")
			if name == "gtk4" {
				ini = filepath.Join(home, ".config", "gtk-4.0", "settings.ini")
				css = filepath.Join(home, ".themes", "sysc-shell-Dark", "gtk-4.0", "gtk.css")
			}
			if !on {
				record(name, UnapplyWrite(css))
				record(name, UnapplyGtkThemeName(ini))
				continue
			}
			// Pointing gtk-theme-name at a css the shell refused to write
			// would half-apply the theme: skip the ini when the css fails.
			if err := applyWrite(css, rendered, forceOn(name)); err != nil {
				record(name, err)
				continue
			}
			record(name, ApplyGtkThemeName(ini, gtkOurs))
		default:
			target := writeTarget(home, name)
			if target == "" {
				continue
			}
			if !on {
				record(name, UnapplyWrite(target))
				continue
			}
			if err := applyWrite(target, rendered, forceOn(name)); err != nil {
				record(name, err)
				continue
			}
			if name == "kitty" {
				record(name, signalKitty(procRoot))
			}
		}
	}
	return outcomes, first
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
