package lock

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/godbus/dbus/v5"
)

// assertComparable is a compile-time guard: instantiating it fails the build
// if a type gains a field that makes == invalid. disconnectLocked relies on
// State == to decide whether a change is worth publishing.
func assertComparable[T comparable](_ T) {}

func TestStateIsComparable(t *testing.T) {
	assertComparable(State{})
	assertComparable(Snapshot{})
}

type fakeBus struct {
	mu         sync.Mutex
	owner      string
	ownerErr   error
	uid        uint32
	stateJSON  string
	stateErr   error
	signals    chan *dbus.Signal
	done       chan struct{}
	closeCount int
}

func newFakeBus() *fakeBus {
	return &fakeBus{
		signals: make(chan *dbus.Signal, 8),
		done:    make(chan struct{}),
	}
}

func (b *fakeBus) setLocker(owner string, snap Snapshot) {
	data, err := json.Marshal(snap)
	if err != nil {
		panic(err)
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.owner = owner
	b.uid = uint32(os.Getuid())
	b.stateJSON = string(data)
}

func (b *fakeBus) closes() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.closeCount
}

func (b *fakeBus) Close() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.closeCount++
	return nil
}

func (b *fakeBus) Done() <-chan struct{} { return b.done }

func (b *fakeBus) Signals() chan *dbus.Signal { return b.signals }

func (b *fakeBus) GetNameOwner(context.Context) (string, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.owner, b.ownerErr
}

func (b *fakeBus) GetConnectionUnixUser(_ context.Context, owner string) (uint32, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.owner != owner {
		return 0, errors.New("owner changed")
	}
	return b.uid, nil
}

func (b *fakeBus) GetState(context.Context, string) (string, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.stateErr != nil {
		return "", b.stateErr
	}
	if b.stateJSON == "" {
		return "", errors.New("no state")
	}
	return b.stateJSON, nil
}

func (b *fakeBus) RequestLock(context.Context, string) (string, error) {
	return "", errors.New("fake bus does not lock")
}

func idleSnapshot() Snapshot {
	return Snapshot{
		Session:    "session",
		Compositor: "niri:1:2",
		Sequence:   1,
		Generation: 1,
		Phase:      "idle",
	}
}

func recvUpdate(t *testing.T, c *Client, timeout time.Duration) (State, bool) {
	t.Helper()
	select {
	case s := <-c.Updates():
		return s, true
	case <-time.After(timeout):
		return State{}, false
	}
}

func TestDisconnectDoesNotRepublishSameState(t *testing.T) {
	c := New("session", "niri")
	if err := c.accept(":1.5", idleSnapshot()); err != nil {
		t.Fatal(err)
	}
	if _, ok := recvUpdate(t, c, time.Second); !ok {
		t.Fatal("accept did not publish")
	}

	// First disconnect: known state becomes unavailable. That is a real
	// change, so it publishes.
	changed := c.changed
	c.disconnect()
	select {
	case <-changed:
	default:
		t.Fatal("state change did not close the changed channel")
	}
	if _, ok := recvUpdate(t, c, time.Second); !ok {
		t.Fatal("first disconnect did not publish")
	}

	// Second disconnect: the state is already unavailable, so nothing new
	// reaches subscribers.
	changed = c.changed
	c.disconnect()
	select {
	case <-changed:
		t.Fatal("repeat disconnect republished an identical state")
	default:
	}
	select {
	case s := <-c.Updates():
		t.Fatalf("repeat disconnect pushed %v", s)
	case <-time.After(50 * time.Millisecond):
	}
}

func TestNoOwnerDoesNotRedial(t *testing.T) {
	var mu sync.Mutex
	dials := 0
	var bus *fakeBus
	dial := func() (busConn, error) {
		mu.Lock()
		defer mu.Unlock()
		dials++
		if bus == nil {
			bus = newFakeBus()
		}
		return bus, nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c := newClient("session", "niri", dial, waitAfter)
	c.Start(ctx)

	mu.Lock()
	b := bus
	mu.Unlock()
	if dials != 1 {
		t.Fatalf("dials = %d, want 1", dials)
	}
	// A NameOwnerChanged that still reports no owner must re-snapshot on the
	// existing connection, not open another one.
	b.signals <- &dbus.Signal{
		Name: "org.freedesktop.DBus.NameOwnerChanged",
		Body: []any{busName, "", ""},
	}
	time.Sleep(100 * time.Millisecond)
	mu.Lock()
	got := dials
	mu.Unlock()
	if got != 1 {
		t.Fatalf("redial happened without an owner: dials = %d", got)
	}
	if n := b.closes(); n != 0 {
		t.Fatalf("bus closed %d times while watching", n)
	}
	select {
	case s := <-c.Updates():
		t.Fatalf("owner-less watch published %v", s)
	default:
	}
}

func TestOwnerAppearsTriggersSnapshot(t *testing.T) {
	var mu sync.Mutex
	dials := 0
	var bus *fakeBus
	dial := func() (busConn, error) {
		mu.Lock()
		defer mu.Unlock()
		dials++
		if bus == nil {
			bus = newFakeBus()
		}
		return bus, nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c := newClient("session", "niri", dial, waitAfter)
	c.Start(ctx)

	mu.Lock()
	b := bus
	mu.Unlock()
	b.setLocker(":1.9", idleSnapshot())
	b.signals <- &dbus.Signal{
		Name: "org.freedesktop.DBus.NameOwnerChanged",
		Body: []any{busName, "", ":1.9"},
	}

	state, ok := recvUpdate(t, c, 2*time.Second)
	if !ok {
		t.Fatal("owner appearance did not publish")
	}
	if !state.Known || state.Owner != ":1.9" || state.Phase != "idle" {
		t.Fatalf("published %+v", state)
	}
	mu.Lock()
	if dials != 1 {
		t.Fatalf("dials = %d, want 1", dials)
	}
	mu.Unlock()
}

// backoffStep is one reconnect wait handed from a client to the test. The
// test releases the step, so the reconnect loop advances exactly one step per
// release and can never outrun the reader.
type backoffStep struct {
	delay   time.Duration
	release chan time.Time
}

func TestBusUnavailableBacksOff(t *testing.T) {
	fail := errors.New("no bus")
	steps := make(chan backoffStep, 16)
	c := newClient("session", "niri",
		func() (busConn, error) { return nil, fail },
		func(d time.Duration) <-chan time.Time {
			release := make(chan time.Time, 1)
			steps <- backoffStep{delay: d, release: release}
			return release
		})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() { defer close(done); c.run(ctx, nil) }()

	want := []time.Duration{
		time.Second, 2 * time.Second, 4 * time.Second, 8 * time.Second,
		16 * time.Second, reconnectBackoffCap, reconnectBackoffCap,
	}
	for i, w := range want {
		select {
		case step := <-steps:
			if step.delay != w {
				t.Fatalf("wait %d = %v, want %v", i, step.delay, w)
			}
			step.release <- time.Now()
		case <-time.After(2 * time.Second):
			t.Fatalf("only %d retries arrived", i)
		}
	}

	// A cancelled context ends the backoff wait promptly.
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("run did not return after cancel")
	}
}

// A client's reconnect loop must wait on its own seam. A seam shared by the
// whole package would let one client's wait land in another client's channel.
func TestBackoffSeamsArePerClient(t *testing.T) {
	fail := errors.New("no bus")
	first := make(chan time.Duration, 4)
	second := make(chan time.Duration, 4)
	waiting := func(waits chan time.Duration) func(time.Duration) <-chan time.Time {
		return func(d time.Duration) <-chan time.Time {
			select {
			case waits <- d:
			default:
			}
			// A nil channel parks the loop until the context is cancelled.
			return nil
		}
	}
	a := newClient("session", "niri", func() (busConn, error) { return nil, fail }, waiting(first))
	b := newClient("session", "niri", func() (busConn, error) { return nil, fail }, waiting(second))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{}, 2)
	go func() { defer func() { done <- struct{}{} }(); a.run(ctx, nil) }()
	go func() { defer func() { done <- struct{}{} }(); b.run(ctx, nil) }()

	for _, tc := range []struct {
		name  string
		waits chan time.Duration
	}{{"first", first}, {"second", second}} {
		select {
		case d := <-tc.waits:
			if d != time.Second {
				t.Fatalf("%s client wait = %v, want %v", tc.name, d, time.Second)
			}
		case <-time.After(2 * time.Second):
			t.Fatalf("%s client did not wait", tc.name)
		}
	}
	// Neither client recorded a wait in the other client's channel.
	for _, tc := range []struct {
		name  string
		waits chan time.Duration
	}{{"first", first}, {"second", second}} {
		select {
		case d := <-tc.waits:
			t.Fatalf("%s client recorded an extra wait %v", tc.name, d)
		default:
		}
	}

	cancel()
	for range 2 {
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Fatal("reconnect loop did not return after cancel")
		}
	}
}
