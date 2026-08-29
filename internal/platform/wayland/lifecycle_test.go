package wayland

import (
	"errors"
	"math"
	"slices"
	"testing"

	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

// advertiseAll fills a registry with everything the proof requires, using the
// versions Niri 26.04 offers.
func advertiseAll(t *testing.T) *registryState {
	t.Helper()

	rs := newRegistryState()
	rs.addGlobal(1, "wl_compositor", 6)
	rs.addGlobal(2, "wl_shm", 2)
	rs.addGlobal(3, "wl_seat", 9)
	rs.addGlobal(4, "zwlr_layer_shell_v1", 5)
	rs.addGlobal(5, "wp_fractional_scale_manager_v1", 1)
	rs.addGlobal(6, "wp_viewporter", 1)
	rs.addGlobal(7, "wl_output", 4)
	rs.setOutputName(7, "DP-1")
	rs.addFormat(formatARGB8888)
	return rs
}

func TestRegistryReadyOnlyWithEveryRequiredGlobal(t *testing.T) {
	t.Parallel()

	if got := advertiseAll(t).missingRequired(); len(got) != 0 {
		t.Fatalf("a complete registry reported %v missing", got)
	}

	// Dropping any one requirement must name that requirement.
	drops := []struct {
		name string
		omit uint32
		want string
	}{
		{"compositor", 1, "wl_compositor"},
		{"shm", 2, "wl_shm"},
		{"seat", 3, "wl_seat"},
		{"layer shell", 4, "zwlr_layer_shell_v1"},
		{"fractional scale", 5, "wp_fractional_scale_manager_v1"},
		{"viewporter", 6, "wp_viewporter"},
		{"output", 7, "wl_output"},
	}
	for _, tc := range drops {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			rs := advertiseAll(t)
			rs.removeGlobal(tc.omit)
			if got := rs.missingRequired(); !slices.Contains(got, tc.want) {
				t.Fatalf("missing = %v, want it to name %s", got, tc.want)
			}
		})
	}
}

func TestRegistryBindsAtTheLowerVersion(t *testing.T) {
	t.Parallel()

	tests := []struct {
		iface  string
		server uint32
		want   uint32
	}{
		{"wl_compositor", 6, 6},
		{"wl_shm", 2, 1},
		{"wl_seat", 9, 7},
		{"wl_output", 4, 4},
		{"zwlr_layer_shell_v1", 5, 4},
		{"wp_fractional_scale_manager_v1", 1, 1},
		{"wp_viewporter", 1, 1},
		// A server older than our maximum caps us at the server's version.
		{"wl_compositor", 4, 4},
	}
	for _, tc := range tests {
		t.Run(tc.iface, func(t *testing.T) {
			t.Parallel()
			got, ok := bindVersion(tc.iface, tc.server)
			if !ok {
				t.Fatalf("bindVersion did not want %s", tc.iface)
			}
			if got != tc.want {
				t.Fatalf("bindVersion(%s, %d) = %d, want %d", tc.iface, tc.server, got, tc.want)
			}
		})
	}

	if _, ok := bindVersion("wl_data_device_manager", 3); ok {
		t.Error("bindVersion wanted an interface the proof does not use")
	}
}

func TestRegistryRequiresARGB8888(t *testing.T) {
	t.Parallel()

	rs := advertiseAll(t)
	if !rs.hasFormat(formatARGB8888) {
		t.Fatal("advertised ARGB8888 was not recorded")
	}

	// Niri lists ARGB8888 last, so a partial list must not satisfy the check.
	bare := newRegistryState()
	bare.addFormat(1) // XRGB8888
	if bare.hasFormat(formatARGB8888) {
		t.Fatal("ARGB8888 reported present when only XRGB8888 was advertised")
	}
}

// Output selection is gone: every connected output receives a bar, so the
// registry no longer picks one. What it still owns is the connector name, which
// is a per-global attribute used for configuration matching and Niri joining,
// never as host identity.
func TestRegistryRecordsConnectorNamesPerGlobal(t *testing.T) {
	t.Parallel()

	rs := advertiseAll(t)
	rs.addGlobal(8, "wl_output", 4)
	rs.setOutputName(7, "DP-1")
	rs.setOutputName(8, "DP-3")

	if got := rs.outputs[7].connector; got != "DP-1" {
		t.Fatalf("global 7 connector = %q, want DP-1", got)
	}
	if got := rs.outputs[8].connector; got != "DP-3" {
		t.Fatalf("global 8 connector = %q, want DP-3", got)
	}

	// A name for a global that is not an output is discarded rather than
	// creating an entry.
	rs.setOutputName(999, "DP-9")
	if _, ok := rs.outputs[999]; ok {
		t.Fatal("setOutputName created an output entry for an unknown global")
	}
}

// TestRegistryKeysOutputsByGlobalName proves a connector string is a attribute
// of a host, not its identity: a reconnect reuses the name at a new global.
func TestRegistryKeysOutputsByGlobalName(t *testing.T) {
	t.Parallel()

	rs := advertiseAll(t)
	rs.addGlobal(9, "wl_output", 4)
	rs.setOutputName(9, "DP-1")

	if len(rs.outputs) != 2 {
		t.Fatalf("registry holds %d outputs, want 2 distinct hosts sharing a connector", len(rs.outputs))
	}
	if rs.outputs[7] == rs.outputs[9] {
		t.Fatal("two globals collapsed onto one host")
	}
}

func TestRegistryRemoveReportsTheHostToDestroy(t *testing.T) {
	t.Parallel()

	rs := advertiseAll(t)
	gone, ok := rs.removeGlobal(7)
	if !ok {
		t.Fatal("removing a known output did not report a host to destroy")
	}
	if gone.connector != "DP-1" {
		t.Fatalf("removed host %+v, want the DP-1 output", gone)
	}
	if _, ok := rs.removeGlobal(7); ok {
		t.Fatal("removing the same global twice reported a host twice")
	}
	if _, ok := rs.removeGlobal(1); ok {
		t.Fatal("removing a singleton reported an output host")
	}
}

func TestConfigureAcknowledgementPrecedesBufferEligibility(t *testing.T) {
	t.Parallel()

	s := newSurfaceState()
	if s.eligible() {
		t.Fatal("a fresh surface was eligible for a buffer")
	}

	s.configure(3396, 48)
	if s.eligible() {
		t.Fatal("surface became eligible before the configure was acknowledged")
	}

	s.acknowledge()
	if !s.eligible() {
		t.Fatal("surface stayed ineligible after acknowledging its configure")
	}
}

func TestConfigureDefaultsToUnitScale(t *testing.T) {
	t.Parallel()

	if got := newSurfaceState().scale120; got != ui.ScaleUnit {
		t.Fatalf("default scale120 = %d, want %d", got, ui.ScaleUnit)
	}
}

func TestConfigureBufferSizeUsesFractionalScale(t *testing.T) {
	t.Parallel()

	s := newSurfaceState()
	// 3396 is the observed configure width on a 3440 output whose remaining
	// 44 logical pixels are held by another shell's exclusive zone.
	s.configure(3396, 48)
	s.acknowledge()

	w, h, err := s.bufferSize()
	if err != nil {
		t.Fatal(err)
	}
	if w != 3396 || h != 48 {
		t.Fatalf("buffer at unit scale = %dx%d, want 3396x48", w, h)
	}

	s.preferredScale(180)
	w, h, err = s.bufferSize()
	if err != nil {
		t.Fatal(err)
	}
	if w != 5094 || h != 72 {
		t.Fatalf("buffer at scale 1.5 = %dx%d, want 5094x72", w, h)
	}
}

// TestConfigureScaleOnlyEventReconfigures covers a preferred_scale arriving
// alone, with no configure following, when the output scale changes at an
// unchanged logical size.
func TestConfigureScaleOnlyEventReconfigures(t *testing.T) {
	t.Parallel()

	s := newSurfaceState()
	s.configure(3396, 48)
	s.acknowledge()

	if !s.preferredScale(180) {
		t.Fatal("a new preferred scale did not request a reconfigure")
	}
	if s.preferredScale(180) {
		t.Fatal("an unchanged preferred scale requested a reconfigure")
	}
	if !s.eligible() {
		t.Fatal("a scale-only change invalidated the acknowledged configure")
	}
}

func TestConfigureRejectsUnusableSizes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		w, h  int
		scale ui.Scale120
	}{
		{"zero width", 0, 48, ui.ScaleUnit},
		{"zero height", 3396, 0, ui.ScaleUnit},
		{"negative width", -1, 48, ui.ScaleUnit},
		{"overflows int32", math.MaxInt32, 48, 180},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			s := newSurfaceState()
			s.configure(tc.w, tc.h)
			s.acknowledge()
			s.preferredScale(tc.scale)
			if _, _, err := s.bufferSize(); err == nil {
				t.Fatal("bufferSize accepted an unusable size")
			}
		})
	}
}

func TestLifecycleCleanupRunsChildToParent(t *testing.T) {
	t.Parallel()

	var stack cleanupStack
	// Pushed in creation order, parent first.
	for _, name := range []string{
		"display", "globals", "output", "surface", "layer-surface",
		"fractional-scale", "viewport", "mapping", "pool", "buffer",
	} {
		stack.push(name, func() error { return nil })
	}

	order, err := stack.unwind()
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"buffer", "pool", "mapping", "viewport", "fractional-scale",
		"layer-surface", "surface", "output", "globals", "display",
	}
	if !slices.Equal(order, want) {
		t.Fatalf("cleanup order =\n%v\nwant\n%v", order, want)
	}
}

func TestLifecycleCleanupContinuesAfterAFailure(t *testing.T) {
	t.Parallel()

	var stack cleanupStack
	stack.push("display", func() error { return nil })
	stack.push("pool", func() error { return errTest })
	stack.push("buffer", func() error { return nil })

	order, err := stack.unwind()
	if err == nil {
		t.Fatal("unwind hid a destructor failure")
	}
	if !slices.Equal(order, []string{"buffer", "pool", "display"}) {
		t.Fatalf("a failure stopped the unwind: %v", order)
	}
}

func TestLifecycleShutdownReportsCleanupFailure(t *testing.T) {
	t.Parallel()

	o := &owner{}
	o.cleanup.push("failing", func() error { return errTest })
	if err := o.shutdown(); !errors.Is(err, errTest) {
		t.Fatalf("shutdown error = %v, want %v", err, errTest)
	}
}

func TestLifecycleGenerationRetiresAfterEveryRelease(t *testing.T) {
	t.Parallel()

	var r retirement
	if !r.freeable() {
		t.Fatal("a generation with no attached buffers was not freeable")
	}

	r.attached()
	r.attached()
	if r.freeable() {
		t.Fatal("a generation was freeable while two buffers were held")
	}
	if err := r.released(); err != nil {
		t.Fatal(err)
	}
	if r.freeable() {
		t.Fatal("a generation was freeable while one buffer was still held")
	}
	if err := r.released(); err != nil {
		t.Fatal(err)
	}
	if !r.freeable() {
		t.Fatal("a generation stayed held after every buffer was released")
	}
	if err := r.released(); err == nil {
		t.Fatal("an unmatched release was accepted")
	}
}

func TestLifecycleGenerationRetiresOnSurfaceDestroy(t *testing.T) {
	t.Parallel()

	var r retirement
	r.attached()
	r.destroy()
	if !r.freeable() {
		t.Fatal("a destroyed surface did not release its generation")
	}
}

// errTest marks a destructor failure in cleanup tests.
var errTest = errors.New("destructor failed")
