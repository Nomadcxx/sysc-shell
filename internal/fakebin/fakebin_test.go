package fakebin

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

// TestWriteWaitsUntilTheKernelTakesTheScript is the check that the wait
// does its job. An inode that still has a write access open cannot be
// exec'd, so the test holds one open on purpose and releases it after a
// moment. Write must not return before the kernel takes the script.
func TestWriteWaitsUntilTheKernelTakesTheScript(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "fake")
	marker := filepath.Join(dir, "marker")
	script := "#!/bin/sh\necho ran >> " + marker + "\n"

	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	held, err := os.OpenFile(path, os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := probe(path); !errors.Is(err, syscall.ETXTBSY) {
		held.Close()
		t.Fatalf("probe error = %v, want ETXTBSY", err)
	}

	go func() {
		time.Sleep(50 * time.Millisecond)
		held.Close()
	}()
	if err := Write(path, script, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("marker error = %v, want the probe to leave no trace", err)
	}
}

// TestTheScriptRunsAfterWrite is the check that the guard does not break
// the script: the guard only answers the probe.
func TestTheScriptRunsAfterWrite(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "fake")
	marker := filepath.Join(dir, "marker")
	if err := Write(path, "#!/bin/sh\necho ran >> "+marker+"\n", 0o755); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command(path).CombinedOutput(); err != nil {
		t.Fatalf("run %s: %v (%s)", path, err, out)
	}
	data, err := os.ReadFile(marker)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "ran\n" {
		t.Fatalf("marker = %q, want the script to have run", data)
	}
}

// TestWriteRefusesAScriptWithoutAShebang keeps the probe guard honest: it
// only works for a shell script.
func TestWriteRefusesAScriptWithoutAShebang(t *testing.T) {
	dir := t.TempDir()
	if err := Write(filepath.Join(dir, "fake"), "echo ran\n", 0o755); err == nil {
		t.Fatal("a script without a shebang must be refused")
	}
}

// TestWriteKeepsTheMode keeps the mode the caller asked for: the helper
// that needs a private program needs it to stay private.
func TestWriteKeepsTheMode(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "fake")
	if err := Write(path, "#!/bin/sh\nexit 0\n", 0o700); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o700 {
		t.Fatalf("mode = %v, want 0700", info.Mode().Perm())
	}
}

// TestWaitGivesUpWhenTheFileStaysBusy keeps the wait bounded: a file that
// stays busy must not hold the caller forever.
func TestWaitGivesUpWhenTheFileStaysBusy(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "fake")
	if err := os.WriteFile(path, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	held, err := os.OpenFile(path, os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer held.Close()
	if err := Wait(path, 50*time.Millisecond); err == nil {
		t.Fatal("Wait must give up when the file stays busy")
	}
}
