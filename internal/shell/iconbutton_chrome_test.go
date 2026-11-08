package shell

import (
	"testing"

	"github.com/Nomadcxx/sysc-shell/internal/services"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

// TestIconButtonChromeIsDeclaredByCaller pins slice E's exceptions. The
// renderer paints an unfilled icon-only button flat, so the buttons that keep
// a chip -- the Control Centre rail and play/pause -- must declare it, and the
// notification centre header and its cards must not.
func TestIconButtonChromeIsDeclaredByCaller(t *testing.T) {
	h := &PanelHost{id: PanelControlCenter, section: "home", theme: DefaultTheme()}
	for _, entry := range ccRail(h).Children {
		if entry.Kind != ui.KindButton {
			continue
		}
		want := ui.FillContainerHighest
		if entry.Action == "section:home" {
			want = ui.FillAccent
		}
		if entry.Fill != want {
			t.Errorf("rail %q fill = %v, want %v", entry.Name, entry.Fill, want)
		}
	}

	buttons := mediaTransportButtons(services.MediaState{CanPlay: true, CanPrev: true, CanNext: true}, false)
	for _, b := range buttons {
		want := ui.FillNone
		if b.Action == "media:playpause" {
			want = ui.FillContainerHighest
		}
		if b.Fill != want {
			t.Errorf("transport %q fill = %v, want %v", b.Action, b.Fill, want)
		}
	}

	if n := centreIconButton("settings", "notify:settings", "Settings"); n.Fill != ui.FillNone {
		t.Errorf("notification header fill = %v, want none", n.Fill)
	}
	if n := centreRemoveButton("notify:remove:1", "Remove"); n.Fill != ui.FillNone {
		t.Errorf("notification remove fill = %v, want none", n.Fill)
	}
}
