package wayland

import "testing"

func TestHostReadyRequiresDoneAndName(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name      string
		done      bool
		connector string
		want      bool
	}{
		{"neither", false, "", false},
		{"done only", true, "", false},
		{"name only", false, "DP-1", false},
		{"both", true, "DP-1", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h := &OutputHost{global: 7, doneSeen: c.done, connector: c.connector}
			if got := h.ready(); got != c.want {
				t.Fatalf("ready() = %v, want %v", got, c.want)
			}
		})
	}
}

func TestHostBufferSizeRoundsHalfAwayFromZero(t *testing.T) {
	t.Parallel()
	h := newHost(0, nil)
	h.state = hostMapped
	h.bar.ss.configure(1707, 44)
	h.bar.ss.acknowledge()
	h.bar.ss.preferredScale(180) // 1.5

	w, hh, err := h.bar.bufferSize()
	if err != nil {
		t.Fatalf("bufferSize: %v", err)
	}
	// (1707*180 + 60)/120 = 2561, (44*180 + 60)/120 = 66
	if w != 2561 || hh != 66 {
		t.Fatalf("bufferSize() = %dx%d, want 2561x66", w, hh)
	}
}
