package wayland

import (
	"testing"

	"github.com/Nomadcxx/sysc-shell/internal/render"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

// Scale isolation is a consequence of wp_fractional_scale_v1 being created per
// surface: the event arrives on the host's own object and its handler closure
// holds that host. These tests prove no scale state leaked back onto owner.
func TestScaleChangeTouchesOnlyItsOwnHost(t *testing.T) {
	t.Parallel()
	a, b := newHost(1, nil), newHost(2, nil)
	for _, h := range []*OutputHost{a, b} {
		h.bar.ss.configure(1000, 44)
		h.bar.ss.acknowledge()
		h.bar.sched.Configure(1000, 44)
		_ = h.bar.sched.Submitted(0)
	}
	beforeGen := b.bar.genID
	beforeDecision, _ := b.bar.sched.Next()

	if !a.bar.ss.preferredScale(180) {
		t.Fatal("preferredScale reported no change")
	}

	aw, _, err := a.bar.bufferSize()
	if err != nil {
		t.Fatalf("bufferSize: %v", err)
	}
	if aw != 1500 {
		t.Fatalf("host A buffer width = %d, want 1500", aw)
	}

	bw, _, err := b.bar.bufferSize()
	if err != nil {
		t.Fatalf("bufferSize: %v", err)
	}
	if bw != 1000 {
		t.Fatalf("host B buffer width = %d, want 1000 unchanged", bw)
	}
	if b.bar.genID != beforeGen {
		t.Fatalf("host B generation moved from %d to %d", beforeGen, b.bar.genID)
	}
	if d, _ := b.bar.sched.Next(); d != beforeDecision {
		t.Fatal("host B scheduler state changed")
	}
}

func TestScaleOnlyEventIsIgnoredWhenUnchanged(t *testing.T) {
	t.Parallel()
	h := newHost(1, nil)
	h.bar.ss.configure(800, 44)
	h.bar.ss.acknowledge()

	// The default is 120, and preferred_scale is not ordered against configure,
	// so a redundant event must not retire a generation.
	if h.bar.ss.preferredScale(120) {
		t.Fatal("preferredScale(120) reported a change from the default 120")
	}
}

func TestMixedScalesProduceIndependentBufferSizes(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		logical int
		scale   int
		want    int32
	}{
		{"scale 1", 3440, 120, 3440},
		{"scale 1.25", 1000, 150, 1250},
		{"scale 1.5 rounds up", 1707, 180, 2561},
		{"scale 2", 800, 240, 1600},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			h := newHost(1, nil)
			h.bar.ss.configure(c.logical, 44)
			h.bar.ss.acknowledge()
			h.bar.ss.preferredScale(ui.Scale120(c.scale))

			got, _, err := h.bar.bufferSize()
			if err != nil {
				t.Fatalf("bufferSize: %v", err)
			}
			if got != c.want {
				t.Fatalf("buffer width = %d, want %d", got, c.want)
			}
		})
	}
}

func TestSchedulerIsPerHost(t *testing.T) {
	t.Parallel()
	a, b := newHost(1, nil), newHost(2, nil)
	a.bar.sched.Configure(10, 10)

	// b never configured, so it has no generation and offers no work.
	if d, _ := b.bar.sched.Next(); d == render.DecisionRender {
		t.Fatal("an unconfigured host offered a render job")
	}
	if d, _ := a.bar.sched.Next(); d != render.DecisionRender {
		t.Fatal("a configured host offered no render job")
	}
}
