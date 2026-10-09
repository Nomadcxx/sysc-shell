// Command sysc-panel-preview paints a plugin panel tree to a PNG the way the
// shell would, for catalog screenshots and documentation images.
//
// It reads a panel's wire tree as JSON (a v1.Node), lays it out and paints it
// with the host's own converter, layout and painter, and writes a PNG. A view
// the host would refuse is refused here with the host's wording and a non-zero
// exit.
//
//	sysc-panel-preview -width 360 -height 480 [-scale 180] [-font family] \
//	    [-o out.png] [tree.json]
//
// With no file argument it reads the tree from standard input; with no -o it
// writes the PNG to standard output. -scale is in 120ths of 100%: 120 is 100%,
// 180 (the default) is 150%.
//
// The picture is a stateless still of an opaque, detached panel on the default
// dark theme, with transparent corners. It has no animation, hover or focus
// state and no running plugin. Text is drawn with the fonts installed on the
// machine that runs it, so the same tree can produce different pixels on
// different machines.
package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"image/png"
	"io"
	"os"

	v1 "github.com/Nomadcxx/sysc-shell/plugin/v1"
)

func main() {
	if err := run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, "sysc-panel-preview:", err)
		os.Exit(1)
	}
}

func run(args []string, stdin io.Reader, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("sysc-panel-preview", flag.ContinueOnError)
	flags.SetOutput(stderr)
	width := flags.Int("width", 0, "panel width in logical pixels")
	height := flags.Int("height", 0, "panel height in logical pixels")
	scale := flags.Int("scale", defaultScale120, "display scale in 120ths (120 = 100%)")
	font := flags.String("font", "", "font family (default: the shell's default)")
	out := flags.String("o", "", "write the PNG here instead of standard output")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() > 1 {
		return errors.New("at most one tree file")
	}

	in := stdin
	if flags.NArg() == 1 {
		f, err := os.Open(flags.Arg(0))
		if err != nil {
			return err
		}
		defer f.Close()
		in = f
	}
	var root v1.Node
	dec := json.NewDecoder(in)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&root); err != nil {
		return fmt.Errorf("tree: %w", err)
	}
	if _, err := dec.Token(); err != io.EOF {
		return errors.New("tree: unexpected data after the tree")
	}

	// Render before creating the output file, so a refused view leaves nothing
	// behind for a caller to mistake for a screenshot.
	img, err := renderPanel(&root, *width, *height, *scale, *font)
	if err != nil {
		return err
	}

	w := stdout
	if *out != "" {
		f, err := os.Create(*out)
		if err != nil {
			return err
		}
		defer f.Close()
		w = f
	}
	return png.Encode(w, img)
}
