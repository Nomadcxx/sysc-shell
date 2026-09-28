package icons

import (
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"image"
	stdDraw "image/draw"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/srwiley/oksvg"
	"github.com/srwiley/rasterx"
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

// Key identifies one decoded result. The target box is part of the key, so the
// same image at two sizes is two entries rather than one rescaled badly.
//
// W and H are separate because not every consumer wants a square: an icon asks
// for one edge twice, a wallpaper thumbnail asks for a landscape box. Both must
// be positive.
type Key struct {
	Name    string
	W, H    int
	Overlay string
}

// Square is the key for an icon: one edge, used for both.
func Square(name string, size int) Key { return Key{Name: name, W: size, H: size} }

// nominal is the size used for icon-theme directory lookup, which is indexed by
// a single edge. The larger edge is the honest answer for a landscape box.
func (k Key) nominal() int { return max(k.W, k.H) }

// PathResolver is the Worker's source of candidate files: a name in, a
// decodable path out. The theme Resolver and the paths-only FileResolver
// both satisfy it.
type PathResolver interface {
	Resolve(name string, size int) (string, bool)
}

// Worker decodes icons away from the Wayland owner and publishes immutable
// results. One decode runs per job; duplicate requests for a key in flight
// collapse onto the first.
type Worker struct {
	resolver PathResolver
	jobs     chan Key
	publish  func(Key, *ui.Image)

	mu       sync.Mutex
	cache    map[Key]*ui.Image
	order    []Key
	bytes    int
	inFlight map[Key]struct{}

	// svgFailed memoises paths whose SVG failed to rasterise, so one bad
	// document logs once rather than on every repaint. Touched only on the
	// worker goroutine.
	svgFailed map[string]bool
}

func NewWorker(resolver PathResolver, publish func(Key, *ui.Image)) *Worker {
	return &Worker{
		resolver: resolver, publish: publish,
		jobs:      make(chan Key, MaxQueue),
		cache:     make(map[Key]*ui.Image),
		inFlight:  make(map[Key]struct{}),
		svgFailed: make(map[string]bool),
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
	if key.Name == "" || key.W <= 0 || key.H <= 0 {
		return nil, false, errors.New("icons: request has no name or no target box")
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
	base := w.load(ctx, key.Name, key.W, key.H, key.nominal())
	if base == nil {
		return nil
	}
	if key.Overlay == "" {
		return base
	}
	overlay := w.load(ctx, key.Overlay, key.W, key.H, key.nominal())
	if overlay == nil {
		return base
	}
	return compose(base, overlay)
}

func (w *Worker) load(ctx context.Context, name string, width, height, nominal int) *ui.Image {
	if ctx.Err() != nil {
		return nil
	}
	path, ok := w.resolver.Resolve(name, nominal)
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
	if strings.EqualFold(filepath.Ext(path), ".svg") {
		if img := decodeSVG(data, width, height); img != nil {
			return img
		}
		return w.fallBackToRaster(ctx, name, path, width, height, nominal)
	}
	return decodeRaster(data, width, height)
}

// fallBackToRaster re-resolves without the vector tier after an SVG failed to
// parse or draw. The failure is logged once per path: a theme full of
// unsupported SVG features must not spam the journal on every repaint.
func (w *Worker) fallBackToRaster(ctx context.Context, name, failed string, width, height, nominal int) *ui.Image {
	if !w.svgFailed[failed] {
		w.svgFailed[failed] = true
		log.Printf("icons: svg %s did not rasterise; using the raster chain", failed)
	}
	skip, ok := w.resolver.(interface {
		ResolveRaster(name string, size int) (string, bool)
	})
	if !ok {
		return nil
	}
	path, found := skip.ResolveRaster(name, nominal)
	if !found {
		return nil
	}
	data, err := readBounded(path, MaxFileBytes)
	if err != nil || ctx.Err() != nil {
		return nil
	}
	return decodeRaster(data, width, height)
}

// decodeSVG rasterises an SVG document into the canvas's BGRA layout, meet-fit
// and centred in the requested box. nil means the document could not be parsed
// or drawn; the caller falls back to the raster chain. Runs on the worker
// goroutine only -- never on the Wayland owner.
func decodeSVG(data []byte, width, height int) *ui.Image {
	if width <= 0 || height <= 0 {
		return nil
	}
	if svgUseCyclic(data) {
		return nil
	}
	icon, err := oksvg.ReadIconStream(bytes.NewReader(data), oksvg.StrictErrorMode)
	if err != nil || icon.ViewBox.W <= 0 || icon.ViewBox.H <= 0 {
		return nil
	}
	scale := min(float64(width)/icon.ViewBox.W, float64(height)/icon.ViewBox.H)
	w, h := icon.ViewBox.W*scale, icon.ViewBox.H*scale
	icon.SetTarget((float64(width)-w)/2, (float64(height)-h)/2, w, h)
	rgba := image.NewRGBA(image.Rect(0, 0, width, height))
	scanner := rasterx.NewScannerGV(width, height, rgba, rgba.Bounds())
	icon.Draw(rasterx.NewDasher(width, height, scanner), 1.0)
	return fromRGBA(rgba)
}

// svgUseCyclic reports whether a document's <use> references can reach
// themselves. oksvg expands <use> by recursing into the referenced subtree
// with no cycle check, so one self- or mutually-referencing file -- from a
// downloaded theme or any plugin-provided path -- overflows the stack, which
// is a fatal, unrecoverable crash of the whole shell. The guard walks ids and
// hrefs with the stdlib XML decoder and rejects exactly what oksvg would
// recurse on, so ordinary symbol/use documents still draw.
func svgUseCyclic(data []byte) bool {
	type frame struct{ id string }
	var stack []frame
	refs := map[string]map[string]bool{}
	dec := xml.NewDecoder(bytes.NewReader(data))
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return false // a broken document fails the real parse too
		}
		switch t := tok.(type) {
		case xml.StartElement:
			id, href := "", ""
			for _, a := range t.Attr {
				switch a.Name.Local {
				case "id":
					id = a.Value
				case "href":
					href = strings.TrimPrefix(a.Value, "#")
				}
			}
			stack = append(stack, frame{id: id})
			if href != "" {
				for _, f := range stack {
					if f.id != "" {
						if refs[f.id] == nil {
							refs[f.id] = map[string]bool{}
						}
						refs[f.id][href] = true
					}
				}
			}
		case xml.EndElement:
			stack = stack[:len(stack)-1]
		}
	}
	for start := range refs {
		cur, seen := []string{start}, map[string]bool{start: true}
		for len(cur) > 0 {
			id := cur[len(cur)-1]
			cur = cur[:len(cur)-1]
			for next := range refs[id] {
				if next == start {
					return true
				}
				if !seen[next] {
					seen[next] = true
					cur = append(cur, next)
				}
			}
		}
	}
	return false
}

// DecodeRaster applies the same image-header and source-dimension checks as
// Worker to bytes obtained by a caller-owned transport.
func DecodeRaster(data []byte, width, height int) *ui.Image {
	if width <= 0 || height <= 0 {
		return nil
	}
	return decodeRaster(data, width, height)
}

// decodeRaster turns encoded bytes into a premultiplied BGRA raster at the
// requested box. The source is scaled to cover the box and cropped around its
// centre, matching PreserveAspectCrop rather than stretching the source.
func decodeRaster(data []byte, width, height int) *ui.Image {
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
	target := image.NewRGBA(image.Rect(0, 0, width, height))
	scaleCrop(target, source)
	return fromRGBA(target)
}

// scaleCrop covers dst with src at one uniform scale, then takes the centred
// target-sized window. Scaling first keeps fractional crop edges correct even
// when the target is much wider or taller than one source pixel.
func scaleCrop(dst *image.RGBA, src image.Image) {
	if dst == nil || src == nil || dst.Bounds().Dx() <= 0 || dst.Bounds().Dy() <= 0 {
		return
	}
	sb := src.Bounds()
	sw, sh := sb.Dx(), sb.Dy()
	dw, dh := dst.Bounds().Dx(), dst.Bounds().Dy()
	if sw <= 0 || sh <= 0 {
		return
	}

	scaledW, scaledH := dw, dh
	if int64(sw)*int64(dh) > int64(sh)*int64(dw) {
		// The source is wider: match the target height and crop its sides.
		scaledW = (sw*dh + sh - 1) / sh
	} else {
		// The source is taller or equal: match the target width and crop top
		// and bottom.
		scaledH = (sh*dw + sw - 1) / sw
	}
	scaled := image.NewRGBA(image.Rect(0, 0, scaledW, scaledH))
	xdraw.CatmullRom.Scale(scaled, scaled.Bounds(), src, sb, xdraw.Src, nil)
	stdDraw.Draw(dst, dst.Bounds(), scaled,
		image.Point{X: (scaledW - dw) / 2, Y: (scaledH - dh) / 2}, stdDraw.Src)
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
