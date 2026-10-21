package icons

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// resolverClock is a clock the index recheck can be driven by, so the interval
// is exercised without sleeping.
type resolverClock struct {
	mu      sync.Mutex
	current time.Time
}

func newResolverClock() *resolverClock {
	return &resolverClock{current: time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC)}
}

func (c *resolverClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.current
}

func (c *resolverClock) Advance(step time.Duration) {
	c.mu.Lock()
	c.current = c.current.Add(step)
	c.mu.Unlock()
}

func indexOf(t *testing.T, resolver *Resolver) uint64 {
	t.Helper()
	return resolver.Generation()
}

// The whole point of the index is that the theme tree is walked once, not once
// per lookup: a launcher rebuild asks for every visible row.
func TestResolverBuildsItsIndexOnce(t *testing.T) {
	root := t.TempDir()
	writeIcon(t, root, "Adwaita", "48x48/apps", "chat.png")
	resolver := NewResolver("Adwaita", []string{root})

	for range 20 {
		if _, ok := resolver.Resolve("chat", 48); !ok {
			t.Fatal("a known icon stopped resolving")
		}
		if _, ok := resolver.Resolve("absent", 48); ok {
			t.Fatal("an absent icon resolved")
		}
	}
	if got := indexOf(t, resolver); got != 1 {
		t.Fatalf("theme tree was indexed %d times, want once", got)
	}
}

func TestResolverInvalidateFindsANewIcon(t *testing.T) {
	root := t.TempDir()
	writeIcon(t, root, "Adwaita", "48x48/apps", "chat.png")
	resolver := NewResolver("Adwaita", []string{root})

	if _, ok := resolver.Resolve("added", 48); ok {
		t.Fatal("an icon resolved before it was installed")
	}
	writeIcon(t, root, "Adwaita", "48x48/apps", "added.png")
	if _, ok := resolver.Resolve("added", 48); ok {
		t.Fatal("the index answered from a directory it had not re-read")
	}
	resolver.Invalidate()
	if got, ok := resolver.Resolve("added", 48); !ok || !strings.HasSuffix(got, "added.png") {
		t.Fatalf("after Invalidate = %q (%v), want the newly installed icon", got, ok)
	}
}

func TestResolverRereadsTheTreeAfterTheRecheckInterval(t *testing.T) {
	root := t.TempDir()
	writeIcon(t, root, "Adwaita", "48x48/apps", "chat.png")
	resolver := NewResolver("Adwaita", []string{root})
	clock := newResolverClock()
	resolver.now = clock.Now

	resolver.Resolve("chat", 48)
	writeIcon(t, root, "Adwaita", "48x48/apps", "added.png")

	// Inside the interval the index is reused: no stat of the whole tree per
	// lookup, which is what made the old resolver cost milliseconds.
	clock.Advance(themeRecheckInterval / 2)
	if _, ok := resolver.Resolve("added", 48); ok {
		t.Fatal("the tree was re-read before the recheck interval elapsed")
	}
	clock.Advance(themeRecheckInterval)
	if got, ok := resolver.Resolve("added", 48); !ok || !strings.HasSuffix(got, "added.png") {
		t.Fatalf("after the interval = %q (%v), want the newly installed icon", got, ok)
	}
}

// Earlier search directories win when they offer an equally good candidate, so
// a user theme keeps overriding the system one.
func TestResolverKeepsSearchDirPrecedence(t *testing.T) {
	first, second := t.TempDir(), t.TempDir()
	writeIcon(t, first, "Adwaita", "48x48/apps", "chat.png")
	writeIcon(t, second, "Adwaita", "48x48/apps", "chat.png")
	resolver := NewResolver("Adwaita", []string{first, second})

	got, ok := resolver.Resolve("chat", 48)
	if !ok || !strings.HasPrefix(got, first) {
		t.Fatalf("resolve = %q (%v), want the first search directory", got, ok)
	}
}

// Size still beats search order: an exact raster in a later directory wins over
// a wrongly sized one earlier, which is the order the old resolver produced.
func TestResolverPrefersAnExactSizeOverSearchOrder(t *testing.T) {
	first, second := t.TempDir(), t.TempDir()
	writeIcon(t, first, "Adwaita", "22x22/apps", "chat.png")
	writeIcon(t, second, "Adwaita", "48x48/apps", "chat.png")
	resolver := NewResolver("Adwaita", []string{first, second})

	got, ok := resolver.Resolve("chat", 48)
	if !ok || !strings.HasPrefix(got, second) {
		t.Fatalf("resolve = %q (%v), want the exact-size file from the later directory", got, ok)
	}
}

// An empty file is not an icon. The index records names without statting every
// file, so the accepted candidate is verified before it is returned.
func TestResolverSkipsAZeroByteIcon(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "Adwaita", "48x48", "apps")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "chat.png"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	writeIcon(t, root, "hicolor", "48x48/apps", "chat.png")
	resolver := NewResolver("Adwaita", []string{root})

	got, ok := resolver.Resolve("chat", 48)
	if !ok || !strings.Contains(got, "hicolor") {
		t.Fatalf("resolve = %q (%v), want the inherited theme's copy", got, ok)
	}
}

// A file removed behind the index's back must not be returned: the caller
// would fail to read it and render a placeholder.
func TestResolverSkipsACandidateThatVanished(t *testing.T) {
	root := t.TempDir()
	writeIcon(t, root, "Adwaita", "48x48/apps", "chat.png")
	resolver := NewResolver("Adwaita", []string{root})
	if _, ok := resolver.Resolve("chat", 48); !ok {
		t.Fatal("the icon did not resolve before it was removed")
	}
	if err := os.Remove(filepath.Join(root, "Adwaita", "48x48", "apps", "chat.png")); err != nil {
		t.Fatal(err)
	}
	if _, ok := resolver.Resolve("chat", 48); ok {
		t.Fatal("a file that no longer exists was resolved")
	}
}
