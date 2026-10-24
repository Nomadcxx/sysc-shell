package ipc

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

// TestSameUIDComparesThePeerAndTheShell covers the comparison on its own.
// The uid of a peer cannot be forged from a same-uid process, so the
// comparison is the only part of the gate a table test can reach directly.
func TestSameUIDComparesThePeerAndTheShell(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		peer uint32
		self uint32
		want bool
	}{
		{"the same user", 1000, 1000, true},
		{"another user", 1001, 1000, false},
		{"root peer", 0, 1000, false},
		{"root shell", 1000, 0, false},
		{"both root", 0, 0, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := sameUID(tc.peer, tc.self); got != tc.want {
				t.Fatalf("sameUID(%d, %d) = %t, want %t", tc.peer, tc.self, got, tc.want)
			}
		})
	}
}

// TestPeerCredentialsReportTheOwningUser covers the success path against a
// real socket: the peer of a connection this process opened is this process.
func TestPeerCredentialsReportTheOwningUser(t *testing.T) {
	t.Parallel()
	sock := filepath.Join(t.TempDir(), "ipc.v1.sock")
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer func() { _ = ln.Close() }()
	go func() {
		conn, err := ln.Accept()
		if err == nil {
			_ = conn.Close()
		}
	}()
	conn, err := net.Dial("unix", sock)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = conn.Close() }()

	uid, ok := peerCredentials(conn)
	if !ok {
		t.Fatalf("the peer credentials were not available")
	}
	if uid != uint32(os.Getuid()) {
		t.Fatalf("peer uid = %d, want %d", uid, os.Getuid())
	}
	if !(&Server{}).sameUser(conn) {
		t.Fatalf("sameUser refused a peer that owns the socket")
	}
}

// TestServeClosesAForeignPeerWithoutAnswering is the rejection path the gate
// exists for: a peer from another user is dropped without a reply.
func TestServeClosesAForeignPeerWithoutAnswering(t *testing.T) {
	t.Parallel()
	var asked atomic.Int64
	sock := serveWithPeer(t, func(net.Conn) (uint32, bool) {
		asked.Add(1)
		return uint32(os.Getuid()) + 1, true
	})

	conn, err := net.Dial("unix", sock)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = conn.Close() }()
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	// The server may close the connection before this write lands; that is
	// the outcome this test wants, so a write error is not a failure here.
	_, _ = conn.Write([]byte(`{"id":1,"method":"status"}` + "\n"))

	buf := make([]byte, 128)
	n, err := conn.Read(buf)
	if n > 0 {
		t.Fatalf("the server answered a peer from another user: %q", buf[:n])
	}
	if err == nil {
		t.Fatalf("the server kept a peer from another user connected")
	}
	if ne, ok := err.(net.Error); ok && ne.Timeout() {
		t.Fatalf("the server did not close the connection of a peer from another user")
	}
	if asked.Load() == 0 {
		t.Fatalf("the server did not ask for the peer credentials")
	}
}

// TestServeClosesAPeerWhoseCredentialsAreUnknown covers the other half of the
// gate: a peer whose uid cannot be read is not the shell's user.
func TestServeClosesAPeerWhoseCredentialsAreUnknown(t *testing.T) {
	t.Parallel()
	sock := serveWithPeer(t, func(net.Conn) (uint32, bool) { return 0, false })

	conn, err := net.Dial("unix", sock)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = conn.Close() }()
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, _ = conn.Write([]byte(`{"id":1,"method":"status"}` + "\n"))

	buf := make([]byte, 128)
	n, err := conn.Read(buf)
	if n > 0 {
		t.Fatalf("the server answered a peer with unknown credentials: %q", buf[:n])
	}
	if err == nil {
		t.Fatalf("the server kept a peer with unknown credentials connected")
	}
	if ne, ok := err.(net.Error); ok && ne.Timeout() {
		t.Fatalf("the server did not close the connection of a peer with unknown credentials")
	}
}

// serveWithPeer starts a server whose peer credentials come from the given
// lookup, so a test can present a peer the process cannot present on its own.
func serveWithPeer(t *testing.T, peer func(net.Conn) (uint32, bool)) string {
	t.Helper()
	sock := filepath.Join(t.TempDir(), "ipc.v1.sock")
	ctx, cancel := context.WithCancel(context.Background())
	srv := NewServer(sock, Handlers{
		Status: func() map[string]any { return map[string]any{"version": "test"} },
	})
	srv.peerUID = peer
	go func() { _ = srv.Serve(ctx) }()
	t.Cleanup(func() { cancel(); _ = srv.Close() })
	waitSock(t, sock)
	return sock
}
