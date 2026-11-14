package main

import (
	"context"
	"fmt"
	"log"

	"github.com/Nomadcxx/sysc-shell/internal/config"
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
	if len(args) != 1 || args[0] == "" {
		return fmt.Errorf("usage: %s <token>", restoreDisplaysFlag)
	}
	return displays.RunRecovery(ctx, args[0])
}

// restoreDisplaysRequested reports whether argv is a recovery invocation. The
// flag is recognised before any normal startup, because a helper that started
// the shell first would be racing the crash it is meant to clean up after.
// Only the first argument counts, so a later argument to another mode, such
// as an ipc payload, can never select it.
func restoreDisplaysRequested(argv []string) ([]string, bool) {
	if len(argv) == 0 || argv[0] != restoreDisplaysFlag {
		return nil, false
	}
	if len(argv) == 1 {
		return nil, true
	}
	return argv[1:], true
}

// startDisplays attaches display settings once the user has confirmed a root
// for the compositor that owns socket. Without one the page says so and offers
// to find the root; nothing is written until then. The socket's peer process,
// not this shell's environment, picks the saved root and the validator, so a
// Niri root never manages sysc-compositor or the reverse.
func startDisplays(socket string, saved config.Displays) *displays.Service {
	kind, err := displays.PeerKind(socket)
	if err != nil {
		log.Printf("sysc-shell: display settings unavailable: %v", err)
		return nil
	}
	root := displays.SavedRoot(kind, saved)
	if root == "" {
		return nil
	}
	validate, err := displays.NativeValidator(kind)
	if err != nil {
		// A shell with no native validator still reads outputs; it refuses
		// to publish a candidate it could not check first.
		log.Printf("sysc-shell: display settings will not publish: %v", err)
	}
	svc, err := displays.New(socket, root, validate)
	if err != nil {
		log.Printf("sysc-shell: display settings unavailable: %v", err)
		return nil
	}
	return svc
}
