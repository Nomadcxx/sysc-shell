package shell

import (
	"sync"

	tray "github.com/Nomadcxx/sysc-tray/protocol"
)

// trayCloseTracker holds the keys a Close row asked the service to terminate.
//
// Termination is not finished when the service accepts it: the process is
// signalled and given a bounded window to leave, and it may refuse. The only
// evidence the application is gone is the item's removal, so success is that
// delta and nothing else. Until then the row stays visible and usable, because
// a row that disappears on request tells the user the application closed when
// it may still be running.
type trayCloseTracker struct {
	mu      sync.Mutex
	pending map[tray.ItemKey]struct{}
}

func newTrayCloseTracker() *trayCloseTracker {
	return &trayCloseTracker{pending: map[tray.ItemKey]struct{}{}}
}

// begin records that a terminate was sent for this exact key. The full key
// includes the generation, so a reply about an application that has since
// quit and been replaced cannot be read as being about its successor.
func (t *trayCloseTracker) begin(key tray.ItemKey) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.pending[key] = struct{}{}
}

// pendingFor reports whether a terminate is outstanding for this key.
func (t *trayCloseTracker) pendingFor(key tray.ItemKey) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	_, ok := t.pending[key]
	return ok
}

// settle drops the key, whatever the outcome was.
func (t *trayCloseTracker) settle(key tray.ItemKey) {
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.pending, key)
}

// reset clears everything. A disconnect or a new generation invalidates every
// key the old connection produced, so nothing may stay pending across one.
func (t *trayCloseTracker) reset() {
	t.mu.Lock()
	defer t.mu.Unlock()
	clear(t.pending)
}

// count reports how many terminations are outstanding.
func (t *trayCloseTracker) count() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return len(t.pending)
}
