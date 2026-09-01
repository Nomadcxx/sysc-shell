// Command sysc-shell renders the Niri shell surfaces.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/Nomadcxx/sysc-shell/internal/config"
	"github.com/Nomadcxx/sysc-shell/internal/ipc"
	"github.com/Nomadcxx/sysc-shell/internal/platform/niri"
	"github.com/Nomadcxx/sysc-shell/internal/platform/wayland"
	"github.com/Nomadcxx/sysc-shell/internal/shell"
)

func pumpNiri(
	snapshots <-chan niri.Snapshot,
	errs <-chan error,
	update func(niri.Snapshot),
) error {
	for snapshots != nil || errs != nil {
		select {
		case snapshot, ok := <-snapshots:
			if !ok {
				snapshots = nil
				continue
			}
			update(snapshot)
		case err, ok := <-errs:
			if !ok {
				errs = nil
				continue
			}
			if err != nil {
				return err
			}
		}
	}
	return nil
}

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
		update := func(snapshot niri.Snapshot) { registry.UpdateNiri(snapshot) }
		if err := pumpNiri(snapshots, niriErrs, update); err != nil {
			select {
			case streamFailed <- err:
			default:
			}
			cancel()
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
	registry.BindPersist(cfgPath, reloads)

	ipcErr := make(chan error, 1)
	go func() {
		srv := ipc.NewServer(ipc.DefaultSocket(), ipc.Handlers{
			Panel: func(action, panel string) error {
				switch action {
				case "toggle":
					return registry.TogglePanelByName(panel)
				case "open":
					return registry.OpenPanelByName(panel)
				case "close":
					return registry.ClosePanelByName(panel)
				default:
					return fmt.Errorf("unknown panel action")
				}
			},
			Status:  registry.Status,
			OSDStep: registry.OSDStep,
		})
		ipcErr <- srv.Serve(ctx)
	}()
	select {
	case err := <-ipcErr:
		if errors.Is(err, ipc.ErrSingleInstance) {
			return fmt.Errorf("another sysc-shell is already running")
		}
		if err != nil {
			return err
		}
	case <-time.After(50 * time.Millisecond):
	}

	runErr := wayland.Run(ctx, cfg, wayland.Callbacks{
		NewHost:       registry.NewHost,
		PrepareConfig: registry.PrepareConfig,
		DropHost:      registry.DropHost,
		DropAux:       registry.DropAux,
		Invalidations: registry.Invalidations(),
		Aux:           registry.AuxRequests(),
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
	if len(os.Args) > 1 && os.Args[1] == "ipc" {
		if err := runIPC(os.Args[2:]); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := run(ctx); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func runIPC(args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("usage: sysc-shell ipc <method> [params-json]")
	}
	method := args[0]
	params := []byte("{}")
	if len(args) > 1 {
		params = []byte(args[1])
	}
	var raw any
	if err := json.Unmarshal(params, &raw); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	out, err := ipc.Call(ctx, ipc.DefaultSocket(), method, raw)
	if err != nil {
		return err
	}
	fmt.Println(out)
	if strings.Contains(out, `"error"`) {
		os.Exit(1)
	}
	return nil
}
