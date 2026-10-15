package plugin

import (
	"encoding/json"
	"fmt"
	"os"
	"runtime"
	"syscall"
)

// LaunchFlag turns a shell binary into the plugin launcher: it applies the
// plugin's boundary to its own process and then executes the plugin in its
// place. A boundary has to be set inside the child before exec, and a Go
// program cannot run code between fork and exec, so the shell starts itself
// to do it. Every binary that starts plugins must intercept the flag first.
const LaunchFlag = "-sysc-plugin-launch"

// Boundary is what a running plugin is held inside.
type Boundary string

const (
	// BoundaryNone is a plugin started without the launcher: nothing beyond
	// the operating system's own rules applies.
	BoundaryNone Boundary = "none"
	// BoundaryLimited is a plugin under the resource ceilings and nothing
	// more: it can read and write anything the user can.
	BoundaryLimited Boundary = "limited"
)

// The resource ceilings. They were measured, not chosen: on 2026-10-15 the
// fourteen plugins running on the owner's desktop peaked at 2.44 GB of
// address space (a Go runtime reserves far more than it touches; resident
// peaks were 7-76 MB), held at most 11 descriptors, and the largest file any
// plugin had written was a 99 MB model. Each ceiling leaves a wide margin over
// that and exists to stop a runaway, not to shape a working plugin. CPU time
// is deliberately unbounded: RLIMIT_CPU is cumulative, so it would end every
// long-lived plugin eventually and only turn a spinning one into a restart.
const (
	maxAddressSpace = 8 << 30
	maxOpenFiles    = 1024
	maxFileSize     = 1 << 30
)

// launchSpec is what the shell hands the launcher.
type launchSpec struct {
	Exec   string        `json:"exec"`
	Limits []launchLimit `json:"limits"`
}

type launchLimit struct {
	Resource int    `json:"resource"`
	Max      uint64 `json:"max"`
}

func ceilings() []launchLimit {
	return []launchLimit{
		{syscall.RLIMIT_AS, maxAddressSpace},
		{syscall.RLIMIT_NOFILE, maxOpenFiles},
		{syscall.RLIMIT_FSIZE, maxFileSize},
	}
}

// launchArgs is the launcher's argument list for one plugin.
func launchArgs(m Manifest) ([]string, error) {
	spec, err := json.Marshal(launchSpec{Exec: m.ExecPath, Limits: ceilings()})
	if err != nil {
		return nil, err
	}
	return []string{LaunchFlag, string(spec)}, nil
}

// LaunchMain applies a boundary to this process and executes the plugin. It
// returns only when it could not, with the exit status to use; what went
// wrong is on stderr, where the supervisor's failed-start path shows it.
func LaunchMain(args []string) int {
	// The boundary is set on this thread and inherited through exec, so the
	// exec must happen on the same one.
	runtime.LockOSThread()
	if len(args) != 1 {
		fmt.Fprintln(os.Stderr, "plugin launcher: want one specification")
		return 126
	}
	var spec launchSpec
	if err := json.Unmarshal([]byte(args[0]), &spec); err != nil {
		fmt.Fprintf(os.Stderr, "plugin launcher: %v\n", err)
		return 126
	}
	for _, l := range spec.Limits {
		// syscall.Setrlimit, not x/sys: it tells the runtime not to restore
		// its own descriptor limit across the exec below.
		if err := syscall.Setrlimit(l.Resource, &syscall.Rlimit{Cur: l.Max, Max: l.Max}); err != nil {
			fmt.Fprintf(os.Stderr, "plugin launcher: set resource %d to %d: %v\n", l.Resource, l.Max, err)
			return 126
		}
	}
	err := syscall.Exec(spec.Exec, []string{spec.Exec}, os.Environ())
	fmt.Fprintf(os.Stderr, "plugin launcher: exec %s: %v\n", spec.Exec, err)
	return 126
}
