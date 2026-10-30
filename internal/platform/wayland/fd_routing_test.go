package wayland

import (
	"encoding/binary"
	"net"
	"os"
	"syscall"
	"testing"
	"time"

	"github.com/Nomadcxx/sysc-shell/internal/platform/wayland/layershell"
	"github.com/Nomadcxx/sysc-wayland/client"
)

func TestDispatchRoutesKeymapFDsAfterCoalescedEvents(t *testing.T) {
	t.Run("layer configure", func(t *testing.T) {
		o, peer := newDispatchTestOwner(t)
		layer := &layershell.ZwlrLayerSurfaceV1{}
		o.display.Context().Register(layer)
		layer.SetConfigureHandler(func(layershell.ZwlrLayerSurfaceV1ConfigureEvent) {})
		keyboard := client.NewKeyboard(o.display.Context())

		configure := make([]byte, 12)
		binary.LittleEndian.PutUint32(configure, 7)
		binary.LittleEndian.PutUint32(configure[4:], 1920)
		binary.LittleEndian.PutUint32(configure[8:], 32)
		routeKeymapFD(t, o, peer, fdRoutingFrame(layer.ID(), 0, configure), keyboard)
	})

	t.Run("callback done control", func(t *testing.T) {
		o, peer := newDispatchTestOwner(t)
		callback := client.NewCallback(o.display.Context())
		callback.SetDoneHandler(func(client.CallbackDoneEvent) {})
		keyboard := client.NewKeyboard(o.display.Context())

		done := make([]byte, 4)
		binary.LittleEndian.PutUint32(done, 1)
		routeKeymapFD(t, o, peer, fdRoutingFrame(callback.ID(), 0, done), keyboard)
	})
}

func routeKeymapFD(t *testing.T, o *owner, peer *net.UnixConn, firstFrame []byte, keyboard *client.Keyboard) {
	t.Helper()

	keymapFD := -1
	keyboard.SetKeymapHandler(func(event client.KeyboardKeymapEvent) {
		keymapFD = event.Fd
		if event.Fd >= 0 {
			_ = syscall.Close(event.Fd)
		}
	})

	source, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = source.Close() })

	keymap := make([]byte, 8)
	binary.LittleEndian.PutUint32(keymap, 1)
	binary.LittleEndian.PutUint32(keymap[4:], 4)
	frames := append(firstFrame, fdRoutingFrame(keyboard.ID(), 0, keymap)...)
	before := fdRoutingOpenFDCount(t)
	n, oobn, err := peer.WriteMsgUnix(frames, syscall.UnixRights(int(source.Fd())), nil)
	if err != nil {
		t.Fatalf("write coalesced events: %v", err)
	}
	if n != len(frames) || oobn == 0 {
		t.Fatalf("WriteMsgUnix() = (%d, %d), want (%d, rights attached)", n, oobn, len(frames))
	}

	ctx := o.display.Context()
	if err := ctx.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := ctx.Dispatch(); err != nil {
			t.Fatalf("dispatch coalesced event: %v", err)
		}
	}
	if keymapFD < 0 {
		t.Error("keymap event did not receive its descriptor")
	}
	if after := fdRoutingOpenFDCount(t); after != before {
		t.Errorf("open file descriptors after dispatch = %d, before = %d; want no leaked descriptor", after, before)
	}
}

func fdRoutingFrame(sender, opcode uint32, body []byte) []byte {
	frame := make([]byte, 8+len(body))
	binary.LittleEndian.PutUint32(frame, sender)
	binary.LittleEndian.PutUint32(frame[4:], uint32(len(frame))<<16|opcode)
	copy(frame[8:], body)
	return frame
}

func fdRoutingOpenFDCount(t *testing.T) int {
	t.Helper()
	entries, err := os.ReadDir("/proc/self/fd")
	if err != nil {
		t.Fatal(err)
	}
	return len(entries)
}
