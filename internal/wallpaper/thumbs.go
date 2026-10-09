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
	"runtime"
	"strings"
	"sync"

	// Decoders for the still formats the library names. jxl has no decoder
	// in the module graph, so those tiles say they have no preview rather
	// than pulling in a new dependency.
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"

	_ "golang.org/x/image/bmp"
	_ "golang.org/x/image/webp"

	xdraw "golang.org/x/image/draw"
)

// Bounds on one thumbnail job.
const (
	// thumbMaxSource caps one source file read. A 4K still is tens of
	// megabytes; anything past this is not a wallpaper.
	thumbMaxSource = 256 << 20
	// thumbMaxDimension rejects a hostile or absurd header before it is
	// decoded into memory.
	thumbMaxDimension = 16384
	// thumbDecodeBudget bounds the decoded pixels the workers hold at once.
	// One 4K still is about 33 MB decoded; an 8K one about 130 MB. A source
	// larger than the whole budget still decodes, alone.
	thumbDecodeBudget = 512 << 20
	// thumbMaxWorkers caps the pool. Generation is background work; the pool
	// size is its throttle, so it stays well under the machine's cores.
	thumbMaxWorkers = 4
	// thumbQuality is the cached JPEG quality. These are 420x192; the file is
	// tens of kilobytes either way, and artefacts would be visible at this size.
	thumbQuality = 88
	// thumbVideoSeek is how far into a video the still is taken. Frame zero of
	// a video is very often black.
	thumbVideoSeek = "2"
)

// errNoExtractor is the machine lacking a video frame extractor. It is not
// recorded against the file: installing ffmpeg must bring the previews back.
var errNoExtractor = errors.New("wallpaper: no video frame extractor")

// ThumbCounts reports generation progress for the library and for the folder
// that was open when the walk started, which goes first.
type ThumbCounts struct {
	Done, Total             int
	Folder                  string
	FolderDone, FolderTotal int
}

// Thumbnailer keeps a disk cache of small previews so the picker never decodes
// a full-size wallpaper on demand.
//
// A small pool of workers runs off the Wayland owner, the open folder first. A
// file that cannot be previewed is recorded beside the cache entry, so it is
// tried once per version of the file rather than on every rescan.
type Thumbnailer struct {
	dir     string
	workers int
	budget  *decodeBudget
	// extract pulls a still out of a video. It is injected so the generator is
	// testable without a media stack, and nil disables video previews.
	extract func(ctx context.Context, src, dst string) error

	queue    chan thumbBatch
	progress chan struct{}

	// A first run over a real library takes a while; without a count the
	// picker just looks broken.
	mu     sync.Mutex
	counts ThumbCounts
}

// NewThumbnailer builds a generator writing into dir.
func NewThumbnailer(dir string) *Thumbnailer {
	return &Thumbnailer{
		dir:      dir,
		workers:  min(thumbMaxWorkers, max(1, runtime.GOMAXPROCS(0)/2)),
		budget:   newDecodeBudget(thumbDecodeBudget),
		extract:  extractVideoStill,
		queue:    make(chan thumbBatch, 1),
		progress: make(chan struct{}, 1),
	}
}

// thumbBatch is one library walk: the files, the open folder's first.
type thumbBatch struct {
	entries     []Entry
	folder      string
	folderCount int
}

// newThumbBatch orders a library for generation. Directories are dropped: they
// have no preview and must not count toward progress.
func newThumbBatch(entries []Entry, folder string) thumbBatch {
	b := thumbBatch{folder: folder, entries: make([]Entry, 0, len(entries))}
	var rest []Entry
	for _, e := range entries {
		switch {
		case e.IsDir:
		case folder != "" && filepath.Dir(e.Path) == folder:
			b.entries = append(b.entries, e)
		default:
			rest = append(rest, e)
		}
	}
	b.folderCount = len(b.entries)
	b.entries = append(b.entries, rest...)
	return b
}

// Counts reports how far generation has got.
func (t *Thumbnailer) Counts() ThumbCounts {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.counts
}

func (t *Thumbnailer) start(b thumbBatch) {
	t.mu.Lock()
	t.counts = ThumbCounts{Total: len(b.entries), Folder: b.folder, FolderTotal: b.folderCount}
	t.mu.Unlock()
}

func (t *Thumbnailer) advance(inFolder bool) {
	t.mu.Lock()
	t.counts.Done++
	if inFolder {
		t.counts.FolderDone++
	}
	t.mu.Unlock()
}

// Progress fires when at least one new preview has landed. It coalesces: a
// reader that misses a tick still sees every finished preview on disk.
func (t *Thumbnailer) Progress() <-chan struct{} { return t.progress }

// Enqueue submits a library for generation, the files in folder first,
// replacing anything still waiting. It never blocks: the caller is the service
// loop.
func (t *Thumbnailer) Enqueue(entries []Entry, folder string) {
	b := newThumbBatch(entries, folder)
	for {
		select {
		case t.queue <- b:
			return
		default:
		}
		select {
		case <-t.queue:
		default:
		}
	}
}

// Run generates previews until the context is cancelled.
func (t *Thumbnailer) Run(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case b := <-t.queue:
			for next := &b; next != nil; {
				next = t.generate(ctx, *next)
			}
		}
	}
}

// generate walks one batch with the worker pool. A newer batch arriving stops
// the walk, cancels the work in flight, and is returned to run next.
func (t *Thumbnailer) generate(ctx context.Context, b thumbBatch) *thumbBatch {
	t.start(b)
	walk, cancel := context.WithCancel(ctx)
	defer cancel()
	jobs := make(chan int)
	var wg sync.WaitGroup
	for range max(t.workers, 1) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range jobs {
				t.one(walk, b.entries[i], i < b.folderCount)
			}
		}()
	}
	var next *thumbBatch
feed:
	for i := range b.entries {
		select {
		case jobs <- i:
		case newer := <-t.queue:
			next = &newer
			cancel()
			break feed
		case <-ctx.Done():
			break feed
		}
	}
	close(jobs)
	wg.Wait()
	// The tail of a walk is often all cache hits, which fire no per-item
	// tick. Announce the finished counts anyway, or the picker shows a stale
	// "generating previews" number forever.
	t.note()
	return next
}

// one resolves a single entry and counts it.
func (t *Thumbnailer) one(ctx context.Context, entry Entry, inFolder bool) {
	if ctx.Err() != nil {
		return
	}
	made, _ := t.ensure(ctx, entry)
	if ctx.Err() != nil {
		return
	}
	t.advance(inFolder)
	if made {
		t.note()
	}
}

func (t *Thumbnailer) note() {
	select {
	case t.progress <- struct{}{}:
	default:
	}
}

// ensure writes the preview for one entry if it is not already there or
// recorded as impossible. It reports whether it tried to make one.
func (t *Thumbnailer) ensure(ctx context.Context, entry Entry) (bool, error) {
	dst := t.pathFor(entry.Path)
	if dst == "" {
		return false, errors.New("wallpaper: no cache directory")
	}
	if _, err := os.Stat(dst); err == nil {
		return false, nil
	}
	if _, err := os.Stat(failMarker(dst)); err == nil {
		return false, nil
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
		return false, err
	}
	var err error
	if entry.Kind == KindVideo {
		if t.extract == nil {
			return true, errNoExtractor
		}
		err = t.extract(ctx, entry.Path, dst)
	} else {
		err = t.renderStill(entry.Path, dst)
	}
	if err != nil && ctx.Err() == nil && !errors.Is(err, errNoExtractor) {
		// The file itself cannot be previewed. Recording it keeps a rescan
		// from paying for the same failure, and lets the tile say so.
		_ = os.WriteFile(failMarker(dst), nil, 0o600)
	}
	return true, err
}

// failed reports a recorded failure for this version of source.
func (t *Thumbnailer) failed(source string) bool {
	dst := t.pathFor(source)
	if dst == "" {
		return false
	}
	_, err := os.Stat(failMarker(dst))
	return err == nil
}

// failMarker is the empty file recording that dst could not be made. It shares
// the preview's key, so a changed source is retried.
func failMarker(dst string) string {
	return strings.TrimSuffix(dst, filepath.Ext(dst)) + ".fail"
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

// decodeBudget bounds the bytes of decoded pixels held across workers. A
// request larger than the whole budget waits for an empty budget and then runs
// alone.
type decodeBudget struct {
	mu    sync.Mutex
	cond  *sync.Cond
	limit int64
	used  int64
}

func newDecodeBudget(limit int64) *decodeBudget {
	b := &decodeBudget{limit: limit}
	b.cond = sync.NewCond(&b.mu)
	return b
}

func (b *decodeBudget) acquire(n int64) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for b.used > 0 && b.used+n > b.limit {
		b.cond.Wait()
	}
	b.used += n
}

func (b *decodeBudget) release(n int64) {
	b.mu.Lock()
	b.used -= n
	b.mu.Unlock()
	b.cond.Broadcast()
}

// renderStill decodes one image and writes a cover-cropped preview. The
// decode holds its share of the budget until the preview is scaled.
func (t *Thumbnailer) renderStill(src, dst string) error {
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
	cost := int64(config.Width) * int64(config.Height) * 4
	t.budget.acquire(cost)
	source, _, err := image.Decode(file)
	var preview *image.RGBA
	if err == nil {
		preview = coverScale(source)
	}
	t.budget.release(cost)
	if err != nil {
		return err
	}
	return writeJPEG(dst, preview)
}

// coverScale crops the source to the tile's aspect and scales it down. The
// crop happens here, once, because the painter scales a raster to fill its box
// with no aspect preservation: an uncropped preview would show every wallpaper
// stretched.
//
// It scales in two steps. CatmullRom straight from a 4K source costs as much
// as decoding it; a bilinear pass to twice the target first, then CatmullRom,
// gives the same result at this size for a few percent of the cost.
func coverScale(source image.Image) *image.RGBA {
	crop := coverRect(source.Bounds(), PreviewWidth, PreviewHeight)
	from := source
	if crop.Dx() > 2*PreviewWidth && crop.Dy() > 2*PreviewHeight {
		mid := image.NewRGBA(image.Rect(0, 0, 2*PreviewWidth, 2*PreviewHeight))
		xdraw.ApproxBiLinear.Scale(mid, mid.Bounds(), source, crop, xdraw.Src, nil)
		from, crop = mid, mid.Bounds()
	}
	target := image.NewRGBA(image.Rect(0, 0, PreviewWidth, PreviewHeight))
	xdraw.CatmullRom.Scale(target, target.Bounds(), from, crop, xdraw.Src, nil)
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
		return fmt.Errorf("%w: %w", errNoExtractor, err)
	}
	filter := fmt.Sprintf("scale=%d:%d:force_original_aspect_ratio=increase,crop=%d:%d",
		PreviewWidth, PreviewHeight, PreviewWidth, PreviewHeight)
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
