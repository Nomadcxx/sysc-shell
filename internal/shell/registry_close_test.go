package shell

import (
	"bytes"
	"log"
	"strings"
	"testing"
	"time"

	"github.com/Nomadcxx/sysc-shell/internal/config"
	"github.com/Nomadcxx/sysc-shell/internal/walls"
)

// TestCloseGivesUpOnAStrandedLock pins shutdown behavior when r.mu remains
// locked. The test deliberately holds the mutex past closeLockGrace and checks
// that Close returns, reports the timeout, and closes r.closed.
func TestCloseGivesUpOnAStrandedLock(t *testing.T) {
	r := NewRegistry(config.Config{})
	var diagnostic bytes.Buffer
	previousLogWriter := log.Writer()
	log.SetOutput(&diagnostic)
	defer log.SetOutput(previousLogWriter)

	// Model a stranded lock directly; the originating code path is unknown.
	r.mu.Lock()

	done := make(chan struct{})
	go func() {
		r.Close()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(closeLockGrace + 5*time.Second):
		t.Fatal("Close stayed blocked beyond its lock-acquisition grace with r.mu held")
	}
	if !strings.Contains(diagnostic.String(), "shell: closing without r.mu; could not acquire r.mu within shutdown grace") {
		t.Fatalf("Close diagnostic = %q", diagnostic.String())
	}

	// The channel every publish selects on must still be closed, or a sender
	// blocked on a full queue never unwinds either.
	select {
	case <-r.closed:
	default:
		t.Fatal("Close returned without closing r.closed")
	}
}

// TestCloseWithoutTheLockLeavesPluginsStable pins the plugin teardown when
// r.mu is stranded. viewLocked reads r.plugins under the lock, so a fallback
// that gives up on the lock must not write the field: that write is the data
// race. The host is closed through a local copy instead, which is also what
// leaves a later render seeing no plugin frames.
func TestCloseWithoutTheLockLeavesPluginsStable(t *testing.T) {
	r := NewRegistry(config.Config{})
	installed := &pluginHost{r: r}
	r.plugins = installed

	var diagnostic bytes.Buffer
	previousLogWriter := log.Writer()
	log.SetOutput(&diagnostic)
	defer log.SetOutput(previousLogWriter)

	// A render holds r.mu and reads r.plugins. Hold the lock past
	// closeLockGrace so Close gives up and takes the fallback path while
	// those reads are still running.
	reading := make(chan struct{})
	readDone := make(chan struct{})
	go func() {
		r.mu.Lock()
		close(reading)
		deadline := time.Now().Add(closeLockGrace + 250*time.Millisecond)
		for time.Now().Before(deadline) {
			_ = r.plugins
		}
		r.mu.Unlock()
		close(readDone)
	}()
	<-reading

	closed := make(chan struct{})
	go func() {
		r.Close()
		close(closed)
	}()
	select {
	case <-closed:
	case <-time.After(2*closeLockGrace + 5*time.Second):
		t.Fatal("Close stayed blocked beyond its lock-acquisition grace with r.mu held")
	}
	<-readDone

	// Ordered after the reader and after Close, so this read needs no lock.
	if r.plugins != installed {
		t.Fatalf("Close rewrote r.plugins without r.mu: got %p, want %p", r.plugins, installed)
	}
}

// TestCloseWithoutTheLockStopsTheOwnedServices pins that a Close which cannot
// take r.mu still stops the services the shell owns. The handles are recorded
// where each resource is created, so the shutdown path reaches them without
// the lock, and the shutdown stays inside its grace: it waits for the lock
// once, not twice.
func TestCloseWithoutTheLockStopsTheOwnedServices(t *testing.T) {
	r := NewRegistry(config.Config{})
	service := newFakeWallsService(walls.Snapshot{})
	r.attachWallsService(service)

	// A holder that keeps r.mu past the grace strands it for the shutdown.
	r.mu.Lock()

	done := make(chan struct{})
	go func() {
		r.Close()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(closeLockGrace + 2*time.Second):
		t.Fatal("Close stayed blocked beyond its lock-acquisition grace with r.mu held")
	}

	if _, _, _, closes := service.counts(); closes != 1 {
		t.Fatalf("the walls service was closed %d times, want 1", closes)
	}
	r.mu.Unlock()
}

// TestCloseTwiceWithoutTheLockReleasesOnce pins that a second Close does not
// release the same resources again. A test that closes the registry and also
// leaves the close to its cleanup is ordinary, and the launcher service panics
// when it is closed twice.
func TestCloseTwiceWithoutTheLockReleasesOnce(t *testing.T) {
	r := NewRegistry(config.Config{})
	service := newFakeWallsService(walls.Snapshot{})
	r.attachWallsService(service)

	r.mu.Lock()

	for attempt := range 2 {
		done := make(chan struct{})
		go func() {
			r.Close()
			close(done)
		}()
		select {
		case <-done:
		case <-time.After(closeLockGrace + 2*time.Second):
			t.Fatalf("Close %d stayed blocked beyond its lock-acquisition grace with r.mu held", attempt+1)
		}
	}

	if _, _, _, closes := service.counts(); closes != 1 {
		t.Fatalf("the walls service was closed %d times, want 1", closes)
	}
	r.mu.Unlock()
}
