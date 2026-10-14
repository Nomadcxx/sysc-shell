package wallpaper

import (
	"errors"
	"testing"
	"time"
)

func fakePausedState(t *testing.T, eng *fakeEngine, connector string) bool {
	t.Helper()
	eng.mu.Lock()
	defer eng.mu.Unlock()
	return eng.paused[connector]
}

func fakeFPSState(t *testing.T, eng *fakeEngine, connector string) int {
	t.Helper()
	eng.mu.Lock()
	defer eng.mu.Unlock()
	return eng.fps[connector]
}

func fakePauseLog(t *testing.T, eng *fakeEngine) []bool {
	t.Helper()
	eng.mu.Lock()
	defer eng.mu.Unlock()
	return append([]bool(nil), eng.pauseLog...)
}

// applyEffect commits a terminal-art effect, the kind that is expected to
// accept a live rate command from the engine.
func applyEffect(t *testing.T, svc *Service) {
	t.Helper()
	svc.Enqueue(Command{Op: OpApply, Token: "DP-1", Kind: KindEffect, Effect: "fire", Theme: "nord"})
	awaitSnapshot(t, svc, func(s Snapshot) bool {
		return s.Assignments["DP-1"].Kind == KindEffect && s.Runtime["DP-1"].State == StatePlaying
	})
}

func applyVideo(t *testing.T, svc *Service) {
	t.Helper()
	svc.Enqueue(Command{Op: OpApply, Token: "DP-1", Path: "/w/current.mp4", Kind: KindVideo})
	awaitSnapshot(t, svc, func(s Snapshot) bool {
		return s.Assignments["DP-1"].Path == "/w/current.mp4" && s.Runtime["DP-1"].State == StatePlaying
	})
}

func TestServiceAutoPauseLeavesManualPlaybackUntouched(t *testing.T) {
	eng := newFakeEngine()
	svc := newTestService(t, eng)
	applyVideo(t, svc)

	svc.Enqueue(Command{Op: OpAutoPause, Token: "DP-1", Reason: ReasonLock})
	snap := awaitSnapshot(t, svc, func(s Snapshot) bool {
		return s.PausedBy["DP-1"].Has(ReasonLock)
	})
	if got := snap.PausedBy["DP-1"]; !got.Has(ReasonLock) || got.Has(ReasonManual) {
		t.Fatalf("PausedBy = %v, want lock without manual", got)
	}
	if snap.Assignments["DP-1"].DesiredPlayback != StatePlaying {
		t.Fatalf("auto pause changed the persisted desired playback to %v", snap.Assignments["DP-1"].DesiredPlayback)
	}
	if !fakePausedState(t, eng, "DP-1") {
		t.Fatal("auto pause did not reach the engine")
	}

	svc.Enqueue(Command{Op: OpAutoResume, Token: "DP-1", Reason: ReasonLock})
	snap = awaitSnapshot(t, svc, func(s Snapshot) bool {
		return !s.PausedBy["DP-1"].Has(ReasonLock)
	})
	if _, ok := snap.PausedBy["DP-1"]; ok {
		t.Fatalf("PausedBy = %v after auto resume, want no entry", snap.PausedBy["DP-1"])
	}
	if fakePausedState(t, eng, "DP-1") {
		t.Fatal("auto resume did not reach the engine")
	}
}

func TestServiceManualPauseSurvivesAutoResume(t *testing.T) {
	eng := newFakeEngine()
	svc := newTestService(t, eng)
	applyVideo(t, svc)

	svc.Enqueue(Command{Op: OpPause, Token: "DP-1"})
	awaitSnapshot(t, svc, func(s Snapshot) bool {
		return s.Assignments["DP-1"].DesiredPlayback == StatePaused && s.Runtime["DP-1"].State == StatePaused
	})

	svc.Enqueue(Command{Op: OpAutoPause, Token: "DP-1", Reason: ReasonLock})
	svc.Enqueue(Command{Op: OpAutoResume, Token: "DP-1", Reason: ReasonLock})
	snap := awaitSnapshot(t, svc, func(s Snapshot) bool {
		got, ok := s.PausedBy["DP-1"]
		return ok && !got.Has(ReasonLock) && got.Has(ReasonManual)
	})
	if snap.Runtime["DP-1"].State != StatePaused {
		t.Fatalf("runtime = %v after auto resume of a hand-paused output, want paused", snap.Runtime["DP-1"].State)
	}
	if !fakePausedState(t, eng, "DP-1") {
		t.Fatal("engine resumed an output the user paused by hand")
	}
}

func TestServiceAutoPauseCrossesTheEngineBoundaryOnce(t *testing.T) {
	eng := newFakeEngine()
	svc := newTestService(t, eng)
	applyVideo(t, svc)

	if log := fakePauseLog(t, eng); len(log) != 0 {
		t.Fatalf("pause log after apply = %v, want empty", log)
	}

	svc.Enqueue(Command{Op: OpAutoPause, Token: "DP-1", Reason: ReasonLock})
	awaitSnapshot(t, svc, func(s Snapshot) bool { return s.PausedBy["DP-1"].Has(ReasonLock) })
	if log := fakePauseLog(t, eng); len(log) != 1 || !log[0] {
		t.Fatalf("pause log after first reason = %v, want [true]", log)
	}

	svc.Enqueue(Command{Op: OpAutoPause, Token: "DP-1", Reason: ReasonOccluded})
	awaitSnapshot(t, svc, func(s Snapshot) bool { return s.PausedBy["DP-1"].Has(ReasonOccluded) })
	if log := fakePauseLog(t, eng); len(log) != 1 {
		t.Fatalf("second reason crossed the boundary again: %v", log)
	}

	svc.Enqueue(Command{Op: OpAutoResume, Token: "DP-1", Reason: ReasonLock})
	awaitSnapshot(t, svc, func(s Snapshot) bool {
		got := s.PausedBy["DP-1"]
		return got.Has(ReasonOccluded) && !got.Has(ReasonLock)
	})
	if log := fakePauseLog(t, eng); len(log) != 1 {
		t.Fatalf("resuming one of two reasons crossed the boundary: %v", log)
	}

	svc.Enqueue(Command{Op: OpAutoResume, Token: "DP-1", Reason: ReasonOccluded})
	awaitSnapshot(t, svc, func(s Snapshot) bool { return s.PausedBy["DP-1"].Empty() })
	if fakePausedState(t, eng, "DP-1") {
		t.Fatal("engine still paused after every reason cleared")
	}
	if log := fakePauseLog(t, eng); len(log) != 2 || log[0] != true || log[1] != false {
		t.Fatalf("pause log after clearing every reason = %v, want [true false]", log)
	}
}

func TestServiceSetFPSReachesTheEngine(t *testing.T) {
	eng := newFakeEngine()
	svc := newTestService(t, eng)
	applyVideo(t, svc)

	svc.Enqueue(Command{Op: OpSetFPS, Token: "DP-1", FPS: 10})
	snap := awaitSnapshot(t, svc, func(s Snapshot) bool { return s.FPS["DP-1"] == 10 })
	if got := fakeFPSState(t, eng, "DP-1"); got != 10 {
		t.Fatalf("engine fps = %d, want 10", got)
	}
	if snap.FPSUnsupported["DP-1"] {
		t.Fatal("a supported fps change was marked unsupported")
	}
}

func TestServiceFPSUnsupportedIsReportedForEffects(t *testing.T) {
	eng := newFakeEngine()
	svc := newTestService(t, eng)
	applyEffect(t, svc)

	eng.mu.Lock()
	eng.fpsErr = ErrFPSUnsupported
	eng.mu.Unlock()

	svc.Enqueue(Command{Op: OpSetFPS, Token: "DP-1", FPS: 10})
	snap := awaitSnapshot(t, svc, func(s Snapshot) bool { return s.FPSUnsupported["DP-1"] })
	if snap.PausedBy["DP-1"].Has(ReasonBattery) {
		t.Fatal("the service paused for a gap the driver should fall back on")
	}
	if log := fakePauseLog(t, eng); len(log) != 0 {
		t.Fatalf("pause log = %v, want none", log)
	}
	if got := fakeFPSState(t, eng, "DP-1"); got != 0 {
		t.Fatalf("unsupported fps reached the engine as %d", got)
	}
}

func TestServiceFPSUnsupportedReasonClearsOnNewProcess(t *testing.T) {
	eng := newFakeEngine()
	svc := newTestService(t, eng)
	applyEffect(t, svc)

	eng.mu.Lock()
	eng.fpsErr = ErrFPSUnsupported
	eng.mu.Unlock()
	svc.Enqueue(Command{Op: OpSetFPS, Token: "DP-1", FPS: 10})
	awaitSnapshot(t, svc, func(s Snapshot) bool { return s.FPSUnsupported["DP-1"] })

	eng.mu.Lock()
	eng.fpsErr = nil
	eng.mu.Unlock()
	svc.Enqueue(Command{Op: OpSetFPS, Token: "DP-1", FPS: 20})
	snap := awaitSnapshot(t, svc, func(s Snapshot) bool {
		return s.FPS["DP-1"] == 20 && !s.FPSUnsupported["DP-1"]
	})
	if snap.FPS["DP-1"] != 20 {
		t.Fatalf("fps after recovery = %d, want 20", snap.FPS["DP-1"])
	}
}

// TestServiceDisconnectDropsAutomaticReasons keeps an automatic reason from
// outliving the session that set it: a connector that returns later must not
// come back paused for a lock that is long gone.
func TestServiceDisconnectDropsAutomaticReasons(t *testing.T) {
	eng := newFakeEngine()
	svc := newTestService(t, eng)
	applyVideo(t, svc)

	svc.Enqueue(Command{Op: OpAutoPause, Token: "DP-1", Reason: ReasonLock})
	awaitSnapshot(t, svc, func(s Snapshot) bool { return s.PausedBy["DP-1"].Has(ReasonLock) })
	if log := fakePauseLog(t, eng); len(log) != 1 {
		t.Fatalf("pause log = %v, want one entry", log)
	}

	svc.Enqueue(Command{Op: OpDisconnect, Token: "DP-1"})
	awaitSnapshot(t, svc, func(s Snapshot) bool { return len(s.PausedBy) == 0 })
	svc.Enqueue(Command{Op: OpConnect, Token: "DP-1"})
	applyVideo(t, svc)

	if snap := svc.Snapshot(); snap.PausedBy["DP-1"].Has(ReasonLock) {
		t.Fatalf("the reconnected output kept the lock reason: %v", snap.PausedBy)
	}
	if log := fakePauseLog(t, eng); len(log) != 1 {
		t.Fatalf("pause log = %v, want no pause after the reconnect", log)
	}
}

// TestServiceUnsupportedVideoKeepsTheRequestedRate pins the remembered bound:
// a video engine cannot hold a rate, but the same connector must inherit the
// request when its assignment later becomes a Terminal Art effect.
func TestServiceUnsupportedVideoKeepsTheRequestedRate(t *testing.T) {
	eng := newFakeEngine()
	svc := newTestService(t, eng)
	applyVideo(t, svc)

	eng.mu.Lock()
	eng.fpsErr = ErrFPSUnsupported
	eng.mu.Unlock()
	svc.Enqueue(Command{Op: OpSetFPS, Token: "DP-1", FPS: 10})
	snap := awaitSnapshot(t, svc, func(s Snapshot) bool { return s.FPS["DP-1"] == 10 })
	if snap.FPSUnsupported["DP-1"] {
		t.Fatal("a video reported the effect-only unsupported flag")
	}

	eng.mu.Lock()
	eng.fpsErr = nil
	eng.mu.Unlock()
	applyEffect(t, svc)
	deadline := time.Now().Add(2 * time.Second)
	for fakeFPSState(t, eng, "DP-1") != 10 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if got := fakeFPSState(t, eng, "DP-1"); got != 10 {
		t.Fatalf("rate after switching to an effect = %d, want 10", got)
	}
}

// TestServiceFinishKeepsAFailedPauseUnrecorded proves the retry: when a
// replaced process refuses the re-pause, the service must not record it as
// delivered, or the next transition would skip the engine as a no-op.
func TestServiceFinishKeepsAFailedPauseUnrecorded(t *testing.T) {
	eng := newFakeEngine()
	svc := newTestService(t, eng)
	applyEffect(t, svc)

	svc.Enqueue(Command{Op: OpAutoPause, Token: "DP-1", Reason: ReasonLock})
	awaitSnapshot(t, svc, func(s Snapshot) bool { return s.PausedBy["DP-1"].Has(ReasonLock) })

	// The next apply replaces the process, and this time the re-pause fails.
	eng.mu.Lock()
	eng.pauseErr = errors.New("engine busy")
	eng.mu.Unlock()
	svc.Enqueue(Command{Op: OpApply, Token: "DP-1", Kind: KindEffect, Effect: "fire", Theme: "nord"})
	deadline := time.Now().Add(2 * time.Second)
	for {
		eng.mu.Lock()
		attempts := eng.pauseCalls
		eng.mu.Unlock()
		if attempts >= 2 || time.Now().After(deadline) {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	eng.mu.Lock()
	attempts := eng.pauseCalls
	refused := eng.pauseErr != nil
	eng.mu.Unlock()
	if attempts < 2 {
		t.Fatalf("the re-apply never tried to restore the pause (%d attempts)", attempts)
	}
	if !refused || fakePausedState(t, eng, "DP-1") {
		t.Fatal("the engine was recorded as paused although the call failed")
	}

	// With the error gone, a second reason must still reach the engine.
	eng.mu.Lock()
	eng.pauseErr = nil
	eng.mu.Unlock()
	svc.Enqueue(Command{Op: OpAutoPause, Token: "DP-1", Reason: ReasonScreensOff})
	deadline = time.Now().Add(2 * time.Second)
	for !fakePausedState(t, eng, "DP-1") && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if !fakePausedState(t, eng, "DP-1") {
		t.Fatal("the retried pause never reached the engine")
	}
}

// TestServiceOpsKeepTheirNumbers pins every Op value. The panel and the
// registry speak these numbers over the command channel, so a new operation
// must be appended, never wedged in beside an existing one.
func TestServiceOpsKeepTheirNumbers(t *testing.T) {
	cases := []struct {
		name string
		op   Op
		want int
	}{
		{"apply", OpApply, 0},
		{"pause", OpPause, 1},
		{"resume", OpResume, 2},
		{"restore", OpRestore, 3},
		{"connect", OpConnect, 4},
		{"disconnect", OpDisconnect, 5},
		{"refresh", OpRefresh, 6},
		{"refresh terminal catalog", OpRefreshTerminalCatalog, 7},
		{"focus folder", OpFocusFolder, 8},
		{"auto pause", OpAutoPause, 9},
		{"auto resume", OpAutoResume, 10},
		{"set fps", OpSetFPS, 11},
	}
	for _, tc := range cases {
		if got := int(tc.op); got != tc.want {
			t.Errorf("%s: op number = %d, want %d", tc.name, got, tc.want)
		}
	}
}

// TestServiceFinishResendsPauseAndRate proves that a fresh apply for an
// output whose engine just restarted carries the automatic pause and the
// rate bound back to the engine in the same finish pass.
func TestServiceFinishResendsPauseAndRate(t *testing.T) {
	eng := newFakeEngine()
	svc := newTestService(t, eng)
	applyVideo(t, svc)

	svc.Enqueue(Command{Op: OpAutoPause, Token: "DP-1", Reason: ReasonLock})
	awaitSnapshot(t, svc, func(s Snapshot) bool {
		return s.PausedBy["DP-1"].Has(ReasonLock)
	})
	svc.Enqueue(Command{Op: OpSetFPS, Token: "DP-1", FPS: 10})
	awaitSnapshot(t, svc, func(s Snapshot) bool { return s.FPS["DP-1"] == 10 })

	if log := fakePauseLog(t, eng); len(log) != 1 || !log[0] {
		t.Fatalf("pause log after auto pause = %v, want one true", log)
	}

	applyVideo(t, svc)
	deadline := time.Now().Add(2 * time.Second)
	for len(fakePauseLog(t, eng)) < 2 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if log := fakePauseLog(t, eng); len(log) != 2 || !log[1] {
		t.Fatalf("pause log after re-apply = %v, want two trues", log)
	}
	if got := fakeFPSState(t, eng, "DP-1"); got != 10 {
		t.Fatalf("rate after re-apply = %d, want 10", got)
	}
}
