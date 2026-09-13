package wayland

import (
	"errors"

	"github.com/Nomadcxx/sysc-shell/internal/platform/wayland/screencopy"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
	"github.com/Nomadcxx/sysc-wayland/client"
	"golang.org/x/sys/unix"
)

// maxCaptureBytes bounds one capture allocation. The largest sensible capture is
// a whole output, and this machine's is 19 MiB, so this refuses a nonsense
// geometry while leaving room for a much larger display.
const maxCaptureBytes = 64 << 20

// backdropDownsample is the factor a backdrop is reduced by before blurring,
// and kept at afterwards. Design D4: a blur is a low-pass filter, so the detail
// dropped by downsampling is detail the blur would have destroyed anyway, and
// keeping the result reduced is what turns a 2.6x saving into 8.8x.
const backdropDownsample = 4

// captureBuffer is one wl_shm buffer in a pool sized exactly for it.
//
// newGeneration cannot serve a capture. It always builds slotCount buffers, so
// its pool is a multiple of one buffer, and measured 2026-09-12 this Niri
// rejects a screencopy target whose pool is larger than the buffer itself with
// zwlr_screencopy_frame_v1.invalid_buffer -- a protocol error, which takes the
// whole connection down with it rather than just the backdrop. Pool size is the
// entire cause: the same geometry in an exactly sized pool is accepted, while
// memfd against a tmpfs file, and one buffer against two, change nothing.
//
// The compositor writes into this buffer and announces it with ready, and it is
// never attached to a surface, so it needs none of generation's retirement
// machinery: once ready has arrived the storage is ours to release.
type captureBuffer struct {
	fd     int
	data   []byte
	pool   *client.ShmPool
	buffer *client.Buffer
	stride int32
}

// newCaptureBuffer returns nil on every failure path, because a backdrop is
// decoration: a panel that cannot get one paints opaque.
func newCaptureBuffer(shm *client.Shm, width, height int32, format uint32) *captureBuffer {
	if shm == nil || width <= 0 || height <= 0 {
		return nil
	}
	stride := int64(width) * 4
	size := stride * int64(height)
	if size > maxCaptureBytes {
		return nil
	}

	fd, err := unix.MemfdCreate("sysc-shell-capture", unix.MFD_CLOEXEC)
	if err != nil {
		return nil
	}
	c := &captureBuffer{fd: fd, stride: int32(stride)}
	if err := unix.Ftruncate(fd, size); err != nil {
		_ = c.destroy()
		return nil
	}
	data, err := unix.Mmap(fd, 0, int(size), unix.PROT_READ|unix.PROT_WRITE, unix.MAP_SHARED)
	if err != nil {
		_ = c.destroy()
		return nil
	}
	c.data = data

	pool, err := shm.CreatePool(fd, int32(size))
	if err != nil {
		_ = c.destroy()
		return nil
	}
	c.pool = pool

	buffer, err := pool.CreateBuffer(0, width, height, int32(stride), format)
	if err != nil {
		_ = c.destroy()
		return nil
	}
	c.buffer = buffer
	return c
}

// destroy releases the buffer child-to-parent, as a generation does: buffer,
// pool, mapping, then the descriptor.
func (c *captureBuffer) destroy() error {
	var errs []error
	if c.buffer != nil {
		errs = append(errs, c.buffer.Destroy())
		c.buffer = nil
	}
	if c.pool != nil {
		errs = append(errs, c.pool.Destroy())
		c.pool = nil
	}
	if c.data != nil {
		errs = append(errs, unix.Munmap(c.data))
		c.data = nil
	}
	if c.fd >= 0 {
		errs = append(errs, unix.Close(c.fd))
		c.fd = -1
	}
	return errors.Join(errs...)
}

// captureRoundtripLimit bounds how many sync round trips one capture waits
// through. A compositor that accepts the frame and then never answers must not
// stall the owner goroutine, because that goroutine is also the render loop.
const captureRoundtripLimit = 32

// formatXRGB8888 is the same byte layout as formatARGB8888 with the fourth byte
// undefined instead of alpha. It is what a compositor offers for an opaque
// output: measured 2026-09-12, this Niri offers xrgb8888 and nothing else for a
// region capture, so a capture path that insists on argb8888 never captures.
const formatXRGB8888 = uint32(client.ShmFormatXrgb8888)

// captureBufferFits reports whether a buffer offer is one newGeneration can
// satisfy exactly.
//
// newGeneration always lays out stride = width*4, so an offer with any other
// stride cannot be honoured. Handing zwlr_screencopy_frame_v1.copy a buffer
// whose geometry or format disagrees with the offer raises invalid_buffer, and a
// protocol error tears down the whole connection -- the entire shell, for a
// decoration. Declining an offer we cannot match costs one backdrop instead.
//
// Both 8888 layouts are accepted because both are the canvas's byte order; only
// the fourth byte differs, and normaliseCapture fills it in when it carries no
// alpha.
func captureBufferFits(format, width, height, stride uint32) bool {
	if width == 0 || height == 0 || stride != width*4 {
		return false
	}
	return format == formatARGB8888 || format == formatXRGB8888
}

// normaliseCapture copies a captured buffer into canvas order.
//
// A compositor may report y_invert, meaning the first row in the buffer is the
// bottom row of the screen. And a buffer in an x-format carries no alpha at all,
// so its fourth byte is undefined: left alone it would reach the painter as a
// random per-pixel opacity, and a zero there would make the backdrop vanish.
// A screen capture is opaque by construction, so the fourth byte becomes 0xff,
// which is also premultiplication by one and therefore leaves the colours alone.
func normaliseCapture(dst, src []byte, rows, stride int, yInvert, forceOpaque bool) {
	for y := range rows {
		from := y
		if yInvert {
			from = rows - 1 - y
		}
		copy(dst[y*stride:(y+1)*stride], src[from*stride:(from+1)*stride])
	}
	if !forceOpaque {
		return
	}
	for i := 3; i < len(dst); i += 4 {
		dst[i] = 0xff
	}
}

// pumpUntil round trips until ready reports true, the connection fails, or the
// trip budget is spent. It reports whether ready became true.
//
// It must only be called from the owner goroutine and outside dispatch -- the
// loop body, which is where openAux runs -- never from inside an event handler:
// a roundtrip dispatches, and nesting dispatch re-enters the connection.
func (o *owner) pumpUntil(ready func() bool) bool {
	for trips := 0; trips < captureRoundtripLimit; trips++ {
		if ready() {
			return true
		}
		if o.fatal != nil || o.closed {
			return false
		}
		if err := o.display.Roundtrip(); err != nil {
			return false
		}
	}
	return ready()
}

// captureRegion copies one output region through zwlr_screencopy_frame_v1 and
// returns it as a premultiplied ARGB image.
//
// It returns nil rather than an error on every failure path. A backdrop is
// decoration: a panel that cannot get one paints opaque, and must never fail to
// open because the compositor declined a copy.
//
// The region is in output logical coordinates, which is the same space as
// Placement, so no conversion happens here. The returned image is in output
// buffer pixels; the caller reconciles that through Scale120.
func (o *owner) captureRegion(out *client.Output, r ui.Rect) *ui.Image {
	if o == nil || o.screencopy == nil || o.shm == nil || o.display == nil || out == nil {
		return nil
	}
	if r.W <= 0 || r.H <= 0 {
		return nil
	}

	// overlayCursor 0: a frozen pointer in the backdrop is an artefact.
	frame, err := o.screencopy.CaptureOutputRegion(0, out,
		int32(r.X), int32(r.Y), int32(r.W), int32(r.H))
	if err != nil {
		return nil
	}
	defer func() { _ = frame.Destroy() }()

	var (
		format        uint32
		width, height uint32
		offered       bool
		enumerated    bool
		finished      bool
		copied        bool
		yInvert       bool
	)

	frame.SetBufferHandler(func(e screencopy.ZwlrScreencopyFrameV1BufferEvent) {
		// Take the first offer we can allocate exactly and ignore the rest.
		if offered || !captureBufferFits(e.Format, e.Width, e.Height, e.Stride) {
			return
		}
		format, width, height, offered = e.Format, e.Width, e.Height, true
	})
	frame.SetBufferDoneHandler(func(screencopy.ZwlrScreencopyFrameV1BufferDoneEvent) {
		enumerated = true
	})
	frame.SetFlagsHandler(func(e screencopy.ZwlrScreencopyFrameV1FlagsEvent) {
		yInvert = e.Flags&uint32(screencopy.ZwlrScreencopyFrameV1FlagsYInvert) != 0
	})
	frame.SetReadyHandler(func(screencopy.ZwlrScreencopyFrameV1ReadyEvent) {
		copied, finished = true, true
	})
	frame.SetFailedHandler(func(screencopy.ZwlrScreencopyFrameV1FailedEvent) {
		copied, finished = false, true
	})

	// Version 3 terminates the offers with buffer_done, so wait for that rather
	// than for the first offer: a usable ARGB8888 offer may not be the first one
	// sent. A failure before any offer also ends the wait.
	if !o.pumpUntil(func() bool { return enumerated || finished }) || !offered {
		return nil
	}

	// The buffer must answer the offer in its own format, not the painter's.
	buf := newCaptureBuffer(o.shm, int32(width), int32(height), format)
	if buf == nil {
		return nil
	}
	defer func() { _ = buf.destroy() }()

	if err := frame.Copy(buf.buffer); err != nil {
		return nil
	}
	if !o.pumpUntil(func() bool { return finished }) || !copied {
		return nil
	}

	img := &ui.Image{
		Width:  int(width),
		Height: int(height),
		Stride: int(buf.stride),
		Pix:    make([]byte, len(buf.data)),
	}
	normaliseCapture(img.Pix, buf.data, int(height), int(buf.stride), yInvert, format == formatXRGB8888)
	return img
}
