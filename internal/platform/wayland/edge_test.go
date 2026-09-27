package wayland

import (
	"testing"

	"github.com/Nomadcxx/sysc-shell/internal/config"
	"github.com/Nomadcxx/sysc-shell/internal/platform/wayland/layershell"
)

func TestBarAnchorPerEdge(t *testing.T) {
	t.Parallel()
	const (
		top    = uint32(layershell.ZwlrLayerSurfaceV1AnchorTop)
		bottom = uint32(layershell.ZwlrLayerSurfaceV1AnchorBottom)
		left   = uint32(layershell.ZwlrLayerSurfaceV1AnchorLeft)
		right  = uint32(layershell.ZwlrLayerSurfaceV1AnchorRight)
	)
	for _, tc := range []struct {
		edge string
		want uint32
	}{
		{"top", top | left | right},
		{"bottom", bottom | left | right},
		{"left", left | top | bottom},
		{"right", right | top | bottom},
	} {
		if got := barAnchor(tc.edge); got != tc.want {
			t.Errorf("barAnchor(%q) = %#b, want %#b", tc.edge, got, tc.want)
		}
	}
}

// An unrecognised edge must still anchor. config rejects unknown edges before
// they reach here, so this is a belt on a surface that would otherwise be
// invisible rather than a supported input.
func TestBarAnchorFallsBackToTop(t *testing.T) {
	t.Parallel()
	if got, want := barAnchor("sideways"), barAnchor("top"); got != want {
		t.Fatalf("barAnchor(unknown) = %#b, want the top anchor %#b", got, want)
	}
	if barAnchor("") == 0 {
		t.Fatal("an empty edge must not produce a zero anchor: the surface would not be placed")
	}
}

func TestBarSizePutsTheZeroOnTheAnchoredAxis(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		edge   string
		w, h   uint32
		reason string
	}{
		{"top", 0, 44, "a horizontal bar spans the width the compositor gives it"},
		{"bottom", 0, 44, "a horizontal bar spans the width the compositor gives it"},
		{"left", 44, 0, "a vertical bar spans the height the compositor gives it"},
		{"right", 44, 0, "a vertical bar spans the height the compositor gives it"},
	} {
		w, h := barSize(tc.edge, 44)
		if w != tc.w || h != tc.h {
			t.Errorf("barSize(%q, 44) = (%d, %d), want (%d, %d): %s",
				tc.edge, w, h, tc.w, tc.h, tc.reason)
		}
	}
}

// The host's extent and its exclusive zone are separate quantities as of
// sysc-321. They agree by default and must stop agreeing on request.
func TestHostZoneFollowsTheExtentByDefault(t *testing.T) {
	t.Parallel()
	h := newHost(11, nil)
	h.policy = config.Default().Bar
	if got, want := h.policy.ExclusiveZone(), h.policy.Extent(); got != want {
		t.Fatalf("zone = %d, want the extent %d", got, want)
	}
	// The default bar is attached: its surface also holds the overhang, which
	// the zone does not reserve.
	if got := h.surfaceHeight(); got != h.policy.Extent()+h.policy.Overhang() {
		t.Fatalf("surface = %d, want the extent plus the overhang", got)
	}
}

func TestHostZoneOfZeroLeavesTheExtentAlone(t *testing.T) {
	t.Parallel()
	h := newHost(12, nil)
	h.policy = config.Default().Bar
	zero := 0
	h.policy.Reserve = &zero

	if got := h.policy.ExclusiveZone(); got != 0 {
		t.Fatalf("zone = %d, want 0 so windows may tile under the bar", got)
	}
	if got := h.surfaceHeight(); got != 52 {
		t.Fatalf("extent = %d, want 52: un-reserving must not shrink the surface", got)
	}
}

func TestHostOnTheLowerEdgeComposesItsAnchor(t *testing.T) {
	t.Parallel()
	h := newHost(13, nil)
	h.policy = config.Default().Bar
	h.policy.Edge = "bottom"

	const bottom = uint32(layershell.ZwlrLayerSurfaceV1AnchorBottom)
	if got := barAnchor(h.policy.Edge); got&bottom == 0 {
		t.Fatalf("anchor = %#b, want the lower edge set", got)
	}
	if got := barAnchor(h.policy.Edge); got&uint32(layershell.ZwlrLayerSurfaceV1AnchorTop) != 0 {
		t.Fatalf("anchor = %#b, must not also claim the upper edge", got)
	}
	w, hgt := barSize(h.policy.Edge, h.surfaceHeight())
	if w != 0 || hgt != 52 {
		t.Fatalf("size = (%d, %d), want (0, 52)", w, hgt)
	}
}
