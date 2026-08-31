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
	if err := applyCursorShape(rec, 7, true); err != nil {
		t.Fatal(err)
	}
	if rec.serial != 7 || rec.shape != uint32(cursorshape.WpCursorShapeDeviceV1ShapeText) {
		t.Fatalf("ibeam = serial %d shape %d", rec.serial, rec.shape)
	}
	if err := applyCursorShape(rec, 8, false); err != nil {
		t.Fatal(err)
	}
	if rec.serial != 8 || rec.shape != uint32(cursorshape.WpCursorShapeDeviceV1ShapeDefault) {
		t.Fatalf("default = serial %d shape %d", rec.serial, rec.shape)
	}
}
