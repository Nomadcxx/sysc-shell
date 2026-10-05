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
	conn            *dbus.Conn
	state           State
	updates         chan State
	changed         chan struct{}
}

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
func (c *Client) acceptReply(conn *dbus.Conn, owner, currentOwner string, v Snapshot) error {
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
func (c *Client) disconnectLocked() {
	if c.state.Phase != "idle" && c.state.Phase != "failed-before-acquisition" && c.state.Generation != 0 {
		c.state.Phase = "sealed/unknown"
	} else {
		c.state.Phase = "unavailable"
	}
	c.state.Known = false
	c.state.SleepProtected = false
	c.state.Owner = ""
	c.conn = nil
	c.publish()
}
func (c *Client) invalidateReply(conn *dbus.Conn, owner string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.conn == conn && c.state.Known && c.state.Owner == owner {
		c.disconnectLocked()
	}
}
func (c *Client) connect(ctx context.Context) (*dbus.Conn, chan *dbus.Signal, error) {
	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		return nil, nil, err
	}
	fail := func(err error) (*dbus.Conn, chan *dbus.Signal, error) { conn.Close(); return nil, nil, err }
	signals := make(chan *dbus.Signal, 32)
	conn.Signal(signals)
	if err = conn.AddMatchSignal(dbus.WithMatchSender(busName), dbus.WithMatchObjectPath(busPath), dbus.WithMatchInterface(busName), dbus.WithMatchMember("Changed")); err != nil {
		return fail(err)
	}
	if err = conn.AddMatchSignal(dbus.WithMatchSender("org.freedesktop.DBus"), dbus.WithMatchInterface("org.freedesktop.DBus"), dbus.WithMatchMember("NameOwnerChanged"), dbus.WithMatchArg(0, busName)); err != nil {
		return fail(err)
	}
	var owner string
	if err = conn.BusObject().CallWithContext(ctx, "org.freedesktop.DBus.GetNameOwner", 0, busName).Store(&owner); err != nil {
		return fail(err)
	}
	var uid uint32
	if err = conn.BusObject().CallWithContext(ctx, "org.freedesktop.DBus.GetConnectionUnixUser", 0, owner).Store(&uid); err != nil {
		return fail(err)
	}
	if uid != uint32(os.Getuid()) {
		return fail(fmt.Errorf("locker UID differs"))
	}
	var data string
	if err = conn.Object(owner, busPath).CallWithContext(ctx, busName+".GetState", 0).Store(&data); err != nil {
		return fail(err)
	}
	v, err := decode(data)
	if err != nil {
		return fail(err)
	}
	var currentOwner string
	if err = conn.BusObject().CallWithContext(ctx, "org.freedesktop.DBus.GetNameOwner", 0, busName).Store(&currentOwner); err != nil {
		return fail(err)
	}
	if currentOwner != owner {
		return fail(fmt.Errorf("lock ownership changed during snapshot"))
	}
	c.mu.Lock()
	err = c.acceptLocked(owner, v)
	if err == nil {
		c.conn = conn
	}
	c.mu.Unlock()
	if err != nil {
		return fail(err)
	}
	return conn, signals, nil
}
func decode(data string) (Snapshot, error) {
	var v Snapshot
	if len(data) > 16384 {
		return v, fmt.Errorf("lock snapshot exceeds limit")
	}
	err := json.Unmarshal([]byte(data), &v)
	return v, err
}

// Start queries before background startup, then reconciles on every owner change.
func (c *Client) Start(ctx context.Context) {
	query, cancel := context.WithTimeout(ctx, 2*time.Second)
	conn, signals, err := c.connect(query)
	cancel()
	if err != nil {
		c.disconnect()
	}
	go c.run(ctx, conn, signals)
}
func (c *Client) run(ctx context.Context, conn *dbus.Conn, signals chan *dbus.Signal) {
	defer func() {
		if conn != nil {
			conn.Close()
		}
		c.disconnect()
	}()
	for {
		if conn == nil {
			timer := time.NewTimer(time.Second)
			select {
			case <-ctx.Done():
				timer.Stop()
				return
			case <-timer.C:
			}
			query, cancel := context.WithTimeout(ctx, 2*time.Second)
			var err error
			conn, signals, err = c.connect(query)
			cancel()
			if err != nil {
				c.disconnect()
				continue
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-conn.Context().Done():
			conn = nil
			c.disconnect()
		case sig, ok := <-signals:
			if !ok || sig == nil || sig.Name == "org.freedesktop.DBus.NameOwnerChanged" {
				conn.Close()
				conn = nil
				c.disconnect()
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
				conn.Close()
				conn = nil
				c.disconnect()
			}
		}
	}
}
func (c *Client) Lock(ctx context.Context) (State, error) {
	c.mu.Lock()
	conn, owner := c.conn, c.state.Owner
	known := c.state.Known
	c.mu.Unlock()
	if conn == nil || !known {
		return c.State(), fmt.Errorf("managed locker unavailable")
	}
	var data string
	if err := conn.Object(owner, busPath).CallWithContext(ctx, busName+".Lock", 0).Store(&data); err != nil {
		c.invalidateReply(conn, owner)
		conn.Close()
		return c.State(), err
	}
	v, err := decode(data)
	if err != nil {
		return c.State(), err
	}
	checkCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	var currentOwner string
	err = conn.BusObject().CallWithContext(checkCtx, "org.freedesktop.DBus.GetNameOwner", 0, busName).Store(&currentOwner)
	cancel()
	if err != nil {
		c.invalidateReply(conn, owner)
		conn.Close()
		return c.State(), fmt.Errorf("recheck lock owner: %w", err)
	}
	if err = c.acceptReply(conn, owner, currentOwner, v); err != nil {
		c.invalidateReply(conn, owner)
		conn.Close()
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
