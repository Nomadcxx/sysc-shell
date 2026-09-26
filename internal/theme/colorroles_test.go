package theme

import "testing"

func TestColorRoleNamesAreTheMaterialSet(t *testing.T) {
	names := ColorRoleNames()
	if len(names) != 26 {
		t.Fatalf("len = %d, want 26", len(names))
	}
	for _, n := range []string{"surface_variant", "on_surface_variant", "primary_container", "outline"} {
		if !ValidColorRole(n) {
			t.Errorf("%q rejected", n)
		}
	}
	for _, n := range []string{"", "magenta", "Surface_Variant", "#ff00ff"} {
		if ValidColorRole(n) {
			t.Errorf("%q accepted", n)
		}
	}
	names[0] = "mutated"
	if ColorRoleNames()[0] == "mutated" {
		t.Fatal("ColorRoleNames returned the backing array")
	}
}
