// Command docscheck fails when a fenced example in a markdown document names a
// vocabulary the code does not have: a config field the loader refuses, a bar
// item the shell cannot draw, an ipc method or panel the socket does not
// dispatch. It reads only files and prints only text, so it can run in CI with
// no compositor, no socket and no network.
package main

import (
	"fmt"
	"os"
)

func main() {
	code := 0
	for _, path := range os.Args[1:] {
		for _, finding := range checkFile(path) {
			fmt.Fprintln(os.Stderr, "docs/examples: "+finding)
			code = 1
		}
	}
	os.Exit(code)
}
