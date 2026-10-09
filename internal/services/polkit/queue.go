package polkit

import (
	"errors"
	"sync"
)

// maxPending is how many prompts may wait behind the one on screen. A user
// cannot attend to more than a handful at once, and polkitd has no queue of
// its own: a full queue has to answer, or the caller waits forever.
const maxPending = 4

var errQueueFull = errors.New("too many pending authentication requests")
var errQueueCancelled = errors.New("authentication request cancelled")
var errQueueCookie = errors.New("invalid or duplicate authentication cookie")

// newRequest builds a request with the channels its helper session needs. The
// caller fills in the polkit fields.
func newRequest() Request {
	return Request{
		done:     newWaiter(),
		started:  make(chan struct{}),
		identity: make(chan Identity, 1),
		prompts:  make(chan Prompt),
		answers:  make(chan string, 1),
	}
}

func waitStarted(started, cancelled <-chan struct{}) bool {
	select {
	case <-started:
		return true
	case <-cancelled:
		select {
		case <-started:
			return true
		default:
			return false
		}
	}
}

// Identity is one identity polkitd will accept the response for.
type Identity struct {
	Kind   string
	Name   string
	Values map[string]string
}

// Request is one BeginAuthentication waiting for an answer. The user never
// sees it until the queue hands it to a surface.
type Request struct {
	ActionID   string
	Message    string
	IconName   string
	Details    map[string]string
	Cookie     string
	Identities []Identity

	// started closes when this request reaches the display slot. done closes
	// when the request is cancelled or its helper session ends.
	started  chan struct{}
	done     *waiter
	identity chan Identity
	// prompts and answers are the display side of the helper session: the
	// helper asks one line at a time, the surface shows it and replies.
	prompts chan Prompt
	answers chan string
}

// ask routes one helper prompt to the surface and waits for its answer, or
// gives up when polkitd withdraws the request. A withdrawal here is what ends
// the session: the surface has closed and nobody will ever answer.
func (r Request) ask(p Prompt) (string, error) {
	if r.prompts == nil || r.done == nil {
		return "", ErrHelperCancelled
	}
	select {
	case r.prompts <- p:
	case <-r.done.ch:
		return "", ErrHelperCancelled
	}
	select {
	case answer := <-r.answers:
		return answer, nil
	case <-r.done.ch:
		return "", ErrHelperCancelled
	}
}

// Start selects the identity for this request. The helper cannot start until
// the user confirms the chooser, because its username is fixed at launch.
func (r Request) Start(identity Identity) bool {
	select {
	case r.identity <- identity:
		return true
	case <-r.Done():
		return false
	}
}

// Prompts carries PAM questions to the prompt surface.
func (r Request) Prompts() <-chan Prompt { return r.prompts }

// Submit answers the current PAM prompt without blocking the Wayland owner.
func (r Request) Submit(answer string) bool {
	select {
	case r.answers <- answer:
		return true
	case <-r.Done():
		return false
	default:
		return false
	}
}

// Cancel ends the helper conversation. The BeginAuthentication call releases
// the queue slot after the helper has stopped.
func (r Request) Cancel() { r.done.stop() }

// Done closes on withdrawal, user dismissal, or completion of the helper.
func (r Request) Done() <-chan struct{} {
	if r.done == nil {
		return nil
	}
	return r.done.ch
}

// waiter is a one-shot signal shared by the copies of a request the queue
// keeps: withdrawal and a shutdown cancel can both reach one request, and
// neither may close the channel twice.
type waiter struct {
	once sync.Once
	ch   chan struct{}
}

func newWaiter() *waiter { return &waiter{ch: make(chan struct{})} }

func (w *waiter) stop() {
	if w != nil {
		w.once.Do(func() { close(w.ch) })
	}
}

// IdentityValues returns the identity's values for kind, or nil.
func (r Request) IdentityValues(kind string) map[string]string {
	for _, id := range r.Identities {
		if id.Kind == kind {
			return id.Values
		}
	}
	return nil
}

// queue is the pending set behind one displayed prompt. It is pure state with
// no goroutine and no D-Bus, which is what makes the whole contract testable.
//
// out carries the displayed request. It is buffered one deep and that depth is
// the invariant: out holds a request exactly while active is set, so a send
// under the lock never blocks and a send without the lock could lose its slot.
type queue struct {
	mu      sync.Mutex
	out     chan Request
	changed chan<- struct{}
	held    bool
	pending []Request
	active  *Request
}

type withdrawnRequest struct {
	request Request
	queued  bool
}

// push waits until the request reaches the display slot. The D-Bus method that
// called it therefore cannot return success while its request is still queued.
// A refused request is canceled by the caller because nothing else will.
func (q *queue) push(req Request) (bool, error) {
	q.mu.Lock()
	if req.Cookie == "" {
		q.mu.Unlock()
		return false, errQueueCookie
	}
	if q.hasCookieLocked(req.Cookie) {
		q.mu.Unlock()
		return false, errQueueCookie
	}
	if q.held || q.active != nil {
		if len(q.pending) >= maxPending {
			q.mu.Unlock()
			return false, errQueueFull
		}
		q.pending = append(q.pending, req)
		q.signalLocked()
		q.mu.Unlock()
		if !waitStarted(req.started, req.done.ch) {
			return false, errQueueCancelled
		}
		return true, nil
	}
	q.active = &req
	q.out <- req
	close(req.started)
	q.signalLocked()
	q.mu.Unlock()
	return true, nil
}

func (q *queue) hasCookieLocked(cookie string) bool {
	if q.active != nil && q.active.Cookie == cookie {
		return true
	}
	for _, req := range q.pending {
		if req.Cookie == cookie {
			return true
		}
	}
	return false
}

// finish frees the display slot after its helper has stopped. Late finishes
// are ignored, so cancellation and shutdown cannot advance the queue twice.
func (q *queue) finish(cookie string) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.active == nil || q.active.Cookie != cookie {
		return
	}
	// A request can be withdrawn before the shell reads out. Remove that stale
	// item so dispatching its successor cannot block on the one-slot channel.
	q.discardBufferedLocked(cookie)
	q.active = nil
	q.dispatchLocked()
	q.signalLocked()
}

func (q *queue) discardBufferedLocked(cookie string) {
	select {
	case queued := <-q.out:
		if queued.Cookie != cookie {
			q.out <- queued
		}
	default:
	}
}

func (q *queue) signalLocked() {
	if q.changed == nil {
		return
	}
	select {
	case q.changed <- struct{}{}:
	default:
	}
}

// dispatchLocked shows the next pending request unless the surface is held.
// The caller has already cleared active, so out has room.
func (q *queue) dispatchLocked() {
	for !q.held && len(q.pending) > 0 {
		next := q.pending[0]
		q.pending = q.pending[1:]
		if next.Cookie == "" {
			continue
		}
		q.active = &next
		q.out <- next
		close(next.started)
		return
	}
}

// withdraw is polkitd cancelling a request that has not been answered. A
// pending one is dropped and answered canceled; the displayed one is signalled
// so the surface closes it and the session stops.
func (q *queue) withdraw(cookie string) (withdrawnRequest, bool) {
	q.mu.Lock()
	for i, pending := range q.pending {
		if pending.Cookie != cookie {
			continue
		}
		q.pending = append(q.pending[:i], q.pending[i+1:]...)
		q.signalLocked()
		q.mu.Unlock()
		pending.done.stop()
		return withdrawnRequest{request: pending, queued: true}, true
	}
	if q.active != nil && q.active.Cookie == cookie {
		active := *q.active
		q.active.done.stop()
		q.signalLocked()
		q.mu.Unlock()
		return withdrawnRequest{request: active}, true
	}
	q.mu.Unlock()
	return withdrawnRequest{}, false
}

// hold defers dispatch while the session is locked, so a prompt waits behind
// the lock screen instead of being unreachable. Un-holding shows the next one.
func (q *queue) hold(locked bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.held = locked
	if !locked {
		q.dispatchLocked()
	}
	q.signalLocked()
}

// cancelAll drops every request, displayed or waiting. It is the answer for a
// helper that died or a bus that went away: the caller is left without an
// answer, which polkitd treats as a cancellation.
func (q *queue) cancelAll() []Request {
	q.mu.Lock()
	pending := q.pending
	active := q.active
	q.pending, q.active = nil, nil
	if active != nil {
		q.discardBufferedLocked(active.Cookie)
	}
	q.signalLocked()
	q.mu.Unlock()
	if active != nil {
		active.done.stop()
	}
	for _, req := range pending {
		req.done.stop()
	}
	cancelled := make([]Request, 0, len(pending)+1)
	if active != nil {
		cancelled = append(cancelled, *active)
	}
	return append(cancelled, pending...)
}

// waiting reports how many prompts are queued behind the displayed one, for
// the "another request is waiting" line on the prompt.
func (q *queue) waiting() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return len(q.pending)
}
