package timer

import (
	"testing"

	v1 "github.com/Nomadcxx/sysc-shell/plugin/v1"
)

func TestBarTreeActivatesTheTimerPanelFromItsReadout(t *testing.T) {
	t.Parallel()
	root := BarTree("04:12")
	if err := v1.Validate(root, v1.ViewBar); err != nil {
		t.Fatal(err)
	}
	if root.Kind != v1.KindButton || root.ID != "open" || root.Text != "04:12" || !root.Tabular || root.Name != "Open timer" || root.Role != "button" ||
		len(root.Events) != 1 || root.Events[0] != v1.EventActivate {
		t.Fatalf("bar readout = %+v, want one accessible activation button", root)
	}
}

func TestPanelTreeSeparatesTimerCardAndControls(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		running bool
		id      string
		label   string
	}{
		{running: false, id: "start", label: "Start"},
		{running: true, id: "pause", label: "Pause"},
	} {
		t.Run(tc.label, func(t *testing.T) {
			root := PanelTree("05:00", "5m", tc.running)
			if err := v1.Validate(root, v1.ViewPanel); err != nil {
				t.Fatal(err)
			}
			if root.Kind != v1.KindColumn || root.Padding != 16 || root.Gap != 12 || len(root.Children) != 2 {
				t.Fatalf("panel root = %+v, want padded column with card and footer", root)
			}

			card := root.Children[0]
			if card.Kind != v1.KindRow || card.Padding < 6 || len(card.Children) != 2 {
				t.Fatalf("timer card = %+v, want padded row with readout and duration", card)
			}
			readout, duration := card.Children[0], card.Children[1]
			if readout.Kind != v1.KindText || readout.Text != "05:00" || !readout.Tabular {
				t.Errorf("card readout = %+v, want tabular remaining time", readout)
			}
			if duration.Kind != v1.KindTextInput || duration.ID != "duration" || duration.Text != "5m" {
				t.Errorf("duration field = %+v", duration)
			}

			footer := root.Children[1]
			if footer.Kind != v1.KindRow || len(footer.Children) != 2 {
				t.Fatalf("timer footer = %+v, want Reset and %s", footer, tc.label)
			}
			reset, toggle := footer.Children[0], footer.Children[1]
			if reset.Kind != v1.KindButton || reset.ID != "reset" || reset.Tone != v1.ToneError {
				t.Errorf("reset = %+v, want destructive button", reset)
			}
			if toggle.Kind != v1.KindButton || toggle.ID != tc.id || toggle.Text != tc.label || toggle.Tone == v1.ToneError {
				t.Errorf("toggle = %+v, want normal %s button", toggle, tc.label)
			}
		})
	}
}
