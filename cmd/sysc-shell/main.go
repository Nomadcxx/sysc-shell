// Command sysc-shell renders the Niri shell surfaces.
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/Nomadcxx/sysc-shell/internal/platform/wayland"
)

// barHeight is the logical height and exclusive zone of the proof surface.
const barHeight = 48

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

// run wires flat-colour callbacks to the Wayland owner. Task 9 replaces this
// temporary wiring with the proof application.
func run(ctx context.Context, opts options) error {
	invalidations := make(chan struct{}, 1)

	return wayland.Run(ctx, wayland.Options{Output: opts.Output, Height: barHeight}, wayland.Callbacks{
		Configure: func(logicalWidth, logicalHeight, scale120 int) error {
			log.Printf("configure: logical %dx%d scale120 %d", logicalWidth, logicalHeight, scale120)
			return nil
		},
		Render: func(pixels []byte, width, height, stride int) error {
			log.Printf("render: buffer %dx%d stride %d bytes %d", width, height, stride, len(pixels))
			// Opaque slate, premultiplied and stored as B, G, R, A.
			for i := 0; i+3 < len(pixels); i += 4 {
				pixels[i], pixels[i+1], pixels[i+2], pixels[i+3] = 0x28, 0x1c, 0x14, 0xff
			}
			return nil
		},
		Handle: func(e wayland.Event) bool {
			log.Printf("pointer: kind %d at %d,%d button %d", e.Kind, e.X, e.Y, e.Button)
			return false
		},
		Invalidations: invalidations,
	})
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
