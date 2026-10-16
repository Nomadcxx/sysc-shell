package ipc

import (
	"encoding/json"
	"fmt"
	"net"
	"sort"
	"sync"
	"time"
)

// Provider feeds one state topic. The first subscriber starts it and the last
// to leave stops it, so no provider runs while nobody listens.
//
// Start must publish the topic's initial state before it returns. Every value
// passed to publish is the topic's whole state and must not be mutated
// afterwards: subscribers share it and marshal it later, off every shell lock.
// publish never blocks, so it is safe under the caller's own locks. Start and
// Stop run on the subscribing or leaving connection's goroutine.
type Provider struct {
	Start func(publish func(any)) error
	Stop  func()
}

// Event is one line on an event connection.
type Event struct {
	Event string          `json:"event"`
	Topic string          `json:"topic"`
	Seq   uint64          `json:"seq,omitempty"`
	Data  json.RawMessage `json:"data,omitempty"`
	Code  string          `json:"code,omitempty"`
	Error string          `json:"error,omitempty"`
}

// Event kinds.
const (
	EventSnapshot = "snapshot"
	EventState    = "state"
	EventResync   = "resync"
	EventError    = "error"
)

// hub owns one topic: its provider's lifetime, the latest state and its seq,
// and the subscribers. Taking the snapshot and registering a subscriber happen
// under one lock with publish, so no state can fall between them.
type hub struct {
	name     string
	provider Provider

	// life serialises the provider's Start and Stop with the refcount. It is
	// never taken under mu.
	life sync.Mutex
	refs int

	mu     sync.Mutex
	gen    uint64 // the provider run a publish func belongs to
	live   bool
	has    bool // the current run has published
	latest any
	seq    uint64 // increases across provider runs
	subs   map[*session]struct{}
}

func (h *hub) publish(gen uint64, v any) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if gen != h.gen || !h.live {
		// A publish from a stopped run, or one racing its Stop.
		return
	}
	h.seq++
	h.latest, h.has = v, true
	for c := range h.subs {
		c.offer(h.name, h.seq, v)
	}
}

// subscribe registers c, starting the provider for the first subscriber, and
// queues c's snapshot atomically with the registration.
func (h *hub) subscribe(c *session) error {
	h.life.Lock()
	defer h.life.Unlock()
	if h.refs == 0 {
		h.mu.Lock()
		h.gen++
		gen := h.gen
		h.live, h.has = true, false
		h.mu.Unlock()
		err := h.provider.Start(func(v any) { h.publish(gen, v) })
		h.mu.Lock()
		has := h.has
		if err != nil || !has {
			h.live, h.latest = false, nil
		}
		h.mu.Unlock()
		if err != nil {
			return fmt.Errorf("topic %s: %v", h.name, err)
		}
		if !has {
			if h.provider.Stop != nil {
				h.provider.Stop()
			}
			return fmt.Errorf("topic %s: provider published no initial state", h.name)
		}
	}
	h.refs++
	h.mu.Lock()
	if h.subs == nil {
		h.subs = map[*session]struct{}{}
	}
	h.subs[c] = struct{}{}
	c.offerSnapshot(h.name, h.seq, h.latest)
	h.mu.Unlock()
	return nil
}

// unsubscribe removes c, stopping the provider when c was the last
// subscriber. Removing a session that is not subscribed does nothing.
func (h *hub) unsubscribe(c *session) {
	h.life.Lock()
	defer h.life.Unlock()
	h.mu.Lock()
	if _, ok := h.subs[c]; !ok {
		h.mu.Unlock()
		return
	}
	delete(h.subs, c)
	h.refs--
	stop := h.refs == 0
	if stop {
		h.live, h.latest = false, nil
	}
	h.mu.Unlock()
	if stop && h.provider.Stop != nil {
		h.provider.Stop()
	}
}

// pendingEvent is the one undelivered state a session holds for a topic.
type pendingEvent struct {
	kind string
	seq  uint64
	data any
}

// session is one connection. It starts as a command connection, answered in
// order on the reading goroutine. A successful ipc.subscribe makes it an event
// connection for good: one writer goroutine then owns every write, and each
// topic holds at most one undelivered state, so publishing never waits on a
// reader.
type session struct {
	s  *Server
	nc net.Conn

	mu      sync.Mutex
	events  bool
	topics  map[string]bool
	pending map[string]*pendingEvent
	replies [][]byte
	hold    bool // a subscribe is in flight: its reply goes before its snapshots

	wake       chan struct{}
	done       chan struct{}
	writerDone chan struct{}
}

func newSession(s *Server, nc net.Conn) *session {
	return &session{
		s:          s,
		nc:         nc,
		topics:     map[string]bool{},
		pending:    map[string]*pendingEvent{},
		wake:       make(chan struct{}, 1),
		done:       make(chan struct{}),
		writerDone: make(chan struct{}),
	}
}

func (c *session) isEvents() bool {
	if c == nil {
		return false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.events
}

// reply sends a reply in order: directly on a command connection, through the
// writer on an event connection.
func (c *session) reply(b []byte) {
	c.mu.Lock()
	if c.events {
		c.replies = append(c.replies, b)
		c.mu.Unlock()
		c.poke()
		return
	}
	c.mu.Unlock()
	c.write(b)
}

func (c *session) poke() {
	select {
	case c.wake <- struct{}{}:
	default:
	}
}

func (c *session) write(b []byte) bool {
	_ = c.nc.SetWriteDeadline(time.Now().Add(c.s.writeTimeout))
	_, err := c.nc.Write(append(b, '\n'))
	return err == nil
}

// offer replaces the topic's undelivered state with a newer one. Replacing a
// state the reader never saw makes the next delivery a resync; a snapshot not
// yet delivered stays a snapshot, now carrying the newer value.
func (c *session) offer(topic string, seq uint64, data any) {
	c.mu.Lock()
	kind := EventState
	if p := c.pending[topic]; p != nil {
		kind = EventResync
		if p.kind == EventSnapshot {
			kind = EventSnapshot
		}
	}
	c.pending[topic] = &pendingEvent{kind: kind, seq: seq, data: data}
	c.mu.Unlock()
	c.poke()
}

func (c *session) offerSnapshot(topic string, seq uint64, data any) {
	c.mu.Lock()
	c.pending[topic] = &pendingEvent{kind: EventSnapshot, seq: seq, data: data}
	c.mu.Unlock()
	c.poke()
}

type topicsParams struct {
	Topics []string `json:"topics"`
}

type topicsResult struct {
	Topics []string `json:"topics"`
}

func decodeTopics(req request) ([]string, []byte) {
	var p topicsParams
	if err := DecodeParams(req.Params, &p); err != nil {
		return nil, codedError(req.ID, CodeInvalidParams, err.Error())
	}
	if len(p.Topics) == 0 {
		return nil, codedError(req.ID, CodeInvalidParams, "topics: name at least one")
	}
	seen := map[string]bool{}
	var out []string
	for _, t := range p.Topics {
		if !seen[t] {
			seen[t] = true
			out = append(out, t)
		}
	}
	return out, nil
}

// subscribe answers ipc.subscribe. It queues its own reply on success, so the
// reply always precedes the snapshots it caused.
func (c *session) subscribe(req request) []byte {
	topics, bad := decodeTopics(req)
	if bad != nil {
		return bad
	}
	if c == nil {
		return codedError(req.ID, CodeUnsupported, "ipc.subscribe needs a connection")
	}
	var fresh []*hub
	c.mu.Lock()
	for _, t := range topics {
		if c.topics[t] {
			continue
		}
		h := c.s.hubs[t]
		if h == nil {
			c.mu.Unlock()
			return codedError(req.ID, CodeUnsupported, fmt.Sprintf("unknown topic %q", t))
		}
		fresh = append(fresh, h)
	}
	if len(c.topics)+len(fresh) > maxTopicsPerConn {
		c.mu.Unlock()
		return codedError(req.ID, CodeLimitExceeded, fmt.Sprintf("more than %d topics on one connection", maxTopicsPerConn))
	}
	becoming := !c.events
	c.hold = true
	c.mu.Unlock()

	if becoming && !c.s.acquireConsumer() {
		c.release(nil)
		return codedError(req.ID, CodeLimitExceeded, fmt.Sprintf("more than %d event connections", maxConsumers))
	}
	for i, h := range fresh {
		if err := h.subscribe(c); err != nil {
			c.release(fresh[:i])
			if becoming {
				c.s.releaseConsumer()
			}
			return envelope(req.ID, "", err.Error())
		}
	}

	c.mu.Lock()
	for _, h := range fresh {
		c.topics[h.name] = true
	}
	c.replies = append(c.replies, resultEnvelope(req.ID, topicsResult{c.topicList()}))
	c.hold = false
	if becoming {
		c.events = true
		go c.writeLoop()
	}
	c.mu.Unlock()
	c.poke()
	return nil
}

// release undoes a failed subscribe: it leaves the hubs already joined and
// drops their queued snapshots.
func (c *session) release(joined []*hub) {
	for _, h := range joined {
		h.unsubscribe(c)
	}
	c.mu.Lock()
	for _, h := range joined {
		delete(c.pending, h.name)
	}
	c.hold = false
	c.mu.Unlock()
	c.poke()
}

// unsubscribe answers ipc.unsubscribe. Topics the connection does not hold are
// ignored. The connection stays an event connection.
func (c *session) unsubscribe(req request) []byte {
	topics, bad := decodeTopics(req)
	if bad != nil {
		return bad
	}
	if c == nil {
		return codedError(req.ID, CodeUnsupported, "ipc.unsubscribe needs a connection")
	}
	for _, t := range topics {
		c.drop(t)
	}
	c.mu.Lock()
	result := topicsResult{c.topicList()}
	c.mu.Unlock()
	return resultEnvelope(req.ID, result)
}

// drop ends one topic on this connection and releases its provider reference.
func (c *session) drop(topic string) {
	c.mu.Lock()
	held := c.topics[topic]
	delete(c.topics, topic)
	delete(c.pending, topic)
	c.mu.Unlock()
	if held {
		c.s.hubs[topic].unsubscribe(c)
	}
}

// topicList is the connection's topics, sorted. The caller holds c.mu.
func (c *session) topicList() []string {
	out := make([]string, 0, len(c.topics))
	for t := range c.topics {
		out = append(out, t)
	}
	sort.Strings(out)
	return out
}

// take hands the writer everything ready: queued replies, then, unless a
// subscribe is in flight, the pending states in topic order.
func (c *session) take() ([][]byte, []Event, []any) {
	c.mu.Lock()
	defer c.mu.Unlock()
	replies := c.replies
	c.replies = nil
	if c.hold || len(c.pending) == 0 {
		return replies, nil, nil
	}
	names := make([]string, 0, len(c.pending))
	for t := range c.pending {
		names = append(names, t)
	}
	sort.Strings(names)
	events := make([]Event, len(names))
	data := make([]any, len(names))
	for i, t := range names {
		p := c.pending[t]
		events[i] = Event{Event: p.kind, Topic: t, Seq: p.seq}
		data[i] = p.data
	}
	c.pending = map[string]*pendingEvent{}
	return replies, events, data
}

// writeLoop is the event connection's only writer. It marshals outside every
// lock. A write that misses its deadline closes the connection, which ends the
// reader and releases every topic.
func (c *session) writeLoop() {
	defer close(c.writerDone)
	for {
		select {
		case <-c.wake:
		case <-c.done:
			replies, _, _ := c.take()
			for _, r := range replies {
				if !c.write(r) {
					break
				}
			}
			return
		}
		replies, events, data := c.take()
		for _, r := range replies {
			if !c.write(r) {
				_ = c.nc.Close()
				return
			}
		}
		for i, ev := range events {
			if !c.write(c.encode(ev, data[i])) {
				_ = c.nc.Close()
				return
			}
		}
	}
}

// encode marshals one event. A state too large for the pending budget, or one
// that cannot be marshalled, ends its topic on this connection with an error
// event instead.
func (c *session) encode(ev Event, data any) []byte {
	raw, err := json.Marshal(data)
	switch {
	case err != nil:
		c.drop(ev.Topic)
		ev = Event{Event: EventError, Topic: ev.Topic, Error: "unencodable state: " + err.Error()}
	case len(raw) > pendingBytes:
		c.drop(ev.Topic)
		ev = Event{Event: EventError, Topic: ev.Topic, Code: CodeLimitExceeded,
			Error: fmt.Sprintf("state of %d bytes is over pending_bytes %d", len(raw), pendingBytes)}
	default:
		ev.Data = raw
	}
	b, _ := json.Marshal(ev)
	return b
}

// close releases everything the connection holds. It runs once the reader has
// stopped, and lets the writer flush queued replies before the socket closes.
func (c *session) close() {
	c.mu.Lock()
	topics := c.topicList()
	events := c.events
	c.mu.Unlock()
	for _, t := range topics {
		c.drop(t)
	}
	if events {
		close(c.done)
		<-c.writerDone
		c.s.releaseConsumer()
	}
}

func (s *Server) acquireConsumer() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.consumers >= maxConsumers {
		return false
	}
	s.consumers++
	return true
}

func (s *Server) releaseConsumer() {
	s.mu.Lock()
	s.consumers--
	s.mu.Unlock()
}
