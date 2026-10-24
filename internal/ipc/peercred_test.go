package ipc

import (
	"context"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/Nomadcxx/sysc-shell/internal/sockpath"
)

// TestServeRejectsAForeignPeer covers the rejection path of the only
// authentication check on the control socket. A same-uid process cannot fake
// SO_PEERCRED, so the test stands in a peer from another uid and asserts the
// server closes the connection without answering it.
func TestServeRejectsAForeignPeer(t *testing.T) {
	sock := filepath.Join(sockpath.Dir(t, "ipc"), "ipc.v1.sock")
	served := false
	srv := NewServer(sock, Handlers{
		Panel: func(string, string, string) error {
			served = true
			return nil
		},
	})
	srv.peerUID = func(net.Conn) (uint32, bool) { return uint32(os.Getuid()) + 1, true }
	ctx, cancel := context.WithCancel(context.Background())
	errc := make(chan error, 1)
	go func() { errc <- srv.Serve(ctx) }()
	t.Cleanup(func() { cancel(); _ = srv.Close() })
	waitSock(t, sock)

	conn, err := net.Dial("unix", sock)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()
	if err := conn.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 128)
	n, err := conn.Read(buf)
	if err == nil {
		t.Fatalf("the server answered a peer from another uid: %s", buf[:n])
	}
	if !errors.Is(err, io.EOF) && !errors.Is(err, syscall.ECONNRESET) {
		t.Fatalf("read error = %v, want the connection closed without an answer", err)
	}
	if served {
		t.Fatal("the request of a peer from another uid was served")
	}
}

// TestSameUserAcceptsTheShellItself covers the success path of the real
// credential read: the shell's own peer is admitted.
func TestSameUserAcceptsTheShellItself(t *testing.T) {
	path := filepath.Join(sockpath.Dir(t, "ipc"), "peer.sock")
	ln, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	accepted := make(chan net.Conn, 1)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			close(accepted)
			return
		}
		accepted <- conn
	}()
	client, err := net.Dial("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	peer, ok := <-accepted
	if !ok {
		t.Fatal("accept failed")
	}
	defer peer.Close()
	if !NewServer("", Handlers{}).sameUser(peer) {
		t.Fatal("the shell's own peer was refused")
	}
}

// TestSameUserRefusesAConnThatIsNotAUnixSocket covers the type assertion: a
// connection that never carried SO_PEERCRED cannot be a known peer.
func TestSameUserRefusesAConnThatIsNotAUnixSocket(t *testing.T) {
	client, peer := net.Pipe()
	defer client.Close()
	defer peer.Close()
	if NewServer("", Handlers{}).sameUser(client) {
		t.Fatal("a connection that is not a unix socket was admitted")
	}
}

// TestSameUIDComparesThePeerWithTheShell is the table test of the one
// comparison the gate makes.
func TestSameUIDComparesThePeerWithTheShell(t *testing.T) {
	cases := []struct {
		name       string
		peer, self uint32
		want       bool
	}{
		{"the same uid", 1000, 1000, true},
		{"another uid", 1001, 1000, false},
		{"root against a user", 0, 1000, false},
		{"a user against root", 1000, 0, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := sameUID(tc.peer, tc.self); got != tc.want {
				t.Fatalf("sameUID(%d, %d) = %v, want %v", tc.peer, tc.self, got, tc.want)
			}
		})
	}
}
