// Package lock is the managed session owner's credential-free client.
package lock

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/godbus/dbus/v5"
	"os"
	"strings"
	"sync"
	"time"
)

const busName = "org.sysc.LockSession1"
const busPath = dbus.ObjectPath("/org/sysc/LockSession1")

type Snapshot struct {
	Sequence, Generation, ConfirmedUnlock uint64
	Session, Compositor, Phase            string
	ForegroundReady                       bool
	Background                            string
	SleepProtected                        bool
	SleepError                            string
}
type State struct {
	Snapshot
	Known bool
	Owner string
}
type Client struct {
	mu              sync.Mutex
	session, socket string
	conn            busConn
	state           State
	updates         chan State
	changed         chan struct{}
}

// busConn is the small slice of the session bus the watcher needs. Keeping it
// an interface lets the watch loop and snapshot be tested without a live bus.
type busConn interface {
	Close() error
	Done() <-chan struct{}
	Signals() chan *dbus.Signal
	GetNameOwner(ctx context.Context) (string, error)
	GetConnectionUnixUser(ctx context.Context, owner string) (uint32, error)
	GetState(ctx context.Context, owner string) (string, error)
	RequestLock(ctx context.Context, owner string) (string, error)
}

type liveBus struct {
	conn    *dbus.Conn
	signals chan *dbus.Signal
}

func (b *liveBus) Close() error               { return b.conn.Close() }
func (b *liveBus) Done() <-chan struct{}      { return b.conn.Context().Done() }
func (b *liveBus) Signals() chan *dbus.Signal { return b.signals }
func (b *liveBus) GetNameOwner(ctx context.Context) (string, error) {
	var owner string
	err := b.conn.BusObject().CallWithContext(ctx, "org.freedesktop.DBus.GetNameOwner", 0, busName).Store(&owner)
	return owner, err
}
func (b *liveBus) GetConnectionUnixUser(ctx context.Context, owner string) (uint32, error) {
	var uid uint32
	err := b.conn.BusObject().CallWithContext(ctx, "org.freedesktop.DBus.GetConnectionUnixUser", 0, owner).Store(&uid)
	return uid, err
}
func (b *liveBus) GetState(ctx context.Context, owner string) (string, error) {
	var data string
	err := b.conn.Object(owner, busPath).CallWithContext(ctx, busName+".GetState", 0).Store(&data)
	return data, err
}
func (b *liveBus) RequestLock(ctx context.Context, owner string) (string, error) {
	var data string
	err := b.conn.Object(owner, busPath).CallWithContext(ctx, busName+".Lock", 0).Store(&data)
	return data, err
}

// dialFn opens one session-bus connection with both signal matches installed.
// Tests replace it to count dials without a bus.
var dialFn = func() (busConn, error) {
	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		return nil, err
	}
	fail := func(err error) (busConn, error) { conn.Close(); return nil, err }
	signals := make(chan *dbus.Signal, 32)
	conn.Signal(signals)
	if err = conn.AddMatchSignal(dbus.WithMatchSender(busName), dbus.WithMatchObjectPath(busPath), dbus.WithMatchInterface(busName), dbus.WithMatchMember("Changed")); err != nil {
		return fail(err)
	}
	if err = conn.AddMatchSignal(dbus.WithMatchSender("org.freedesktop.DBus"), dbus.WithMatchInterface("org.freedesktop.DBus"), dbus.WithMatchMember("NameOwnerChanged"), dbus.WithMatchArg(0, busName)); err != nil {
		return fail(err)
	}
	return &liveBus{conn: conn, signals: signals}, nil
}

// waitFn paces reconnect attempts after a dial failure. Tests replace it.
var waitFn = time.After

const reconnectBackoffCap = 30 * time.Second

func New(session, socket string) *Client {
	return &Client{session: session, socket: socket, state: State{Snapshot: Snapshot{Phase: "unavailable"}}, updates: make(chan State, 1), changed: make(chan struct{})}
}
func (c *Client) State() State          { c.mu.Lock(); defer c.mu.Unlock(); return c.state }
func (c *Client) Updates() <-chan State { return c.updates }
func (c *Client) publish() {
	close(c.changed)
	c.changed = make(chan struct{})
	select {
	case c.updates <- c.state:
	default:
		select {
		case <-c.updates:
		default:
		}
		c.updates <- c.state
	}
}
func (c *Client) accept(owner string, v Snapshot) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.acceptLocked(owner, v)
}
func (c *Client) acceptLocked(owner string, v Snapshot) error {
	if owner == "" || v.Session == "" || (c.session != "" && v.Session != c.session) || !strings.HasPrefix(v.Compositor, c.socket+":") || c.socket == "" {
		return fmt.Errorf("locker session identity differs")
	}
	switch v.Phase {
	case "idle", "requesting", "sealed", "unlocking", "recovering", "failed-before-acquisition", "sealed/unknown":
	default:
		return fmt.Errorf("invalid lock phase")
	}
	if v.ConfirmedUnlock > v.Generation {
		return fmt.Errorf("invalid unlock receipt")
	}
	if c.state.Known && c.state.Owner == owner {
		if v.Sequence < c.state.Sequence {
			return nil
		}
		if v.Generation < c.state.Generation || v.ConfirmedUnlock < c.state.ConfirmedUnlock {
			return fmt.Errorf("lock generation regressed")
		}
	}
	c.state = State{Snapshot: v, Known: true, Owner: owner}
	c.publish()
	return nil
}

// A method reply cannot revalidate a connection superseded by an owner change.
func (c *Client) acceptReply(conn busConn, owner, currentOwner string, v Snapshot) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.conn != conn || !c.state.Known || c.state.Owner != owner {
		return fmt.Errorf("lock ownership changed during request")
	}
	if currentOwner != owner {
		c.disconnectLocked()
		return fmt.Errorf("lock ownership changed during request")
	}
	return c.acceptLocked(owner, v)
}
func (c *Client) disconnect() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.disconnectLocked()
}

// disconnectLocked publishes only on an actual change. State and Snapshot are
// scalar-only by design so == is the change test; TestStateIsComparable
// fails the build if a future field breaks that.
func (c *Client) disconnectLocked() {
	prev := c.state
	if c.state.Phase != "idle" && c.state.Phase != "failed-before-acquisition" && c.state.Generation != 0 {
		c.state.Phase = "sealed/unknown"
	} else {
		c.state.Phase = "unavailable"
		// An unavailable client knows no generation. Clearing it keeps a
		// repeated disconnect identical, so the check below stays quiet.
		c.state.Generation = 0
		c.state.ConfirmedUnlock = 0
	}
	c.state.Known = false
	c.state.SleepProtected = false
	c.state.Owner = ""
	c.conn = nil
	if c.state != prev {
		c.publish()
	}
}
func (c *Client) invalidateReply(conn busConn, owner string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.conn == conn && c.state.Known && c.state.Owner == owner {
		c.disconnectLocked()
	}
}

// snapshot reads the locker over an already-dialed connection and never closes
// it. An absent or wrong owner is a state change, not a bus fault: the
// connection stays open and the next NameOwnerChanged re-runs the snapshot.
func (c *Client) snapshot(ctx context.Context, bus busConn) {
	query, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	owner, err := bus.GetNameOwner(query)
	if err != nil || owner == "" {
		c.disconnect()
		return
	}
	uid, err := bus.GetConnectionUnixUser(query, owner)
	if err != nil || uid != uint32(os.Getuid()) {
		c.disconnect()
		return
	}
	data, err := bus.GetState(query, owner)
	if err != nil {
		c.disconnect()
		return
	}
	v, err := decode(data)
	if err != nil {
		c.disconnect()
		return
	}
	currentOwner, err := bus.GetNameOwner(query)
	if err != nil || currentOwner != owner {
		c.disconnect()
		return
	}
	c.mu.Lock()
	err = c.acceptLocked(owner, v)
	if err == nil {
		c.conn = bus
	}
	c.mu.Unlock()
	if err != nil {
		c.disconnect()
	}
}
func decode(data string) (Snapshot, error) {
	var v Snapshot
	if len(data) > 16384 {
		return v, fmt.Errorf("lock snapshot exceeds limit")
	}
	err := json.Unmarshal([]byte(data), &v)
	return v, err
}

// Start dials once before background startup, then keeps that connection for
// the life of the client. Reconnect happens only when the bus itself closes,
// paced by exponential backoff; an unowned bus name costs nothing but a wait.
func (c *Client) Start(ctx context.Context) {
	bus, err := dialFn()
	if err != nil {
		c.disconnect()
	} else {
		c.snapshot(ctx, bus)
	}
	go c.run(ctx, bus)
}
func (c *Client) run(ctx context.Context, bus busConn) {
	defer func() {
		if bus != nil {
			bus.Close()
		}
		c.disconnect()
	}()
	delay := time.Second
	for {
		if bus == nil {
			select {
			case <-ctx.Done():
				return
			case <-waitFn(delay):
			}
			next, err := dialFn()
			if err != nil {
				c.disconnect()
				delay *= 2
				if delay > reconnectBackoffCap {
					delay = reconnectBackoffCap
				}
				continue
			}
			bus, delay = next, time.Second
			c.snapshot(ctx, bus)
		}
		select {
		case <-ctx.Done():
			return
		case <-bus.Done():
			bus = nil
			c.disconnect()
		case sig, ok := <-bus.Signals():
			if !ok || sig == nil {
				bus.Close()
				bus = nil
				c.disconnect()
				continue
			}
			if sig.Name == "org.freedesktop.DBus.NameOwnerChanged" {
				c.snapshot(ctx, bus)
				continue
			}
			state := c.State()
			if sig.Sender != state.Owner || sig.Path != busPath || sig.Name != busName+".Changed" || len(sig.Body) != 1 {
				continue
			}
			data, ok := sig.Body[0].(string)
			if !ok {
				continue
			}
			v, err := decode(data)
			if err != nil || c.accept(sig.Sender, v) != nil {
				c.disconnect()
			}
		}
	}
}
func (c *Client) Lock(ctx context.Context) (State, error) {
	c.mu.Lock()
	bus, owner := c.conn, c.state.Owner
	known := c.state.Known
	c.mu.Unlock()
	if bus == nil || !known {
		return c.State(), fmt.Errorf("managed locker unavailable")
	}
	data, err := bus.RequestLock(ctx, owner)
	if err != nil {
		c.invalidateReply(bus, owner)
		bus.Close()
		return c.State(), err
	}
	v, err := decode(data)
	if err != nil {
		return c.State(), err
	}
	checkCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	currentOwner, err := bus.GetNameOwner(checkCtx)
	cancel()
	if err != nil {
		c.invalidateReply(bus, owner)
		bus.Close()
		return c.State(), fmt.Errorf("recheck lock owner: %w", err)
	}
	if err = c.acceptReply(bus, owner, currentOwner, v); err != nil {
		c.invalidateReply(bus, owner)
		bus.Close()
		return c.State(), err
	}
	return c.State(), nil
}
func (c *Client) WaitSealed(ctx context.Context) error {
	initial, err := c.Lock(ctx)
	if err != nil {
		return err
	}
	return c.waitSealed(ctx, initial.Owner, initial.Generation)
}
func (c *Client) waitSealed(ctx context.Context, owner string, generation uint64) error {
	for {
		c.mu.Lock()
		v, changed := c.state, c.changed
		c.mu.Unlock()
		if !v.Known || v.Owner != owner || v.Generation != generation {
			return fmt.Errorf("lock ownership lost; suspend cancelled")
		}
		if !v.SleepProtected {
			return fmt.Errorf("sleep protection unavailable; suspend cancelled")
		}
		if v.Phase == "sealed" {
			return nil
		}
		if v.Phase != "requesting" && v.Phase != "recovering" {
			return fmt.Errorf("lock %s; suspend cancelled", v.Phase)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-changed:
		}
	}
}
func (c *Client) Suspend(ctx context.Context, run func() error) error {
	if err := c.WaitSealed(ctx); err != nil {
		return err
	}
	return run()
}
