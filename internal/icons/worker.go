package icons

import (
	"bytes"
	"context"
	"errors"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"os"
	"sync"

	xdraw "golang.org/x/image/draw"

	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

// Bounds on decode work. A tray or notification source is another process's
// data: everything it can grow is capped before allocation.
const (
	// MaxQueue is the depth of the pending job queue. Past it a request is
	// refused rather than queued, so a storm of icons cannot grow memory.
	MaxQueue = 32
	// MaxFileBytes caps one icon file read from disk.
	MaxFileBytes = 8 << 20
	// MaxSourceDimension caps a decoded source edge before scaling.
	MaxSourceDimension = 4096
	// MaxCacheEntries and MaxCacheBytes bound the result cache.
	MaxCacheEntries = 256
	MaxCacheBytes   = 32 << 20
)

// ErrBusy reports a full job queue.
var ErrBusy = errors.New("icons: decode queue is full")

// Key identifies one decoded result. The size is part of the key, so the same
// icon at two sizes is two entries rather than one rescaled badly.
type Key struct {
	Name    string
	Size    int
	Overlay string
}

// Worker decodes icons away from the Wayland owner and publishes immutable
// results. One decode runs per job; duplicate requests for a key in flight
// collapse onto the first.
type Worker struct {
	resolver *Resolver
	jobs     chan Key
	publish  func(Key, *ui.Image)

	mu       sync.Mutex
	cache    map[Key]*ui.Image
	order    []Key
	bytes    int
	inFlight map[Key]struct{}
}

func NewWorker(resolver *Resolver, publish func(Key, *ui.Image)) *Worker {
	return &Worker{
		resolver: resolver, publish: publish,
		jobs:     make(chan Key, MaxQueue),
		cache:    make(map[Key]*ui.Image),
		inFlight: make(map[Key]struct{}),
	}
}

// Lookup reports a cached result without queueing work.
func (w *Worker) Lookup(key Key) (*ui.Image, bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	image, ok := w.cache[key]
	return image, ok
}

// Request queues a decode. A cached key returns at once; a key already being
// decoded collapses onto that job rather than queueing a second one.
func (w *Worker) Request(key Key) (*ui.Image, bool, error) {
	if key.Name == "" || key.Size <= 0 {
		return nil, false, errors.New("icons: request has no name or size")
	}
	w.mu.Lock()
	if cached, ok := w.cache[key]; ok {
		w.mu.Unlock()
		return cached, true, nil
	}
	if _, pending := w.inFlight[key]; pending {
		w.mu.Unlock()
		return nil, false, nil
	}
	w.inFlight[key] = struct{}{}
	w.mu.Unlock()

	select {
	case w.jobs <- key:
		return nil, false, nil
	default:
		w.mu.Lock()
		delete(w.inFlight, key)
		w.mu.Unlock()
		return nil, false, ErrBusy
	}
}

// Run decodes queued jobs until the context is cancelled.
func (w *Worker) Run(ctx context.Context) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case key := <-w.jobs:
			w.decode(ctx, key)
		}
	}
}

// decode resolves and decodes one key. A malformed or missing icon publishes a
// nil result, which the caller renders as its own placeholder: one bad icon
// never fails a card.
func (w *Worker) decode(ctx context.Context, key Key) {
	result := w.raster(ctx, key)
	w.mu.Lock()
	delete(w.inFlight, key)
	if result != nil {
		w.store(key, result)
	}
	w.mu.Unlock()
	if w.publish != nil {
		w.publish(key, result)
	}
}

func (w *Worker) raster(ctx context.Context, key Key) *ui.Image {
	base := w.load(ctx, key.Name, key.Size)
	if base == nil {
		return nil
	}
	if key.Overlay == "" {
		return base
	}
	overlay := w.load(ctx, key.Overlay, key.Size)
	if overlay == nil {
		return base
	}
	return compose(base, overlay)
}

func (w *Worker) load(ctx context.Context, name string, size int) *ui.Image {
	if ctx.Err() != nil {
		return nil
	}
	path, ok := w.resolver.Resolve(name, size)
	if !ok {
		return nil
	}
	data, err := readBounded(path, MaxFileBytes)
	if err != nil {
		return nil
	}
	if ctx.Err() != nil {
		return nil
	}
	return decodeRaster(data, size)
}

// decodeRaster turns encoded bytes into a premultiplied BGRA raster at size.
func decodeRaster(data []byte, size int) *ui.Image {
	config, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return nil
	}
	// The dimensions are checked before decoding, so a hostile header cannot
	// make the decoder allocate an enormous buffer.
	if config.Width <= 0 || config.Height <= 0 ||
		config.Width > MaxSourceDimension || config.Height > MaxSourceDimension {
		return nil
	}
	source, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil
	}
	target := image.NewRGBA(image.Rect(0, 0, size, size))
	xdraw.ApproxBiLinear.Scale(target, target.Bounds(), source, source.Bounds(), xdraw.Src, nil)
	return fromRGBA(target)
}

// fromRGBA converts Go's premultiplied RGBA into the canvas's B, G, R, A order.
func fromRGBA(src *image.RGBA) *ui.Image {
	width, height := src.Rect.Dx(), src.Rect.Dy()
	out := &ui.Image{Width: width, Height: height, Stride: width * 4, Pix: make([]byte, width*height*4)}
	for y := range height {
		for x := range width {
			from := y*src.Stride + x*4
			to := y*out.Stride + x*4
			out.Pix[to+0] = src.Pix[from+2]
			out.Pix[to+1] = src.Pix[from+1]
			out.Pix[to+2] = src.Pix[from+0]
			out.Pix[to+3] = src.Pix[from+3]
		}
	}
	return out
}

// compose draws an overlay over the lower-right quadrant of a base icon, the
// placement the StatusNotifierItem overlay convention expects.
func compose(base, overlay *ui.Image) *ui.Image {
	out := &ui.Image{
		Width: base.Width, Height: base.Height, Stride: base.Stride,
		Pix: append([]byte(nil), base.Pix...),
	}
	originX, originY := base.Width/2, base.Height/2
	for y := range base.Height - originY {
		for x := range base.Width - originX {
			srcX := x * overlay.Width / max(base.Width-originX, 1)
			srcY := y * overlay.Height / max(base.Height-originY, 1)
			from := srcY*overlay.Stride + srcX*4
			if from+4 > len(overlay.Pix) {
				continue
			}
			alpha := uint32(overlay.Pix[from+3])
			if alpha == 0 {
				continue
			}
			to := (originY+y)*out.Stride + (originX+x)*4
			if to+4 > len(out.Pix) {
				continue
			}
			inverse := 255 - alpha
			for i := range 4 {
				out.Pix[to+i] = uint8(uint32(overlay.Pix[from+i]) + uint32(out.Pix[to+i])*inverse/255)
			}
		}
	}
	return out
}

// store caches a result, evicting oldest-first to stay inside both bounds.
func (w *Worker) store(key Key, result *ui.Image) {
	size := len(result.Pix)
	if size > MaxCacheBytes {
		return
	}
	if _, exists := w.cache[key]; !exists {
		w.order = append(w.order, key)
	}
	w.cache[key] = result
	w.bytes += size
	for len(w.cache) > MaxCacheEntries || w.bytes > MaxCacheBytes {
		if len(w.order) == 0 {
			return
		}
		oldest := w.order[0]
		w.order = w.order[1:]
		if evicted, ok := w.cache[oldest]; ok {
			w.bytes -= len(evicted.Pix)
			delete(w.cache, oldest)
		}
	}
}

func readBounded(path string, limit int) ([]byte, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if info.Size() > int64(limit) {
		return nil, errors.New("icons: file exceeds its bound")
	}
	return os.ReadFile(path)
}
