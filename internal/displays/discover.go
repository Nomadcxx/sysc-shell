package displays

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Discovery answers one question: which file is the compositor on this socket
// actually loading? It is read-only. The shell never writes a configuration
// root it was not told, and the answer is only a proposal until the user
// confirms it.
type Discovery struct {
	// Root is the absolute path the compositor most likely loads.
	Root string
	// Source names where the proposal came from, for the confirmation text.
	Source string
	// Confirm is true whenever the proposal is inferred rather than read from
	// the compositor's own arguments.
	Confirm bool
	// ReadOnly marks a root the shell must not write: a system-wide file, or
	// one owned by another user.
	ReadOnly bool
	// Note explains the proposal in one sentence.
	Note string
}

// systemConfigPath is niri's own fallback, and the only path it uses when the
// user has no configuration at all.
const systemConfigPath = "/etc/niri/config.kdl"

// Discover resolves the configuration root of the compositor listening on
// socket, following niri's own precedence: -c/--config, then NIRI_CONFIG, then
// XDG_CONFIG_HOME/niri/config.kdl, and /etc/niri/config.kdl only when the user
// has none. The socket's peer credentials give the process to read, so a shell
// started against the wrong socket cannot propose a path the compositor is not
// using.
func Discover(socket string) (Discovery, error) {
	identity, err := compositorIdentity(socket)
	if err != nil {
		return Discovery{}, err
	}
	argv, err := procFields(identity.PID, "cmdline")
	if err != nil {
		return Discovery{}, err
	}
	env, err := procFields(identity.PID, "environ")
	if err != nil {
		return Discovery{}, err
	}
	if path, ok := configFromArgv(argv); ok {
		return proposal(path, "the compositor's -c argument", false)
	}
	if path, ok := envValue(env, "NIRI_CONFIG"); ok {
		return proposal(path, "NIRI_CONFIG in the compositor's environment", false)
	}
	if base, ok := envValue(env, "XDG_CONFIG_HOME"); ok {
		return proposal(filepath.Join(base, "niri", "config.kdl"), "XDG_CONFIG_HOME in the compositor's environment", true)
	}
	if home, ok := envValue(env, "HOME"); ok {
		return proposal(filepath.Join(home, ".config", "niri", "config.kdl"), "the compositor's home directory", true)
	}
	return proposal(systemConfigPath, "niri's system configuration", true)
}

func proposal(path, source string, confirm bool) (Discovery, error) {
	// A relative NIRI_CONFIG is read by the compositor as given, relative to
	// the working directory it was started in. Resolving it against this
	// process's directory would be a guess, so it stays a guess and says so.
	absolute := filepath.IsAbs(path)
	root := path
	if absolute {
		root = filepath.Clean(path)
	}
	d := Discovery{Root: root, Source: source, Confirm: confirm || !absolute}
	switch {
	case !absolute:
		d.Note = source + " is relative, so the shell cannot tell which file it names"
	case root == systemConfigPath:
		d.ReadOnly = true
		d.Note = "this is niri's system-wide configuration"
	}
	if !d.ReadOnly {
		if readOnly, why := notWritable(root); readOnly {
			d.ReadOnly = true
			d.Note = why
		}
	}
	return d, nil
}

// notWritable reports whether the shell must leave a file alone, and why. A
// file that does not exist yet is writable: niri creates it from its bundled
// default, and Setup refuses to bootstrap one it cannot write, which is the
// honest answer either way.
func notWritable(path string) (bool, string) {
	info, err := os.Lstat(path)
	switch {
	case os.IsNotExist(err):
		if dir := filepath.Dir(path); checkDirWritable(dir) != nil {
			return true, fmt.Sprintf("the directory holding %s is not writable", filepath.Base(path))
		}
		return false, ""
	case err != nil:
		return true, err.Error()
	case info.Mode()&os.ModeSymlink != 0:
		target, err := os.Stat(path)
		if err != nil {
			return true, err.Error()
		}
		if writable := target.Mode().Perm() & 0o200; writable == 0 {
			return true, "the file it points at is not writable"
		}
		return false, ""
	}
	if os.Geteuid() == 0 {
		return false, ""
	}
	if info.Mode().Perm()&0o200 == 0 {
		return true, "the file is not writable by this user"
	}
	return false, ""
}

// configFromArgv finds niri's -c/--config in the compositor's own arguments.
// A flag after the `--` separator belongs to a command niri was asked to run,
// not to niri, so it is not read.
func configFromArgv(argv []string) (string, bool) {
	for i, a := range argv {
		if a == "--" {
			return "", false
		}
		for _, flag := range []string{"-c", "--config"} {
			if a == flag {
				if i+1 < len(argv) && argv[i+1] != "" {
					return argv[i+1], true
				}
				return "", false
			}
			if rest, ok := strings.CutPrefix(a, flag+"="); ok && rest != "" {
				return rest, true
			}
		}
	}
	return "", false
}

// procFields reads a NUL-separated /proc file of a process this user owns. The
// compositor is our own user's process, or nothing here would be readable and
// nothing here would be safe to write.
func procFields(pid int, file string) ([]string, error) {
	data, err := os.ReadFile(procPath(pid, file))
	if err != nil {
		return nil, fmt.Errorf("displays: read %s of the compositor: %w", file, err)
	}
	parts := strings.Split(string(data), "\x00")
	if n := len(parts); n > 0 && parts[n-1] == "" {
		parts = parts[:n-1]
	}
	return parts, nil
}

func envValue(env []string, key string) (string, bool) {
	for _, entry := range env {
		if v, ok := strings.CutPrefix(entry, key+"="); ok {
			return v, true
		}
	}
	return "", false
}
