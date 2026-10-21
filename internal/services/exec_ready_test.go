package services

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

// waitExecable waits until the program at path can be exec'd.
//
// The kernel refuses to exec a file that was written a moment ago: the file
// stays busy until every process that inherited the write handle is gone. A
// fixture that writes a script and lets the code under test start it right
// away therefore fails with ETXTBSY under load, which is a race in the
// fixture and not in the code it exercises. Probing for the program to start
// keeps that race out of the test.
//
// The probe runs the program, so it costs the program whatever the program
// does with an argument it was not expecting.
func waitExecable(path string) error {
	for attempt := 0; attempt < 200; attempt++ {
		probe := exec.Command(path, "probe")
		probe.Stdout = io.Discard
		probe.Stderr = io.Discard
		err := probe.Run()
		if err == nil || !errors.Is(err, syscall.ETXTBSY) {
			// The program started. It may have exited badly, which is the
			// program's business, not the barrier's.
			return nil
		}
		time.Sleep(5 * time.Millisecond)
	}
	return fmt.Errorf("%s stayed busy", path)
}

// TestWaitExecableBlocksWhileTheProgramIsBusy proves the barrier holds the
// program back until the write handle is closed, and lets it go afterwards.
func TestWaitExecableBlocksWhileTheProgramIsBusy(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "prog")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
	if err != nil {
		t.Fatalf("open %s: %v", path, err)
	}
	t.Cleanup(func() { _ = f.Close() })
	if _, err := f.WriteString("#!/bin/sh\nexit 0\n"); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}

	done := make(chan error, 1)
	go func() { done <- waitExecable(path) }()

	select {
	case err := <-done:
		t.Fatalf("waitExecable returned while the program was busy: %v", err)
	case <-time.After(100 * time.Millisecond):
	}

	if err := f.Close(); err != nil {
		t.Fatalf("close %s: %v", path, err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("waitExecable: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("waitExecable did not return after the program stopped being busy")
	}
}
