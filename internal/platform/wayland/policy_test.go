package wayland

import (
	"errors"
	"testing"

	"github.com/Nomadcxx/sysc-shell/internal/config"
)

func TestDisabledHostDoesNotBuildABar(t *testing.T) {
	t.Parallel()

	cfg := config.Default()
	cfg.Outputs = []config.OutputOverride{{
		Connector: "DP-1",
		Bar: func() config.Bar {
			bar := cfg.Bar
			bar.Enabled = false
			return bar
		}(),
	}}
	built := false
	o := owner{
		cfg: &cfg,
		cb: Callbacks{NewHost: func(string) (HostCallbacks, error) {
			built = true
			return HostCallbacks{}, errors.New("disabled host requested callbacks")
		}},
	}
	h := newHost(7, nil)
	h.connector = "DP-1"
	h.doneSeen = true
	h.state = hostReady

	if err := o.hostBecameReady(h); err != nil {
		t.Fatalf("hostBecameReady: %v", err)
	}
	if built {
		t.Fatal("disabled host built a bar")
	}
	if h.state != hostIdle {
		t.Fatalf("state = %v, want hostIdle", h.state)
	}
}

func TestHostGeometryUsesItsOwnPolicy(t *testing.T) {
	t.Parallel()

	first := newHost(7, nil)
	first.policy = config.Default().Bar
	second := newHost(8, nil)
	second.policy = config.Default().Bar
	second.policy.Height = 56
	second.policy.Gap = 6

	if got := first.surfaceHeight(); got != 44 {
		t.Fatalf("first surface height = %d, want 44", got)
	}
	if got := second.surfaceHeight(); got != 50 {
		t.Fatalf("second surface height = %d, want 50", got)
	}
}
