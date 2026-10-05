package main

import (
	"context"
	"fmt"

	"github.com/Nomadcxx/sysc-shell/internal/displays"
)

// restoreDisplaysFlag is the only argument the recovery helper accepts. It
// names a token, never a path and never a command, so a compromised argv cannot
// make the helper write anywhere the ticket did not already authorize.
const restoreDisplaysFlag = "--restore-displays"

// runRestoreDisplays is the whole helper process. It must stay small enough to
// finish before the compositor gives up on the socket, and it must never open a
// Wayland connection: it exists to undo output changes when the shell is gone.
func runRestoreDisplays(ctx context.Context, args []string) error {
	if len(args) != 1 || args[0] != "" {
		return fmt.Errorf("usage: %s <token>", restoreDisplaysFlag)
	}
	return displays.RunRecovery(ctx, args[0])
}

// restoreDisplaysRequested reports whether argv is a recovery invocation. The
// flag is recognised before any normal startup, because a helper that started
// the shell first would be racing the crash it is meant to clean up after.
func restoreDisplaysRequested(argv []string) ([]string, bool) {
	for i, a := range argv {
		if a != restoreDisplaysFlag {
			continue
		}
		if i+1 >= len(argv) {
			return nil, true
		}
		return argv[i+1:], true
	}
	return nil, false
}
