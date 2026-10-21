package icons

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

// A rebuild asks for every visible row, and most rows name an icon that does
// not exist. Remembering the miss stops the theme walk from repeating per
// rebuild, and the window keeps a newly installed icon from staying invisible
// for the session.
func TestWorkerRemembersAMissForTheRetryWindow(t *testing.T) {
	root := t.TempDir()
	writeIcon(t, root, "Adwaita", "24x24/apps", "chat.png")
	worker := NewWorker(NewResolver("Adwaita", []string{root}), nil)
	clock := newResolverClock()
	worker.now = clock.Now

	key := Square("absent", 24)
	worker.decode(context.Background(), key)

	if _, _, err := worker.Request(key); err != nil {
		t.Fatal(err)
	}
	if queued := len(worker.jobs); queued != 0 {
		t.Fatalf("%d jobs queued for a remembered miss", queued)
	}

	clock.Advance(missTTL)
	if _, _, err := worker.Request(key); err != nil {
		t.Fatal(err)
	}
	if queued := len(worker.jobs); queued != 1 {
		t.Fatalf("%d jobs queued after the retry window, want a retry", queued)
	}
}

func TestWorkerNegativeCacheIsBounded(t *testing.T) {
	worker := NewWorker(NewResolver("Adwaita", []string{t.TempDir()}), nil)
	clock := newResolverClock()
	worker.now = clock.Now

	for i := range 1000 {
		worker.decode(context.Background(), Square("missing-"+strconv.Itoa(i), 24))
		// Older misses expire as the loop advances, so only the current
		// window can grow; the bound is on distinct keys, not on time.
		clock.Advance(missTTL / 2000)
	}

	worker.mu.Lock()
	size := len(worker.failed)
	worker.mu.Unlock()
	if size > MaxCacheEntries {
		t.Fatalf("the miss cache holds %d entries, want at most %d", size, MaxCacheEntries)
	}
}

// Wallpaper previews and plugin images name an absolute path that may be
// written a moment later. Remembering that miss would hide a fresh file, so an
// absolute path is always retried.
func TestWorkerBypassesTheMissCacheForAbsolutePaths(t *testing.T) {
	worker := NewWorker(FileResolver{}, nil)
	worker.now = newResolverClock().Now
	missing := filepath.Join(t.TempDir(), "preview.png")

	worker.decode(context.Background(), Square(missing, 24))
	if _, _, err := worker.Request(Square(missing, 24)); err != nil {
		t.Fatal(err)
	}
	if queued := len(worker.jobs); queued != 1 {
		t.Fatalf("%d jobs queued, want the absolute path retried", queued)
	}
}

// A rebuild changes what the resolver can find, so a remembered miss must not
// outlive it.
func TestWorkerForgetsMissesWhenTheResolverRebuilds(t *testing.T) {
	root := t.TempDir()
	writeIcon(t, root, "Adwaita", "24x24/apps", "chat.png")
	resolver := NewResolver("Adwaita", []string{root})
	worker := NewWorker(resolver, nil)
	worker.now = newResolverClock().Now

	key := Square("added", 24)
	worker.decode(context.Background(), key)
	writeIcon(t, root, "Adwaita", "24x24/apps", "added.png")
	resolver.Invalidate()

	if _, _, err := worker.Request(key); err != nil {
		t.Fatal(err)
	}
	if queued := len(worker.jobs); queued != 1 {
		t.Fatalf("%d jobs queued, want the miss retried after the resolver rebuilt", queued)
	}
}

// A file that exists but cannot be decoded is a miss too. Retrying it on every
// rebuild costs a read and a failed decode each time.
func TestWorkerRemembersAFailedDecode(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "Adwaita", "24x24", "apps")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "broken.png"), []byte("not a png"), 0o644); err != nil {
		t.Fatal(err)
	}
	worker := NewWorker(NewResolver("Adwaita", []string{root}), nil)
	worker.now = newResolverClock().Now

	key := Square("broken", 24)
	worker.decode(context.Background(), key)
	if _, _, err := worker.Request(key); err != nil {
		t.Fatal(err)
	}
	if queued := len(worker.jobs); queued != 0 {
		t.Fatalf("%d jobs queued for a remembered decode failure", queued)
	}
}
