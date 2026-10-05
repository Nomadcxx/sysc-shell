package wayland

import (
	"testing"

	"github.com/Nomadcxx/sysc-wayland/cursorshape"
)

type shapeRecorder struct {
	serial, shape uint32
	n             int
}

func (s *shapeRecorder) SetShape(serial, shape uint32) error {
	s.serial, s.shape = serial, shape
	s.n++
	return nil
}

func TestCursorShapeSetOnFocus(t *testing.T) {
	t.Parallel()
	rec := &shapeRecorder{}
	if err := applyCursorShape(rec, 7, cursorShapeFor(&HostCallbacks{IBeamAt: func(float64, float64) bool { return true }}, 0, 0)); err != nil {
		t.Fatal(err)
	}
	if rec.serial != 7 || rec.shape != uint32(cursorshape.WpCursorShapeDeviceV1ShapeText) {
		t.Fatalf("ibeam = serial %d shape %d", rec.serial, rec.shape)
	}
	if err := applyCursorShape(rec, 8, cursorShapeFor(nil, 0, 0)); err != nil {
		t.Fatal(err)
	}
	if rec.serial != 8 || rec.shape != uint32(cursorshape.WpCursorShapeDeviceV1ShapeDefault) {
		t.Fatalf("default = serial %d shape %d", rec.serial, rec.shape)
	}
	if err := applyCursorShape(rec, 9, cursorShapeFor(&HostCallbacks{Crosshair: true}, 0, 0)); err != nil {
		t.Fatal(err)
	}
	if rec.serial != 9 || rec.shape != uint32(cursorshape.WpCursorShapeDeviceV1ShapeCrosshair) {
		t.Fatalf("crosshair = serial %d shape %d", rec.serial, rec.shape)
	}
}

// zwp_text_input_v3 marks text allow-null, so the binding hands the owner a *string and
// a NULL arrives as nil. The IME buffers hold plain strings, and the decoder this
// replaced produced "" for a NULL too, so nil and empty resolve alike. This pins that
// reading: the two are deliberately not distinguished yet, which is an input-method
// behaviour decision rather than a binding one.
func TestTextOrEmptyReadsNullAsEmpty(t *testing.T) {
	t.Parallel()
	empty := ""
	embedded := "a\x00b"
	for _, tt := range []struct {
		name string
		in   *string
		want string
	}{
		{"null is empty", nil, ""},
		{"empty stays empty", &empty, ""},
		{"embedded nul is preserved", &embedded, "a\x00b"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := textOrEmpty(tt.in); got != tt.want {
				t.Fatalf("textOrEmpty(%v) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}
