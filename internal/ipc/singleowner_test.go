package ipc

import (
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Nomadcxx/sysc-shell/internal/sockpath"
)

// leaveStaleSocket puts a dead socket at path: a bound socket is renamed
// onto the path and then closed, so the file there is a socket that nobody
// listens on.
func leaveStaleSocket(t *testing.T, path string) {
	t.Helper()
	live := path + ".live"
	ln, err := net.Listen("unix", live)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(live, path); err != nil {
		t.Fatal(err)
	}
	if err := ln.Close(); err != nil {
		t.Fatal(err)
	}
}

// Two shells that recover the same dead socket must not both end up with a
// listener. The path names one owner, so the second shell reports the path
// as taken.
func TestListenConcurrentStaleRecoverySingleOwner(t *testing.T) {
	t.Parallel()
	sock := filepath.Join(sockpath.Dir(t, "ipc"), "ipc.v1.sock")
	leaveStaleSocket(t, sock)

	const racers = 2
	results := make(chan error, racers)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	for i := 0; i < racers; i++ {
		srv := NewServer(sock, Handlers{Status: func() map[string]any { return map[string]any{} }})
		go func() { results <- srv.Serve(ctx) }()
	}

	// The owner serves until the context ends, so the first result belongs
	// to the shell that lost the path.
	select {
	case err := <-results:
		if !errors.Is(err, ErrSingleInstance) {
			t.Fatalf("the losing Serve = %v, want ErrSingleInstance", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("two servers took the same socket path")
	}
	cancel()
	select {
	case err := <-results:
		if err != nil {
			t.Fatalf("the owning Serve = %v, want nil", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the owning server did not stop")
	}
}

// A live owner's socket must never be unlinked by a shell that recovers the
// path. The contract: the second shell reports the path as taken and leaves
// the owner's listener alone.
func TestListenDoesNotUnlinkLivePeer(t *testing.T) {
	t.Parallel()
	sock := filepath.Join(sockpath.Dir(t, "ipc"), "ipc.v1.sock")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	first := NewServer(sock, Handlers{Status: func() map[string]any { return map[string]any{} }})
	go func() { _ = first.Serve(ctx) }()
	t.Cleanup(func() { cancel(); _ = first.Close() })
	waitSock(t, sock)

	// A spurious removal of the path models the recovery step of a shell
	// that believes the socket is dead.
	if err := os.Remove(sock); err != nil {
		t.Fatal(err)
	}

	second := NewServer(sock, Handlers{})
	if err := second.Listen(); !errors.Is(err, ErrSingleInstance) {
		t.Fatalf("the second Listen = %v, want ErrSingleInstance", err)
	}
	if _, err := os.Stat(sock); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("the second shell took the path: stat = %v", err)
	}
	if first.listener() == nil {
		t.Fatal("the owner lost its listener")
	}
}

// The socket stays private to its user and the directory that holds it
// stays private to the owner of the runtime directory.
func TestListenKeepsTheSocketPrivate(t *testing.T) {
	t.Parallel()
	dir := filepath.Join(sockpath.Dir(t, "ipc"), "run")
	sock := filepath.Join(dir, "ipc.v1.sock")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	srv := NewServer(sock, Handlers{})
	go func() { _ = srv.Serve(ctx) }()
	t.Cleanup(func() { cancel(); _ = srv.Close() })
	waitSock(t, sock)

	info, err := os.Stat(sock)
	if err != nil {
		t.Fatal(err)
	}
	if mode := info.Mode().Perm(); mode != 0o600 {
		t.Fatalf("the socket mode = %o, want 600", mode)
	}
	dirInfo, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	if mode := dirInfo.Mode().Perm(); mode != 0o700 {
		t.Fatalf("the directory mode = %o, want 700", mode)
	}
}
