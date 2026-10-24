package shell

import (
	"testing"

	"github.com/Nomadcxx/sysc-shell/internal/theme"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

// TestControlCentreQuickTilesReadTheCardRung is the metric half of the
// quick-tile fix. The control centre's filled tiles padded on the panel
// rung, so a card inside the panel read as the panel around it and no
// density row could move it.
//
// The test hands the panel a metrics row with a card padding the ladder
// does not contain. A tile that reads the row follows it; a tile that still
// names a package constant keeps the constant and fails here.
func TestControlCentreQuickTilesReadTheCardRung(t *testing.T) {
	m, ok := theme.MetricsFor(theme.DensityDefault)
	if !ok {
		t.Fatal("no default metrics row")
	}
	const probe = 17
	m.CardPadding = probe

	h := &PanelHost{id: PanelControlCenter, section: "home", theme: DefaultTheme()}
	h.theme.Metrics = m
	tiles := ccHomeQuickTiles(ccHome(&Registry{}, h))
	if len(tiles) != 4 {
		t.Fatalf("home quick tiles = %d, want the four home tiles", len(tiles))
	}
	for i, tile := range tiles {
		if tile.Kind != ui.KindCapsule || tile.Fill != ui.FillContainerHigh {
			t.Errorf("tile %d = %+v, want a filled card", i, tile)
			continue
		}
		if tile.Padding != probe {
			t.Errorf("tile %d padding = %d, want the row's %d", i, tile.Padding, probe)
		}
	}
}

// ccHomeQuickTiles returns the filled quick tiles on the Home page: the
// right column of the split, two rows of two. The path is walked rather than
// searched by shape, so a tile that stops being a card is a failure of the
// walk, not a silently shorter list.
func ccHomeQuickTiles(home *ui.Node) []*ui.Node {
	if home == nil || len(home.Children) < 3 {
		return nil
	}
	split := home.Children[2]
	if split == nil || len(split.Children) < 2 {
		return nil
	}
	column := split.Children[1]
	if column == nil {
		return nil
	}
	var out []*ui.Node
	for _, row := range column.Children {
		if row == nil {
			continue
		}
		out = append(out, row.Children...)
	}
	return out
}
