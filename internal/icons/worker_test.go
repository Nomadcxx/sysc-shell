package icons

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

func TestWorkerDecodesAndCachesByKey(t *testing.T) {
	worker, _ := startWorker(t)

	key := Key{Name: "chat", Size: 24}
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
	other := Key{Name: "chat", Size: 48}
	if _, cached, _ := worker.Request(other); cached {
		t.Fatal("a different size was served from the 24px entry")
	}
	if awaitImage(t, worker, other).Width != 48 {
		t.Fatal("the second size did not decode at its own size")
	}
}

func TestWorkerCollapsesDuplicateJobs(t *testing.T) {
	worker, _ := startWorker(t)
	key := Key{Name: "chat", Size: 24}

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
		_, _, busy = worker.Request(Key{Name: "chat", Size: i + 1})
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
		"no name":  {Size: 24},
		"no size":  {Name: "chat"},
		"negative": {Name: "chat", Size: -1},
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
		"malformed": {Name: "broken", Size: 24},
		"absent":    {Name: "missing", Size: 24},
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
		worker.store(Key{Name: "icon", Size: i + 1}, &ui.Image{
			Width: 1, Height: 1, Stride: 4, Pix: make([]byte, 4),
		})
	}
	entries := len(worker.cache)
	_, oldestKept := worker.cache[Key{Name: "icon", Size: 1}]
	_, newestKept := worker.cache[Key{Name: "icon", Size: MaxCacheEntries + 8}]
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
	key := Key{Name: "chat", Size: 32, Overlay: "chat"}
	if _, _, err := worker.Request(key); err != nil {
		t.Fatal(err)
	}
	result := awaitImage(t, worker, key)
	if result.Width != 32 || result.Height != 32 {
		t.Fatalf("composed %dx%d, want 32x32", result.Width, result.Height)
	}
	// A missing overlay leaves the base icon usable rather than failing.
	fallback := Key{Name: "chat", Size: 32, Overlay: "absent"}
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
