package wayland

import "testing"

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
