package wallpaper

import (
	"context"
	"errors"
	"fmt"
	"image"
	"image/jpeg"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"

	// Decoders for the still formats the library names. webp and jxl have no
	// decoder in the module graph, so those tiles keep the kind glyph rather
	// than pulling in a new dependency.
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"

	_ "golang.org/x/image/bmp"

	xdraw "golang.org/x/image/draw"
)

// Bounds on one thumbnail job.
const (
	// ThumbPace is the gap between two thumbnails. Generating a whole library
	// at once is what makes a wallpaper picker feel like a fork bomb: the work
	// is background work, so it is spread out deliberately rather than run as
	// fast as the disk allows.
	ThumbPace = 150 * time.Millisecond
	// thumbMaxSource caps one source file read. A 4K still is tens of
	// megabytes; anything past this is not a wallpaper.
	thumbMaxSource = 256 << 20
	// thumbMaxDimension rejects a hostile or absurd header before it is
	// decoded into memory.
	thumbMaxDimension = 16384
	// thumbQuality is the cached JPEG quality. These are 210x96; the file is a
	// few kilobytes either way, and artefacts would be visible at this size.
	thumbQuality = 88
	// thumbVideoSeek is how far into a video the still is taken. Frame zero of
	// a video is very often black.
	thumbVideoSeek = "2"
)

// Thumbnailer keeps a disk cache of small previews so the picker never decodes
// a full-size wallpaper on demand.
//
// It runs one job at a time with a deliberate gap between them, off the Wayland
// owner. A missing preview is not an error: the tile shows its kind glyph and
// picks the preview up on a later snapshot.
type Thumbnailer struct {
	dir  string
	pace time.Duration
	// extract pulls a still out of a video. It is injected so the generator is
	// testable without a media stack, and nil disables video previews.
	extract func(ctx context.Context, src, dst string) error

	queue    chan []Entry
	progress chan struct{}

	// done and total report generation progress. A first run over a real
	// library takes minutes, and without a count the picker just looks broken.
	mu    sync.Mutex
	done  int
	total int
}

// NewThumbnailer builds a generator writing into dir.
func NewThumbnailer(dir string, pace time.Duration) *Thumbnailer {
	if pace <= 0 {
		pace = ThumbPace
	}
	return &Thumbnailer{
		dir:      dir,
		pace:     pace,
		extract:  extractVideoStill,
		queue:    make(chan []Entry, 4),
		progress: make(chan struct{}, 1),
	}
}

// Counts reports how far generation has got: previews resolved, and how many
// the current library needs. Equal counts mean there is nothing left to say.
func (t *Thumbnailer) Counts() (done, total int) {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.done, t.total
}

func (t *Thumbnailer) setTotal(n int) {
	t.mu.Lock()
	t.done, t.total = 0, n
	t.mu.Unlock()
}

func (t *Thumbnailer) advance() {
	t.mu.Lock()
	t.done++
	t.mu.Unlock()
}

// Progress fires when at least one new preview has landed. It coalesces: a
// reader that misses a tick still sees every finished preview on disk.
func (t *Thumbnailer) Progress() <-chan struct{} { return t.progress }

// Enqueue submits a library for generation, replacing anything still waiting.
// It never blocks: the caller is the service loop.
func (t *Thumbnailer) Enqueue(entries []Entry) {
	for {
		select {
		case t.queue <- entries:
			return
		default:
		}
		select {
		case <-t.queue:
		default:
			return
		}
	}
}

// Run generates previews until the context is cancelled.
func (t *Thumbnailer) Run(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case entries := <-t.queue:
			t.generate(ctx, entries)
		}
	}
}

// generate walks one library, pacing between items and giving up the batch as
// soon as a newer one arrives.
func (t *Thumbnailer) generate(ctx context.Context, entries []Entry) {
	t.setTotal(len(entries))
	for _, entry := range entries {
		select {
		case <-ctx.Done():
			return
		case newer := <-t.queue:
			// The library changed under us; the new list wins.
			t.generate(ctx, newer)
			return
		default:
		}
		if entry.IsDir {
			t.advance()
			continue
		}
		made, err := t.ensure(ctx, entry)
		t.advance()
		if err == nil && made {
			t.note()
		}
		if !made {
			// Already cached: no work was done, so no reason to pace.
			continue
		}
		t.note()
		select {
		case <-ctx.Done():
			return
		case <-time.After(t.pace):
		}
	}
}

func (t *Thumbnailer) note() {
	select {
	case t.progress <- struct{}{}:
	default:
	}
}

// ensure writes the preview for one entry if it is not already there. It
// reports whether it created one.
func (t *Thumbnailer) ensure(ctx context.Context, entry Entry) (bool, error) {
	dst := t.pathFor(entry.Path)
	if dst == "" {
		return false, errors.New("wallpaper: no cache directory")
	}
	if _, err := os.Stat(dst); err == nil {
		return false, nil
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
		return false, err
	}
	if entry.Kind == KindVideo {
		if t.extract == nil {
			return false, errors.New("wallpaper: no video frame extractor")
		}
		return true, t.extract(ctx, entry.Path, dst)
	}
	return true, renderStill(entry.Path, dst)
}

// pathFor is where one source's preview lives inside this generator's cache.
func (t *Thumbnailer) pathFor(source string) string {
	if t.dir == "" {
		return ""
	}
	info, err := os.Stat(source)
	if err != nil {
		return ""
	}
	return filepath.Join(t.dir, cacheName(source, info.ModTime().Unix(), info.Size()))
}

// renderStill decodes one image and writes a cover-cropped preview.
func renderStill(src, dst string) error {
	file, err := os.Open(src)
	if err != nil {
		return err
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil {
		return err
	}
	if info.Size() > thumbMaxSource {
		return fmt.Errorf("wallpaper: %s is %d bytes", src, info.Size())
	}

	config, _, err := image.DecodeConfig(file)
	if err != nil {
		return err
	}
	if config.Width <= 0 || config.Height <= 0 ||
		config.Width > thumbMaxDimension || config.Height > thumbMaxDimension {
		return fmt.Errorf("wallpaper: %s is %dx%d", src, config.Width, config.Height)
	}
	if _, err := file.Seek(0, 0); err != nil {
		return err
	}
	source, _, err := image.Decode(file)
	if err != nil {
		return err
	}
	return writeJPEG(dst, coverScale(source))
}

// coverScale crops the source to the tile's aspect and scales it down. The
// crop happens here, once, because the painter scales a raster to fill its box
// with no aspect preservation: an uncropped preview would show every wallpaper
// stretched.
func coverScale(source image.Image) *image.RGBA {
	target := image.NewRGBA(image.Rect(0, 0, ThumbWidth, ThumbHeight))
	xdraw.CatmullRom.Scale(target, target.Bounds(), source,
		coverRect(source.Bounds(), ThumbWidth, ThumbHeight), xdraw.Src, nil)
	return target
}

// coverRect is the largest centred rectangle of bounds carrying the target
// aspect ratio.
func coverRect(bounds image.Rectangle, tw, th int) image.Rectangle {
	sw, sh := bounds.Dx(), bounds.Dy()
	if sw <= 0 || sh <= 0 || tw <= 0 || th <= 0 {
		return bounds
	}
	if sw*th > tw*sh {
		w := sh * tw / th
		x := bounds.Min.X + (sw-w)/2
		return image.Rect(x, bounds.Min.Y, x+w, bounds.Max.Y)
	}
	h := sw * th / tw
	y := bounds.Min.Y + (sh-h)/2
	return image.Rect(bounds.Min.X, y, bounds.Max.X, y+h)
}

// writeJPEG replaces dst atomically, so a half-written preview is never read.
func writeJPEG(dst string, img image.Image) error {
	tmp, err := os.CreateTemp(filepath.Dir(dst), ".thumb-*.tmp")
	if err != nil {
		return err
	}
	name := tmp.Name()
	committed := false
	defer func() {
		if !committed {
			_ = os.Remove(name)
		}
	}()
	if err := jpeg.Encode(tmp, img, &jpeg.Options{Quality: thumbQuality}); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(name, dst); err != nil {
		return err
	}
	committed = true
	return nil
}

// extractVideoStill pulls one frame out of a video.
//
// ffmpeg is preferred over gst-launch-1.0 because a single invocation does the
// seek, the crop, and the scale; the design treats the extractor as optional
// either way, and a failure leaves the tile on its kind glyph.
func extractVideoStill(ctx context.Context, src, dst string) error {
	name, err := exec.LookPath("ffmpeg")
	if err != nil {
		return fmt.Errorf("wallpaper: no video frame extractor: %w", err)
	}
	filter := fmt.Sprintf("scale=%d:%d:force_original_aspect_ratio=increase,crop=%d:%d",
		ThumbWidth, ThumbHeight, ThumbWidth, ThumbHeight)
	cmd := exec.CommandContext(ctx, name,
		"-nostdin", "-v", "error",
		"-ss", thumbVideoSeek, "-i", src,
		"-frames:v", "1", "-vf", filter,
		"-y", dst,
	)
	if err := cmd.Run(); err != nil {
		// A video shorter than the seek yields nothing; retry from the start
		// before giving up on it.
		retry := exec.CommandContext(ctx, name,
			"-nostdin", "-v", "error",
			"-i", src, "-frames:v", "1", "-vf", filter, "-y", dst)
		if retryErr := retry.Run(); retryErr != nil {
			return fmt.Errorf("wallpaper: extract still from %s: %w", src, err)
		}
	}
	return nil
}
