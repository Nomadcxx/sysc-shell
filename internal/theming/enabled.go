package theming

import (
	"os"
	"path/filepath"

	"github.com/Nomadcxx/sysc-shell/internal/theme"
)

func ApplyEnabled(home string, enabled func(string) bool, tok theme.Tokens) {
	if home == "" || enabled == nil {
		return
	}
	cat := Catalog()
	for _, name := range cat.Names() {
		rendered := Render(cat.Template(name), tok)
		on := enabled(name)
		switch name {
		case "niri":
			cfg := filepath.Join(home, ".config", "niri", "config.kdl")
			gen := filepath.Join(home, ".config", "niri", "sysc-shell.kdl")
			if !on {
				_ = UnapplyNiri(cfg, gen)
				continue
			}
			if _, err := os.Stat(cfg); err != nil {
				continue
			}
			_ = ApplyNiri(cfg, gen, rendered)
		case "gtk3", "gtk4":
			ini := filepath.Join(home, ".config", "gtk-3.0", "settings.ini")
			if name == "gtk4" {
				ini = filepath.Join(home, ".config", "gtk-4.0", "settings.ini")
			}
			css := filepath.Join(home, ".themes", "sysc-shell-Dark", name, "gtk.css")
			if name == "gtk4" {
				css = filepath.Join(home, ".themes", "sysc-shell-Dark", "gtk-4.0", "gtk.css")
			} else {
				css = filepath.Join(home, ".themes", "sysc-shell-Dark", "gtk-3.0", "gtk.css")
			}
			if !on {
				_ = UnapplyWrite(css)
				_ = UnapplyGtkThemeName(ini)
				continue
			}
			_ = ApplyWrite(css, rendered)
			_ = ApplyGtkThemeName(ini, gtkOurs)
		default:
			target := filepath.Join(home, ".config", "sysc-shell", "themes", name+".conf")
			if !on {
				_ = UnapplyWrite(target)
				continue
			}
			_ = ApplyWrite(target, rendered)
		}
	}
}
