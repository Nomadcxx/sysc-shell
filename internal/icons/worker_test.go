package icons

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/png"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

func TestWorkerDecodesAndCachesByKey(t *testing.T) {
	worker, _ := startWorker(t)

	key := Square("chat", 24)
	if _, cached, err := worker.Request(key); err != nil || cached {
		t.Fatalf("first request = cached %v, err %v", cached, err)
	}
	result := awaitImage(t, worker, key)
	if result.Width != 24 || result.Height != 24 {
		t.Fatalf("decoded %dx%d, want the requested 24x24", result.Width, result.Height)
	}
	if result.Stride != 24*4 || len(result.Pix) != 24*24*4 {
		t.Fatalf("raster geometry = stride %d, %d bytes", result.Stride, len(result.Pix))
	}

	// A second request is served from the cache without new work.
	got, cached, err := worker.Request(key)
	if err != nil || !cached || got != result {
		t.Fatalf("second request = %v cached %v err %v", got != nil, cached, err)
	}

	// Size is part of the key, so another size is another entry.
	other := Square("chat", 48)
	if _, cached, _ := worker.Request(other); cached {
		t.Fatal("a different size was served from the 24px entry")
	}
	if awaitImage(t, worker, other).Width != 48 {
		t.Fatal("the second size did not decode at its own size")
	}
}

func TestWorkerCollapsesDuplicateJobs(t *testing.T) {
	worker, _ := startWorker(t)
	key := Square("chat", 24)

	for range 5 {
		if _, _, err := worker.Request(key); err != nil {
			t.Fatal(err)
		}
	}
	awaitImage(t, worker, key)

	worker.mu.Lock()
	pending := len(worker.inFlight)
	worker.mu.Unlock()
	if pending != 0 {
		t.Fatalf("%d jobs remained in flight", pending)
	}
	if len(worker.jobs) != 0 {
		t.Fatalf("%d duplicate jobs were queued", len(worker.jobs))
	}
}

func TestWorkerRefusesWorkPastItsQueue(t *testing.T) {
	root := iconRoot(t)
	worker := NewWorker(NewResolver("Adwaita", []string{root}), nil)
	// Nothing is running, so the queue fills and then refuses.
	var busy error
	for i := 0; i < MaxQueue*4 && busy == nil; i++ {
		_, _, busy = worker.Request(Square("chat", i+1))
	}
	if !errors.Is(busy, ErrBusy) {
		t.Fatalf("Request eventually returned %v, want ErrBusy", busy)
	}
	if len(worker.jobs) != MaxQueue {
		t.Fatalf("queue holds %d jobs, want %d", len(worker.jobs), MaxQueue)
	}
}

func TestWorkerRefusesMalformedRequests(t *testing.T) {
	worker, _ := startWorker(t)
	for name, key := range map[string]Key{
		"no name":  {W: 24, H: 24},
		"no box":   {Name: "chat"},
		"negative": {Name: "chat", W: -1, H: -1},
		"half box": {Name: "chat", W: 24},
	} {
		t.Run(name, func(t *testing.T) {
			if _, _, err := worker.Request(key); err == nil {
				t.Fatal("Request accepted an unusable key")
			}
		})
	}
}

func TestWorkerPublishesNilForUnreadableIcons(t *testing.T) {
	root := iconRoot(t)
	// A file that claims to be a PNG but is not.
	dir := filepath.Join(root, "Adwaita", "48x48", "apps")
	if err := os.WriteFile(filepath.Join(dir, "broken.png"), []byte("not a png"), 0o644); err != nil {
		t.Fatal(err)
	}
	worker, results := startWorkerAt(t, root)

	for name, key := range map[string]Key{
		"malformed": Square("broken", 24),
		"absent":    Square("missing", 24),
	} {
		t.Run(name, func(t *testing.T) {
			if _, _, err := worker.Request(key); err != nil {
				t.Fatal(err)
			}
			result := awaitResult(t, results, key)
			if result != nil {
				t.Fatal("an unreadable icon produced a raster")
			}
			if _, cached := worker.Lookup(key); cached {
				t.Fatal("a failed decode was cached")
			}
		})
	}
}

func TestWorkerEvictsOldestEntriesPastTheCacheBound(t *testing.T) {
	worker, _ := startWorker(t)
	worker.mu.Lock()
	for i := range MaxCacheEntries + 8 {
		worker.store(Square("icon", i+1), &ui.Image{
			Width: 1, Height: 1, Stride: 4, Pix: make([]byte, 4),
		})
	}
	entries := len(worker.cache)
	_, oldestKept := worker.cache[Square("icon", 1)]
	_, newestKept := worker.cache[Square("icon", MaxCacheEntries+8)]
	worker.mu.Unlock()

	if entries > MaxCacheEntries {
		t.Fatalf("cache holds %d entries, want at most %d", entries, MaxCacheEntries)
	}
	if oldestKept {
		t.Fatal("the oldest entry survived eviction")
	}
	if !newestKept {
		t.Fatal("the newest entry was evicted")
	}
}

func TestWorkerStopsWhenItsContextIsCancelled(t *testing.T) {
	root := iconRoot(t)
	worker := NewWorker(NewResolver("Adwaita", []string{root}), nil)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- worker.Run(ctx) }()

	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Run() returned %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Run() did not return after cancellation")
	}
}

func TestWorkerComposesAnOverlay(t *testing.T) {
	worker, _ := startWorker(t)
	key := Key{Name: "chat", Overlay: "chat", W: 32, H: 32}
	if _, _, err := worker.Request(key); err != nil {
		t.Fatal(err)
	}
	result := awaitImage(t, worker, key)
	if result.Width != 32 || result.Height != 32 {
		t.Fatalf("composed %dx%d, want 32x32", result.Width, result.Height)
	}
	// A missing overlay leaves the base icon usable rather than failing.
	fallback := Key{Name: "chat", Overlay: "absent", W: 32, H: 32}
	if _, _, err := worker.Request(fallback); err != nil {
		t.Fatal(err)
	}
	if awaitImage(t, worker, fallback) == nil {
		t.Fatal("a missing overlay lost the base icon")
	}
}

func startWorker(t *testing.T) (*Worker, chan result) {
	t.Helper()
	return startWorkerAt(t, iconRoot(t))
}

func startWorkerAt(t *testing.T, root string) (*Worker, chan result) {
	t.Helper()
	results := make(chan result, 64)
	worker := NewWorker(NewResolver("Adwaita", []string{root}), func(k Key, img *ui.Image) {
		results <- result{key: k, image: img}
	})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = worker.Run(ctx)
	}()
	t.Cleanup(func() {
		cancel()
		<-done
	})
	return worker, results
}

type result struct {
	key   Key
	image *ui.Image
}

func awaitImage(t *testing.T, worker *Worker, key Key) *ui.Image {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if img, ok := worker.Lookup(key); ok {
			return img
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("no raster for %+v", key)
	return nil
}

func awaitResult(t *testing.T, results chan result, key Key) *ui.Image {
	t.Helper()
	deadline := time.After(5 * time.Second)
	for {
		select {
		case got := <-results:
			if got.key == key {
				return got.image
			}
		case <-deadline:
			t.Fatalf("no result for %+v", key)
		}
	}
}

func iconRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	for _, size := range []string{"24x24", "32x32", "48x48"} {
		dir := filepath.Join(root, "Adwaita", size, "apps")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "chat.png"), pngBytes(t, 32), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

// pngBytes encodes a small opaque square.
func pngBytes(t *testing.T, size int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, size, size))
	for y := range size {
		for x := range size {
			img.Set(x, y, color.RGBA{R: 0x40, G: 0x80, B: 0xc0, A: 0xff})
		}
	}
	var buffer bytes.Buffer
	if err := png.Encode(&buffer, img); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}

func TestDecodeRasterPreservesAspectByCroppingTheCentre(t *testing.T) {
	src := image.NewRGBA(image.Rect(0, 0, 3, 1))
	src.SetRGBA(0, 0, color.RGBA{R: 0xff, A: 0xff})
	src.SetRGBA(1, 0, color.RGBA{G: 0xff, A: 0xff})
	src.SetRGBA(2, 0, color.RGBA{B: 0xff, A: 0xff})
	var data bytes.Buffer
	if err := png.Encode(&data, src); err != nil {
		t.Fatal(err)
	}

	got := DecodeRaster(data.Bytes(), 1, 1)
	if got == nil {
		t.Fatal("decode returned no raster")
	}
	if got.Pix[0] != 0 || got.Pix[1] != 0xff || got.Pix[2] != 0 || got.Pix[3] != 0xff {
		t.Fatalf("cropped pixel in BGRA = %v, want the centred green pixel", got.Pix[:4])
	}
}

func TestWorkerDecodesANonSquareTarget(t *testing.T) {
	// A wallpaper thumbnail is landscape. The decode target has to carry both
	// edges, because scaling a 16:9 source into a square box is a visible
	// stretch, not a crop.
	dir := t.TempDir()
	path := filepath.Join(dir, "wall.png")
	if err := os.WriteFile(path, pngBytes(t, 64), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}

	var got *ui.Image
	done := make(chan struct{})
	worker := NewWorker(NewResolver("", nil), func(_ Key, image *ui.Image) {
		got = image
		close(done)
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = worker.Run(ctx) }()

	if _, _, err := worker.Request(Key{Name: path, W: 210, H: 96}); err != nil {
		t.Fatalf("request: %v", err)
	}
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("decode did not publish")
	}
	if got == nil {
		t.Fatal("decode published nothing")
	}
	if got.Width != 210 || got.Height != 96 {
		t.Fatalf("decoded %dx%d, want 210x96", got.Width, got.Height)
	}
}

func TestWorkerRejectsAHalfSpecifiedBox(t *testing.T) {
	worker := NewWorker(NewResolver("", nil), func(Key, *ui.Image) {})
	for _, key := range []Key{
		{Name: "/tmp/a.png"},
		{Name: "/tmp/a.png", W: 210},
		{Name: "/tmp/a.png", H: 96},
		{Name: "", W: 210, H: 96},
	} {
		if _, _, err := worker.Request(key); err == nil {
			t.Errorf("Request(%+v) must fail", key)
		}
	}
}

func TestResolverAcceptsAnyDecodableAbsolutePath(t *testing.T) {
	// An absolute path is the caller naming an exact file; the icon-theme
	// extension preference does not apply. Wallpaper previews are JPEG, and
	// gating them on the theme list rejected every one of them.
	dir := t.TempDir()
	resolver := NewResolver("", nil)
	for _, name := range []string{"preview.jpg", "preview.jpeg", "icon.png"} {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, pngBytes(t, 8), 0o644); err != nil {
			t.Fatalf("seed %s: %v", name, err)
		}
		if got, ok := resolver.Resolve(path, 210); !ok || got != path {
			t.Errorf("Resolve(%s) = %q, %v; want the path itself", name, got, ok)
		}
	}
	// A format the decoder does not read is still refused.
	other := filepath.Join(dir, "notes.txt")
	if err := os.WriteFile(other, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, ok := resolver.Resolve(other, 210); ok {
		t.Error("a non-image absolute path must not resolve")
	}
}

func TestFileResolverTakesOnlyAbsoluteDecodablePaths(t *testing.T) {
	t.Parallel()

	var r FileResolver
	dir := t.TempDir()
	real := filepath.Join(dir, "a.png")
	if err := os.WriteFile(real, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if path, ok := r.Resolve(real, 96); !ok || path != real {
		t.Fatalf("absolute png = %q, %v", path, ok)
	}
	// An absolute .svg resolves since Task 4 put .svg in the decodable set;
	// FileResolver and notification image-paths go through here.
	svg := filepath.Join(dir, "a.svg")
	if err := os.WriteFile(svg, []byte(testGlyph), 0o600); err != nil {
		t.Fatal(err)
	}
	if path, ok := r.Resolve(svg, 96); !ok || path != svg {
		t.Fatalf("absolute svg = %q, %v", path, ok)
	}
	for _, name := range []string{"", "relative.png", filepath.Join(dir, "a.txt"), filepath.Join(dir, "missing.png")} {
		if _, ok := r.Resolve(name, 96); ok {
			t.Fatalf("resolved %q", name)
		}
	}
}

// testGlyph is a two-tone document: a circle with a rectangle path over its
// lower half. Both fills are opaque, where premultiplied and straight alpha
// agree, so interior points assert exact BGRA bytes.
const testGlyph = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24">` +
	`<circle cx="12" cy="12" r="10" fill="#204080"/>` +
	`<path d="M6 12h12v6h-12z" fill="#802040"/></svg>`

func TestDecodeSVGRasterisesTheGoldenGlyph(t *testing.T) {
	for _, size := range []int{24, 48} {
		img := decodeSVG([]byte(testGlyph), size, size)
		if img == nil {
			t.Fatalf("size %d: decode returned no raster", size)
		}
		if img.Width != size || img.Height != size || img.Stride != size*4 {
			t.Fatalf("size %d: geometry %dx%d stride %d", size, img.Width, img.Height, img.Stride)
		}
		at := func(x, y int) []byte {
			i := y*img.Stride + x*4
			return img.Pix[i : i+4 : i+4]
		}
		if got := at(size/2, size*15/24); got[0] != 0x40 || got[1] != 0x20 || got[2] != 0x80 || got[3] != 0xff {
			t.Fatalf("size %d: path interior = %v, want #802040 in BGRA", size, got)
		}
		if got := at(size/2, size*6/24); got[0] != 0x80 || got[1] != 0x40 || got[2] != 0x20 || got[3] != 0xff {
			t.Fatalf("size %d: circle interior = %v, want #204080 in BGRA", size, got)
		}
		if at(0, 0)[3] != 0 {
			t.Fatalf("size %d: the corner is not transparent", size)
		}
		coverage := 0
		for i := 3; i < len(img.Pix); i += 4 {
			if img.Pix[i] > 0 {
				coverage++
			}
		}
		// The circle plus the path's lower half cover about 60% of the box; a
		// band catches anti-aliased edges without pinning a renderer version.
		if coverage < size*size/2 || coverage > size*size*7/10 {
			t.Fatalf("size %d: coverage %d/%d outside the 50-70%% band", size, coverage, size*size)
		}
	}
}

func TestWorkerFallsBackToARasterWhenTheSvgFails(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "Adwaita", "48x48", "apps")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	// A filter is real SVG that oksvg cannot draw: strict mode must reject it
	// so the resolver's raster tier takes over.
	body := `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24">` +
		`<filter id="f"/><circle cx="12" cy="12" r="10" fill="#000000"/></svg>`
	if err := os.WriteFile(filepath.Join(dir, "chat.svg"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "chat.png"), pngBytes(t, 32), 0o644); err != nil {
		t.Fatal(err)
	}
	worker, _ := startWorkerAt(t, root)
	key := Square("chat", 24)
	if _, _, err := worker.Request(key); err != nil {
		t.Fatal(err)
	}
	img := awaitImage(t, worker, key)
	if img == nil || img.Width != 24 {
		t.Fatalf("fallback raster = %v", img)
	}
}

func TestWorkerDecodesAnSvgOnlyTheme(t *testing.T) {
	root := t.TempDir()
	// Adwaita because startWorkerAt resolves that theme; the point is that
	// nothing but an SVG exists.
	dir := filepath.Join(root, "Adwaita", "scalable", "apps")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "chat.svg"), []byte(testGlyph), 0o644); err != nil {
		t.Fatal(err)
	}
	worker, _ := startWorkerAt(t, root)
	key := Square("chat", 24)
	if _, _, err := worker.Request(key); err != nil {
		t.Fatal(err)
	}
	if img := awaitImage(t, worker, key); img == nil || img.Width != 24 {
		t.Fatalf("svg decode = %v", img)
	}
}

// Recursive <use> references make oksvg's drawer recurse without a cycle
// check; a 125-byte document overflows the stack, which is fatal and
// unrecoverable. The decoder must reject them before handing bytes over.
const svgUseSelfRef = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24">` +
	`<defs><g id="a"><use href="#a"/></g></defs><use href="#a"/></svg>`

const svgUseMutualRef = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24">` +
	`<defs><g id="a"><use href="#b"/></g><g id="b"><use href="#a"/></g></defs>` +
	`<use href="#a"/></svg>`

const svgUseLegit = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24">` +
	`<defs><g id="s"><rect x="0" y="0" width="24" height="24" fill="#00FF00"/></g></defs>` +
	`<use href="#s"/></svg>`

func TestDecodeSVGRejectsRecursiveUse(t *testing.T) {
	if !svgUseCyclic([]byte(svgUseSelfRef)) {
		t.Error("self-referencing use not detected")
	}
	if !svgUseCyclic([]byte(svgUseMutualRef)) {
		t.Error("mutually referencing uses not detected")
	}
	if svgUseCyclic([]byte(svgUseLegit)) {
		t.Error("a plain use flagged as cyclic")
	}
	if img := decodeSVG([]byte(svgUseSelfRef), 24, 24); img != nil {
		t.Error("recursive svg decoded instead of rejected")
	}
	if img := decodeSVG([]byte(svgUseLegit), 24, 24); img == nil || img.Width != 24 {
		t.Fatalf("legit use decoded to %v", img)
	}
}

func TestWorkerSurvivesARecursiveSvg(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "Adwaita", "48x48", "apps")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "chat.svg"), []byte(svgUseSelfRef), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "chat.png"), pngBytes(t, 32), 0o644); err != nil {
		t.Fatal(err)
	}
	worker, _ := startWorkerAt(t, root)
	key := Square("chat", 24)
	if _, _, err := worker.Request(key); err != nil {
		t.Fatal(err)
	}
	if img := awaitImage(t, worker, key); img == nil || img.Width != 24 {
		t.Fatalf("worker after recursive svg = %v", img)
	}
}

func TestSvgFailureIsLoggedOncePerPath(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "Adwaita", "48x48", "apps")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "chat.svg"), []byte(svgUseSelfRef), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "chat.png"), pngBytes(t, 32), 0o644); err != nil {
		t.Fatal(err)
	}
	worker, _ := startWorkerAt(t, root)

	var buf bytes.Buffer
	before := log.Writer()
	log.SetOutput(&buf)
	defer log.SetOutput(before)

	// Two sizes of the same icon resolve to the same failing svg path.
	for _, size := range []int{24, 32} {
		key := Square("chat", size)
		if _, _, err := worker.Request(key); err != nil {
			t.Fatal(err)
		}
		if img := awaitImage(t, worker, key); img == nil || img.Width != size {
			t.Fatalf("size %d = %v", size, img)
		}
	}
	log.SetOutput(before)
	if n := strings.Count(buf.String(), "did not rasterise"); n != 1 {
		t.Fatalf("failure logged %d times, want 1:\n%s", n, buf.String())
	}
}
