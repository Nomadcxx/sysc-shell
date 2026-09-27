package services

import (
	"os"
	"path/filepath"
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
