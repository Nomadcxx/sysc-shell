// Command wlspy proxies one Wayland client to the compositor and logs the
// buffer size and damage rectangles at each surface commit.
package main

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
)

type damage struct {
	kind string
	x, y int32
	w, h int32
}

type tracker struct {
	mu       sync.Mutex
	out      io.Writer
	logMu    *sync.Mutex
	kind     map[uint32]string
	surfaces map[uint32]string
	buffers  map[uint32][2]int32
	attached map[uint32][2]int32
	pending  map[uint32][]damage
}

func newTracker(out io.Writer, logMu *sync.Mutex) *tracker {
	return &tracker{
		out:      out,
		logMu:    logMu,
		kind:     map[uint32]string{},
		surfaces: map[uint32]string{},
		buffers:  map[uint32][2]int32{},
		attached: map[uint32][2]int32{},
		pending:  map[uint32][]damage{},
	}
}

func (t *tracker) logf(format string, args ...any) {
	t.logMu.Lock()
	defer t.logMu.Unlock()
	if _, err := fmt.Fprintf(t.out, "%s %s\n", time.Now().Format("15:04:05.000"), fmt.Sprintf(format, args...)); err != nil {
		log.Printf("wlspy: write trace: %v", err)
	}
}

func readU32(b []byte, i int) (uint32, bool) {
	start := i * 4
	if i < 0 || start < 0 || len(b)-start < 4 {
		return 0, false
	}
	return binary.LittleEndian.Uint32(b[start:]), true
}

func readWaylandString(b []byte) (string, []byte, bool) {
	if len(b) < 4 {
		return "", nil, false
	}
	n := int(binary.LittleEndian.Uint32(b))
	if n < 1 || n > len(b)-4 {
		return "", nil, false
	}
	padded := (n + 3) &^ 3
	if len(b)-4 < padded || b[4+n-1] != 0 {
		return "", nil, false
	}
	return string(b[4 : 4+n-1]), b[4+padded:], true
}

func (t *tracker) deleteID(id uint32) {
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.kind, id)
	delete(t.surfaces, id)
	delete(t.buffers, id)
	delete(t.attached, id)
	delete(t.pending, id)
}

func (t *tracker) request(obj uint32, op uint16, args []byte) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if obj == 1 && op == 1 { // wl_display.get_registry
		if id, ok := readU32(args, 0); ok {
			t.kind[id] = "wl_registry"
		}
		return
	}

	switch kind := t.kind[obj]; {
	case kind == "wl_registry" && op == 0: // bind
		if len(args) < 4 {
			return
		}
		iface, rest, ok := readWaylandString(args[4:])
		if !ok {
			return
		}
		if id, ok := readU32(rest, 1); ok {
			t.kind[id] = iface
		}
	case kind == "wl_shm" && op == 0: // create_pool
		if id, ok := readU32(args, 0); ok {
			t.kind[id] = "wl_shm_pool"
		}
	case kind == "wl_shm_pool" && op == 0: // create_buffer
		id, idOK := readU32(args, 0)
		w, wOK := readU32(args, 2)
		h, hOK := readU32(args, 3)
		if idOK && wOK && hOK {
			t.kind[id] = "wl_buffer"
			t.buffers[id] = [2]int32{int32(w), int32(h)}
		}
	case kind == "wl_compositor" && op == 0: // create_surface
		if id, ok := readU32(args, 0); ok {
			t.kind[id] = "wl_surface"
			t.surfaces[id] = fmt.Sprintf("surface#%d", id)
		}
	case kind == "zwlr_layer_shell_v1" && op == 0: // get_layer_surface
		surface, surfaceOK := readU32(args, 1)
		name, _, nameOK := "", []byte(nil), false
		if len(args) >= 16 {
			name, _, nameOK = readWaylandString(args[16:])
		}
		if surfaceOK && nameOK {
			t.surfaces[surface] = fmt.Sprintf("%s#%d", name, surface)
			t.logf("map %s", t.surfaces[surface])
		}
	case kind == "wl_surface":
		t.surfaceRequest(obj, op, args)
	}
}

func (t *tracker) surfaceRequest(obj uint32, op uint16, args []byte) {
	label := t.surfaces[obj]
	switch op {
	case 0: // destroy
		t.logf("destroy %s", label)
	case 1: // attach
		id, ok := readU32(args, 0)
		if ok {
			t.attached[obj] = t.buffers[id]
		}
	case 2, 9: // damage, damage_buffer
		var rect [4]int32
		for i := range rect {
			value, ok := readU32(args, i)
			if !ok {
				return
			}
			rect[i] = int32(value)
		}
		kind := "damage"
		if op == 9 {
			kind = "damage_buffer"
		}
		t.pending[obj] = append(t.pending[obj], damage{kind: kind, x: rect[0], y: rect[1], w: rect[2], h: rect[3]})
	case 6: // commit
		if damages := t.pending[obj]; len(damages) > 0 {
			size := t.attached[obj]
			parts := make([]string, 0, len(damages))
			for _, d := range damages {
				parts = append(parts, fmt.Sprintf("%s (%d,%d %dx%d)", d.kind, d.x, d.y, d.w, d.h))
			}
			t.logf("commit %s buf %dx%d %s", label, size[0], size[1], strings.Join(parts, " "))
		}
		delete(t.pending, obj)
	}
}

func pump(src, dst *net.UnixConn, parse func([]byte), closeBoth func(), direction string) {
	defer closeBoth()
	buf := make([]byte, 1<<16)
	oob := make([]byte, syscall.CmsgSpace(4*64))
	var acc []byte
	for {
		n, oobn, flags, _, readErr := src.ReadMsgUnix(buf, oob)
		if n == 0 && oobn == 0 {
			if readErr != nil && !errors.Is(readErr, net.ErrClosed) && !errors.Is(readErr, io.EOF) {
				log.Printf("wlspy: %s read: %v", direction, readErr)
			}
			return
		}
		if flags&(syscall.MSG_CTRUNC|syscall.MSG_TRUNC) != 0 {
			closeRights(oob[:oobn])
			log.Printf("wlspy: %s read truncated Wayland data", direction)
			return
		}
		acc = append(acc, buf[:n]...)
		for len(acc) >= 8 {
			word := binary.LittleEndian.Uint32(acc[4:])
			size := int(word >> 16)
			if size < 8 {
				closeRights(oob[:oobn])
				log.Printf("wlspy: %s received invalid Wayland message size %d", direction, size)
				return
			}
			if len(acc) < size {
				break
			}
			parse(acc[:size])
			acc = acc[size:]
		}
		if err := writeUnixMessage(dst, buf[:n], oob[:oobn]); err != nil {
			closeRights(oob[:oobn])
			if !errors.Is(err, net.ErrClosed) {
				log.Printf("wlspy: %s write: %v", direction, err)
			}
			return
		}
		closeRights(oob[:oobn])
		if readErr != nil {
			return
		}
	}
}

func writeUnixMessage(dst *net.UnixConn, data, oob []byte) error {
	n, oobn, err := dst.WriteMsgUnix(data, oob, nil)
	if err != nil {
		return err
	}
	if oobn != len(oob) {
		return io.ErrShortWrite
	}
	for len(data)-n > 0 {
		written, err := dst.Write(data[n:])
		if err != nil {
			return err
		}
		if written == 0 {
			return io.ErrShortWrite
		}
		n += written
	}
	return nil
}

func closeRights(oob []byte) {
	msgs, err := syscall.ParseSocketControlMessage(oob)
	if err != nil {
		return
	}
	for i := range msgs {
		fds, err := syscall.ParseUnixRights(&msgs[i])
		if err != nil {
			continue
		}
		for _, fd := range fds {
			_ = syscall.Close(fd)
		}
	}
}

func socketPath(runtimeDir, name string, allowAbsolute bool) (string, error) {
	if name == "" {
		return "", errors.New("empty socket name")
	}
	if filepath.IsAbs(name) {
		if !allowAbsolute {
			return "", fmt.Errorf("socket name %q must be relative to XDG_RUNTIME_DIR", name)
		}
		return filepath.Clean(name), nil
	}
	if filepath.Base(name) != name || name == "." || name == ".." {
		return "", fmt.Errorf("socket name %q must be a single path component", name)
	}
	return filepath.Join(runtimeDir, name), nil
}

func run(args []string) error {
	if len(args) != 3 {
		return errors.New("usage: wlspy LISTEN_NAME UPSTREAM_NAME LOGFILE (socket names are relative to XDG_RUNTIME_DIR)")
	}
	runtimeDir := os.Getenv("XDG_RUNTIME_DIR")
	if runtimeDir == "" {
		return errors.New("XDG_RUNTIME_DIR is not set")
	}
	listen, err := socketPath(runtimeDir, args[0], false)
	if err != nil {
		return err
	}
	upstream, err := socketPath(runtimeDir, args[1], true)
	if err != nil {
		return err
	}
	if _, err := os.Lstat(listen); err == nil {
		return fmt.Errorf("listen path already exists: %s (remove a stale socket first)", listen)
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	logFile, err := os.OpenFile(args[2], os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer logFile.Close()
	ln, err := net.ListenUnix("unix", &net.UnixAddr{Name: listen, Net: "unix"})
	if err != nil {
		return err
	}
	defer ln.Close()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		_ = ln.Close()
	}()
	var logMu sync.Mutex
	for {
		client, err := ln.AcceptUnix()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
		server, err := net.DialUnix("unix", nil, &net.UnixAddr{Name: upstream, Net: "unix"})
		if err != nil {
			client.Close()
			log.Printf("wlspy: connect to compositor: %v", err)
			continue
		}
		// ponytail: serialize traced clients; add concurrent sessions if a gate needs them.
		serveClient(ctx, client, server, newTracker(logFile, &logMu))
		if ctx.Err() != nil {
			return nil
		}
	}
}

func serveClient(ctx context.Context, client, server *net.UnixConn, tr *tracker) {
	var once sync.Once
	done := make(chan struct{})
	closeBoth := func() {
		once.Do(func() {
			client.Close()
			server.Close()
			close(done)
		})
	}
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		pump(client, server, func(message []byte) {
			if len(message) >= 8 {
				word := binary.LittleEndian.Uint32(message[4:])
				tr.request(binary.LittleEndian.Uint32(message), uint16(word), message[8:])
			}
		}, closeBoth, "client to compositor")
	}()
	go func() {
		defer wg.Done()
		pump(server, client, func(message []byte) {
			if len(message) >= 12 && binary.LittleEndian.Uint32(message) == 1 && uint16(binary.LittleEndian.Uint32(message[4:])) == 1 {
				if id, ok := readU32(message[8:], 0); ok {
					tr.deleteID(id)
				}
			}
		}, closeBoth, "compositor to client")
	}()
	select {
	case <-ctx.Done():
		closeBoth()
	case <-done:
	}
	wg.Wait()
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		log.Print(err)
		os.Exit(1)
	}
}
