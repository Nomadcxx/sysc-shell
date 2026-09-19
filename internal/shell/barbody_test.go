package shell

import (
	"testing"

	"github.com/Nomadcxx/sysc-shell/internal/config"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

// The body derivation insets the gap against the screen edge and both
// cross-axis ends, and stays flush on the far end along the main axis. The
// top row is the shape the bar has always had; the lower row is the sysc-419
// regression guard.
func TestTheBodyRectFollowsTheEdge(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, edge string
		want       ui.Rect
	}{
		{"top", "top", ui.Rect{X: 4, Y: 4, W: 1912, H: 44}},
		{"lower", "bottom", ui.Rect{X: 4, Y: 0, W: 1912, H: 44}},
		{"left", "left", ui.Rect{X: 4, Y: 4, W: 1916, H: 40}},
		{"right", "right", ui.Rect{X: 0, Y: 4, W: 1916, H: 40}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := config.Default()
			cfg.Bar.Height = 48
			cfg.Bar.Gap = 4
			cfg.Bar.Edge = tc.edge
			bar, err := NewWithTheme(ThemeFrom(cfg, cfg.Bar), cfg.Bar, "DP-9")
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(bar.stopAnimation)
			bar.mu.Lock()
			defer bar.mu.Unlock()
			if got := bar.bodyLocked(1920, 48); got != tc.want {
				t.Fatalf("body = %+v, want %+v", got, tc.want)
			}
		})
	}
}
