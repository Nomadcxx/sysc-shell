package shell

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/Nomadcxx/sysc-notify/protocol"
	"github.com/Nomadcxx/sysc-shell/internal/platform/wayland"
	"github.com/Nomadcxx/sysc-shell/internal/platform/wayland/layershell"
	"github.com/Nomadcxx/sysc-shell/internal/render"
	"github.com/Nomadcxx/sysc-shell/internal/screenshot"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

const (
	selectorNamespace = "sysc-shell-screenshot"
	selectorIDPrefix  = "screenshot:"
	// selectorBorder is the selection outline's width in buffer pixels.
	selectorBorder = 2
	// selectorCopyWait bounds the wait for the owner to hand the crop to the
	// compositor before the selector closes regardless.
	selectorCopyWait = 2 * time.Second

	anchorAll = uint32(layershell.ZwlrLayerSurfaceV1AnchorTop | layershell.ZwlrLayerSurfaceV1AnchorBottom |
		layershell.ZwlrLayerSurfaceV1AnchorLeft | layershell.ZwlrLayerSurfaceV1AnchorRight)
)

var errSelectorOpen = errors.New("a region selector is already open")

// regionSelector is one region capture in progress: a frozen, dimmed surface
// on every output, and one rectangle drawn on one of them. Registry.mu guards
// it; its callbacks run on the Wayland goroutine and take the lock.
type regionSelector struct {
	surfaces map[uint32]*selectorSurface
	state    screenshot.Selector
	// active is the output whose surface holds the rectangle.
	active uint32
	// done is set once the selection is confirmed or cancelled; input after
	// that is ignored. closed is set once its surfaces are asked to close.
	done, closed bool
	border       [4]byte
}

// selectorSurface is the selector on one output.
type selectorSurface struct {
	global uint32
	id     string
	// frame is the output as it was before the surface existed, in buffer
	// pixels; dimmed is the same pixels darkened, built on first paint.
	frame              *ui.Image
	dimmed             []byte
	logicalW, logicalH int
	// opened is set once the open was acknowledged; only an opened surface
	// is closed by endSelectorLocked, so a close never overtakes its open.
	opened bool
	// painted is the outline's buffer rect in the last frame, for damage;
	// damage is what the last Render touched, nil for the whole buffer.
	painted          ui.Rect
	damage           []ui.Rect
	bufferW, bufferH int
}

func newRegionSelector(globals map[string]uint32, border [4]byte) *regionSelector {
	sel := &regionSelector{surfaces: make(map[uint32]*selectorSurface, len(globals)), border: border}
	for connector, global := range globals {
		sel.surfaces[global] = &selectorSurface{global: global, id: selectorIDPrefix + connector}
	}
	return sel
}

// openRegionSelector freezes every output and opens a selector over each. It
// returns once the opens are queued; a failed open ends the selector with a
// failure toast.
func (r *Registry) openRegionSelector() error {
	r.mu.Lock()
	if r.selector != nil {
		r.mu.Unlock()
		return errSelectorOpen
	}
	globals := r.outputGlobalsLocked()
	if len(globals) == 0 {
		r.mu.Unlock()
		return errors.New("no output to select a region on")
	}
	sel := newRegionSelector(globals, premultiplied(r.surfaceTheme().PanelStyle().Accent))
	r.selector = sel
	reqs := make([]wayland.AuxRequest, 0, len(sel.surfaces))
	for _, s := range sel.surfaces {
		reqs = append(reqs, wayland.AuxRequest{Output: s.global, ID: s.id, Open: r.selectorSpec(sel, s)})
	}
	r.mu.Unlock()

	go func() {
		for _, req := range reqs {
			r.mu.Lock()
			done := sel.done
			r.mu.Unlock()
			if done {
				return
			}
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			err := r.sendAuxWait(ctx, req)
			cancel()
			r.mu.Lock()
			if err == nil {
				sel.surfaces[req.Output].opened = true
			}
			done = sel.done
			r.mu.Unlock()
			if err != nil {
				r.endSelector(sel)
				r.screenshotToast("", fmt.Errorf("could not freeze the screen: %w", err))
				return
			}
			if done {
				// Ended while this surface was opening, so the end did
				// not close it.
				r.sendAux(wayland.AuxRequest{Output: req.Output, ID: req.ID})
				return
			}
		}
	}()
	return nil
}

// blur-exempt: the selector is fullscreen and paints the frozen output itself;
// a blurred backdrop would be a second capture of the same screen.
func (r *Registry) selectorSpec(sel *regionSelector, s *selectorSurface) *wayland.AuxSpec {
	return &wayland.AuxSpec{
		ID:            s.id,
		Namespace:     selectorNamespace,
		Layer:         layerOverlay,
		Anchor:        anchorAll,
		ExclusiveZone: -1,
		Keyboard:      keyboardExclusive,
		Freeze:        true,
		Callbacks: wayland.HostCallbacks{
			Configure: func(w, h, _ int) error {
				r.mu.Lock()
				defer r.mu.Unlock()
				s.logicalW, s.logicalH = w, h
				return nil
			},
			Backdrop: func(img *ui.Image) {
				r.mu.Lock()
				defer r.mu.Unlock()
				s.frame, s.dimmed = img, nil
			},
			Render: func(pixels []byte, w, h, stride int) error {
				r.mu.Lock()
				defer r.mu.Unlock()
				return r.renderSelectorLocked(sel, s, pixels, w, h, stride)
			},
			Damaged: func() []ui.Rect {
				r.mu.Lock()
				defer r.mu.Unlock()
				return s.damage
			},
			Handle: func(e wayland.Event) bool {
				r.mu.Lock()
				defer r.mu.Unlock()
				return r.handleSelectorLocked(sel, s, e)
			},
			Crosshair: true,
		},
	}
}

func (r *Registry) handleSelectorLocked(sel *regionSelector, s *selectorSurface, e wayland.Event) bool {
	if sel.done {
		return false
	}
	outcome := screenshot.Pending
	switch e.Kind {
	case wayland.EventPointerPress:
		outcome = sel.state.Press(e.Button, e.X, e.Y)
		if outcome == screenshot.Pending && sel.active != s.global {
			// The rectangle moves to this output; the old one must lose it.
			if old, ok := sel.surfaces[sel.active]; ok {
				r.publishSurfaceAsync(old.global, old.id)
			}
			sel.active = s.global
		}
		if outcome == screenshot.Pending {
			return true
		}
	case wayland.EventPointerMotion:
		if sel.active != s.global {
			return false
		}
		return sel.state.Motion(e.X, e.Y)
	case wayland.EventPointerRelease:
		sel.state.Release()
		return false
	case wayland.EventKeyPress:
		outcome = sel.state.Key(e.Sym)
	default:
		return false
	}

	switch outcome {
	case screenshot.Confirm:
		sel.done = true
		active := sel.surfaces[sel.active]
		rect, _ := sel.state.Rect()
		if active == nil || active.frame == nil {
			r.endSelectorAsyncLocked(sel)
			return true
		}
		crop := screenshot.CropRect(rect, active.logicalW, active.logicalH, active.frame.Width, active.frame.Height)
		go r.finishRegion(sel, active.frame, crop, e.Serial)
		return true
	case screenshot.Cancel:
		r.endSelectorAsyncLocked(sel)
		return true
	}
	return false
}

// finishRegion copies, saves and reports a confirmed crop, then closes the
// selector. It runs off the Wayland goroutine: the copy must be handed to the
// compositor while a selector surface still has keyboard focus, so the close
// waits for the owner to acknowledge it.
func (r *Registry) finishRegion(sel *regionSelector, frame *ui.Image, crop ui.Rect, serial uint32) {
	img := screenshot.Crop(frame, crop)
	if img == nil {
		r.endSelector(sel)
		r.screenshotToast("", errors.New("the selected region is empty"))
		return
	}
	data, err := screenshot.EncodePNG(img)
	if err != nil {
		r.endSelector(sel)
		r.screenshotToast("", fmt.Errorf("encode: %w", err))
		return
	}
	copyErr := r.copyImage(data, serial)
	r.endSelector(sel)

	path, saveErr := r.saveScreenshot(data)
	switch {
	case saveErr == nil && copyErr == nil:
		r.screenshotToast(path, nil)
	case saveErr == nil:
		r.screenshotToast(path, fmt.Errorf("saved to %s but not copied: %w", path, copyErr))
	case copyErr == nil:
		log.Print("screenshot: ", saveErr)
		if _, err := r.publishToast(screenshotToastKey, "Screenshot copied", "Not saved: "+saveErr.Error(), protocol.UrgencyNormal, -1); err != nil {
			log.Printf("screenshot toast: %v", err)
		}
	default:
		r.screenshotToast("", errors.Join(saveErr, copyErr))
	}
}

// copyImage sets data as the image/png selection and waits for the owner to
// hand it to the compositor.
func (r *Registry) copyImage(data []byte, serial uint32) error {
	done := make(chan error, 1)
	timer := time.NewTimer(selectorCopyWait)
	defer timer.Stop()
	select {
	case r.selections <- wayland.SelectionRequest{Mime: "image/png", Data: data, Serial: serial, Done: done}:
	case <-r.closed:
		return errors.New("shell is closing")
	case <-timer.C:
		return errors.New("the clipboard request queue is full")
	}
	select {
	case err := <-done:
		return err
	case <-r.closed:
		return errors.New("shell is closing")
	case <-timer.C:
		return errors.New("the clipboard did not answer")
	}
}

func (r *Registry) saveScreenshot(data []byte) (string, error) {
	dir := r.screenshotDirectory()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("screenshot directory: %w", err)
	}
	path := screenshot.NextPath(dir, time.Now(), screenshot.Exists)
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return "", err
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		return "", err
	}
	return path, f.Close()
}

// endSelector closes sel's surfaces and unregisters it. It is idempotent.
func (r *Registry) endSelector(sel *regionSelector) {
	r.mu.Lock()
	closes := r.endSelectorLocked(sel)
	r.mu.Unlock()
	for _, req := range closes {
		r.sendAux(req)
	}
}

// endSelectorAsyncLocked is endSelector for a caller on the Wayland goroutine,
// which must not wait on the aux queue it drains.
func (r *Registry) endSelectorAsyncLocked(sel *regionSelector) {
	closes := r.endSelectorLocked(sel)
	go func() {
		for _, req := range closes {
			r.sendAux(req)
		}
	}()
}

func (r *Registry) endSelectorLocked(sel *regionSelector) []wayland.AuxRequest {
	sel.done = true
	if r.selector == sel {
		r.selector = nil
	}
	if sel.closed {
		return nil
	}
	sel.closed = true
	var closes []wayland.AuxRequest
	for _, s := range sel.surfaces {
		if s.opened {
			closes = append(closes, wayland.AuxRequest{Output: s.global, ID: s.id})
		}
	}
	return closes
}

// dropSelectorAux ends the selector when the compositor closes one of its
// surfaces, or its output goes. Its own closes arrive here too, after it has
// already ended, and change nothing.
func (r *Registry) dropSelectorAux(output uint32, surfaceID string) bool {
	if !strings.HasPrefix(surfaceID, selectorIDPrefix) {
		return false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	sel := r.selector
	if sel == nil {
		return true
	}
	if s, ok := sel.surfaces[output]; ok && s.id == surfaceID {
		s.opened = false
		r.endSelectorAsyncLocked(sel)
	}
	return true
}

// publishSurfaceAsync invalidates one surface from the Wayland goroutine,
// which must not block on the invalidation queue its own bridge drains.
func (r *Registry) publishSurfaceAsync(global uint32, id string) {
	go r.publishSurface(global, id)
}

func (r *Registry) renderSelectorLocked(sel *regionSelector, s *selectorSurface, pixels []byte, w, h, stride int) error {
	if s.frame == nil {
		clear(pixels)
		s.damage = nil
		return nil
	}
	if s.dimmed == nil {
		s.dimmed = dimFrame(s.frame.Pix)
	}
	var outline ui.Rect
	rect, has := sel.state.Rect()
	has = has && sel.active == s.global
	if has {
		rect = screenshot.CropRect(rect, s.logicalW, s.logicalH, w, h)
		has = rect.W > 0
	}
	paintSelector(pixels, w, h, stride, s.frame, s.dimmed, rect, has, sel.border, selectorBorder)
	if has {
		outline = growRect(rect, selectorBorder)
	}

	// Nil is the whole buffer: a first frame, a resize, or no outline in
	// either frame.
	s.damage = nil
	if d := clipRect(unionRect(s.painted, outline), w, h); d.W > 0 && s.bufferW == w && s.bufferH == h {
		s.damage = []ui.Rect{d}
	}
	s.painted, s.bufferW, s.bufferH = outline, w, h
	return nil
}

// dimFrame halves an opaque frame's colour, leaving its alpha.
func dimFrame(pix []byte) []byte {
	out := make([]byte, len(pix))
	for i := 0; i+3 < len(pix); i += 4 {
		out[i], out[i+1], out[i+2], out[i+3] = pix[i]>>1, pix[i+1]>>1, pix[i+2]>>1, pix[i+3]
	}
	return out
}

// paintSelector fills a w x h buffer from the frozen frame: dimmed everywhere
// but sel, which shows the frame as it is inside an outline bw pixels wide.
// Where the buffer and frame differ in size by a rounding pixel, the buffer's
// excess stays black.
func paintSelector(dst []byte, w, h, stride int, frame *ui.Image, dimmed []byte, sel ui.Rect, has bool, border [4]byte, bw int) {
	cols := min(w, frame.Width)
	for y := range h {
		row := dst[y*stride : y*stride+w*4]
		if y >= frame.Height {
			clear(row)
			continue
		}
		src := y * frame.Stride
		copy(row[:cols*4], dimmed[src:src+cols*4])
		clear(row[cols*4:])
		if has && y >= sel.Y && y < sel.Y+sel.H {
			x0, x1 := max(sel.X, 0), min(sel.X+sel.W, cols)
			if x1 > x0 {
				copy(row[x0*4:x1*4], frame.Pix[src+x0*4:src+x1*4])
			}
		}
	}
	if !has {
		return
	}
	outer := growRect(sel, bw)
	fill := func(r ui.Rect) {
		r = clipRect(r, w, h)
		for y := r.Y; y < r.Y+r.H; y++ {
			for x := r.X; x < r.X+r.W; x++ {
				copy(dst[y*stride+x*4:], border[:])
			}
		}
	}
	fill(ui.Rect{X: outer.X, Y: outer.Y, W: outer.W, H: bw})
	fill(ui.Rect{X: outer.X, Y: sel.Y + sel.H, W: outer.W, H: bw})
	fill(ui.Rect{X: outer.X, Y: sel.Y, W: bw, H: sel.H})
	fill(ui.Rect{X: sel.X + sel.W, Y: sel.Y, W: bw, H: sel.H})
}

func premultiplied(c render.Color) [4]byte {
	a := uint32(c.A)
	return [4]byte{byte(uint32(c.B) * a / 255), byte(uint32(c.G) * a / 255), byte(uint32(c.R) * a / 255), c.A}
}

func growRect(r ui.Rect, n int) ui.Rect {
	return ui.Rect{X: r.X - n, Y: r.Y - n, W: r.W + 2*n, H: r.H + 2*n}
}

// union is the bounding box of two rects; an empty rect adds nothing.
func unionRect(a, b ui.Rect) ui.Rect {
	if a.W <= 0 || a.H <= 0 {
		return b
	}
	if b.W <= 0 || b.H <= 0 {
		return a
	}
	x0, y0 := min(a.X, b.X), min(a.Y, b.Y)
	x1, y1 := max(a.X+a.W, b.X+b.W), max(a.Y+a.H, b.Y+b.H)
	return ui.Rect{X: x0, Y: y0, W: x1 - x0, H: y1 - y0}
}

func clipRect(r ui.Rect, w, h int) ui.Rect {
	x0, y0 := max(r.X, 0), max(r.Y, 0)
	x1, y1 := min(r.X+r.W, w), min(r.Y+r.H, h)
	if x1 <= x0 || y1 <= y0 {
		return ui.Rect{}
	}
	return ui.Rect{X: x0, Y: y0, W: x1 - x0, H: y1 - y0}
}
