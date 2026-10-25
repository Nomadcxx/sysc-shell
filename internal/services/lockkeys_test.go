package services

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func writeLED(t *testing.T, root, name, val string) {
	t.Helper()
	dir := filepath.Join(root, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "brightness"), []byte(val+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestReadLockStateAnyKeyboardLitCountsAsOn(t *testing.T) {
	root := t.TempDir()
	writeLED(t, root, "input13::capslock", "0")
	writeLED(t, root, "input17::capslock", "1")
	writeLED(t, root, "input13::numlock", "0")
	st, ok := readLockState(root)
	if !ok || !st.Caps || st.Num {
		t.Fatalf("state %+v ok %v", st, ok)
	}
}

func TestLockKeysWithoutLEDsNeverPolls(t *testing.T) {
	l := NewLockKeys(t.TempDir(), time.Millisecond)
	l.Start()
	defer l.Close()
	if l.Available() {
		t.Fatal("no LED files but Available")
	}
	l.mu.Lock()
	running := l.stop != nil
	l.mu.Unlock()
	if running {
		t.Fatal("a machine without LEDs started a poll loop")
	}
	select {
	case st := <-l.Changes():
		t.Fatalf("unexpected change %+v", st)
	case <-time.After(20 * time.Millisecond):
	}
}

func TestLockKeysWithoutALeaseNeverPolls(t *testing.T) {
	root := t.TempDir()
	writeLED(t, root, "input13::capslock", "0")
	l := NewLockKeys(root, time.Millisecond)
	defer l.Close()
	if l.Polling() {
		t.Fatal("a service nobody leased started a poll loop")
	}
	writeLED(t, root, "input13::capslock", "1")
	select {
	case st := <-l.Changes():
		t.Fatalf("an unleased service published %+v", st)
	case <-time.After(20 * time.Millisecond):
	}
	if l.Polling() {
		t.Fatal("a service nobody leased started a poll loop")
	}
}

func TestLockKeysWithALeasePublishesWithinThePollInterval(t *testing.T) {
	root := t.TempDir()
	writeLED(t, root, "input13::capslock", "0")
	l := NewLockKeys(root, 0)
	defer l.Close()
	release := l.Acquire()
	defer release()
	if !l.Polling() {
		t.Fatal("the lease did not start the poll loop")
	}
	writeLED(t, root, "input13::capslock", "1")
	start := time.Now()
	select {
	case st := <-l.Changes():
		if !st.Caps {
			t.Fatalf("change reported %+v", st)
		}
		if elapsed := time.Since(start); elapsed > lockKeysPoll+100*time.Millisecond {
			t.Fatalf("the change took %v, the poll interval is %v", elapsed, lockKeysPoll)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no change published within two poll intervals")
	}
}

func TestLockKeysReleasingTheLastLeaseStopsThePoller(t *testing.T) {
	root := t.TempDir()
	writeLED(t, root, "input13::capslock", "0")
	l := NewLockKeys(root, time.Millisecond)
	defer l.Close()
	release := l.Acquire()
	if !l.Polling() {
		t.Fatal("the lease did not start the poll loop")
	}
	release()
	if l.Polling() {
		t.Fatal("the released lease left the poll loop running")
	}
	again := l.Acquire()
	if !l.Polling() {
		t.Fatal("a second lease did not start the poll loop")
	}
	again()
	if l.Polling() {
		t.Fatal("the released lease left the poll loop running")
	}
}

func TestLockKeysKeepsPollingWhileAnotherLeaseIsOut(t *testing.T) {
	root := t.TempDir()
	writeLED(t, root, "input13::capslock", "0")
	l := NewLockKeys(root, time.Millisecond)
	defer l.Close()
	first := l.Acquire()
	second := l.Acquire()
	if !l.Polling() {
		t.Fatal("the leases did not start the poll loop")
	}
	first()
	if !l.Polling() {
		t.Fatal("a release stopped the poll loop while another lease was out")
	}
	writeLED(t, root, "input13::capslock", "1")
	select {
	case st := <-l.Changes():
		if !st.Caps {
			t.Fatalf("change reported %+v", st)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no change published while a lease was still out")
	}
	second()
	if l.Polling() {
		t.Fatal("the last release left the poll loop running")
	}
}

func TestLockKeysCloseKeepsTheLeaseCountSound(t *testing.T) {
	root := t.TempDir()
	writeLED(t, root, "input13::capslock", "0")
	l := NewLockKeys(root, time.Millisecond)
	release := l.Acquire()
	if !l.Polling() {
		t.Fatal("the lease did not start the poll loop")
	}
	// Close stops the loop even though the lease is still outstanding.
	l.Close()
	if l.Polling() {
		t.Fatal("close left the poll loop running")
	}
	// The outstanding lease comes back afterwards and must not push the
	// count below zero.
	release()
	l.mu.Lock()
	leases := l.leases
	l.mu.Unlock()
	if leases != 0 {
		t.Fatalf("lease count = %d after the last release, want 0", leases)
	}
	// A lease taken after the close still starts the loop.
	again := l.Acquire()
	if !l.Polling() {
		t.Fatal("a lease taken after close did not start the poll loop")
	}
	again()
}

func TestLockKeysConcurrentLeavesLeaveNoLoopBehind(t *testing.T) {
	root := t.TempDir()
	writeLED(t, root, "input13::capslock", "0")
	l := NewLockKeys(root, time.Millisecond)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				release := l.Acquire()
				release()
			}
		}()
	}
	wg.Wait()
	l.mu.Lock()
	leases := l.leases
	l.mu.Unlock()
	if leases != 0 {
		t.Fatalf("lease count = %d after every release, want 0", leases)
	}
	if l.Polling() {
		t.Fatal("the poll loop is running without a lease")
	}
}

func TestLockKeysBaselineIsSilent(t *testing.T) {
	root := t.TempDir()
	writeLED(t, root, "input1::capslock", "1")
	writeLED(t, root, "input1::numlock", "0")
	l := NewLockKeys(root, 2*time.Millisecond)
	l.Start()
	defer l.Close()
	if got := l.Baseline(); !got.Caps || got.Num {
		t.Fatalf("baseline %+v", got)
	}
	select {
	case st := <-l.Changes():
		t.Fatalf("baseline published %+v", st)
	case <-time.After(20 * time.Millisecond):
	}
	writeLED(t, root, "input1::capslock", "0")
	select {
	case st := <-l.Changes():
		if st.Caps {
			t.Fatalf("change reported %+v", st)
		}
	case <-time.After(time.Second):
		t.Fatal("no change published after Caps Lock went off")
	}
}
