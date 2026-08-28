// Command sysc-shell renders the Niri shell surfaces.
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	"github.com/Nomadcxx/sysc-shell/internal/platform/niri"
	"github.com/Nomadcxx/sysc-shell/internal/platform/wayland"
	"github.com/Nomadcxx/sysc-shell/internal/shell"
)

// options holds the parsed command line.
type options struct {
	// Output is the connector name of the Niri output to use, such as DP-1.
	Output string
}

func parseOptions(args []string) (options, error) {
	fs := flag.NewFlagSet("sysc-shell", flag.ContinueOnError)
	// main reports the parse error; suppress the flag package's own output.
	fs.SetOutput(io.Discard)

	var opts options
	fs.StringVar(&opts.Output, "output", "", "connector name of the Niri output, such as DP-1")

	if err := fs.Parse(args); err != nil {
		return options{}, err
	}
	return opts, nil
}

// run streams Niri workspace state into the proof model and hands the model to
// the Wayland owner. The owner goroutine performs all Wayland work.
func run(ctx context.Context, opts options) error {
	// Validated before opening Wayland so the startup error names the missing
	// environment variable.
	socket := os.Getenv("NIRI_SOCKET")
	if socket == "" {
		return fmt.Errorf("NIRI_SOCKET is not set; start sysc-shell from a Niri session")
	}

	proof, err := shell.New()
	if err != nil {
		return err
	}

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
				proof.UpdateNiri(snapshot)
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

	runErr := wayland.Run(ctx, wayland.Options{Output: opts.Output, Height: shell.BarHeight}, wayland.Callbacks{
		Configure:     proof.Configure,
		Render:        proof.Render,
		Handle:        proof.Handle,
		Invalidations: proof.Invalidations(),
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
	opts, err := parseOptions(os.Args[1:])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := run(ctx, opts); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
