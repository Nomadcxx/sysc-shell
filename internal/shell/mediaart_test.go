package shell

import (
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
