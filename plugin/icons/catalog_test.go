package icons

import "testing"

func TestValidNameCoversBothCatalogues(t *testing.T) {
	for _, tc := range []struct {
		name string
		want bool
	}{
		{"ghost", true},      // project font
		{"music_note", true}, // Material
		{"no-such-icon", false},
		{"", false},
	} {
		if got := ValidName(tc.name); got != tc.want {
			t.Errorf("ValidName(%q) = %v, want %v", tc.name, got, tc.want)
		}
	}
}
