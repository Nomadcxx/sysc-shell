package shell

import (
	"errors"
	"strings"
	"testing"

	"github.com/Nomadcxx/sysc-shell/internal/services"
	"github.com/Nomadcxx/sysc-shell/internal/theme"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

func TestAudioPanelNameAndTriggerCentredPlacement(t *testing.T) {
	id, err := parsePanelName("audio")
	if err != nil || id != PanelAudio {
		t.Fatalf("parsePanelName(audio) = %v, %v", id, err)
	}
	if got := PanelAudio.String(); got != "audio" {
		t.Errorf("String() = %q, want audio", got)
	}
	// Fused: Gap 0 anchors at the bar zone; trigger-centred: the panel's
	// middle sits under the widget's middle, clamped to the output.
	p := Placement{
		BarEdge: "top", Output: ui.Rect{W: 3440, H: 1440},
		BarZone: 34, Gap: 0, Padding: 8,
		Panel: ui.Rect{W: 584, H: 496}, AnchorX: 400,
	}
	m := p.Margins()
	if m.Top != 34 {
		t.Errorf("Top = %d, want 34 (flush with the bar zone)", m.Top)
	}
	if want := 400 - 584/2; m.Left != want {
		t.Errorf("Left = %d, want %d (centred on the trigger)", m.Left, want)
	}
	// Clamp: a trigger near the right edge cannot push the panel off-screen.
	p.AnchorX = 3440 - 40
	if got := p.Margins().Left; got > 3440-584-8 {
		t.Errorf("Left = %d, want clamped inside the output", got)
	}
}

func TestVolumeRowCarriesTheFullContract(t *testing.T) {
	n := services.AudioNode{ID: 42, Name: "dev", Description: "AD106M High Definition Audio Controller", Level: 51}
	row := audioVolumeRow(n, "Output", nil)
	if len(row.Children) != 4 {
		t.Fatalf("row has %d regions, want 4 (per D4)", len(row.Children))
	}
	var slider *ui.Node
	walkAudio(row, func(x *ui.Node) {
		if x.Kind == ui.KindSlider {
			slider = x
		}
	})
	if slider == nil {
		t.Fatal("row carries no slider")
	}
	if slider.Value != 51 {
		t.Errorf("slider.Value = %v, want 51 (cubic from the snapshot)", slider.Value)
	}
	if row.Height != 0 && (row.Height < 68 || row.Height > 76) {
		t.Errorf("row height = %d, want the 68 px contract (density may scale)", row.Height)
	}
}

func TestEmptyApplicationsSectionIsNotAbsent(t *testing.T) {
	r := &Registry{}
	h := &PanelHost{id: PanelAudio, audioTab: "volumes"}
	got := renderAudioText(audioVolumesTree(r, h))
	if !strings.Contains(got, "Applications") {
		t.Error("Volumes renders no Applications label for an empty mixer: it looks broken (D12)")
	}
	if strings.Contains(got, "0%") {
		t.Error("Volumes painted 0% before a sample landed: stale must read as a dash")
	}
}

func TestScheduleControlDropsAStaleResult(t *testing.T) {
	r := &Registry{}
	h := &PanelHost{id: PanelAudio}
	done := make(chan struct{})
	r.mu.Lock()
	r.scheduleControl(h, func() error { close(done); return errors.New("boom") })
	r.mu.Unlock()
	<-done
	r.mu.Lock()
	defer r.mu.Unlock()
	if h.errLabel != "" {
		t.Errorf("errLabel = %q, want empty: the host was stale", h.errLabel)
	}
}

func TestDevicesRowsAreSelectedWellsNotRadios(t *testing.T) {
	r := &Registry{}
	h := &PanelHost{id: PanelAudio, audioTab: "devices"}
	tree := audioDevicesTree(r, h)
	got := renderAudioText(tree)
	if !strings.Contains(got, "Output device") || !strings.Contains(got, "Input device") {
		t.Error("Devices must carry both section labels even when empty")
	}
	current := audioDeviceRow(services.AudioNode{
		ID: 7, Description: "AD106M High Definition Audio Controller Digital Stereo (HDMI)", Default: true,
	})
	if current.Kind != ui.KindCapsule || current.Fill != ui.FillSoft {
		t.Fatalf("current device = kind %v fill %v, want KindCapsule FillSoft", current.Kind, current.Fill)
	}
	var check *ui.Node
	walkAudio(current, func(x *ui.Node) {
		if x.Kind == ui.KindIcon && x.Icon == "check" {
			check = x
		}
	})
	if check == nil {
		t.Fatal("current device well carries no trailing check")
	}
	plain := audioDeviceRow(services.AudioNode{ID: 8, Description: "other"})
	if plain.Fill != ui.FillNone {
		t.Errorf("sibling fill = %v, want a plain row", plain.Fill)
	}
}

func TestAudioDensityContract(t *testing.T) {
	long := "AD106M High Definition Audio Controller Digital Stereo (HDMI)"
	row := audioVolumeRow(services.AudioNode{ID: 1, Description: long, Level: 100}, "Output", nil)
	if row.Height != 68 {
		t.Errorf("volume row height = %d, want 68 so compact still holds role+name+slider", row.Height)
	}
	var value *ui.Node
	walkAudio(row, func(x *ui.Node) {
		if x.MinWidthText == "100%" {
			value = x
		}
	})
	if value == nil {
		t.Fatal("value column does not reserve 100% width")
	}
	compact, ok := theme.MetricsFor(theme.DensityCompact)
	if !ok {
		t.Fatal("compact metrics missing")
	}
	// D2: 560 minus panel pad, row pad, check, gap. Compact pad is 12, so
	// the name box is wider than the 448 px the mock sized at standard.
	remain := 560 - 2*compact.PanelPadding - 2*12 - 20 - 12
	// Body is 14 px; a 7 px/rune stand-in matches D2's 448 px / 61 chars.
	measure := func(s string, _ ui.TextAttrs) (int, int) { return len([]rune(s)) * 7, 16 }
	w, _ := measure(long, ui.TextAttrs{})
	if w > remain {
		t.Errorf("compact name box %d px, 61-char name measures %d", remain, w)
	}
	// 125% body is 18 px (~9 px/rune). Record a miss; live type decides
	// whether Devices elides, which is the only trigger to widen 560.
	remainStd := 560 - 2*16 - 2*12 - 20 - 12
	if got := len([]rune(long)) * 9; got > remainStd {
		t.Logf("fontScale 125%% stand-in: name %d px in a %d px box", got, remainStd)
	}
}

func walkAudio(n *ui.Node, fn func(*ui.Node)) {
	if n == nil {
		return
	}
	fn(n)
	for _, c := range n.Children {
		walkAudio(c, fn)
	}
}

func renderAudioText(n *ui.Node) string {
	var b strings.Builder
	walkAudio(n, func(x *ui.Node) {
		if x.Text != "" {
			if b.Len() > 0 {
				b.WriteByte(' ')
			}
			b.WriteString(x.Text)
		}
	})
	return b.String()
}
