// Command sysc-shell renders the Niri shell surfaces.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"
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

func run(ctx context.Context, opts options) error {
	return errors.New("architectural proof not implemented")
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
