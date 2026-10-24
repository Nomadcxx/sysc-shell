package main

import (
	"bytes"
	"encoding/binary"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"

	"github.com/Nomadcxx/sysc-shell/internal/sockpath"
)

func TestTraceLabelsDamageTypeAndBufferSize(t *testing.T) {
	var log bytes.Buffer
	tr := newTracker(&log, &sync.Mutex{})
	tr.kind[12] = "wl_shm_pool"
	createBuffer := make([]byte, 24)
	for i, value := range []uint32{21, 0, 640, 400, 2560, 0} {
		binary.LittleEndian.PutUint32(createBuffer[i*4:], value)
	}
	tr.request(12, 0, createBuffer) // wl_shm_pool.create_buffer
	tr.kind[7] = "wl_surface"
	tr.surfaces[7] = "panel#7"
	attach := make([]byte, 12)
	binary.LittleEndian.PutUint32(attach, 21)
	tr.request(7, 1, attach) // wl_surface.attach

	area := make([]byte, 16)
	for i, value := range []uint32{1, 2, 30, 40} {
		binary.LittleEndian.PutUint32(area[i*4:], value)
	}
	tr.request(7, 9, area) // wl_surface.damage_buffer
	tr.request(7, 6, nil)  // wl_surface.commit
	tr.request(7, 2, area) // wl_surface.damage
	tr.request(7, 6, nil)  // wl_surface.commit

	got := log.String()
	if strings.Count(got, "damage_buffer (1,2 30x40)") != 1 {
		t.Fatalf("damage_buffer report missing or mislabeled: %q", got)
	}
	if strings.Count(got, "damage (1,2 30x40)") != 1 {
		t.Fatalf("surface damage report missing or mislabeled: %q", got)
	}
	if !strings.Contains(got, "buf 640x400") {
		t.Fatalf("attached buffer size missing: %q", got)
	}
}

func TestTraceLabelsLayerSurfaceWithNamespace(t *testing.T) {
	tr := newTracker(&bytes.Buffer{}, &sync.Mutex{})
	tr.kind[4] = "zwlr_layer_shell_v1"

	args := make([]byte, 16)
	binary.LittleEndian.PutUint32(args[0:], 12) // new layer surface
	binary.LittleEndian.PutUint32(args[4:], 7)  // wl_surface
	binary.LittleEndian.PutUint32(args[12:], 2) // layer
	args = appendWaylandString(args, "sysc-shell")
	tr.request(4, 0, args)

	if got := tr.surfaces[7]; got != "sysc-shell#7" {
		t.Fatalf("surface label = %q, want sysc-shell#7", got)
	}
}

func TestWriteUnixMessageForwardsFileDescriptor(t *testing.T) {
	path := filepath.Join(sockpath.Dir(t, "wlspy"), "wayland.sock")
	listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()

	sender, err := net.DialUnix("unix", nil, &net.UnixAddr{Name: path, Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	defer sender.Close()
	receiver, err := listener.AcceptUnix()
	if err != nil {
		t.Fatal(err)
	}
	defer receiver.Close()

	file, err := os.Open("/dev/null")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if err := writeUnixMessage(sender, []byte("wayland"), syscall.UnixRights(int(file.Fd()))); err != nil {
		t.Fatal(err)
	}

	data := make([]byte, 16)
	oob := make([]byte, syscall.CmsgSpace(4))
	n, oobn, flags, _, err := receiver.ReadMsgUnix(data, oob)
	if err != nil {
		t.Fatal(err)
	}
	if flags&syscall.MSG_CTRUNC != 0 || string(data[:n]) != "wayland" {
		t.Fatalf("received data %q with flags %#x", data[:n], flags)
	}
	messages, err := syscall.ParseSocketControlMessage(oob[:oobn])
	if err != nil || len(messages) != 1 {
		t.Fatalf("received control messages %d, err %v", len(messages), err)
	}
	fds, err := syscall.ParseUnixRights(&messages[0])
	if err != nil || len(fds) != 1 {
		t.Fatalf("received descriptors %d, err %v", len(fds), err)
	}
	_ = syscall.Close(fds[0])
}

func TestSocketPathRejectsListenTraversal(t *testing.T) {
	if _, err := socketPath(t.TempDir(), "../wayland-1", false); err == nil {
		t.Fatal("socket path traversal was accepted")
	}
}

func appendWaylandString(dst []byte, value string) []byte {
	length := uint32(len(value) + 1)
	start := len(dst)
	padded := (int(length) + 3) &^ 3
	dst = append(dst, make([]byte, 4+padded)...)
	binary.LittleEndian.PutUint32(dst[start:], length)
	copy(dst[start+4:], value)
	return dst
}
