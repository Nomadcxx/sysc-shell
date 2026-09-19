package shell

import (
	"fmt"
	"testing"

	"github.com/Nomadcxx/sysc-shell/internal/config"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
	tray "github.com/Nomadcxx/sysc-tray/protocol"
)

// The tray rides the right lane: on a side bar it stacks from the lane's
// placed items toward the lower end of the strip, and the available extent
// comes from the transposed lanes. On a top bar the two-pass sizing is what
// it always was.
func TestTheTrayStacksAlongTheMainAxis(t *testing.T) {
	t.Parallel()
	newBar := func(t *testing.T, edge string) *Bar {
		cfg := config.Default()
		cfg.Bar.Edge = edge
		cfg.Bar.Height = 48
		cfg.Bar.Left, cfg.Bar.Center, cfg.Bar.Right = nil, nil, nil
		bar, err := NewWithTheme(ThemeFrom(cfg, cfg.Bar), cfg.Bar, "DP-9")
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(bar.stopAnimation)
		return bar
	}
	items := func(n int) []tray.Item {
		out := make([]tray.Item, n)
		for i := range out {
			out[i] = tray.Item{
				Key: tray.ItemKey{Owner: "org.test", ObjectPath: fmt.Sprintf("/item%d", i), Generation: 1},
				ID:  fmt.Sprintf("org.test.item%d", i),
			}
		}
		return out
	}

	t.Run("stacks toward the lower end on a side bar", func(t *testing.T) {
		bar := newBar(t, "left")
		bar.setTray(items(3), config.TrayPreferences{}, nil)
		bar.mu.Lock()
		defer bar.mu.Unlock()
		if err := bar.layoutLocked(60, 800); err != nil {
			t.Fatalf("layoutLocked: %v", err)
		}
		content := bar.contentLocked(60, 800)
		if bar.trayAvailable != content.H/2 {
			t.Fatalf("tray available = %d, want the lower half of the strip (%d)",
				bar.trayAvailable, content.H/2)
		}
		if len(bar.trayNodes) != 3 {
			t.Fatalf("tray nodes = %d, want 3", len(bar.trayNodes))
		}
		for i, n := range bar.trayNodes {
			if n.Bounds.W != trayItemSize || n.Bounds.H != trayItemSize {
				t.Fatalf("tray node %d bounds = %+v, want a square of %d", i, n.Bounds, trayItemSize)
			}
			if want := content.X + (content.W-trayItemSize)/2; n.Bounds.X != want {
				t.Fatalf("tray node %d X = %d, want centred at %d", i, n.Bounds.X, want)
			}
			if i > 0 && n.Bounds.Y <= bar.trayNodes[i-1].Bounds.Y {
				t.Fatalf("tray node %d Y = %d, want stacking downward after %d",
					i, n.Bounds.Y, bar.trayNodes[i-1].Bounds.Y)
			}
		}
		if last := bar.trayNodes[len(bar.trayNodes)-1].Bounds; last.Y+last.H != content.Y+content.H {
			t.Fatalf("tray ends at %d, want flush to the lower end %d",
				last.Y+last.H, content.Y+content.H)
		}
	})

	t.Run("overflow produces the indicator", func(t *testing.T) {
		bar := newBar(t, "left")
		bar.setTray(items(40), config.TrayPreferences{}, nil)
		bar.mu.Lock()
		defer bar.mu.Unlock()
		if err := bar.layoutLocked(60, 800); err != nil {
			t.Fatalf("layoutLocked: %v", err)
		}
		overflow := false
		for _, n := range bar.trayNodes {
			if n.Kind == ui.KindButton && n.Text == "…" {
				overflow = true
			}
		}
		if !overflow {
			t.Fatal("an overflowing tray produced no … node")
		}
	})

	t.Run("a top bar sizes the tray as before", func(t *testing.T) {
		bar := newBar(t, "top")
		bar.setTray(items(3), config.TrayPreferences{}, nil)
		bar.mu.Lock()
		defer bar.mu.Unlock()
		if err := bar.layoutLocked(1200, 48); err != nil {
			t.Fatalf("layoutLocked: %v", err)
		}
		content := bar.contentLocked(1200, 48)
		if bar.trayAvailable != content.W/2 {
			t.Fatalf("tray available = %d, want the right half of the band (%d)",
				bar.trayAvailable, content.W/2)
		}
		if len(bar.trayNodes) != 3 {
			t.Fatalf("tray nodes = %d, want 3", len(bar.trayNodes))
		}
		for i, n := range bar.trayNodes {
			if i > 0 && n.Bounds.X <= bar.trayNodes[i-1].Bounds.X {
				t.Fatalf("tray node %d X = %d, want rightward of %d",
					i, n.Bounds.X, bar.trayNodes[i-1].Bounds.X)
			}
		}
		if last := bar.trayNodes[len(bar.trayNodes)-1].Bounds; last.X+last.W != content.X+content.W {
			t.Fatalf("tray ends at %d, want flush to the right end %d",
				last.X+last.W, content.X+content.W)
		}
	})
}
