package shell

import (
	"sync"

	"github.com/Nomadcxx/sysc-shell/internal/trayclient"
	tray "github.com/Nomadcxx/sysc-tray/protocol"
)

// trayCommandSender is the client's Send seam: one method, so tests can
// record instead of dialing.
type trayCommandSender interface {
	Send(tray.Command) (uint64, error)
}

// The registry forwards pointer input on a tray item as a protocol command
// carrying logical coordinates. A command whose key is no longer live never
// leaves the shell: the key embeds the generation, so a stale click is inert.
func (r *Registry) trayActivate(s trayCommandSender, key tray.ItemKey, output uint32, x, y int32) {
	r.traySend(s, tray.Command{Kind: tray.CommandActivate, Item: key,
		Output: output, X: x, Y: y})
}

func (r *Registry) trayActivateSecondary(s trayCommandSender, key tray.ItemKey, output uint32, x, y int32) {
	r.traySend(s, tray.Command{Kind: tray.CommandSecondaryActivate, Item: key,
		Output: output, X: x, Y: y})
}

func (r *Registry) trayScroll(s trayCommandSender, key tray.ItemKey, output uint32, delta int32, o tray.ScrollOrientation) {
	r.traySend(s, tray.Command{Kind: tray.CommandScroll, Item: key,
		Output: output, Delta: delta, Orientation: o})
}

func (r *Registry) traySend(s trayCommandSender, c tray.Command) {
	if !r.tray.has(c.Item) {
		return
	}
	_, _ = s.Send(c)
}

// trayReplyTracker correlates replies with the request that produced them.
// A reply is acted on exactly once and then forgotten: a stale or replayed
// reply for a finished request is dropped. Only ErrorStaleItem asks for a
// reprojection — the shell re-reads the current key for that item — because
// every other failure is terminal for the click that caused it.
//
// The tracker decides; it never acts. It runs under the registry lock, and
// the reprojection it would trigger takes that lock again.
type trayReplyTracker struct {
	mu      sync.Mutex
	pending map[uint64]tray.ItemKey
}

func newTrayReplyTracker() *trayReplyTracker {
	return &trayReplyTracker{pending: map[uint64]tray.ItemKey{}}
}

// note remembers the key one request ID was sent for.
func (t *trayReplyTracker) note(requestID uint64, key tray.ItemKey) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.pending[requestID] = key
}

// apply consumes one reply and reports what the caller has to do about it:
// the key the request was sent for, and whether the service answered with a
// stale item, so the shell must reproject before the next command.
//
// Exactly-once: the request is forgotten before the decision, so a replayed
// reply finds nothing.
func (t *trayReplyTracker) apply(m trayclient.Message) (tray.ItemKey, bool) {
	if m.Kind != trayclient.KindReply {
		return tray.ItemKey{}, false
	}
	t.mu.Lock()
	key, ok := t.pending[m.RequestID]
	delete(t.pending, m.RequestID)
	t.mu.Unlock()
	if !ok || m.Reply.OK || m.Reply.Error == nil {
		return tray.ItemKey{}, false
	}
	return key, m.Reply.Error.Code == tray.ErrorStaleItem
}
