package shell

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"path/filepath"
	"sync"
	"time"

	"github.com/Nomadcxx/sysc-shell/internal/icons"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

const (
	mediaArtBox      = 72
	mediaArtWatchdog = 5 * time.Second
)

// mediaArtWorker bounds album-art reads at the shell boundary. Local jobs use
// their own icons.Worker because that worker passes one context through its
// whole decode; a stuck FIFO must not stop the next track from being tried.
// Remote jobs use the same per-job context for the HTTP request.
// The abandoned reader after a timeout is bounded to one goroutine per unique
// ArtKey and is released when the source eventually returns or the registry
// closes.
type mediaArtWorker struct {
	mu       sync.Mutex
	ctx      context.Context
	cancel   context.CancelFunc
	close    sync.Once
	watchdog time.Duration
	publish  func(icons.Key, *ui.Image)
	cache    map[icons.Key]*ui.Image
	order    []icons.Key
	bytes    int
	failed   map[string]struct{}
	jobs     map[icons.Key]*mediaArtJob
}

type mediaArtJob struct {
	key    icons.Key
	artKey string
	done   chan struct{}
	cancel context.CancelFunc
	once   sync.Once
}

func newMediaArtWorker(publish func(icons.Key, *ui.Image)) *mediaArtWorker {
	ctx, cancel := context.WithCancel(context.Background())
	return &mediaArtWorker{
		ctx: ctx, cancel: cancel, watchdog: mediaArtWatchdog,
		publish: publish, cache: make(map[icons.Key]*ui.Image),
		failed: make(map[string]struct{}), jobs: make(map[icons.Key]*mediaArtJob),
	}
}

// mediaArtRequestName returns the cache/decode name for a local or remote art
// source. URL.Path is already percent-decoded by net/url for local files.
func mediaArtRequestName(artKey string) (string, bool) {
	u, err := url.Parse(artKey)
	if err != nil || u.Fragment != "" {
		return "", false
	}
	switch u.Scheme {
	case "file":
		if u.Host != "" || u.RawQuery != "" || u.Path == "" || !filepath.IsAbs(u.Path) {
			return "", false
		}
		return u.Path, true
	case "http", "https":
		if u.Hostname() == "" {
			return "", false
		}
		return artKey, true
	default:
		return "", false
	}
}

// mediaArtRequestPath accepts only an absolute, local file URL. URL.Path is
// already percent-decoded by net/url, so the resolver receives the actual path.
func mediaArtRequestPath(artKey string) (string, bool) {
	u, err := url.Parse(artKey)
	if err != nil || u.Scheme != "file" || u.Host != "" || u.Path == "" ||
		u.RawQuery != "" || u.Fragment != "" || !filepath.IsAbs(u.Path) {
		return "", false
	}
	return u.Path, true
}

func (w *mediaArtWorker) Lookup(key icons.Key) (*ui.Image, bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	image, ok := w.cache[key]
	return image, ok
}

// Request returns a cached image and reports whether a new decode was queued.
// A failed ArtKey remains negative-cached until the player announces another
// key, avoiding repeated reads of a missing or inaccessible cover.
func (w *mediaArtWorker) Request(artKey string, box int) (*ui.Image, bool) {
	name, ok := mediaArtRequestName(artKey)
	if !ok || box <= 0 {
		return nil, false
	}
	key := icons.Key{Name: name, W: box, H: box}
	w.mu.Lock()
	if image, ok := w.cache[key]; ok {
		w.mu.Unlock()
		return image, false
	}
	if _, failed := w.failed[artKey]; failed {
		w.mu.Unlock()
		return nil, false
	}
	if _, pending := w.jobs[key]; pending {
		w.mu.Unlock()
		return nil, false
	}
	jobCtx, cancel := context.WithCancel(w.ctx)
	job := &mediaArtJob{key: key, artKey: artKey, done: make(chan struct{}), cancel: cancel}
	w.jobs[key] = job
	watchdog := w.watchdog
	if watchdog <= 0 {
		watchdog = mediaArtWatchdog
	}
	w.mu.Unlock()

	if path, local := mediaArtRequestPath(artKey); local {
		worker := icons.NewWorker(icons.NewResolver("", nil), func(_ icons.Key, image *ui.Image) {
			w.finish(job, image, true)
		})
		if _, _, err := worker.Request(icons.Key{Name: path, W: box, H: box}); err != nil {
			w.finish(job, nil, true)
			return nil, false
		}
		go func() { _ = worker.Run(jobCtx) }()
	} else {
		go func() { w.finish(job, fetchRemoteMediaArt(jobCtx, artKey, box), true) }()
	}
	go w.watch(job, watchdog)
	return nil, true
}

func fetchRemoteMediaArt(ctx context.Context, artKey string, box int) *ui.Image {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, artKey, nil)
	if err != nil {
		return nil
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return nil
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices ||
		response.ContentLength > icons.MaxFileBytes {
		return nil
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, icons.MaxFileBytes+1))
	if err != nil || len(data) > icons.MaxFileBytes {
		return nil
	}
	return icons.DecodeRaster(data, box, box)
}

func (w *mediaArtWorker) watch(job *mediaArtJob, timeout time.Duration) {
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-job.done:
	case <-timer.C:
		w.finish(job, nil, true)
	case <-w.ctx.Done():
		w.finish(job, nil, false)
	}
}

func (w *mediaArtWorker) finish(job *mediaArtJob, image *ui.Image, publish bool) {
	job.once.Do(func() {
		w.mu.Lock()
		active := w.jobs[job.key] == job
		if active {
			delete(w.jobs, job.key)
			if image != nil {
				w.storeImageLocked(job.key, image)
			} else if publish {
				w.failed[job.artKey] = struct{}{}
			}
		}
		callback := w.publish
		w.mu.Unlock()
		job.cancel()
		close(job.done)
		if active && publish && callback != nil {
			callback(job.key, image)
		}
	})
}

func (w *mediaArtWorker) storeImageLocked(key icons.Key, image *ui.Image) {
	if image == nil || len(image.Pix) > icons.MaxCacheBytes {
		return
	}
	if previous, exists := w.cache[key]; exists {
		w.bytes -= len(previous.Pix)
	} else {
		w.order = append(w.order, key)
	}
	w.cache[key] = image
	w.bytes += len(image.Pix)
	for len(w.cache) > icons.MaxCacheEntries || w.bytes > icons.MaxCacheBytes {
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

func (w *mediaArtWorker) Close() {
	w.close.Do(func() {
		w.cancel()
		w.mu.Lock()
		jobs := make([]*mediaArtJob, 0, len(w.jobs))
		for _, job := range w.jobs {
			jobs = append(jobs, job)
		}
		w.mu.Unlock()
		for _, job := range jobs {
			w.finish(job, nil, false)
		}
	})
}
