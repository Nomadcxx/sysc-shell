package main

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"strings"

	"github.com/Nomadcxx/sysc-shell/internal/config"
)

// runConfigCheck answers whether the shell would accept a configuration file,
// without a compositor, a Niri socket, or a wayland connection. The exit codes
// are a contract: 0 accepted, 1 the loader rejected the content, 2 usage or a
// read failure. Scripts compare them, so they are not renumbered.
func runConfigCheck(args []string, stdout, stderr io.Writer) int {
	// args are the words after `config`, so the grammar lives here: `check`
	// is the only verb, and a `-` prefixed path is an error, not a flag.
	if len(args) == 0 || args[0] != "check" || len(args) > 2 ||
		(len(args) == 2 && strings.HasPrefix(args[1], "-")) {
		fmt.Fprintln(stderr, "usage: sysc-shell config check [path]")
		return 2
	}
	path := config.DefaultPath()
	if len(args) == 2 {
		path = args[1]
	}
	if path == "" {
		if len(args) == 2 {
			fmt.Fprintln(stderr, "usage: sysc-shell config check [path]")
			return 2
		}
		fmt.Fprintln(stderr, "config: no config path: HOME is unset")
		return 2
	}
	// Load treats an absent file as the defaults, so the file itself has to say
	// whether a missing path is worth reporting. One stat, before the load.
	_, statErr := os.Stat(path)
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
	suffix := ""
	if os.IsNotExist(statErr) {
		suffix = " (defaults, file not found)"
	}
	fmt.Fprintf(stdout, "%s: ok%s\n", path, suffix)
	return 0
}
