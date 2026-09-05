package shell

import (
	"testing"

	"github.com/Nomadcxx/sysc-shell/internal/config"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

// TestWallpaperItemBuildsAWidget is the regression for the reported defect:
// config.knownItems accepted "wallpaper", so a bar naming it loaded without
// complaint, and buildWidgets had no case for it, so nothing was ever drawn.
// A validated item that produces no widget is the worst of both -- it neither
// works nor tells you why.
func TestWallpaperItemBuildsAWidget(t *testing.T) {
	t.Parallel()
	got := buildWidgets([]config.Item{{ID: "wallpaper"}}, 8)
	if len(got) != 1 {
		t.Fatalf("built %d widgets for the wallpaper item, want 1", len(got))
	}
	node := got[0].node
	if node == nil {
		t.Fatal("the wallpaper widget has no node")
	}
	if node.Action != panelWallpaperAction {
		t.Errorf("action = %q, want %q", node.Action, panelWallpaperAction)
	}
	if got[0].tooltip == "" {
		t.Error("the wallpaper widget carries no tooltip")
	}
	var text string
	walkNodes(node, func(n *ui.Node) {
		if n.Kind == ui.KindText && n.Text != "" {
			text = n.Text
		}
	})
	if text == "" {
		t.Error("the wallpaper widget paints no glyph")
	}
}

// TestEveryKnownBarItemBuilds closes the class rather than the instance. The
// config vocabulary and the widget builder are two lists that have to agree,
// and they drifted silently once already.
func TestEveryKnownBarItemBuilds(t *testing.T) {
	t.Parallel()
	for _, id := range config.KnownItemIDs() {
		switch id {
		case "group", "plugin":
			// Both are placements rather than widgets: a group holds nested
			// items and a plugin names an external one, so neither builds
			// from a bare id.
			continue
		}
		t.Run(id, func(t *testing.T) {
			t.Parallel()
			got := buildWidgets([]config.Item{{ID: id}}, 8)
			if len(got) == 0 {
				t.Fatalf("%q is a known bar item but builds no widget", id)
			}
			// Building is half the contract. applyLocked reads state through
			// refresh, or else through format and inner, so a widget missing
			// the one it does not set panics on the first bar apply -- which
			// happens inside the wl_output.done handler, where the panic is
			// recovered into an error and the shell simply never appears.
			bar := &Bar{left: got}
			bar.apply(barView{})
		})
	}
}
