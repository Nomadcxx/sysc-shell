package shell

import (
	"slices"
	"testing"

	"github.com/Nomadcxx/sysc-shell/internal/config"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

func TestMediaIsAKnownBarItem(t *testing.T) {
	t.Parallel()
	if !slices.Contains(config.KnownItemIDs(), "media") {
		t.Fatal(`"media" is not a known bar item`)
	}
}

func TestMediaWidgetCarriesNoPlayerPicker(t *testing.T) {
	t.Parallel()
	// The widget is not the page but smaller: one glyph, an optional title,
	// and gestures. A device or player list exists once, in the page. Without
	// this the widget grows a second picker.
	w := buildMediaWidget()
	var rows int
	walkNodes(w.node, func(n *ui.Node) {
		if n.Kind == ui.KindVirtualList || n.Kind == ui.KindScroll {
			rows++
		}
	})
	if rows != 0 {
		t.Errorf("the widget contains %d list nodes; it must carry no picker", rows)
	}
}

func TestMediaWidgetIsAbsentWithNoPlayer(t *testing.T) {
	t.Parallel()
	// A bar with nothing playing should not reserve a gap.
	w := buildMediaWidget()
	if !w.refresh(barView{}) && !w.node.Absent {
		t.Error("the widget did not mark itself absent with no player")
	}
}
