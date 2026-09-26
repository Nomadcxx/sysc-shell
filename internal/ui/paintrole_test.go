package ui

import (
	"testing"

	"github.com/Nomadcxx/sysc-shell/internal/theme"
)

func TestEveryColorRoleNameMapsToAPaintRole(t *testing.T) {
	seen := map[PaintRole]string{}
	for _, name := range theme.ColorRoleNames() {
		role, ok := PaintRoleFor(name)
		if !ok || role == PaintUnset {
			t.Fatalf("%q has no paint role", name)
		}
		if prev, dup := seen[role]; dup {
			t.Fatalf("%q and %q share role %d", prev, name, role)
		}
		seen[role] = name
	}
	if _, ok := PaintRoleFor("magenta"); ok {
		t.Fatal("unknown name resolved")
	}
}
