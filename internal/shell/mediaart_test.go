package shell

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/Nomadcxx/sysc-shell/internal/icons"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

func TestMediaArtKeyParsesToAPath(t *testing.T) {
	path := "/tmp/a b.png"
	artKey := (&url.URL{Scheme: "file", Path: path}).String()
	cases := []struct {
		name string
		key  string
		want string
		ok   bool
	}{
		{name: "local file", key: artKey, want: path, ok: true},
		{name: "remote scheme", key: "https://example.test/a.png"},
		{name: "remote host", key: "file://host/a.png"},
		{name: "garbage", key: "not a url"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := mediaArtRequestPath(tc.key)
			if got != tc.want || ok != tc.ok {
				t.Fatalf("mediaArtRequestPath(%q) = %q, %v; want %q, %v", tc.key, got, ok, tc.want, tc.ok)
			}
		})
	}
}

func TestMediaArtKeyAcceptsRemoteSources(t *testing.T) {
	for _, source := range []string{
		"http://example.test/cover.jpg",
		"https://example.test/cover.jpg?size=large",
	} {
		if got, ok := mediaArtRequestName(source); !ok || got != source {
			t.Errorf("mediaArtRequestName(%q) = %q, %v; want the source URL, true", source, got, ok)
		}
	}
}

func TestMediaArtRemoteDecode(t *testing.T) {
	data := testMediaArtPNG(t)
	server := newMediaArtTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(data)
	}))
	defer server.Close()

	url := server.URL + "/cover.png"
	results := make(chan *ui.Image, 1)
	art := newMediaArtWorker(func(_ icons.Key, image *ui.Image) { results <- image })
	defer art.Close()
	if _, queued := art.Request(url, 16); !queued {
		t.Fatal("remote art request did not queue")
	}
	select {
	case image := <-results:
		if image == nil || image.Width != 16 || image.Height != 16 {
			t.Fatalf("remote image = %+v, want a decoded 16x16 raster", image)
		}
	case <-time.After(time.Second):
		t.Fatal("remote art did not publish")
	}
}

func TestMediaArtRemoteBoundsAndErrors(t *testing.T) {
	data := testMediaArtPNG(t)
	paths := []string{"/status", "/invalid", "/oversized"}
	server := newMediaArtTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/status":
			w.WriteHeader(http.StatusBadGateway)
		case "/invalid":
			_, _ = io.WriteString(w, "not an image")
		case "/oversized":
			w.Header().Set("Content-Length", "8388609")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(make([]byte, icons.MaxFileBytes+1))
		default:
			_, _ = w.Write(data)
		}
	}))
	defer server.Close()

	results := make(chan *ui.Image, len(paths))
	art := newMediaArtWorker(func(_ icons.Key, image *ui.Image) { results <- image })
	defer art.Close()
	for _, path := range paths {
		if _, queued := art.Request(server.URL+path, 16); !queued {
			t.Fatalf("request for %s did not queue", path)
		}
	}
	for range paths {
		select {
		case image := <-results:
			if image != nil {
				t.Fatalf("invalid remote art published %+v", image)
			}
		case <-time.After(time.Second):
			t.Fatal("invalid remote art did not publish")
		}
	}
	for _, path := range paths {
		if _, queued := art.Request(server.URL+path, 16); queued {
			t.Fatalf("negative cache allowed retry for %s", path)
		}
	}
}

func TestMediaArtCacheStaysBounded(t *testing.T) {
	art := newMediaArtWorker(nil)
	defer art.Close()
	image := &ui.Image{Width: 1, Height: 1, Stride: 4, Pix: make([]byte, 4)}
	art.mu.Lock()
	for i := 0; i < icons.MaxCacheEntries+1; i++ {
		art.storeImageLocked(icons.Key{Name: fmt.Sprintf("art-%d", i), W: 1, H: 1}, image)
	}
	got := len(art.cache)
	art.mu.Unlock()
	if got > icons.MaxCacheEntries {
		t.Fatalf("media art cache entries = %d, want at most %d", got, icons.MaxCacheEntries)
	}
}

func testMediaArtPNG(t *testing.T) []byte {
	t.Helper()
	image := image.NewRGBA(image.Rect(0, 0, 2, 2))
	image.Set(0, 0, color.RGBA{R: 0xff, A: 0xff})
	var data bytes.Buffer
	if err := png.Encode(&data, image); err != nil {
		t.Fatal(err)
	}
	return data.Bytes()
}

func newMediaArtTestServer(t *testing.T, handler http.Handler) *httptest.Server {
	t.Helper()
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewUnstartedServer(handler)
	server.Listener = listener
	server.Start()
	return server
}

func TestMediaArtTimeoutPublishesNilOnce(t *testing.T) {
	root := t.TempDir()
	paths := []string{filepath.Join(root, "first.fifo"), filepath.Join(root, "second.fifo")}
	for _, path := range paths {
		if err := syscall.Mkfifo(path, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	defer func() {
		for _, path := range paths {
			_ = os.Remove(path)
		}
	}()

	var publishes atomic.Int32
	art := newMediaArtWorker(func(_ icons.Key, image *ui.Image) {
		if image != nil {
			t.Error("timed-out art job published an image")
		}
		publishes.Add(1)
	})
	art.watchdog = 20 * time.Millisecond
	defer art.Close()

	keys := make([]string, 0, len(paths))
	for _, path := range paths {
		keys = append(keys, (&url.URL{Scheme: "file", Path: path}).String())
	}
	for _, key := range keys {
		if _, queued := art.Request(key, 16); !queued {
			t.Fatalf("Request(%q) did not queue a job", key)
		}
	}

	deadline := time.After(time.Second)
	for publishes.Load() < int32(len(keys)) {
		select {
		case <-deadline:
			t.Fatalf("timed-out jobs published %d results, want %d", publishes.Load(), len(keys))
		case <-time.After(time.Millisecond):
		}
	}
	if _, queued := art.Request(keys[0], 16); queued {
		t.Fatal("negative cache allowed a retry for the same ArtKey")
	}
	if got := publishes.Load(); got != int32(len(keys)) {
		t.Fatalf("publish count after negative-cache retry = %d, want %d", got, len(keys))
	}

	// Release the abandoned FIFO readers so the test does not leave blocked
	// read syscalls behind after the watchdog has done its work.
	for _, path := range paths {
		file, err := os.OpenFile(path, syscall.O_WRONLY|syscall.O_NONBLOCK, 0)
		if err == nil {
			_ = file.Close()
		}
	}
}
