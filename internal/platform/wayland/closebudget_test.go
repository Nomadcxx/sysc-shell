package wayland

import (
	"testing"
	"time"
)

func TestCloseRetryBudgetIsBounded(t *testing.T) {
	t.Parallel()
	h := newHost(1, nil)
	base := time.Unix(1000, 0)

	for i := range closeRetryLimit {
		at := base.Add(time.Duration(i) * 6 * time.Second)
		if !h.mayRecreate(at) {
			t.Fatalf("attempt %d was refused inside the budget", i+1)
		}
		h.recordCloseAttempt()
	}
	if h.mayRecreate(base.Add(30 * time.Second)) {
		t.Fatalf("attempt %d was permitted beyond the budget", closeRetryLimit+1)
	}
}

func TestLayerClosedDecisionAllowsImmediateBoundedSequence(t *testing.T) {
	t.Parallel()
	h := newHost(1, nil)
	base := time.Unix(1000, 0)

	for attempt := 1; attempt <= closeRetryLimit; attempt++ {
		if !h.mayRecreate(base) {
			t.Fatalf("immediate attempt %d was refused", attempt)
		}
		h.recordCloseAttempt()
	}
	if h.mayRecreate(base) {
		t.Fatalf("attempt %d was permitted beyond the budget", closeRetryLimit+1)
	}
}

func TestSustainedMappingResetsTheBudget(t *testing.T) {
	t.Parallel()
	h := newHost(1, nil)
	base := time.Unix(1000, 0)

	for range closeRetryLimit {
		h.recordCloseAttempt()
	}
	// A bar that stayed up long enough earns a fresh budget, so a transient
	// compositor reset does not permanently exhaust recovery.
	h.mappedSince = base.Add(time.Minute)
	if !h.mayRecreate(base.Add(time.Minute + closeRetryResetAfter)) {
		t.Fatal("the budget did not reset after a sustained mapping")
	}
}

func TestRecordingAnAttemptClearsTheMappedClock(t *testing.T) {
	t.Parallel()
	h := newHost(1, nil)
	h.mappedSince = time.Unix(1000, 0)

	h.recordCloseAttempt()
	if !h.mappedSince.IsZero() {
		t.Fatal("recordCloseAttempt left a stale mapped clock, which would grant a free reset")
	}
}

func TestUnwindToKeepsTheOutputStep(t *testing.T) {
	t.Parallel()
	var order []string
	h := newHost(1, nil)
	for _, name := range []string{"output", "surface", "layer-surface", "viewport"} {
		h.cleanup.push(name, func() error { order = append(order, name); return nil })
	}

	if _, err := h.cleanup.unwindTo("output"); err != nil {
		t.Fatalf("unwindTo: %v", err)
	}
	want := []string{"viewport", "layer-surface", "surface"}
	if len(order) != len(want) {
		t.Fatalf("order = %v, want %v", order, want)
	}
	for i := range want {
		if order[i] != want[i] {
			t.Fatalf("order = %v, want %v", order, want)
		}
	}
	if len(h.cleanup.steps) != 1 || h.cleanup.steps[0].name != "output" {
		t.Fatal("the output step must survive so the host keeps its wl_output")
	}
}

func TestUnwindRunsEveryStepChildToParent(t *testing.T) {
	t.Parallel()
	var order []string
	h := newHost(1, nil)
	for _, name := range []string{"output", "surface", "layer-surface", "viewport"} {
		h.cleanup.push(name, func() error { order = append(order, name); return nil })
	}

	if _, err := h.cleanup.unwind(); err != nil {
		t.Fatalf("unwind: %v", err)
	}
	want := []string{"viewport", "layer-surface", "surface", "output"}
	for i := range want {
		if order[i] != want[i] {
			t.Fatalf("order = %v, want %v", order, want)
		}
	}
}
