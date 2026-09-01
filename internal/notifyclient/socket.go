// Package notifyclient consumes the sysc-notify presenter socket. It owns the
// transport and the generation rule; it renders nothing and holds no UI state.
package notifyclient

import (
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

const (
	socketName    = "presenter.v1.sock"
	runtimeSubdir = "sysc-notify"
	dialTimeout   = 2 * time.Second
)

// SocketPath reports the presenter socket for a runtime directory.
func SocketPath(runtimeDir string) string {
	if runtimeDir == "" {
		return ""
	}
	return filepath.Join(runtimeDir, runtimeSubdir, socketName)
}

// dial connects to the presenter socket only when the path is the service's
// own: no symlinked component, a private directory, and a socket owned by this
// user with mode 0600. The peer's credentials are checked after connecting, so
// the shell never speaks to a socket another user planted.
func dial(runtimeDir string) (*net.UnixConn, error) {
	path := SocketPath(runtimeDir)
	if path == "" {
		return nil, errors.New("notifyclient: XDG_RUNTIME_DIR is empty")
	}
	uid := uint32(os.Geteuid())
	if err := rejectSymlinkComponents(filepath.Dir(path)); err != nil {
		return nil, err
	}
	if err := checkDirectory(filepath.Dir(path), uid); err != nil {
		return nil, err
	}
	if err := checkSocket(path, uid); err != nil {
		return nil, err
	}
	conn, err := net.DialTimeout("unix", path, dialTimeout)
	if err != nil {
		return nil, fmt.Errorf("notifyclient: dial %s: %w", path, err)
	}
	socket, ok := conn.(*net.UnixConn)
	if !ok {
		_ = conn.Close()
		return nil, fmt.Errorf("notifyclient: %s is not a unix socket", path)
	}
	peer, err := peerUID(socket)
	if err != nil || peer != uid {
		_ = socket.Close()
		if err != nil {
			return nil, fmt.Errorf("notifyclient: read peer credentials: %w", err)
		}
		return nil, fmt.Errorf("notifyclient: %s is served by uid %d, want %d", path, peer, uid)
	}
	return socket, nil
}

func checkDirectory(path string, uid uint32) error {
	info, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("notifyclient: inspect runtime directory: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() ||
		info.Mode().Perm() != 0o700 || fileUID(info) != uid {
		return fmt.Errorf("notifyclient: unsafe runtime directory %q", path)
	}
	return nil
}

func checkSocket(path string, uid uint32) error {
	info, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("notifyclient: inspect socket: %w", err)
	}
	if info.Mode()&os.ModeSocket == 0 || info.Mode()&os.ModeSymlink != 0 ||
		info.Mode().Perm() != 0o600 || fileUID(info) != uid {
		return fmt.Errorf("notifyclient: unsafe socket %q", path)
	}
	return nil
}

func rejectSymlinkComponents(path string) error {
	current := string(filepath.Separator)
	relative := strings.TrimPrefix(filepath.Clean(path), current)
	for _, part := range strings.Split(relative, string(filepath.Separator)) {
		if part == "" || part == "." {
			continue
		}
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("notifyclient: symlink path component %q", current)
		}
	}
	return nil
}

func fileUID(info os.FileInfo) uint32 { return info.Sys().(*syscall.Stat_t).Uid }

func peerUID(conn *net.UnixConn) (uint32, error) {
	raw, err := conn.SyscallConn()
	if err != nil {
		return 0, err
	}
	var credential *unix.Ucred
	var controlErr error
	if err := raw.Control(func(fd uintptr) {
		credential, controlErr = unix.GetsockoptUcred(int(fd), unix.SOL_SOCKET, unix.SO_PEERCRED)
	}); err != nil {
		return 0, err
	}
	if controlErr != nil {
		return 0, controlErr
	}
	return credential.Uid, nil
}
