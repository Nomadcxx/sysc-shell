package ipc

import (
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/unix"

	"github.com/Nomadcxx/sysc-shell/internal/sockpath"
)

// A live socket at the path belongs to a running shell. A second shell must
// report the path as taken and leave the socket alone.
func TestALiveSocketIsNotTaken(t *testing.T) {
	t.Parallel()
	sock := filepath.Join(sockpath.Dir(t, "ipc"), "ipc.v1.sock")
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })

	srv := NewServer(sock, Handlers{})
	if err := srv.Listen(); !errors.Is(err, ErrSingleInstance) {
		t.Fatalf("the second Listen = %v, want ErrSingleInstance", err)
	}
	if _, err := os.Stat(sock); err != nil {
		t.Fatalf("the live socket was taken: %v", err)
	}
}

// A socket the user cannot reach says nothing about the peer behind it. The
// state of the path is unknown, so the path is not taken.
func TestASocketTheUserCannotReachIsNotTaken(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root reaches a socket whatever its mode says")
	}
	t.Parallel()
	sock := filepath.Join(sockpath.Dir(t, "ipc"), "ipc.v1.sock")
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	if err := os.Chmod(sock, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(sock, 0o600) })

	srv := NewServer(sock, Handlers{})
	if err := srv.Listen(); !errors.Is(err, ErrSingleInstance) {
		t.Fatalf("the second Listen = %v, want ErrSingleInstance", err)
	}
}

// A peer whose queue is full does not answer the probe. The answer says
// nothing about the peer, so the state of the path is unknown and the path
// is not taken.
func TestABusySocketIsNotTaken(t *testing.T) {
	t.Parallel()
	sock := filepath.Join(sockpath.Dir(t, "ipc"), "ipc.v1.sock")

	// A raw listener with a queue of one: the first peer fills it and the
	// next one waits for room.
	fd, err := unix.Socket(unix.AF_UNIX, unix.SOCK_STREAM, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = unix.Close(fd) })
	if err := unix.Bind(fd, &unix.SockaddrUnix{Name: sock}); err != nil {
		t.Fatal(err)
	}
	if err := unix.Listen(fd, 1); err != nil {
		t.Fatal(err)
	}
	queued, err := net.Dial("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = queued.Close() })

	srv := NewServer(sock, Handlers{})
	if err := srv.Listen(); !errors.Is(err, ErrSingleInstance) {
		t.Fatalf("the second Listen = %v, want ErrSingleInstance", err)
	}
}

// The claim needs a directory the shell can write to. Without one the shell
// reports the error instead of serving a path nobody owns.
func TestTheClaimNeedsAWritableDirectory(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root writes to a directory whatever its mode says")
	}
	t.Parallel()
	dir := filepath.Join(sockpath.Dir(t, "ipc"), "run")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })

	srv := NewServer(filepath.Join(dir, "ipc.v1.sock"), Handlers{})
	err := srv.Listen()
	if err == nil {
		t.Fatal("the Listen succeeded, want the claim error")
	}
	if !errors.Is(err, unix.EACCES) {
		t.Fatalf("the Listen = %v, want a permission error", err)
	}
}

// The claim names the owner of the path. A shell that finds the claim held
// by a peer that answers the path reports the path as taken at once.
func TestAClaimHeldByAnotherShellIsNotTaken(t *testing.T) {
	t.Parallel()
	dir := sockpath.Dir(t, "ipc")
	sock := filepath.Join(dir, "ipc.v1.sock")
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })

	claim, err := os.OpenFile(sock+".claim", os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = claim.Close() })
	if err := unix.Flock(int(claim.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		t.Fatal(err)
	}

	srv := NewServer(sock, Handlers{})
	if err := srv.claimPath(); !errors.Is(err, ErrSingleInstance) {
		t.Fatalf("the claim = %v, want ErrSingleInstance", err)
	}
}

// A path the kernel cannot carry holds no peer. The shell reports the bind
// error instead of claiming the path.
func TestAPathTheKernelCannotCarryIsReported(t *testing.T) {
	t.Parallel()
	dir := sockpath.Dir(t, "ipc")
	sock := filepath.Join(dir, strings.Repeat("s", 120))
	srv := NewServer(sock, Handlers{})
	err := srv.Listen()
	if err == nil {
		t.Fatal("the Listen succeeded, want the bind error")
	}
	if errors.Is(err, ErrSingleInstance) {
		t.Fatalf("the Listen = %v, want the bind error", err)
	}
}

// Something that is not a socket sits at the path. The shell reports the
// error instead of taking a path it cannot serve.
func TestAPathThatIsNotASocketIsReported(t *testing.T) {
	t.Parallel()
	dir := sockpath.Dir(t, "ipc")
	sock := filepath.Join(dir, "ipc.v1.sock")
	if err := os.Mkdir(sock, 0o700); err != nil {
		t.Fatal(err)
	}
	// The directory holds a file, so it cannot be replaced by a socket.
	if err := os.WriteFile(filepath.Join(sock, "keep"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	srv := NewServer(sock, Handlers{})
	err := srv.Listen()
	if err == nil {
		t.Fatal("the Listen succeeded, want the error")
	}
	if errors.Is(err, ErrSingleInstance) {
		t.Fatalf("the Listen = %v, want the error", err)
	}
}

// The socket lives in a directory. A file in its place is reported.
func TestTheSocketDirectoryMustBeADirectory(t *testing.T) {
	t.Parallel()
	dir := sockpath.Dir(t, "ipc")
	file := filepath.Join(dir, "run")
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	srv := NewServer(filepath.Join(file, "ipc.v1.sock"), Handlers{})
	if err := srv.Listen(); err == nil {
		t.Fatal("the Listen succeeded, want the directory error")
	}
}
