package wayland

import (
	"errors"
	"net"
	"path/filepath"
	"testing"
	"time"

	"github.com/Nomadcxx/sysc-wayland/client"
)

func newDispatchTestOwner(t *testing.T) (*owner, *net.UnixConn) {
	t.Helper()

	listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: filepath.Join(t.TempDir(), "wayland.sock"), Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()

	display, err := client.Connect(listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = display.Context().Close() })

	peer, err := listener.AcceptUnix()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = peer.Close() })
	return &owner{display: display}, peer
}

func TestDispatchAllContinuesAfterIdleReadDeadline(t *testing.T) {
	o, _ := newDispatchTestOwner(t)

	if err := o.display.Context().SetReadDeadline(time.Now().Add(10 * time.Millisecond)); err != nil {
		t.Fatal(err)
	}
	if err := o.dispatchAll(-1); err != nil {
		t.Fatalf("dispatchAll() = %v after an idle read deadline, want nil", err)
	}
}

func TestDispatchAllPropagatesPartialHeaderTimeout(t *testing.T) {
	o, peer := newDispatchTestOwner(t)
	if _, err := peer.Write([]byte{1, 2, 3}); err != nil {
		t.Fatal(err)
	}
	if err := o.display.Context().SetReadDeadline(time.Now().Add(10 * time.Millisecond)); err != nil {
		t.Fatal(err)
	}

	err := o.dispatchAll(-1)
	if err == nil || errors.Is(err, client.ErrReadTimeout) {
		t.Fatalf("dispatchAll() = %v after a partial header, want a fatal read error", err)
	}
}
