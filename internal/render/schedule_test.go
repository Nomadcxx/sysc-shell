package render

import "testing"

// submitFirstFrame drives a new scheduler through its first render: one
// invalidation, one render decision, and the matching submission.
func submitFirstFrame(t *testing.T, s *Scheduler) Job {
	t.Helper()

	s.Configure(240, 48)
	d, job := s.Next()
	if d != DecisionRender {
		t.Fatalf("first decision = %v, want DecisionRender", d)
	}
	if job.Slot != 0 {
		t.Fatalf("first job used slot %d, want slot 0", job.Slot)
	}
	if job.Width != 240 || job.Height != 48 {
		t.Fatalf("first job = %dx%d, want 240x48", job.Width, job.Height)
	}
	if err := s.Submitted(job.Slot); err != nil {
		t.Fatal(err)
	}
	return job
}

func TestSchedulerIdleProducesNoWork(t *testing.T) {
	t.Parallel()

	s := NewScheduler()
	if d, _ := s.Next(); d != DecisionWait {
		t.Fatalf("idle decision = %v, want DecisionWait", d)
	}

	s.Configure(240, 48)
	d, job := s.Next()
	if d != DecisionRender {
		t.Fatal("configure did not make a redraw ready")
	}
	if err := s.Submitted(job.Slot); err != nil {
		t.Fatal(err)
	}
	if d, _ := s.Next(); d != DecisionWait {
		t.Fatalf("decision after submission = %v, want DecisionWait", d)
	}
}

func TestSchedulerCoalescesRedrawsWhileFramePending(t *testing.T) {
	t.Parallel()

	s := NewScheduler()
	submitFirstFrame(t, s)

	s.Invalidate()
	s.Invalidate()
	if d, _ := s.Next(); d != DecisionWait {
		t.Fatal("scheduler submitted a second frame while one was pending")
	}
}

// TestSchedulerFrameDoneDoesNotFreeSlot covers the observed Niri 26.04
// ordering: wl_callback.done arrives while the submitted buffer is still held.
func TestSchedulerFrameDoneDoesNotFreeSlot(t *testing.T) {
	t.Parallel()

	s := NewScheduler()
	submitFirstFrame(t, s)
	s.Invalidate()

	if err := s.Frame(); err != nil {
		t.Fatal(err)
	}
	d, job := s.Next()
	if d != DecisionRender {
		t.Fatalf("decision after frame done = %v, want DecisionRender", d)
	}
	if job.Slot == 0 {
		t.Fatal("scheduler offered the slot the compositor still holds")
	}
}

func TestSchedulerSingleRedrawFrameThenRelease(t *testing.T) {
	t.Parallel()

	s := NewScheduler()
	submitFirstFrame(t, s)
	s.Invalidate()
	s.Invalidate()

	if err := s.Frame(); err != nil {
		t.Fatal(err)
	}
	if err := s.Release(0); err != nil {
		t.Fatal(err)
	}

	assertExactlyOneRedraw(t, s)
}

func TestSchedulerSingleRedrawReleaseThenFrame(t *testing.T) {
	t.Parallel()

	s := NewScheduler()
	submitFirstFrame(t, s)
	s.Invalidate()
	s.Invalidate()

	if err := s.Release(0); err != nil {
		t.Fatal(err)
	}
	if d, _ := s.Next(); d != DecisionWait {
		t.Fatal("release alone unblocked a redraw while a frame was pending")
	}
	if err := s.Frame(); err != nil {
		t.Fatal(err)
	}

	assertExactlyOneRedraw(t, s)
}

// assertExactlyOneRedraw consumes one render decision and proves the coalesced
// invalidations produce no second one.
func assertExactlyOneRedraw(t *testing.T, s *Scheduler) {
	t.Helper()

	d, job := s.Next()
	if d != DecisionRender {
		t.Fatalf("decision = %v, want one DecisionRender", d)
	}
	if err := s.Submitted(job.Slot); err != nil {
		t.Fatal(err)
	}
	if d, _ := s.Next(); d != DecisionWait {
		t.Fatal("a second redraw followed the coalesced invalidations")
	}
}

func TestSchedulerConfigureDiscardsPendingRender(t *testing.T) {
	t.Parallel()

	s := NewScheduler()
	submitFirstFrame(t, s)
	s.Invalidate()

	s.Configure(360, 48)

	d, job := s.Next()
	if d != DecisionRender {
		t.Fatalf("decision after configure = %v, want DecisionRender", d)
	}
	if job.Width != 360 || job.Height != 48 {
		t.Fatalf("job = %dx%d, want the new 360x48", job.Width, job.Height)
	}
	if job.Generation == 0 {
		t.Fatal("configure did not start a new buffer generation")
	}
}

func TestSchedulerCloseStopsAllWork(t *testing.T) {
	t.Parallel()

	s := NewScheduler()
	submitFirstFrame(t, s)
	s.Invalidate()

	s.Close()

	if d, _ := s.Next(); d != DecisionWait {
		t.Fatal("closed scheduler offered work")
	}
	s.Invalidate()
	s.Configure(360, 48)
	if d, _ := s.Next(); d != DecisionWait {
		t.Fatal("closed scheduler offered work after later events")
	}
}

func TestSchedulerRejectsUnexpectedEvents(t *testing.T) {
	t.Parallel()

	s := NewScheduler()
	if err := s.Frame(); err == nil {
		t.Error("Frame accepted a callback with none pending")
	}
	if err := s.Release(0); err == nil {
		t.Error("Release accepted a slot that is not busy")
	}
	if err := s.Release(2); err == nil {
		t.Error("Release accepted an out-of-range slot")
	}
	if err := s.Submitted(2); err == nil {
		t.Error("Submitted accepted an out-of-range slot")
	}

	job := submitFirstFrame(t, s)
	if err := s.Submitted(job.Slot); err == nil {
		t.Error("Submitted accepted a slot that is already busy")
	}
}

// TestSchedulerConfigureFreesEverySlot reproduces a stall observed on Niri:
// after two reconfigures, both slots were still marked busy and the surface
// never redrew again.
//
// Slot busy-ness is scoped to one buffer generation. A configure allocates a
// new generation with two fresh buffers, so both slots are free even while the
// compositor still holds buffers from the retired generation. Those retired
// buffers are tracked separately, and their releases never reach the scheduler.
func TestSchedulerConfigureFreesEverySlot(t *testing.T) {
	t.Parallel()

	s := NewScheduler()
	submitFirstFrame(t, s)

	// Fill the second slot too, so both are busy in this generation.
	s.Invalidate()
	if err := s.Frame(); err != nil {
		t.Fatal(err)
	}
	d, second := s.Next()
	if d != DecisionRender {
		t.Fatalf("decision = %v, want DecisionRender for the second slot", d)
	}
	if err := s.Submitted(second.Slot); err != nil {
		t.Fatal(err)
	}

	// A configure starts a new generation of two fresh buffers.
	s.Configure(360, 48)

	d, job := s.Next()
	if d != DecisionRender {
		t.Fatal("configure left every slot busy, so the surface can never redraw")
	}
	if job.Width != 360 || job.Height != 48 {
		t.Fatalf("job = %dx%d, want the new 360x48", job.Width, job.Height)
	}
}

// TestSchedulerWaitsUntilConfigured guards the startup ordering: an
// invalidation can arrive from the application before the compositor has sent
// its first configure, and at that point no buffer generation exists yet.
func TestSchedulerWaitsUntilConfigured(t *testing.T) {
	t.Parallel()

	s := NewScheduler()
	s.Invalidate()
	if d, _ := s.Next(); d != DecisionWait {
		t.Fatal("scheduler offered a render before any configure, when no buffer exists")
	}

	s.Configure(240, 48)
	if d, _ := s.Next(); d != DecisionRender {
		t.Fatal("scheduler withheld the redraw the configure made ready")
	}
}
