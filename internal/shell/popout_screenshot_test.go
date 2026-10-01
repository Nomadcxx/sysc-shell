package shell

import (
	"testing"

	"github.com/Nomadcxx/sysc-shell/internal/config"
)

func TestScreenshotBarItemIsKnownButNotDefault(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	for _, zone := range [][]config.Item{cfg.Bar.Left, cfg.Bar.Center, cfg.Bar.Right} {
		for _, item := range zone {
			if item.ID == "screenshot" {
				t.Fatal("the default bar layout must not change; the glyph is opt-in")
			}
		}
	}
	if _, err := config.Parse([]byte(`{"bar":{"items":{"right":[{"id":"screenshot"}]}}}`)); err != nil {
		t.Fatalf("a configured screenshot item must load: %v", err)
	}
}

func TestScreenshotWidgetOpensItsPanel(t *testing.T) {
	t.Parallel()
	w := buildScreenshotWidget()
	if w.node.Action != panelScreenshotAction || w.node.Name != "Screenshot" || w.node.Role != "button" {
		t.Fatalf("node = action %q name %q role %q", w.node.Action, w.node.Name, w.node.Role)
	}
	if w.tooltip != "Screenshot" {
		t.Fatalf("tooltip = %q, want Screenshot", w.tooltip)
	}
}
