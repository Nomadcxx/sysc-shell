package wallpaper

import (
	"context"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func writePNG(t *testing.T, path string, w, h int) {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			img.Set(x, y, color.RGBA{R: uint8(x % 256), G: uint8(y % 256), B: 0x80, A: 0xff})
		}
	}
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		t.Fatalf("encode: %v", err)
	}
}

func TestThumbCoverRectKeepsTheTileAspect(t *testing.T) {
	cases := []struct {
		name   string
		sw, sh int
		wantW  int
		wantH  int
	}{
		{"wide source crops horizontally", 4000, 1000, 4000 * 0, 1000},
		{"tall source crops vertically", 1000, 4000, 1000, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := coverRect(image.Rect(0, 0, c.sw, c.sh), ThumbWidth, ThumbHeight)
			// The crop must carry the tile's ratio, within rounding.
			ratio := float64(got.Dx()) / float64(got.Dy())
			want := float64(ThumbWidth) / float64(ThumbHeight)
			if ratio < want*0.99 || ratio > want*1.01 {
				t.Fatalf("crop %v has ratio %.3f, want %.3f", got, ratio, want)
			}
			if got.Dx() > c.sw || got.Dy() > c.sh {
				t.Fatalf("crop %v escapes the source %dx%d", got, c.sw, c.sh)
			}
		})
	}
}

func TestThumbGeneratesAtTheTileSize(t *testing.T) {
	root := t.TempDir()
	cache := t.TempDir()
	src := filepath.Join(root, "wall.png")
	writePNG(t, src, 3840, 2160)

	th := NewThumbnailer(cache, time.Millisecond)
	made, err := th.ensure(context.Background(), Entry{Name: "wall.png", Path: src, Kind: KindImage})
	if err != nil {
		t.Fatalf("ensure: %v", err)
	}
	if !made {
		t.Fatal("first pass must generate")
	}

	dst := th.pathFor(src)
	f, err := os.Open(dst)
	if err != nil {
		t.Fatalf("open preview: %v", err)
	}
	defer f.Close()
	cfg, _, err := image.DecodeConfig(f)
	if err != nil {
		t.Fatalf("decode preview: %v", err)
	}
	if cfg.Width != ThumbWidth || cfg.Height != ThumbHeight {
		t.Fatalf("preview is %dx%d, want %dx%d", cfg.Width, cfg.Height, ThumbWidth, ThumbHeight)
	}

	// A second pass is a no-op: the cache is keyed by path, mtime, and size.
	made, err = th.ensure(context.Background(), Entry{Name: "wall.png", Path: src, Kind: KindImage})
	if err != nil || made {
		t.Fatalf("second pass made=%v err=%v, want a skip", made, err)
	}
}

func TestThumbRekeysWhenTheSourceChanges(t *testing.T) {
	root := t.TempDir()
	cache := t.TempDir()
	src := filepath.Join(root, "wall.png")
	writePNG(t, src, 800, 400)

	th := NewThumbnailer(cache, time.Millisecond)
	entry := Entry{Name: "wall.png", Path: src, Kind: KindImage}
	if _, err := th.ensure(context.Background(), entry); err != nil {
		t.Fatalf("ensure: %v", err)
	}
	first := th.pathFor(src)

	// Replace the file with different content and a different size.
	writePNG(t, src, 1200, 400)
	if err := os.Chtimes(src, time.Now().Add(time.Hour), time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("chtimes: %v", err)
	}
	second := th.pathFor(src)
	if first == second {
		t.Fatal("a changed source must key to a different preview, or the picker shows a stale thumbnail")
	}
}

func TestThumbPacesAndStopsOnCancel(t *testing.T) {
	root := t.TempDir()
	cache := t.TempDir()
	var entries []Entry
	for _, name := range []string{"a.png", "b.png", "c.png", "d.png"} {
		p := filepath.Join(root, name)
		writePNG(t, p, 400, 200)
		entries = append(entries, Entry{Name: name, Path: p, Kind: KindImage})
	}

	// A pace this long means at most one preview lands before the cancel.
	th := NewThumbnailer(cache, 400*time.Millisecond)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { th.Run(ctx); close(done) }()
	th.Enqueue(entries)

	time.Sleep(250 * time.Millisecond)
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("cancel did not stop the generator")
	}

	made := 0
	for _, e := range entries {
		if _, err := os.Stat(th.pathFor(e.Path)); err == nil {
			made++
		}
	}
	if made == 0 {
		t.Fatal("nothing was generated at all")
	}
	if made == len(entries) {
		t.Fatal("the whole library was generated at once; the pace is not being honoured")
	}
}
