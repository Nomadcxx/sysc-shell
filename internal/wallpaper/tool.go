package wallpaper

import (
	"os"
	"os/exec"
	"path/filepath"

	"github.com/Nomadcxx/sysc-shell/internal/seam"
)

// defaultToolDirs returns the install directories an engine may live in when
// PATH does not name them.
//
// The shell runs from a systemd unit whose PATH is the manager default,
// /usr/local/bin:/usr/bin, so a per-user install under ~/.local/bin is present
// on disk and still reported as missing (sysc-925). Probe and spawn both
// resolve through this list, so the shell can never report a binary it then
// fails to start.
func defaultToolDirs() []string {
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		return []string{filepath.Join(home, ".local", "bin"), "/usr/local/bin", "/usr/bin"}
	}
	return []string{"/usr/local/bin", "/usr/bin"}
}

// toolDirs is the fallback list resolveTool searches. A test replaces it so
// the search does not depend on what this machine has installed.
var toolDirs = seam.New[[]string](nil)

func searchDirs() []string {
	if dirs := toolDirs.Get(); dirs != nil {
		return dirs
	}
	return defaultToolDirs()
}

// resolveTool finds the program to run for name.
//
// A name that already carries a path is used as written. Otherwise PATH wins,
// because a user who put a build earlier on PATH means that build, and the
// well-known install directories are the fallback once PATH misses.
func resolveTool(name string) (string, error) {
	if name == "" {
		return "", exec.ErrNotFound
	}
	if filepath.Base(name) != name {
		return name, nil
	}
	if path, err := exec.LookPath(name); err == nil {
		return path, nil
	}
	for _, dir := range searchDirs() {
		candidate := filepath.Join(dir, name)
		info, err := os.Stat(candidate)
		if err != nil || info.IsDir() || info.Mode().Perm()&0o111 == 0 {
			continue
		}
		return candidate, nil
	}
	return "", exec.ErrNotFound
}

// spawnName is what exec runs for argv[0]: the resolved program when one is
// found, and the bare name when it is not, so exec reports its own error.
func spawnName(argv []string) []string {
	if len(argv) == 0 {
		return argv
	}
	path, err := resolveTool(argv[0])
	if err != nil || path == argv[0] {
		return argv
	}
	out := make([]string, len(argv))
	copy(out, argv)
	out[0] = path
	return out
}

// probePath is the argv[0] for a capability probe: the resolved program when
// one is found, and the bare name when it is not, so a missing binary fails
// the probe instead of changing what it runs.
func probePath(name string) string {
	if path, err := resolveTool(name); err == nil {
		return path
	}
	return name
}
