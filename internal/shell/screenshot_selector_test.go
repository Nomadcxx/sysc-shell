package shell

import (
	"bytes"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Nomadcxx/sysc-shell/internal/config"
	"github.com/Nomadcxx/sysc-shell/internal/platform/wayland"
	"github.com/Nomadcxx/sysc-shell/internal/render"
	"github.com/Nomadcxx/sysc-shell/internal/screenshot"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

// testFrame is a w x h opaque BGRA image whose pixel (x, y) is B=x, G=y, R=9.
func testFrame(w, h int) *ui.Image {
	img := &ui.Image{Width: w, Height: h, Stride: w * 4, Pix: make([]byte, w*h*4)}
	for y := range h {
		for x := range w {
			i := y*img.Stride + x*4
			img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3] = byte(x), byte(y), 9, 0xff
		}
	}
	return img
}

// openTestSelector installs a one-output selector the way openRegionSelector
// does, with its surface already open, frozen on frame and configured at the
// given logical size.
func openTestSelector(t *testing.T, r *Registry, frame *ui.Image, logicalW, logicalH int) (*regionSelector, *wayland.AuxSpec) {
	t.Helper()
	r.mu.Lock()
	sel := newRegionSelector(map[string]uint32{"DP-1": 7}, render.Color{R: 255, A: 255})
	r.selector = sel
	s := sel.surfaces[7]
	spec := r.selectorSpec(sel, s)
	s.opened = true
	r.mu.Unlock()
	spec.Callbacks.Backdrop(frame)
	if err := spec.Callbacks.Configure(logicalW, logicalH, 120); err != nil {
		t.Fatal(err)
	}
	return sel, spec
}

func drag(spec *wayland.AuxSpec, x0, y0, x1, y1 float64) {
	h := spec.Callbacks.Handle
	h(wayland.Event{Kind: wayland.EventPointerPress, Button: screenshot.ButtonLeft, X: x0, Y: y0, Serial: 1})
	h(wayland.Event{Kind: wayland.EventPointerMotion, X: x1, Y: y1})
	h(wayland.Event{Kind: wayland.EventPointerRelease, Button: screenshot.ButtonLeft, X: x1, Y: y1, Serial: 2})
}

func recvAux(t *testing.T, r *Registry) wayland.AuxRequest {
	t.Helper()
	select {
	case req := <-r.aux:
		return req
	case <-time.After(2 * time.Second):
		t.Fatal("no aux request")
		return wayland.AuxRequest{}
	}
}

func TestSelectorSpecFreezesAnExclusiveOverlay(t *testing.T) {
	r := NewRegistry(config.Default())
	_, spec := openTestSelector(t, r, testFrame(4, 4), 4, 4)
	if !spec.Freeze || spec.Namespace != "sysc-shell-screenshot" || spec.Layer != layerOverlay ||
		spec.Keyboard != keyboardExclusive || spec.ExclusiveZone != -1 || spec.Anchor != anchorAll ||
		!spec.Callbacks.Crosshair || spec.ID != "screenshot:DP-1" {
		t.Fatalf("spec = %+v", spec)
	}
	if err := r.Screenshot("region"); err == nil {
		t.Fatal("a second selector opened while one was open")
	}
}

func TestSelectorConfirmCopiesSavesClosesAndToasts(t *testing.T) {
	dir := t.TempDir()
	r := NewRegistry(config.Default())
	rec := &pluginToastRecorder{}
	r.BindNotifications(rec)
	r.screenshotDir = func() string { return dir }
	// A 1.25 scale: the frame is buffer pixels, the events logical ones.
	frame := testFrame(50, 40)
	_, spec := openTestSelector(t, r, frame, 40, 32)

	drag(spec, 20, 16, 4, 8) // up and to the left
	if !spec.Callbacks.Handle(wayland.Event{Kind: wayland.EventKeyPress, Sym: ui.SymReturn, Serial: 42}) {
		t.Fatal("confirm reported no change")
	}

	var req wayland.SelectionRequest
	select {
	case req = <-r.selections:
	case <-time.After(2 * time.Second):
		t.Fatal("no selection request")
	}
	if req.Mime != "image/png" || req.Serial != 42 || req.Done == nil {
		t.Fatalf("selection = mime %q serial %d done %v", req.Mime, req.Serial, req.Done)
	}
	// The surface must stay open until the copy was handled, or the
	// compositor refuses it for lack of keyboard focus.
	select {
	case got := <-r.aux:
		t.Fatalf("closed before the copy was acknowledged: %+v", got)
	case <-time.After(50 * time.Millisecond):
	}
	req.Done <- nil
	if closed := recvAux(t, r); closed.ID != "screenshot:DP-1" || closed.Open != nil || closed.Update != nil || closed.Output != 7 {
		t.Fatalf("close = %+v", closed)
	}

	// Logical 4,8 20x... at 1.25 is frame 5,10 to 25,20.
	img, err := png.Decode(bytes.NewReader(req.Data))
	if err != nil {
		t.Fatal(err)
	}
	if b := img.Bounds(); b.Dx() != 20 || b.Dy() != 10 {
		t.Fatalf("crop is %v, want 20x10", b)
	}
	if r0, g, b, _ := img.At(0, 0).RGBA(); b>>8 != 5 || g>>8 != 10 || r0>>8 != 9 {
		t.Fatalf("crop origin pixel = %d,%d,%d", r0>>8, g>>8, b>>8)
	}

	got := waitProducer(t, rec, 1)
	p := got[0].Producer
	if p.Summary != "Screenshot saved" || filepath.Dir(p.Body) != dir {
		t.Fatalf("toast = %+v", p)
	}
	saved, err := os.ReadFile(p.Body)
	if err != nil || !bytes.Equal(saved, req.Data) {
		t.Fatalf("saved file differs from the copy: %v", err)
	}
	r.mu.Lock()
	open := r.selector
	r.mu.Unlock()
	if open != nil {
		t.Fatal("the selector is still registered")
	}
}

func TestSelectorEscapeClosesWithoutCapture(t *testing.T) {
	r := NewRegistry(config.Default())
	rec := &pluginToastRecorder{}
	r.BindNotifications(rec)
	_, spec := openTestSelector(t, r, testFrame(8, 8), 8, 8)
	drag(spec, 1, 1, 5, 5)
	spec.Callbacks.Handle(wayland.Event{Kind: wayland.EventKeyPress, Sym: ui.SymEscape})
	if closed := recvAux(t, r); closed.ID != "screenshot:DP-1" || closed.Open != nil {
		t.Fatalf("close = %+v", closed)
	}
	select {
	case req := <-r.selections:
		t.Fatalf("a cancel copied: %+v", req)
	case <-time.After(50 * time.Millisecond):
	}
	if n := len(rec.commands()); n != 0 {
		t.Fatalf("a cancel toasted %d times", n)
	}
	// Input after the end is ignored rather than reopening anything.
	if spec.Callbacks.Handle(wayland.Event{Kind: wayland.EventKeyPress, Sym: ui.SymReturn}) {
		t.Fatal("a closed selector handled input")
	}
	if err := r.Screenshot("region"); err == nil {
		// No bars exist in this registry, so a fresh selector has nowhere
		// to open; what matters is that the old one no longer blocks it.
		t.Log("region opened")
	} else if err.Error() == errSelectorOpen.Error() {
		t.Fatal("a cancelled selector still blocks a new one")
	}
}

func TestSelectorDropByTheCompositorCancels(t *testing.T) {
	r := NewRegistry(config.Default())
	openTestSelector(t, r, testFrame(8, 8), 8, 8)
	r.DropAux(7, "screenshot:DP-1")
	r.mu.Lock()
	open := r.selector
	r.mu.Unlock()
	if open != nil {
		t.Fatal("a dropped selector surface left the selector registered")
	}
}

func TestPaintSelector(t *testing.T) {
	t.Parallel()
	const w, h = 12, 10
	frame := testFrame(w, h)
	dimmed := dimFrame(frame.Pix)
	dst := make([]byte, w*h*4)
	border := [4]byte{1, 2, 3, 0xff}
	sel := ui.Rect{X: 4, Y: 3, W: 4, H: 4}
	paintSelector(dst, w, h, w*4, frame, dimmed, sel, true, border, 1)
	px := func(x, y int) []byte { return dst[y*w*4+x*4 : y*w*4+x*4+4] }
	if got := px(5, 4); got[0] != 5 || got[1] != 4 || got[2] != 9 {
		t.Fatalf("inside = %v, want the frame", got)
	}
	if got := px(0, 0); got[2] != 9/2 || got[3] != 0xff {
		t.Fatalf("outside = %v, want dimmed and opaque", got)
	}
	if got := px(3, 4); !bytes.Equal(got, border[:]) {
		t.Fatalf("left edge = %v, want the border", got)
	}
	if got := px(8, 7); !bytes.Equal(got, border[:]) {
		t.Fatalf("lower right corner = %v, want the border", got)
	}
	if got := px(10, 9); got[0] != 10/2 {
		t.Fatalf("far corner = %v, want dimmed", got)
	}

	// No rectangle: all dimmed, no border.
	paintSelector(dst, w, h, w*4, frame, dimmed, ui.Rect{}, false, border, 1)
	if got := px(5, 4); got[0] != 5/2 {
		t.Fatalf("no selection = %v, want dimmed", got)
	}
}

func TestSelectorDamageCoversTheOldAndNewRectangle(t *testing.T) {
	r := NewRegistry(config.Default())
	_, spec := openTestSelector(t, r, testFrame(100, 100), 100, 100)
	buf := make([]byte, 100*100*4)
	render := func() []ui.Rect {
		if err := spec.Callbacks.Render(buf, 100, 100, 400); err != nil {
			t.Fatal(err)
		}
		return spec.Callbacks.Damaged()
	}
	if d := render(); d != nil {
		t.Fatalf("first frame damage = %v, want the whole buffer", d)
	}
	h := spec.Callbacks.Handle
	h(wayland.Event{Kind: wayland.EventPointerPress, Button: screenshot.ButtonLeft, X: 10, Y: 10})
	h(wayland.Event{Kind: wayland.EventPointerMotion, X: 20, Y: 20})
	render()
	h(wayland.Event{Kind: wayland.EventPointerMotion, X: 50, Y: 40})
	d := render()
	if len(d) != 1 {
		t.Fatalf("damage = %v", d)
	}
	// Damage must cover the grown outlines of both the old and the new
	// rectangle; the handle and label chrome may widen it further.
	contains := func(d, r ui.Rect) bool {
		return d.X <= r.X && d.Y <= r.Y && d.X+d.W >= r.X+r.W && d.Y+d.H >= r.Y+r.H
	}
	for _, want := range []ui.Rect{
		growRect(ui.Rect{X: 10, Y: 10, W: 10, H: 10}, selectorBorder),
		growRect(ui.Rect{X: 10, Y: 10, W: 40, H: 30}, selectorBorder),
	} {
		if !contains(d[0], want) {
			t.Fatalf("damage %v does not cover %v", d[0], want)
		}
	}
}
