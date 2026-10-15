package plugin

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
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
	// more: it declared no filesystem reach, so it can read and write
	// anything the user can.
	BoundaryLimited Boundary = "limited"
	// BoundaryNoLandlock is a plugin that declared its filesystem reach on a
	// kernel that cannot enforce it: the ceilings hold, the files do not.
	BoundaryNoLandlock Boundary = "no-landlock"
	// BoundaryConfined is a plugin held to its own directories, its declared
	// paths and the system directories, under the ceilings. Its network is
	// not restricted.
	BoundaryConfined Boundary = "confined"
)

// boundaryOf is the boundary a start of m will run inside, and whether the
// launcher must apply a filesystem ruleset for it.
func boundaryOf(m Manifest, launcher string, abi int) (Boundary, bool) {
	switch {
	case launcher == "":
		return BoundaryNone, false
	case m.Filesystem == nil:
		return BoundaryLimited, false
	case abi < 1:
		return BoundaryNoLandlock, false
	}
	return BoundaryConfined, true
}

// Access is what a path rule lets a confined plugin do beneath a path.
type Access uint8

const (
	AccessRead Access = iota + 1
	AccessReadExec
	AccessReadWrite
	AccessReadWriteExec
	// AccessDevice is read, write and device control, for /dev.
	AccessDevice
	// AccessSocket is connecting to the one Unix socket the rule names.
	AccessSocket
)

// PathRule grants one access beneath one path. Create makes the directory
// first when it is missing, for a directory the plugin is meant to fill.
type PathRule struct {
	Path   string `json:"path"`
	Access Access `json:"access"`
	Create bool   `json:"create,omitempty"`
}

// systemPaths are what any process needs from the system: its programs and
// libraries, configuration, the kernel's views of itself, devices and scratch
// space. None of them is the user's. The session's runtime directory is not
// among them: it holds the bus and compositor sockets, so a confined plugin
// gets a directory of its own there instead (pluginRuntimeDir), and reaches
// a socket only by declaring it.
func systemPaths() []PathRule {
	rules := []PathRule{
		{Path: "/usr", Access: AccessReadExec}, {Path: "/bin", Access: AccessReadExec},
		{Path: "/sbin", Access: AccessReadExec}, {Path: "/lib", Access: AccessReadExec},
		{Path: "/lib64", Access: AccessReadExec}, {Path: "/opt", Access: AccessReadExec},
		{Path: "/nix", Access: AccessReadExec},
		{Path: "/etc", Access: AccessRead}, {Path: "/run", Access: AccessRead},
		{Path: "/proc", Access: AccessRead}, {Path: "/sys", Access: AccessRead},
		{Path: "/dev", Access: AccessDevice},
		{Path: "/tmp", Access: AccessReadWrite}, {Path: "/var/tmp", Access: AccessReadWrite},
	}
	return rules
}

// pluginRuntimeDir is a confined plugin's own runtime directory, beneath the
// session's, or empty when the session has none.
func pluginRuntimeDir(sessionRuntime, id string) string {
	if !filepath.IsAbs(sessionRuntime) {
		return ""
	}
	return filepath.Join(sessionRuntime, "sysc-plugins", id)
}

// pathRules is everything a confined plugin may reach: the system, its own
// directory to read and run, its state and runtime directories to change, the
// sockets its declaration resolved to, and the paths it declared. Nothing else
// in the user's home is granted.
func pathRules(m Manifest, stateDir, runtimeDir, home string, sockets []string, system []PathRule) []PathRule {
	rules := slices.Clone(system)
	rules = append(rules,
		PathRule{Path: m.Dir, Access: AccessReadExec},
		PathRule{Path: filepath.Join(stateDir, m.ID), Access: AccessReadWriteExec, Create: true},
	)
	if runtimeDir != "" {
		rules = append(rules, PathRule{Path: runtimeDir, Access: AccessReadWrite, Create: true})
	}
	for _, socket := range sockets {
		rules = append(rules, PathRule{Path: socket, Access: AccessSocket})
	}
	expand := func(path string) string {
		if rest, ok := strings.CutPrefix(path, "~/"); ok {
			return filepath.Join(home, rest)
		}
		return path
	}
	if m.Filesystem != nil {
		for _, path := range m.Filesystem.Read {
			rules = append(rules, PathRule{Path: expand(path), Access: AccessRead})
		}
		for _, path := range m.Filesystem.ReadWrite {
			rules = append(rules, PathRule{Path: expand(path), Access: AccessReadWrite, Create: true})
		}
	}
	return rules
}

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
	// Paths confines the plugin when present.
	Paths []PathRule `json:"paths,omitempty"`
	// SignalAny leaves signals to other processes open, as the plugin
	// declared; otherwise a confined plugin signals only its own.
	SignalAny bool `json:"signal_any,omitempty"`
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
func launchArgs(m Manifest, paths []PathRule) ([]string, error) {
	spec := launchSpec{Exec: m.ExecPath, Limits: ceilings(), Paths: paths}
	if m.Filesystem != nil {
		spec.SignalAny = m.Filesystem.SignalAny
	}
	encoded, err := json.Marshal(spec)
	if err != nil {
		return nil, err
	}
	return []string{LaunchFlag, string(encoded)}, nil
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
	if spec.Paths != nil {
		if err := restrictPaths(spec.Paths, spec.SignalAny); err != nil {
			fmt.Fprintf(os.Stderr, "plugin launcher: confine: %v\n", err)
			return 126
		}
	}
	err := syscall.Exec(spec.Exec, []string{spec.Exec}, os.Environ())
	fmt.Fprintf(os.Stderr, "plugin launcher: exec %s: %v\n", spec.Exec, err)
	return 126
}
