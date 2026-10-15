// Command docscheck fails when a fenced example in a markdown document names a
// vocabulary the code does not have: a config field the loader refuses, a bar
// item the shell cannot draw, an ipc method or panel the socket does not
// dispatch. It reads only files and prints only text, so it can run in CI with
// no compositor, no socket and no network.
//
// Usage:
//
//	docscheck [-v] [path...]
//
// With no path it reads README.md and docs/*.md. Exit codes: 0 clean, 1 a
// finding, 2 a path that is not a readable regular file.
package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

// run checks the named documents, or the repository's own when none are named.
// It is the whole command, so a test can drive every path through it.
func run(args []string, stdout, stderr io.Writer) int {
	verbose := false
	paths := make([]string, 0, len(args))
	for _, arg := range args {
		if arg == "-v" {
			verbose = true
			continue
		}
		paths = append(paths, arg)
	}
	if len(paths) == 0 {
		paths = defaultPaths()
	}
	for _, path := range paths {
		info, err := os.Stat(path)
		if err != nil || !info.Mode().IsRegular() {
			fmt.Fprintf(stderr, "docs/examples: cannot read %s\n", path)
			return 2
		}
	}
	code := 0
	blocks := 0
	for _, path := range paths {
		for _, line := range checkFile(path) {
			fmt.Fprintln(stderr, "docs/examples: "+line)
			code = 1
		}
		blocks += checkedBlocks(path, verbose, stdout)
	}
	if code == 0 {
		fmt.Fprintf(stdout, "checked %d blocks in %d files\n", blocks, len(paths))
	}
	return code
}

// defaultPaths is the document set this gate covers. It is deliberately not the
// whole repository: docs/plans holds design history, and internal/skill/content
// is checked by the skill's own package tests.
func defaultPaths() []string {
	paths := []string{"README.md"}
	matches, err := filepath.Glob(filepath.Join("docs", "*.md"))
	if err != nil {
		return paths
	}
	return append(paths, matches...)
}

// checkedBlocks counts the blocks a document was judged on, and reports the
// skipped ones when the reader asked to see them.
func checkedBlocks(path string, verbose bool, stdout io.Writer) int {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	checked := 0
	for _, b := range extract(string(data)) {
		if b.skipped != "" {
			if verbose {
				fmt.Fprintf(stdout, "skipped %s:%d: %s\n", path, b.line, b.skipped)
			}
			continue
		}
		checked++
	}
	return checked
}
