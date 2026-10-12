package wayland

import (
	"testing"

	"github.com/Nomadcxx/sysc-wayland/client"
)

func TestApplyDoneReportsTheReadyTransitionOnce(t *testing.T) {
	t.Parallel()
	h := newHost(4, nil)

	h.applyName("DP-3")
	h.applyGeometry(2)
	h.applyMode(2560, 1440)

	if !h.applyDone() {
		t.Fatal("first done with a name did not report the ready transition")
	}
	if h.state != hostReady {
		t.Fatalf("state = %d, want hostReady", h.state)
	}
	if h.applyDone() {
		t.Fatal("second done reported the ready transition again")
	}
	if h.transform != 2 || h.modeWidth != 2560 || h.modeHeight != 1440 {
		t.Fatalf("metadata = transform %d mode %dx%d", h.transform, h.modeWidth, h.modeHeight)
	}
}

func TestApplyDoneWithoutNameLeavesHostUnready(t *testing.T) {
	t.Parallel()
	h := newHost(4, nil)
	h.applyMode(1920, 1080)

	if h.applyDone() {
		t.Fatal("done without a name reported ready")
	}
	if h.state != hostBound {
		t.Fatalf("state = %d, want hostBound", h.state)
	}
	// The name arriving later completes readiness on the next done.
	h.applyName("HDMI-A-1")
	if !h.applyDone() {
		t.Fatal("done after a late name did not report ready")
	}
}

func TestOutputDoneReportsModeChangesToConfiguredAuxSurfaces(t *testing.T) {
	t.Parallel()
	h := newHost(4, nil)
	h.state = hostMapped
	u := newSurfaceUnit("toast:DP-1")
	u.ss.configure(404, 800)
	u.ss.acknowledge()
	var got [][2]int
	u.app.OutputSize = func(width, height int) {
		got = append(got, [2]int{width, height})
	}
	h.aux[u.id] = u
	o := &owner{}

	h.applyMode(1920, 1080)
	o.onOutputDone(h)
	h.applyMode(2560, 1080)
	o.onOutputDone(h)

	want := [][2]int{{1920, 1080}, {2560, 1080}}
	if len(got) != len(want) {
		t.Fatalf("reported output sizes = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("reported output sizes = %v, want %v", got, want)
		}
	}
}

func TestTransformedOutputUsesSwappedConfigureDimensions(t *testing.T) {
	t.Parallel()
	h := newHost(1, nil)
	h.applyMode(2560, 1440)
	h.applyGeometry(1) // 90 degrees
	h.applyName("DP-3")
	h.applyDone()

	// A rotated output reports a configure whose width is the mode height. The
	// surface size comes from the configure, never from the mode.
	h.bar.ss.configure(1440, 44)
	h.bar.ss.acknowledge()
	w, hh, err := h.bar.bufferSize()
	if err != nil {
		t.Fatalf("bufferSize: %v", err)
	}
	if w != 1440 || hh != 44 {
		t.Fatalf("buffer = %dx%d, want 1440x44 from the configure", w, hh)
	}
	if h.modeWidth != 2560 {
		t.Fatal("the mode was overwritten by the configure")
	}
}

func TestLogicalSizeAppliesOutputTransformBeforeScale(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		transform client.OutputTransform
		want      [2]int
	}{
		{name: "normal", transform: client.OutputTransformNormal, want: [2]int{2048, 1152}},
		{name: "90", transform: client.OutputTransform90, want: [2]int{1152, 2048}},
		{name: "180", transform: client.OutputTransform180, want: [2]int{2048, 1152}},
		{name: "270", transform: client.OutputTransform270, want: [2]int{1152, 2048}},
		{name: "flipped", transform: client.OutputTransformFlipped, want: [2]int{2048, 1152}},
		{name: "flipped 90", transform: client.OutputTransformFlipped90, want: [2]int{1152, 2048}},
		{name: "flipped 180", transform: client.OutputTransformFlipped180, want: [2]int{2048, 1152}},
		{name: "flipped 270", transform: client.OutputTransformFlipped270, want: [2]int{1152, 2048}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newHost(1, nil)
			h.applyMode(2560, 1440)
			h.applyGeometry(int32(tt.transform))

			w, height := h.logicalSize(150)
			if got := [2]int{w, height}; got != tt.want {
				t.Fatalf("logical size = %v, want %v", got, tt.want)
			}
		})
	}
}
