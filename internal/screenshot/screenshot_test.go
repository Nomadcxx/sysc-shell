package screenshot

import (
	"bytes"
	"errors"
	"image/png"
	"os"
	"testing"
	"time"

	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

func TestPicturesDir(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		file string
		want string
	}{
		{"home relative", "# comment\nXDG_PICTURES_DIR=\"$HOME/Bilder\"\n", "/h/Bilder"},
		{"absolute", "XDG_PICTURES_DIR=\"/data/pics\"\n", "/data/pics"},
		{"unquoted", "XDG_PICTURES_DIR=$HOME/P\n", "/h/P"},
		{"bare home is no pictures dir", "XDG_PICTURES_DIR=\"$HOME/\"\n", "/h/Pictures"},
		{"relative is ignored", "XDG_PICTURES_DIR=\"pics\"\n", "/h/Pictures"},
		{"absent key", "XDG_MUSIC_DIR=\"$HOME/Music\"\n", "/h/Pictures"},
		{"no file", "", "/h/Pictures"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			read := func(path string) ([]byte, error) {
				if path != "/c/user-dirs.dirs" {
					t.Errorf("read %q", path)
				}
				if tc.file == "" {
					return nil, os.ErrNotExist
				}
				return []byte(tc.file), nil
			}
			if got := PicturesDir("/h", "/c", read); got != tc.want {
				t.Fatalf("PicturesDir = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestNextPath(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 9, 29, 14, 5, 9, 0, time.Local)
	taken := map[string]bool{}
	exists := func(p string) bool { return taken[p] }
	want := []string{
		"/d/screenshot_2026-09-29_14-05-09.png",
		"/d/screenshot_2026-09-29_14-05-09-2.png",
		"/d/screenshot_2026-09-29_14-05-09-3.png",
	}
	for i, w := range want {
		got := NextPath("/d", now, exists)
		if got != w {
			t.Fatalf("path %d = %q, want %q", i, got, w)
		}
		taken[got] = true
	}
}

func TestCropRect(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name           string
		sel            ui.Rect
		lw, lh, fw, fh int
		want           ui.Rect
	}{
		{"scale one", ui.Rect{X: 10, Y: 20, W: 30, H: 40}, 3440, 1440, 3440, 1440, ui.Rect{X: 10, Y: 20, W: 30, H: 40}},
		// 1536x864 logical over a 1920x1080 frame is scale 1.25: 10 -> 12.5
		// floors to 12, 10+30=40 -> 50 exactly.
		{"scale 1.25 rounds outward", ui.Rect{X: 10, Y: 11, W: 30, H: 29}, 1536, 864, 1920, 1080, ui.Rect{X: 12, Y: 13, W: 38, H: 37}},
		{"clamped to the frame", ui.Rect{X: -5, Y: 1430, W: 20, H: 50}, 3440, 1440, 3440, 1440, ui.Rect{X: 0, Y: 1430, W: 15, H: 10}},
		{"outside the frame is empty", ui.Rect{X: 4000, Y: 0, W: 20, H: 20}, 3440, 1440, 3440, 1440, ui.Rect{}},
		{"no logical size is empty", ui.Rect{X: 0, Y: 0, W: 20, H: 20}, 0, 0, 3440, 1440, ui.Rect{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := CropRect(tc.sel, tc.lw, tc.lh, tc.fw, tc.fh); got != tc.want {
				t.Fatalf("CropRect = %+v, want %+v", got, tc.want)
			}
		})
	}
}

// frame is a w x h opaque BGRA image whose pixel (x, y) is B=x, G=y, R=7.
func frame(w, h int) *ui.Image {
	img := &ui.Image{Width: w, Height: h, Stride: w * 4, Pix: make([]byte, w*h*4)}
	for y := range h {
		for x := range w {
			i := y*img.Stride + x*4
			img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3] = byte(x), byte(y), 7, 0xff
		}
	}
	return img
}

func TestCropAndEncodeKeepTheFramePixels(t *testing.T) {
	t.Parallel()
	src := frame(20, 10)
	crop := Crop(src, ui.Rect{X: 3, Y: 2, W: 5, H: 4})
	if crop == nil || crop.Width != 5 || crop.Height != 4 {
		t.Fatalf("crop = %+v", crop)
	}
	data, err := EncodePNG(crop)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	if b := decoded.Bounds(); b.Dx() != 5 || b.Dy() != 4 {
		t.Fatalf("decoded bounds %v", b)
	}
	for y := range 4 {
		for x := range 5 {
			r, g, b, a := decoded.At(x, y).RGBA()
			if r>>8 != 7 || g>>8 != uint32(y+2) || b>>8 != uint32(x+3) || a>>8 != 0xff {
				t.Fatalf("pixel (%d,%d) = %d,%d,%d,%d", x, y, r>>8, g>>8, b>>8, a>>8)
			}
		}
	}
	if Crop(src, ui.Rect{X: 30, Y: 0, W: 5, H: 5}) != nil {
		t.Fatal("a crop outside the frame should be nil")
	}
}

func TestSelector(t *testing.T) {
	t.Parallel()
	type step struct {
		op      string // press, right, move, release, key
		x, y    float64
		sym     uint32
		outcome Outcome
	}
	cases := []struct {
		name    string
		steps   []step
		want    ui.Rect
		wantHas bool
	}{
		{"drag down right", []step{{op: "press", x: 10, y: 10}, {op: "move", x: 40, y: 30}, {op: "release"}}, ui.Rect{X: 10, Y: 10, W: 30, H: 20}, true},
		{"drag up left", []step{{op: "press", x: 40, y: 30}, {op: "move", x: 10.5, y: 10.5}, {op: "release"}}, ui.Rect{X: 10, Y: 10, W: 30, H: 20}, true},
		{"drag up right", []step{{op: "press", x: 10, y: 30}, {op: "move", x: 40, y: 10}}, ui.Rect{X: 10, Y: 10, W: 30, H: 20}, true},
		{"motion without press", []step{{op: "move", x: 40, y: 30}}, ui.Rect{}, false},
		{"motion after release does not extend", []step{{op: "press", x: 0, y: 0}, {op: "move", x: 5, y: 5}, {op: "release"}, {op: "move", x: 50, y: 50}}, ui.Rect{W: 5, H: 5}, true},
		{"a click is no rectangle", []step{{op: "press", x: 10, y: 10}, {op: "release"}}, ui.Rect{}, false},
		{"a new press replaces the rectangle", []step{{op: "press", x: 0, y: 0}, {op: "move", x: 50, y: 50}, {op: "release"}, {op: "press", x: 100, y: 100}, {op: "move", x: 110, y: 120}}, ui.Rect{X: 100, Y: 100, W: 10, H: 20}, true},
		{"enter confirms a rectangle", []step{{op: "press"}, {op: "move", x: 5, y: 5}, {op: "release"}, {op: "key", sym: ui.SymReturn, outcome: Confirm}}, ui.Rect{W: 5, H: 5}, true},
		{"keypad enter and space confirm", []step{{op: "press"}, {op: "move", x: 5, y: 5}, {op: "key", sym: ui.SymKPEnter, outcome: Confirm}, {op: "key", sym: ' ', outcome: Confirm}}, ui.Rect{W: 5, H: 5}, true},
		{"enter without a rectangle waits", []step{{op: "key", sym: ui.SymReturn, outcome: Pending}}, ui.Rect{}, false},
		{"escape cancels", []step{{op: "key", sym: ui.SymEscape, outcome: Cancel}}, ui.Rect{}, false},
		{"other keys wait", []step{{op: "key", sym: 'a', outcome: Pending}}, ui.Rect{}, false},
		{"a right button cancels", []step{{op: "right", outcome: Cancel}}, ui.Rect{}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var s Selector
			for i, st := range tc.steps {
				switch st.op {
				case "press":
					if got := s.Press(ButtonLeft, st.x, st.y); got != Pending {
						t.Fatalf("step %d: left press = %v", i, got)
					}
				case "right":
					if got := s.Press(ButtonRight, st.x, st.y); got != st.outcome {
						t.Fatalf("step %d: right press = %v, want %v", i, got, st.outcome)
					}
				case "move":
					s.Motion(st.x, st.y)
				case "release":
					s.Release()
				case "key":
					if got := s.Key(st.sym); got != st.outcome {
						t.Fatalf("step %d: key %#x = %v, want %v", i, st.sym, got, st.outcome)
					}
				}
			}
			got, has := s.Rect()
			if has != tc.wantHas || got != tc.want {
				t.Fatalf("Rect = %+v, %v; want %+v, %v", got, has, tc.want, tc.wantHas)
			}
		})
	}
}

func TestSelectorMotionReportsChange(t *testing.T) {
	t.Parallel()
	var s Selector
	if s.Motion(5, 5) {
		t.Fatal("motion with no drag reported a change")
	}
	s.Press(ButtonLeft, 0, 0)
	if !s.Motion(5, 5) {
		t.Fatal("extending the drag reported no change")
	}
	if s.Motion(4.6, 4.7) {
		t.Fatal("sub-pixel motion that leaves the rectangle alone reported a change")
	}
}

func TestDirJoinsScreenshots(t *testing.T) {
	t.Parallel()
	read := func(string) ([]byte, error) { return nil, errors.New("none") }
	if got := dir("/h", "/c", read); got != "/h/Pictures/Screenshots" {
		t.Fatalf("dir = %q", got)
	}
}
