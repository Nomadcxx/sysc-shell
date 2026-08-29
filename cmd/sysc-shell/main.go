// Command sysc-shell renders the Niri shell surfaces.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/Nomadcxx/sysc-shell/internal/platform/niri"
	"github.com/Nomadcxx/sysc-shell/internal/platform/wayland"
	"github.com/Nomadcxx/sysc-shell/internal/shell"
)

// run streams Niri workspace state into the bar registry and hands the registry
// to the Wayland owner. The owner goroutine performs all Wayland work and
// creates one bar per connected output.
func run(ctx context.Context) error {
	// Validated before opening Wayland so the startup error names the missing
	// environment variable.
	socket := os.Getenv("NIRI_SOCKET")
	if socket == "" {
		return fmt.Errorf("NIRI_SOCKET is not set; start sysc-shell from a Niri session")
	}

	registry := shell.NewRegistry()

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	snapshots, niriErrs := niri.Stream(ctx, socket)
	streamFailed := make(chan error, 1)
	go func() {
		for {
			select {
			case snapshot, ok := <-snapshots:
				if !ok {
					return
				}
				registry.UpdateNiri(snapshot)
			case err, ok := <-niriErrs:
				if ok && err != nil {
					select {
					case streamFailed <- err:
					default:
					}
					cancel()
				}
				return
			}
		}
	}()

	runErr := wayland.Run(ctx, wayland.Options{
		Height: shell.BarHeight,
		Gap:    shell.BarGap,
	}, wayland.Callbacks{
		NewHost:       registry.NewHost,
		DropHost:      registry.DropHost,
		Invalidations: registry.Invalidations(),
	})
	if runErr != nil {
		return runErr
	}
	select {
	case err := <-streamFailed:
		return err
	default:
		return nil
	}
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := run(ctx); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
