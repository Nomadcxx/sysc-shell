package shell

import (
	"slices"
	"testing"

	"github.com/Nomadcxx/sysc-shell/internal/services/polkit"
)

func TestPolkitStatusLabel(t *testing.T) {
	for _, tc := range []struct {
		name string
		in   polkit.Status
		want string
	}{
		{"off", polkit.Status{Policy: polkit.PolicyOff, Reason: "disabled"}, "Off"},
		{"registered", polkit.Status{Policy: polkit.PolicyAuto, Registered: true}, "Registered"},
		{"passive", polkit.Status{Policy: polkit.PolicyAuto, Passive: "polkit-gnome-authentication-agent-1"}, "Passive · polkit-gnome-authentication-agent-1"},
		{"unavailable", polkit.Status{Policy: polkit.PolicyAuto, Reason: "system bus unavailable"}, "Unavailable"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := polkitStatusLabel(tc.in); got != tc.want {
				t.Fatalf("polkitStatusLabel(%+v) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestPolkitPromptFocusOrder(t *testing.T) {
	for _, tc := range []struct {
		phase      polkitPromptPhase
		hasDetails bool
		want       []string
	}{
		{polkitChoosingIdentity, true, []string{"identity", "continue", "cancel", "details"}},
		{polkitEnteringResponse, true, []string{"password", "submit", "cancel", "details"}},
		{polkitShowingInfo, false, []string{"continue", "cancel"}},
		{polkitAwaitingPrompt, false, []string{"cancel"}},
	} {
		if got := polkitFocusOrder(tc.phase, tc.hasDetails); !slices.Equal(got, tc.want) {
			t.Fatalf("polkitFocusOrder(%d, %t) = %v, want %v", tc.phase, tc.hasDetails, got, tc.want)
		}
	}
}

func TestPolkitEchoOnResponseStaysVisible(t *testing.T) {
	if polkitPromptMasked(polkit.Prompt{Secret: true, Echo: true}) {
		t.Fatal("PAM echo-on response is masked")
	}
	if !polkitPromptMasked(polkit.Prompt{Secret: true}) {
		t.Fatal("PAM echo-off response is visible")
	}
}

func TestPolkitOutputPrefersFocusAndFallsBackToLowestGlobal(t *testing.T) {
	r := &Registry{
		focused: "DP-2",
		bars: map[uint32]*Bar{
			10: {conn: "DP-2"},
			4:  {conn: "DP-1"},
		},
	}
	if global, connector, ok := r.polkitOutputLocked(); !ok || global != 10 || connector != "DP-2" {
		t.Fatalf("focused output = %d, %q, %t; want 10, DP-2, true", global, connector, ok)
	}
	r.focused = "missing"
	if global, connector, ok := r.polkitOutputLocked(); !ok || global != 4 || connector != "DP-1" {
		t.Fatalf("fallback output = %d, %q, %t; want 4, DP-1, true", global, connector, ok)
	}
}
