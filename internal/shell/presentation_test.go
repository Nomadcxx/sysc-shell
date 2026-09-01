package shell

import (
	"testing"

	"github.com/Nomadcxx/sysc-notify/protocol"
	"github.com/Nomadcxx/sysc-shell/internal/config"
)

// presentationCase drives one aggregate-state scenario across outputs.
func TestPresentationPrecedence(t *testing.T) {
	for _, tc := range []struct {
		name   string
		hover  []string // outputs hovering the record
		shown  []string // outputs where the card is visible
		queue  []string // outputs where it is queued
		dnd    bool
		center bool
		want   protocol.PresentationState
	}{
		{name: "hover beats everything", hover: []string{"eDP-1"}, shown: []string{"eDP-1"}, queue: []string{"HDMI-A-1"}, want: protocol.PresentationHovered},
		{name: "one visible copy makes it visible", shown: []string{"eDP-1"}, queue: []string{"HDMI-A-1"}, want: protocol.PresentationVisible},
		{name: "queued needs every output", queue: []string{"eDP-1"}, want: protocol.PresentationVisible},
		{name: "queued on all outputs", queue: []string{"eDP-1", "HDMI-A-1"}, want: protocol.PresentationQueued},
		{name: "dnd suppresses", dnd: true, queue: []string{"eDP-1", "HDMI-A-1"}, want: protocol.PresentationSuppressed},
		{name: "open center suppresses", center: true, shown: []string{"eDP-1"}, want: protocol.PresentationSuppressed},
		{name: "nowhere to show it suppresses", want: protocol.PresentationSuppressed},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := NewRegistry(config.Default())
			r.outputsForTest([]string{"eDP-1", "HDMI-A-1"})
			if tc.dnd {
				r.setDNDForTest(true)
			}
			if tc.center {
				r.setCenterOpenForTest(true)
			}
			got := r.aggregatePresentation(1, presentationView{
				hovered: tc.hover,
				visible: tc.shown,
				queued:  tc.queue,
			})
			if got != tc.want {
				t.Fatalf("aggregate = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestPresentationZeroOutputsSuppresses(t *testing.T) {
	r := NewRegistry(config.Default())
	r.outputsForTest(nil)
	got := r.aggregatePresentation(1, presentationView{visible: []string{"eDP-1"}})
	if got != protocol.PresentationSuppressed {
		t.Fatalf("zero outputs: aggregate = %q, want suppressed", got)
	}
}
