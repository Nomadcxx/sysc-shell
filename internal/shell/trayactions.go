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
	if !r.tray.has(c.Item) || r.tray.isClosing(c.Item) {
		return
	}
	_, _ = s.Send(c)
}

// trayTerminate asks the service to end the process behind one item. The
// item stays visible but inert until the service confirms with a removal
// delta; a failed reply or a generation change restores it. Called with
// Registry.mu held, like the other tray actions. The request is marked
// pending only after the command is accepted, so the terminate command
// itself is never dropped by the pending check.
func (r *Registry) trayTerminate(key tray.ItemKey, output uint32) {
	if _, err := r.sendTrayLocked(tray.Command{
		Kind: tray.CommandTerminate, Item: key, Output: output,
	}); err != nil {
		return
	}
	r.tray.markClosing(key)
}

// trayRequest is one sent command awaiting its reply. Terminate requests
// carry their own failure semantics: any error cancels the pending close
// instead of retriggerring, because a stale reply describes a generation
// that no longer exists and re-terminating its replacement was never asked.
type trayRequest struct {
	key       tray.ItemKey
	terminate bool
}

// trayReplyTracker correlates replies with the request that produced them.
// A reply is acted on exactly once and then forgotten: a stale or replayed
// reply for a finished request is dropped. Only ErrorStaleItem retriggers —
// the shell re-reads the current key for that item — because every other
// failure is terminal for the click that caused it.
type trayReplyTracker struct {
	mu      sync.Mutex
	pending map[uint64]trayRequest
	retry   func(tray.ItemKey)
	cancel  func(tray.ItemKey)
}

func newTrayReplyTracker(_ *Registry, retry, cancel func(tray.ItemKey)) *trayReplyTracker {
	return &trayReplyTracker{pending: map[uint64]trayRequest{}, retry: retry, cancel: cancel}
}

// note remembers the key one request ID was sent for.
func (t *trayReplyTracker) note(requestID uint64, key tray.ItemKey) {
	t.noteKind(requestID, key, false)
}

// noteTerminate remembers a termination request by its request ID.
func (t *trayReplyTracker) noteTerminate(requestID uint64, key tray.ItemKey) {
	t.noteKind(requestID, key, true)
}

func (t *trayReplyTracker) noteKind(requestID uint64, key tray.ItemKey, terminate bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.pending[requestID] = trayRequest{key: key, terminate: terminate}
}

// apply consumes one reply. Exactly-once: the request is forgotten before
// the retry fires, so a replayed reply finds nothing.
func (t *trayReplyTracker) apply(m trayclient.Message) {
	if m.Kind != trayclient.KindReply {
		return
	}
	t.mu.Lock()
	req, ok := t.pending[m.RequestID]
	delete(t.pending, m.RequestID)
	t.mu.Unlock()
	if !ok {
		return
	}
	if m.Reply.OK && m.Reply.Error == nil {
		// A successful terminate waits for the removal delta; a successful
		// click is already done.
		return
	}
	if req.terminate {
		t.cancel(req.key)
		return
	}
	if m.Reply.Error != nil && m.Reply.Error.Code == tray.ErrorStaleItem {
		t.retry(req.key)
	}
}
