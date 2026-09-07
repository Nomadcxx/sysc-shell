package shell

import (
	"testing"

	"github.com/Nomadcxx/sysc-shell/internal/config"
	"github.com/Nomadcxx/sysc-shell/internal/services"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

func TestVolumeWidgetSatisfiesTheApplyContract(t *testing.T) {
	ws := buildWidgets([]config.Item{{ID: "volume"}}, 6)
	if len(ws) != 1 {
		t.Fatalf("buildWidgets = %d widgets, want 1", len(ws))
	}
	w := ws[0]
	if w.refresh == nil && (w.node == nil || w.format == nil) {
		t.Error("volume widget satisfies neither seam of the apply contract")
	}
}

func TestVolumeGlyphFollowsMuteAndLevel(t *testing.T) {
	n := &ui.Node{Kind: ui.KindIcon, Icon: "volume_up"}
	if !refreshVolumeWidget(n, barView{Audio: services.AudioState{Level: 50, Muted: true}}) {
		t.Error("mute must change the glyph")
	}
	if n.Icon != "volume_off" {
		t.Errorf("muted glyph = %q, want volume_off", n.Icon)
	}
	if refreshVolumeWidget(n, barView{Audio: services.AudioState{Level: 0}}) && n.Icon != "volume_off" {
		t.Errorf("level 0 glyph = %q, want volume_off", n.Icon)
	}
}
