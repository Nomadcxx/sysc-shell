package shell

import (
	"bytes"
	"log"
	"strings"
	"testing"
	"time"

	"github.com/Nomadcxx/sysc-shell/internal/config"
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
