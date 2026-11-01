// Package icons exposes the shell icon catalogue to plugin tooling that must
// validate manifest icon names before the host loads a plugin.
package icons

import "github.com/Nomadcxx/sysc-shell/internal/render"

// ValidName reports whether the shell can render name from its built-in or
// Material icon catalogue.
func ValidName(name string) bool {
	if _, ok := render.IconByName(name); ok {
		return true
	}
	return render.ValidMaterialIcon(name)
}
