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

// swapSeams replaces dialFn and waitFn for one test and restores them after.
func swapSeams(t *testing.T, dial func() (busConn, error), wait func(time.Duration) <-chan time.Time) {
	t.Helper()
	oldDial, oldWait := dialFn, waitFn
	dialFn = dial
	waitFn = wait
	t.Cleanup(func() { dialFn, waitFn = oldDial, oldWait })
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
	swapSeams(t, func() (busConn, error) {
		mu.Lock()
		defer mu.Unlock()
		dials++
		if bus == nil {
			bus = newFakeBus()
		}
		return bus, nil
	}, waitFn)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c := New("session", "niri")
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
	swapSeams(t, func() (busConn, error) {
		mu.Lock()
		defer mu.Unlock()
		dials++
		if bus == nil {
			bus = newFakeBus()
		}
		return bus, nil
	}, waitFn)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c := New("session", "niri")
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

func TestBusUnavailableBacksOff(t *testing.T) {
	fail := errors.New("no bus")
	waits := make(chan time.Duration, 8)
	swapSeams(t, func() (busConn, error) {
		return nil, fail
	}, func(d time.Duration) <-chan time.Time {
		select {
		case waits <- d:
			return time.After(0)
		default:
			// The test has seen enough retries. Park the loop on a nil
			// channel so seam restoration cannot race with another dial.
			return nil
		}
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c := New("session", "niri")
	c.Start(ctx)

	want := []time.Duration{
		time.Second, 2 * time.Second, 4 * time.Second, 8 * time.Second,
		16 * time.Second, reconnectBackoffCap, reconnectBackoffCap,
	}
	for i, w := range want {
		select {
		case got := <-waits:
			if got != w {
				t.Fatalf("wait %d = %v, want %v", i, got, w)
			}
		case <-time.After(2 * time.Second):
			t.Fatalf("only %d retries arrived", i)
		}
	}

	// A cancelled context ends the backoff wait promptly.
	cancel()
	done := make(chan struct{})
	go func() { c.run(ctx, nil); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("run did not return after cancel")
	}
}
