package wayland

import (
	"encoding/binary"
	"errors"
	"io"
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

func TestAuxBarrierWaitsForCompositorCallback(t *testing.T) {
	o, peer := newDispatchTestOwner(t)
	s := newHostSet()
	mappedHost(s, 7, "DP-1")
	o.hosts = s
	reply := make(chan error, 1)
	o.handleAux(AuxRequest{Output: 7, Barrier: true, Reply: reply})

	if err := peer.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	request := make([]byte, 12)
	if _, err := io.ReadFull(peer, request); err != nil {
		t.Fatalf("read wl_display.sync request: %v", err)
	}
	if objectID, sizeAndOpcode := binary.LittleEndian.Uint32(request), binary.LittleEndian.Uint32(request[4:]); objectID != 1 || sizeAndOpcode != 12<<16 {
		t.Fatalf("wl_display.sync header = object %d, word %#x", objectID, sizeAndOpcode)
	}
	callbackID := binary.LittleEndian.Uint32(request[8:])
	select {
	case err := <-reply:
		t.Fatalf("barrier replied before compositor callback: %v", err)
	default:
	}

	event := make([]byte, 12)
	binary.LittleEndian.PutUint32(event, callbackID)
	binary.LittleEndian.PutUint32(event[4:], 12<<16)
	if _, err := peer.Write(event); err != nil {
		t.Fatalf("write wl_callback.done event: %v", err)
	}
	if err := o.display.Context().SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if err := o.display.Context().Dispatch(); err != nil {
		t.Fatalf("dispatch wl_callback.done: %v", err)
	}
	if err := <-reply; err != nil {
		t.Fatalf("barrier reply = %v", err)
	}
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
