package wallpaper

import (
	"context"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	xdraw "golang.org/x/image/draw"
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

	th := NewThumbnailer(cache)
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

	th := NewThumbnailer(cache)
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

func TestThumbPublishesFinalCountsWhenNothingNewIsMade(t *testing.T) {
	root := t.TempDir()
	cache := t.TempDir()
	var entries []Entry
	for _, name := range []string{"a.png", "b.png"} {
		p := filepath.Join(root, name)
		writePNG(t, p, 400, 200)
		entries = append(entries, Entry{Name: name, Path: p, Kind: KindImage})
	}

	th := NewThumbnailer(cache)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	drain := func() {
		for {
			select {
			case <-th.Progress():
			default:
				return
			}
		}
	}

	th.generate(ctx, newThumbBatch(entries, ""))
	drain()

	// Second pass: every preview is cached, so no per-item tick fires. The
	// walk must still announce that it finished, or the picker shows a stale
	// "generating previews" count forever.
	th.generate(ctx, newThumbBatch(entries, ""))
	select {
	case <-th.Progress():
	default:
		t.Fatal("a fully cached walk published no final progress")
	}
	c := th.Counts()
	done, total := c.Done, c.Total
	if done != total || total != len(entries) {
		t.Fatalf("counts after the walk: %d/%d, want %d/%d", done, total, len(entries), len(entries))
	}
}

func TestThumbStopsOnCancel(t *testing.T) {
	root := t.TempDir()
	cache := t.TempDir()
	var entries []Entry
	for i := range 40 {
		p := filepath.Join(root, fmt.Sprintf("w%02d.png", i))
		writePNG(t, p, 1600, 900)
		entries = append(entries, Entry{Name: filepath.Base(p), Path: p, Kind: KindImage})
	}

	th := NewThumbnailer(cache)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { th.Run(ctx); close(done) }()
	th.Enqueue(entries, "")
	time.Sleep(20 * time.Millisecond)
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("cancel did not stop the generator")
	}
}

// The folder on screen is generated first, and its own counts are reported
// beside the library's, so the picker can say how far the visible grid is.
func TestThumbBatchPutsTheOpenFolderFirst(t *testing.T) {
	entries := []Entry{
		{Name: "a", Path: "/lib/a.png", Kind: KindImage},
		{Name: "sub", Path: "/lib/sub", IsDir: true},
		{Name: "x", Path: "/lib/sub/x.png", Kind: KindImage},
		{Name: "b", Path: "/lib/b.mp4", Kind: KindVideo},
		{Name: "y", Path: "/lib/sub/y.jpg", Kind: KindImage},
	}
	cases := []struct {
		name   string
		folder string
		want   []string
		first  int
	}{
		{"no folder keeps library order", "", []string{"/lib/a.png", "/lib/sub/x.png", "/lib/b.mp4", "/lib/sub/y.jpg"}, 0},
		{"folder first", "/lib/sub", []string{"/lib/sub/x.png", "/lib/sub/y.jpg", "/lib/a.png", "/lib/b.mp4"}, 2},
		{"root folder", "/lib", []string{"/lib/a.png", "/lib/b.mp4", "/lib/sub/x.png", "/lib/sub/y.jpg"}, 2},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			b := newThumbBatch(entries, c.folder)
			var got []string
			for _, e := range b.entries {
				got = append(got, e.Path)
			}
			if !slices.Equal(got, c.want) {
				t.Fatalf("order %v, want %v", got, c.want)
			}
			if b.folderCount != c.first {
				t.Fatalf("folder count %d, want %d", b.folderCount, c.first)
			}
		})
	}
}

func TestThumbPoolGeneratesEverythingAndCountsExactly(t *testing.T) {
	root := t.TempDir()
	sub := filepath.Join(root, "sub")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	var entries []Entry
	for i := range 12 {
		dir := root
		if i%3 == 0 {
			dir = sub
		}
		p := filepath.Join(dir, fmt.Sprintf("w%02d.png", i))
		writePNG(t, p, 640, 360)
		entries = append(entries, Entry{Name: filepath.Base(p), Path: p, Kind: KindImage})
	}
	th := NewThumbnailer(t.TempDir())
	th.workers = 3
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go th.Run(ctx)
	th.Enqueue(entries, sub)

	deadline := time.Now().Add(10 * time.Second)
	for {
		c := th.Counts()
		if c.Total == len(entries) && c.Done == c.Total {
			if c.Folder != sub || c.FolderTotal != 4 || c.FolderDone != 4 {
				t.Fatalf("folder counts %+v, want 4/4 for %s", c, sub)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("counts stalled at %+v", c)
		}
		time.Sleep(10 * time.Millisecond)
	}
	for _, e := range entries {
		if _, err := os.Stat(th.pathFor(e.Path)); err != nil {
			t.Errorf("no preview for %s: %v", e.Name, err)
		}
	}
}

// A file that cannot be decoded is recorded, so a rescan does not decode it
// again and the tile can say it has no preview. Replacing the file retries.
func TestThumbRecordsAFailureOnce(t *testing.T) {
	root := t.TempDir()
	cache := t.TempDir()
	src := filepath.Join(root, "broken.png")
	if err := os.WriteFile(src, []byte("not a png"), 0o644); err != nil {
		t.Fatal(err)
	}
	th := NewThumbnailer(cache)
	entry := Entry{Name: "broken.png", Path: src, Kind: KindImage}
	if _, err := th.ensure(context.Background(), entry); err == nil {
		t.Fatal("a broken file decoded")
	}
	if !th.failed(src) {
		t.Fatal("the failure was not recorded")
	}
	made, err := th.ensure(context.Background(), entry)
	if made || err != nil {
		t.Fatalf("second pass made=%v err=%v, want a recorded skip", made, err)
	}

	writePNG(t, src, 400, 200)
	if err := os.Chtimes(src, time.Now().Add(time.Hour), time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if th.failed(src) {
		t.Fatal("a replaced file still reads as failed")
	}
	if made, err := th.ensure(context.Background(), entry); err != nil || !made {
		t.Fatalf("replaced file made=%v err=%v, want a fresh preview", made, err)
	}
}

// A missing frame extractor is the machine's state, not the file's. Recording
// it would leave every video without a preview after ffmpeg is installed.
func TestThumbDoesNotRecordAMissingExtractor(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "clip.mp4")
	if err := os.WriteFile(src, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	th := NewThumbnailer(t.TempDir())
	th.extract = nil
	if _, err := th.ensure(context.Background(), Entry{Name: "clip.mp4", Path: src, Kind: KindVideo}); err == nil {
		t.Fatal("a video without an extractor reported success")
	}
	if th.failed(src) {
		t.Fatal("a missing extractor was recorded against the file")
	}
}

func TestThumbDecodesWebP(t *testing.T) {
	src, err := filepath.Abs(filepath.Join("testdata", "tiny.webp"))
	if err != nil {
		t.Fatal(err)
	}
	th := NewThumbnailer(t.TempDir())
	if made, err := th.ensure(context.Background(), Entry{Name: "tiny.webp", Path: src, Kind: KindImage}); err != nil || !made {
		t.Fatalf("webp made=%v err=%v", made, err)
	}
}

// The two-step scale is the speed fix; it must not cost visible quality
// against scaling the full source in one CatmullRom pass.
func TestCoverScaleMatchesTheSinglePassReference(t *testing.T) {
	src := image.NewRGBA(image.Rect(0, 0, 3840, 2160))
	for y := range 2160 {
		for x := range 3840 {
			i := src.PixOffset(x, y)
			src.Pix[i], src.Pix[i+1], src.Pix[i+2], src.Pix[i+3] = uint8(x*255/3839), uint8(y*255/2159), uint8((x+y)%256), 0xff
		}
	}
	got := coverScale(src)
	want := image.NewRGBA(image.Rect(0, 0, ThumbWidth, ThumbHeight))
	xdraw.CatmullRom.Scale(want, want.Bounds(), src, coverRect(src.Bounds(), ThumbWidth, ThumbHeight), xdraw.Src, nil)
	var sum int
	for i := range got.Pix {
		d := int(got.Pix[i]) - int(want.Pix[i])
		if d < 0 {
			d = -d
		}
		sum += d
	}
	if mean := float64(sum) / float64(len(got.Pix)); mean > 2 {
		t.Fatalf("mean channel error %.2f against the single-pass reference, want <= 2", mean)
	}
}

func TestDecodeBudgetHoldsOversizedWorkAlone(t *testing.T) {
	b := newDecodeBudget(100)
	b.acquire(60)
	got := make(chan int64, 2)
	go func() { b.acquire(60); got <- 60 }()
	select {
	case <-got:
		t.Fatal("two 60s ran inside a 100 budget")
	case <-time.After(30 * time.Millisecond):
	}
	b.release(60)
	<-got
	b.release(60)
	// More than the whole budget still runs, alone.
	b.acquire(500)
	b.release(500)
}
