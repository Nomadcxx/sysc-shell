// Command sysc-shell renders the Niri shell surfaces.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/Nomadcxx/sysc-shell/internal/config"
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

	// A missing file is not an error: the built-in defaults apply. An invalid
	// one fails startup, because there is no previous configuration to keep.
	cfgPath := config.DefaultPath()
	cfg, err := config.Load(cfgPath)
	if err != nil {
		return err
	}

	registry := shell.NewRegistry(cfg)
	// Releases every service lease and stops the clock goroutine when the
	// process unwinds, whether through cancellation or an error return.
	defer registry.Close()

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

	// The clock publishes on its own goroutine; this pump turns each snapshot
	// into per-bar text and hands the changed outputs to the Wayland owner.
	// One tick serves every bar.
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case now := <-registry.Clock().Updates():
				registry.UpdateClock(now)
			}
		}
	}()

	// The sampling service publishes on its own goroutine; this pump turns
	// each pass into per-bar text and hands the changed outputs to the Wayland
	// owner. One pass serves every bar.
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case snapshot := <-registry.Metrics().Updates():
				registry.UpdateMetrics(snapshot)
			}
		}
	}()

	// The weather service publishes on its own goroutine; this pump turns each
	// reading into per-bar text and hands the changed outputs to the Wayland
	// owner. One reading serves every bar.
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case reading := <-registry.Weather().Updates():
				registry.UpdateWeather(reading)
			}
		}
	}()

	// SIGHUP reloads. The handler only signals; the owner goroutine re-reads
	// and validates the file itself, so no proxy is touched from here.
	reloads := make(chan struct{}, 1)
	hup := make(chan os.Signal, 1)
	signal.Notify(hup, syscall.SIGHUP)
	defer signal.Stop(hup)
	go func() {
		for range hup {
			select {
			case reloads <- struct{}{}:
			default:
			}
		}
	}()

	runErr := wayland.Run(ctx, cfg, wayland.Callbacks{
		NewHost:       registry.NewHost,
		PrepareConfig: registry.PrepareConfig,
		DropHost:      registry.DropHost,
		Invalidations: registry.Invalidations(),
		Tooltips:      registry.Tooltips(),
		Reloads:       reloads,
		ConfigPath:    cfgPath,
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
