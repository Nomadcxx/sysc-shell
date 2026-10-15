package main

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"strings"

	"github.com/Nomadcxx/sysc-shell/internal/config"
)

// runConfigCheck answers whether the shell would accept a configuration file,
// without a compositor, a Niri socket, or a wayland connection. The exit codes
// are a contract: 0 accepted, 1 the loader rejected the content, 2 usage or a
// read failure. Scripts compare them, so they are not renumbered.
func runConfigCheck(args []string, stdout, stderr io.Writer) int {
	if len(args) > 1 || (len(args) == 1 && strings.HasPrefix(args[0], "-")) {
		fmt.Fprintln(stderr, "usage: sysc-shell config check [path]")
		return 2
	}
	path := config.DefaultPath()
	if len(args) == 1 {
		path = args[0]
	}
	if path == "" {
		fmt.Fprintln(stderr, "config: no config path: HOME is unset")
		return 2
	}
	_, err := config.Load(path)
	if err != nil {
		fmt.Fprintln(stderr, err)
		// ponytail: a read failure is the loader's wrapped *fs.PathError; a
		// rejected value is a bare pathErr. Prefixes in the loader's text would
		// say the same thing, but would move when its wording does.
		var readErr *fs.PathError
		if errors.As(err, &readErr) {
			return 2
		}
		return 1
	}
	fmt.Fprintf(stdout, "%s: ok\n", path)
	return 0
}
