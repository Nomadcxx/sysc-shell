package polkit

import (
	"os"
	"path/filepath"
	"strings"
)

// knownAgents are the desktop authentication agents whose presence decides
// whether this shell registers. Naming them is how the shell can say which one
// holds the session instead of only that some agent does.
var knownAgents = []string{
	"polkit-gnome-authentication-agent-1",
	"polkit-kde-authentication-agent-1",
	"lxpolkit",
	"polkit-mate-authentication-agent-1",
	"hyprpolkitagent",
	"soteria",
}

// nameExisting returns the name of a known authentication agent running under
// procRoot, or "" when there is none.
//
// procRoot is a seam: the live answer comes from /proc, a test from a fixture
// directory of fake proc entries.
func nameExisting(procRoot string) string {
	if procRoot == "" {
		procRoot = "/proc"
	}
	entries, err := os.ReadDir(procRoot)
	if err != nil {
		return ""
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		if name := agentName(filepath.Join(procRoot, entry.Name(), "cmdline")); name != "" {
			return name
		}
	}
	return ""
}

// agentName reads one process's cmdline and reports its executable name when
// it is a known agent. cmdline is NUL separated, so argv[0] is its first
// field and a kernel thread's empty file is not a match.
func agentName(path string) string {
	raw, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	argv0, _, _ := strings.Cut(string(raw), "\x00")
	base := filepath.Base(strings.TrimSpace(argv0))
	for _, known := range knownAgents {
		if base == known {
			return known
		}
	}
	return ""
}
