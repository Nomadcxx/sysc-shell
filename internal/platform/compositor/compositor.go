// Package compositor identifies the running compositor and its configuration directory.
package compositor

import (
	"os"
	"path/filepath"
	"strings"
)

type Kind int

const (
	Niri Kind = iota
	SyscCompositor
)

func Detect(getenv func(string) string) Kind {
	if getenv("SYSC_SOCKET") != "" && strings.HasPrefix(filepath.Base(getenv("NIRI_SOCKET")), "sysc-compositor.") {
		return SyscCompositor
	}
	return Niri
}

// ConfigDir follows the XDG base directory specification; relative overrides are ignored.
func ConfigDir(home string, k Kind) string {
	root := os.Getenv("XDG_CONFIG_HOME")
	if !filepath.IsAbs(root) {
		root = filepath.Join(home, ".config")
	}
	name := "niri"
	if k == SyscCompositor {
		name = "sysc-compositor"
	}
	return filepath.Join(root, name)
}
