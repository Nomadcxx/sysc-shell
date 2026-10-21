package plugin

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// idleWindow is how long each phase below is measured for. It is long enough
// that one stray wakeup cannot decide the result, and short enough to stay a
// unit test.
const idleWindow = 5 * time.Second

// wakeupSlack is how many voluntary context switches per second an idle
// Preparer may add over a process with no Preparer at all. The difference was
// about 158/s while the pool ran its own 30 Hz ticker (issue #372); a pool
// with nothing to do must not wake itself at all.
const wakeupSlack = 2.0

// voluntarySwitches reads the voluntary context switch count of every thread
// in this process, keyed by thread id.
func voluntarySwitches() (map[string]int64, error) {
	tasks, err := filepath.Glob("/proc/self/task/*/status")
	if err != nil {
		return nil, err
	}
	if len(tasks) == 0 {
		return nil, fmt.Errorf("no thread status under /proc/self/task")
	}
	out := make(map[string]int64, len(tasks))
	for _, path := range tasks {
		b, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		tid := filepath.Base(filepath.Dir(path))
		for _, line := range strings.Split(string(b), "\n") {
			if !strings.HasPrefix(line, "voluntary_ctxt_switches:") {
				continue
			}
			n, err := strconv.ParseInt(strings.TrimSpace(strings.TrimPrefix(line, "voluntary_ctxt_switches:")), 10, 64)
			if err != nil {
				return nil, err
			}
			out[tid] = n
		}
	}
	return out, nil
}

// idleWakeupsPerSecond counts the voluntary context switches this process made
// during one window, over the threads that were present for all of it. A
// thread created inside the window is left out: its counter starts near zero,
// and counting it would read as wakeups this test caused.
func idleWakeupsPerSecond(t *testing.T, window time.Duration) float64 {
	t.Helper()
	before, err := voluntarySwitches()
	if err != nil {
		t.Skipf("voluntary context switches are not readable here: %v", err)
	}
	time.Sleep(window)
	after, err := voluntarySwitches()
	if err != nil {
		t.Skipf("voluntary context switches are not readable here: %v", err)
	}
	var total int64
	surviving := 0
	for tid, end := range after {
		start, ok := before[tid]
		if !ok {
			continue
		}
		total += end - start
		surviving++
	}
	if surviving == 0 {
		t.Skip("no thread was present for the whole window")
	}
	return float64(total) / window.Seconds()
}

// An idle shell schedules nothing, and the Preparer is created for every
// session whether or not a plugin is enabled. Its pool must therefore wait on
// the condition variable until Submit signals it: a free-running ticker keeps
// a laptop CPU from reaching its deeper idle states thirty times a second.
//
// Not parallel on purpose: the count spans the whole process, so a test
// running alongside would be measured as this pool's wakeups.
func TestPreparerIdleWakeups(t *testing.T) {
	baseline := idleWakeupsPerSecond(t, idleWindow)

	p := NewPreparer(2, measureFixed)
	defer p.Close()
	// Let the pool reach its wait before the window opens.
	time.Sleep(50 * time.Millisecond)
	idle := idleWakeupsPerSecond(t, idleWindow)
	t.Logf("voluntary context switches a second: baseline %.1f, idle Preparer %.1f", baseline, idle)

	if idle > baseline+wakeupSlack {
		t.Fatalf("an idle Preparer made %.1f voluntary context switches a second, baseline %.1f: a pool with no work must not wake itself",
			idle, baseline)
	}
}

// A view held back by the rate gate must still go out when it comes due. The
// pool arms one timer for the earliest held-back view instead of waking on a
// fixed interval, so nothing further has to arrive to move it along.
func TestPreparerPublishesAHeldBackViewWithoutAnotherSubmit(t *testing.T) {
	t.Parallel()

	p := NewPreparer(1, measureFixed)
	defer p.Close()

	p.Submit(barJob("v1", 1, timerBar()))
	if got := await(t, p); got.Revision != 1 {
		t.Fatalf("first revision = %d, want 1", got.Revision)
	}

	// A second update inside the publish interval is held back. No further
	// Submit comes: the timer the pool armed is what releases it.
	p.Submit(barJob("v1", 2, timerBar()))
	select {
	case got := <-p.Results():
		if got.Revision != 2 {
			t.Fatalf("revision = %d, want the held-back newest job", got.Revision)
		}
	case <-time.After(time.Second):
		t.Fatal("the rate-gated view was never published")
	}
}
