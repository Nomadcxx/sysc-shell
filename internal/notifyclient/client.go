package notifyclient

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"slices"
	"sync"
	"time"

	"github.com/Nomadcxx/sysc-notify/protocol"
)

const (
	initialBackoff = 100 * time.Millisecond
	maxBackoff     = 5 * time.Second
	handshakeGrace = 5 * time.Second
)

// Kind names what a Message carries.
type Kind uint8

const (
	// KindSnapshot is the baseline for a generation. Every projection must be
	// rebuilt from it; deltas before it are meaningless.
	KindSnapshot Kind = iota + 1
	KindDelta
	KindReply
	// KindDisconnected ends a generation. The consumer drops projected state:
	// the service may have changed anything while the shell was away.
	KindDisconnected
)

// Message is one immutable event from the service. Generation identifies the
// connection it belongs to, so a consumer can discard anything that arrives
// late from a connection that has already ended.
type Message struct {
	Generation uint64
	Kind       Kind
	Sequence   uint64
	RequestID  uint64
	Snapshot   protocol.Snapshot
	Delta      protocol.Delta
	Reply      protocol.Reply
	Err        error
}

// ErrBusy reports a full command queue. The caller retries later; the client
// never queues without bound because a wedged service would grow it forever.
var ErrBusy = errors.New("notifyclient: command queue is full")

// Client keeps one connection to the presenter socket, reconnecting with a
// capped backoff. One reader decodes frames and one writer serializes them.
type Client struct {
	runtimeDir string
	out        chan<- Message

	mu         sync.Mutex
	generation uint64
	requestID  uint64
	writer     chan []byte
	connected  bool
	pending    map[uint64]struct{}
}

func New(runtimeDir string, out chan<- Message) *Client {
	return &Client{runtimeDir: runtimeDir, out: out, pending: make(map[uint64]struct{})}
}

// Generation reports the current connection generation, zero when detached.
func (c *Client) Generation() uint64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.connected {
		return 0
	}
	return c.generation
}

// Run connects and reconnects until the context is cancelled.
func (c *Client) Run(ctx context.Context) error {
	backoff := initialBackoff
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		connected := c.session(ctx)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if connected {
			backoff = initialBackoff
			continue
		}
		if err := sleep(ctx, backoff); err != nil {
			return err
		}
		if backoff *= 2; backoff > maxBackoff {
			backoff = maxBackoff
		}
	}
}

// session holds one connection for its lifetime and reports whether the
// handshake succeeded, so only a failed attempt pays the backoff.
func (c *Client) session(ctx context.Context) bool {
	socket, err := dial(c.runtimeDir)
	if err != nil {
		return false
	}
	defer func() { _ = socket.Close() }()

	// The watcher starts before the handshake: a cancelled context must close
	// the socket at once rather than wait out the handshake deadline.
	stop := make(chan struct{})
	var once sync.Once
	closeStop := func() { once.Do(func() { close(stop) }) }
	watched := make(chan struct{})
	go func() {
		defer close(watched)
		select {
		case <-ctx.Done():
			_ = socket.Close()
		case <-stop:
		}
	}()
	defer func() {
		closeStop()
		<-watched
	}()

	if err := c.handshake(socket); err != nil {
		return false
	}
	generation := c.begin()
	defer c.end(generation)

	writer := make(chan []byte, protocol.MaxCommandQueue)
	c.attach(generation, writer)

	written := make(chan struct{})
	go func() {
		defer close(written)
		c.writeLoop(socket, writer, stop)
	}()

	c.readLoop(socket, generation)
	closeStop()
	<-written
	return true
}

// handshake sends the shell's hello and requires the service's own hello back
// before any state is accepted.
func (c *Client) handshake(socket *net.UnixConn) error {
	if err := socket.SetDeadline(time.Now().Add(handshakeGrace)); err != nil {
		return err
	}
	hello, err := marshalEnvelope(protocol.Envelope{Kind: protocol.KindHello}, protocol.Hello{
		Major: protocol.ProtocolMajor, Minor: protocol.ProtocolMinor,
		Role: protocol.RolePresenter, Capabilities: []string{RequiredCapability, RequiredLifetimeCapability},
	})
	if err != nil {
		return err
	}
	if err := protocol.WriteFrame(socket, hello); err != nil {
		return err
	}
	envelope, err := readEnvelope(socket)
	if err != nil {
		return err
	}
	if envelope.Kind != protocol.KindHello {
		return fmt.Errorf("notifyclient: first message was %q", envelope.Kind)
	}
	var service protocol.Hello
	if err := protocol.DecodeStrict(envelope.Payload, &service); err != nil {
		return err
	}
	if err := service.Validate(protocol.RolePresenter); err != nil {
		return err
	}
	if !slices.Contains(service.Capabilities, RequiredCapability) ||
		!slices.Contains(service.Capabilities, RequiredLifetimeCapability) {
		return errors.New("notifyclient: service lacks required presenter capability")
	}
	return socket.SetDeadline(time.Time{})
}

// RequiredCapability is the capability the shell needs from the service.
const RequiredCapability = "notification-state"

// RequiredLifetimeCapability keeps countdown state owned by the service.
const RequiredLifetimeCapability = "presentation-lifetime"

func (c *Client) begin() uint64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.generation++
	c.connected = true
	return c.generation
}

func (c *Client) attach(generation uint64, writer chan []byte) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.generation == generation {
		c.writer = writer
	}
}

// end closes a generation. Commands still in flight are never replayed: their
// outcome is unknown, and repeating one could invoke an action twice.
func (c *Client) end(generation uint64) {
	c.mu.Lock()
	if c.generation != generation {
		c.mu.Unlock()
		return
	}
	c.connected = false
	c.writer = nil
	c.pending = make(map[uint64]struct{})
	c.mu.Unlock()
	c.publish(Message{Generation: generation, Kind: KindDisconnected})
}

// readLoop decodes frames until the connection ends. Any structural error ends
// the generation rather than skipping the message, because a projection built
// on a gap is wrong in a way the shell cannot see.
func (c *Client) readLoop(socket *net.UnixConn, generation uint64) {
	var sequence uint64
	baseline := false
	for {
		envelope, err := readEnvelope(socket)
		if err != nil {
			return
		}
		switch envelope.Kind {
		case protocol.KindSnapshot:
			var snapshot protocol.Snapshot
			if protocol.DecodeStrict(envelope.Payload, &snapshot) != nil || snapshot.Validate() != nil {
				return
			}
			sequence, baseline = snapshot.Sequence, true
			c.publish(Message{
				Generation: generation, Kind: KindSnapshot,
				Sequence: snapshot.Sequence, Snapshot: snapshot,
			})
		case protocol.KindAdded, protocol.KindReplaced, protocol.KindClosed,
			protocol.KindHistoryAdded, protocol.KindHistoryRemoved,
			protocol.KindHistorySeen, protocol.KindHistoryCleared:
			if !baseline {
				return
			}
			if protocol.ValidateNextSequence(sequence, envelope.Sequence) != nil {
				return
			}
			var delta protocol.Delta
			if protocol.DecodeStrict(envelope.Payload, &delta) != nil || delta.Validate() != nil {
				return
			}
			sequence = envelope.Sequence
			c.publish(Message{
				Generation: generation, Kind: KindDelta,
				Sequence: envelope.Sequence, Delta: delta,
			})
		case protocol.KindReply:
			var reply protocol.Reply
			if protocol.DecodeStrict(envelope.Payload, &reply) != nil || reply.Validate() != nil {
				return
			}
			if !c.resolve(generation, envelope.RequestID) {
				return
			}
			c.publish(Message{
				Generation: generation, Kind: KindReply,
				RequestID: envelope.RequestID, Reply: reply,
			})
		default:
			return
		}
	}
}

// resolve clears one correlation. A reply naming no outstanding request means
// the stream is not the one the shell is speaking on.
func (c *Client) resolve(generation, requestID uint64) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.generation != generation {
		return false
	}
	if _, ok := c.pending[requestID]; !ok {
		return false
	}
	delete(c.pending, requestID)
	return true
}

func (c *Client) writeLoop(socket *net.UnixConn, writer chan []byte, stop chan struct{}) {
	for {
		select {
		case <-stop:
			return
		case frame := <-writer:
			if err := protocol.WriteFrame(socket, frame); err != nil {
				_ = socket.Close()
				return
			}
		}
	}
}

// Send queues one command and returns its request ID. Request IDs increase
// within a generation, so the service can reject a replay.
func (c *Client) Send(command protocol.Command) (uint64, error) {
	if err := command.Validate(); err != nil {
		return 0, err
	}
	c.mu.Lock()
	if !c.connected || c.writer == nil {
		c.mu.Unlock()
		return 0, errors.New("notifyclient: not connected")
	}
	c.requestID++
	requestID := c.requestID
	writer := c.writer
	c.pending[requestID] = struct{}{}
	c.mu.Unlock()

	frame, err := marshalEnvelope(
		protocol.Envelope{Kind: protocol.KindCommand, RequestID: requestID}, command)
	if err != nil {
		c.forget(requestID)
		return 0, err
	}
	select {
	case writer <- frame:
		return requestID, nil
	default:
		c.forget(requestID)
		return 0, ErrBusy
	}
}

func (c *Client) forget(requestID uint64) {
	c.mu.Lock()
	delete(c.pending, requestID)
	c.mu.Unlock()
}

func (c *Client) publish(message Message) {
	if c.out == nil {
		return
	}
	c.out <- message
}

func marshalEnvelope(envelope protocol.Envelope, payload any) ([]byte, error) {
	encoded, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	envelope.Payload = encoded
	return json.Marshal(envelope)
}

func readEnvelope(socket net.Conn) (protocol.Envelope, error) {
	frame, err := protocol.ReadFrame(socket)
	if err != nil {
		return protocol.Envelope{}, err
	}
	var envelope protocol.Envelope
	if err := protocol.DecodeStrict(frame, &envelope); err != nil {
		return protocol.Envelope{}, err
	}
	if err := envelope.Validate(); err != nil {
		return protocol.Envelope{}, err
	}
	return envelope, nil
}

func sleep(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
