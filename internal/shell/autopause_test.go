package shell

import (
	"testing"

	"github.com/Nomadcxx/sysc-shell/internal/wallpaper"
)

func TestAutoPauseAreaExcludesTheBar(t *testing.T) {
	cases := []struct {
		name      string
		edge      string
		thickness int
		want      wallpaper.AutoArea
	}{
		{"top", "top", 36, wallpaper.AutoArea{Y: 36, W: 1000, H: 764}},
		{"bottom", "bottom", 36, wallpaper.AutoArea{W: 1000, H: 764}},
		{"left", "left", 36, wallpaper.AutoArea{X: 36, W: 964, H: 800}},
		{"right", "right", 36, wallpaper.AutoArea{W: 964, H: 800}},
		{"clamped", "top", 900, wallpaper.AutoArea{Y: 900, W: 1000, H: 0}},
	}
	for _, tc := range cases {
		if got := autoPauseArea(tc.edge, tc.thickness, 1000, 800); got != tc.want {
			t.Fatalf("%s: area = %+v, want %+v", tc.name, got, tc.want)
		}
	}
}
