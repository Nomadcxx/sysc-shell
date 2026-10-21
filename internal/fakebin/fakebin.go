// Package fakebin writes the fake executables that tests run.
//
// Linux refuses to exec an inode that still has a write access open and
// reports ETXTBSY. The kernel releases that access asynchronously, so a
// script that is written and executed straight after it can fail with
// "text file busy" while the machine is busy. Write waits until the kernel
// takes the script before it returns, so the caller's own exec cannot land
// in that window.
//
// The wait uses a probe exec, and the probe must not do any work: Write puts
// a guard behind the shebang that exits at once when ProbeEnv is set, and it
// runs the probe with that variable set.
package fakebin

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"
)

// ProbeEnv is the variable that marks a probe exec of a fake script. A fake
// script that Write wrote exits at once when it is set.
const ProbeEnv = "SYSC_FAKE_PROBE"

// WaitTimeout bounds how long Write waits for the kernel to release the
// script.
const WaitTimeout = 5 * time.Second

// guard exits the script when it runs as a probe. It goes behind the
// shebang, before any line that has a side effect.
var guard = "if [ -n \"$" + ProbeEnv + "\" ]; then exit 0; fi\n"

// Write writes script to path with mode and waits until the kernel accepts
// an exec of it.
//
// The script must be a POSIX shell script: Write puts the probe guard behind
// the shebang, so the guard has to be shell.
func Write(path, script string, mode fs.FileMode) error {
	body, err := withGuard(script)
	if err != nil {
		return err
	}
	if err := os.WriteFile(path, []byte(body), mode); err != nil {
		return err
	}
	if err := os.Chmod(path, mode); err != nil {
		return err
	}
	return Wait(path, WaitTimeout)
}

// Wait waits until the kernel accepts an exec of path.
func Wait(path string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		err := probe(path)
		if err == nil {
			return nil
		}
		if !errors.Is(err, syscall.ETXTBSY) {
			return fmt.Errorf("fakebin: probe %s: %w", path, err)
		}
		if !time.Now().Before(deadline) {
			return fmt.Errorf("fakebin: probe %s: %w after %s", path, err, timeout)
		}
		time.Sleep(time.Millisecond)
	}
}

// withGuard returns script with the probe guard behind its shebang.
func withGuard(script string) (string, error) {
	if !strings.HasPrefix(script, "#!") {
		return "", errors.New("fakebin: the script must start with a shebang")
	}
	end := strings.IndexByte(script, '\n')
	if end < 0 {
		return script + "\n" + guard, nil
	}
	if !strings.Contains(script[:end], "sh") {
		return "", errors.New("fakebin: the script must run under a shell")
	}
	return script[:end+1] + guard + script[end+1:], nil
}

// probe runs the script as a probe exec: it must not do any work.
func probe(path string) error {
	cmd := exec.Command(path)
	cmd.Env = append(os.Environ(), ProbeEnv+"=1")
	return cmd.Run()
}
