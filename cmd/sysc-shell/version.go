package main

import (
	"fmt"
	"io"

	"github.com/Nomadcxx/sysc-shell/internal/buildinfo"
)

// runVersion answers --version. It opens no Wayland connection, touches no
// socket and starts nothing, so scripts can call it while the shell is stopped.
func runVersion(args []string, stdout, stderr io.Writer) int {
	if len(args) != 0 {
		fmt.Fprintln(stderr, "usage: sysc-shell [--version] | ipc <method> [params-json]")
		return 2
	}
	fmt.Fprintln(stdout, buildinfo.Line())
	return 0
}
