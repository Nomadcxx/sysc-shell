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
		default:
			record(name, applyTemplateTarget(name, home, on, rendered, forceOn(name)))
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

// templateTarget is the sidecar+directive model for one application: we own
// a generated file, and manage exactly one include-ish line in the user's
// real config. Names not in the table still use the guarded whole-file path
// until their port task adds a row.
//
// ponytail: directive matching uses the target's key prefix rather than an
// app-config parser; add format-aware parsing only when a supported target
// needs syntax beyond one managed line in a named section.
// block applications get no sidecar: the rendered body is managed in place
// between marker lines inside the user's own config file.
type templateTarget struct {
	sidecar    func(home string) []string
	directives func(home string) []directive
	signal     func(root string) error
	block      func(home string) (file, open, close string)
}

const (
	blockOpen  = "# >>> sysc-shell >>>"
	blockClose = "# <<< sysc-shell <<<"
)

func joined(home string, elems ...string) string {
	return filepath.Join(append([]string{home}, elems...)...)
}

var templateTargets = map[string]templateTarget{
	"alacritty": {
		sidecar: func(h string) []string {
			return []string{joined(h, ".config", "alacritty", "themes", "sysc-shell.toml")}
		},
		directives: func(h string) []directive {
			p := joined(h, ".config", "alacritty", "themes", "sysc-shell.toml")
			return []directive{{
				file:    joined(h, ".config", "alacritty", "alacritty.toml"),
				line:    `import = ["` + p + `"]`,
				key:     "import",
				section: "general",
				seed:    "[general]\n" + `import = ["` + p + `"]` + "\n",
				create:  true,
			}}
		},
	},
	"foot": {
		sidecar: func(h string) []string { return []string{joined(h, ".config", "foot", "themes", "sysc-shell")} },
		directives: func(h string) []directive {
			line := "include=~/.config/foot/themes/sysc-shell"
			return []directive{{
				file:    joined(h, ".config", "foot", "foot.ini"),
				line:    line,
				key:     "include=",
				section: "main",
				seed:    "[main]\n" + line + "\n",
				create:  true,
			}}
		},
	},
	"ghostty": {
		sidecar: func(h string) []string { return []string{joined(h, ".config", "ghostty", "themes", "sysc-shell")} },
		directives: func(h string) []directive {
			return []directive{{
				file:   joined(h, ".config", "ghostty", "config"),
				line:   "theme = sysc-shell",
				key:    "theme",
				create: true,
			}}
		},
	},
	"kitty": {
		sidecar: func(h string) []string { return []string{joined(h, ".config", "kitty", "themes", "sysc-shell.conf")} },
		directives: func(h string) []directive {
			return []directive{{
				file:   joined(h, ".config", "kitty", "kitty.conf"),
				line:   "include themes/sysc-shell.conf",
				key:    "include",
				create: true,
			}}
		},
		signal: signalKitty,
	},
	"helix": {
		sidecar: func(h string) []string { return []string{joined(h, ".config", "helix", "themes", "sysc-shell.toml")} },
		directives: func(h string) []directive {
			return []directive{{
				file: joined(h, ".config", "helix", "config.toml"),
				line: `theme = "sysc-shell"`,
				key:  "theme",
				top:  true,
			}}
		},
	},
	"cava": {
		sidecar: func(h string) []string { return []string{joined(h, ".config", "cava", "themes", "sysc-shell")} },
		directives: func(h string) []directive {
			return []directive{{
				file:    joined(h, ".config", "cava", "config"),
				line:    `theme = "sysc-shell"`,
				key:     "theme",
				section: "color",
			}}
		},
	},
	"btop": {
		sidecar: func(h string) []string { return []string{joined(h, ".config", "btop", "themes", "sysc-shell.theme")} },
		directives: func(h string) []directive {
			return []directive{{
				file: joined(h, ".config", "btop", "btop.conf"),
				line: `color_theme = "sysc-shell"`,
				key:  "color_theme",
			}}
		},
	},
	"kcolorscheme": {
		sidecar: func(h string) []string {
			return []string{joined(h, ".local", "share", "color-schemes", "sysc-shell.colors")}
		},
		directives: func(h string) []directive {
			return []directive{{
				file:    joined(h, ".config", "kdeglobals"),
				line:    "ColorSchemeName=sysc-shell",
				key:     "ColorSchemeName",
				section: "General",
				seed:    "[General]\nColorSchemeName=sysc-shell\n",
				create:  true,
			}}
		},
	},
	"starship": {
		block: func(h string) (string, string, string) {
			return joined(h, ".config", "starship.toml"), blockOpen, blockClose
		},
		directives: func(h string) []directive {
			return []directive{{
				file:   joined(h, ".config", "starship.toml"),
				line:   `palette = "sysc-shell"`,
				key:    "palette",
				top:    true,
				create: true,
			}}
		},
	},
	"qt": {
		sidecar: func(h string) []string {
			return []string{
				joined(h, ".config", "qt5ct", "colors", "sysc-shell.conf"),
				joined(h, ".config", "qt6ct", "colors", "sysc-shell.conf"),
			}
		},
		directives: func(h string) []directive {
			mk := func(ct, path string) directive {
				return directive{
					file:    joined(h, ".config", ct, ct+".conf"),
					line:    "color_scheme_path=" + path,
					key:     "color_scheme_path",
					section: "General",
				}
			}
			return []directive{
				mk("qt5ct", joined(h, ".config", "qt5ct", "colors", "sysc-shell.conf")),
				mk("qt6ct", joined(h, ".config", "qt6ct", "colors", "sysc-shell.conf")),
			}
		},
	},
	"wezterm": {
		sidecar: func(h string) []string { return []string{joined(h, ".config", "wezterm", "colors", "sysc-shell.toml")} },
		directives: func(h string) []directive {
			return []directive{{
				file:         joined(h, ".config", "wezterm", "wezterm.lua"),
				line:         `config.color_scheme = "sysc-shell"`,
				key:          "config.color_scheme",
				returnConfig: true,
			}}
		},
	},
}

// applyTemplateTarget is the per-app dispatch: sidecar+directive for table
// members, and a guarded whole-file write for the rest.
func applyTemplateTarget(name, home string, on bool, rendered string, force bool) error {
	tgt, known := templateTargets[name]
	if !known {
		target := writeTarget(home, name)
		if target == "" {
			return nil
		}
		if on {
			return applyWrite(target, rendered, force)
		}
		return UnapplyWrite(target)
	}
	if tgt.block != nil {
		file, open, close := tgt.block(home)
		if on {
			if err := ManageBlock(file, open, close, rendered, force); err != nil {
				return err
			}
			for _, dir := range tgt.directives(home) {
				if err := ensureDirective(dir, force); err != nil {
					return err
				}
			}
			return nil
		}
		for _, dir := range tgt.directives(home) {
			if err := RemoveDirective(dir); err != nil {
				return err
			}
		}
		return RemoveBlock(file, open, close, force)
	}
	sidecars := tgt.sidecar(home)
	if on {
		for _, sidecar := range sidecars {
			if err := applySidecar(sidecar, rendered, force); err != nil {
				return err
			}
		}
		for _, dir := range tgt.directives(home) {
			if err := ensureDirective(dir, force); err != nil {
				return err
			}
		}
		if tgt.signal != nil {
			return tgt.signal(procRoot)
		}
		return nil
	}
	for _, dir := range tgt.directives(home) {
		if err := RemoveDirective(dir); err != nil {
			return err
		}
	}
	for _, sidecar := range sidecars {
		if err := UnapplyWrite(sidecar); err != nil {
			return err
		}
	}
	return nil
}
