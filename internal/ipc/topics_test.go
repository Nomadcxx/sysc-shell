package ipc

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/Nomadcxx/sysc-shell/internal/sockpath"
	"net"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// fakeTopic is a provider whose publish func the test drives. It publishes
// initial on Start, as every provider must.
type fakeTopic struct {
	initial any
	silent  bool // Start publishes nothing

	mu      sync.Mutex
	publish func(any)
	starts  atomic.Int32
	stops   atomic.Int32
}

func (f *fakeTopic) provider() Provider {
	return Provider{
		Start: func(publish func(any)) error {
			f.starts.Add(1)
			f.mu.Lock()
			f.publish = publish
			f.mu.Unlock()
			if !f.silent {
				publish(f.initial)
			}
			return nil
		},
		Stop: func() {
			f.stops.Add(1)
			f.mu.Lock()
			f.publish = nil
			f.mu.Unlock()
		},
	}
}

func (f *fakeTopic) pub(v any) {
	f.mu.Lock()
	p := f.publish
	f.mu.Unlock()
	if p != nil {
		p(v)
	}
}

func startServerWith(t *testing.T, h Handlers, tune func(*Server)) string {
	t.Helper()
	sock := filepath.Join(sockpath.Dir(t, "ipc"), "ipc.v1.sock")
	ctx, cancel := context.WithCancel(context.Background())
	srv := NewServer(sock, h)
	if tune != nil {
		tune(srv)
	}
	go func() { _ = srv.Serve(ctx) }()
	t.Cleanup(func() { cancel(); _ = srv.Close() })
	waitSock(t, sock)
	return sock
}

// client is a raw test connection that reads one line at a time.
type client struct {
	t  *testing.T
	nc net.Conn
	rd *bufio.Reader
}

func dial(t *testing.T, sock string) *client {
	t.Helper()
	nc, err := net.Dial("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = nc.Close() })
	return &client{t: t, nc: nc, rd: bufio.NewReaderSize(nc, 1<<16)}
}

func (c *client) send(method string, params any) {
	c.t.Helper()
	b, _ := json.Marshal(map[string]any{"id": 1, "method": method, "params": params})
	if _, err := c.nc.Write(append(b, '\n')); err != nil {
		c.t.Fatal(err)
	}
}

// next reads one line, or reports false when none arrives within d.
func (c *client) next(d time.Duration) (map[string]any, bool) {
	c.t.Helper()
	_ = c.nc.SetReadDeadline(time.Now().Add(d))
	line, err := c.rd.ReadBytes('\n')
	if err != nil {
		return nil, false
	}
	return decodeReply(c.t, line), true
}

func (c *client) must(d time.Duration) map[string]any {
	c.t.Helper()
	m, ok := c.next(d)
	if !ok {
		c.t.Fatal("no line arrived")
	}
	return m
}

// reply skips event lines and returns the next reply.
func (c *client) reply() map[string]any {
	c.t.Helper()
	for {
		m := c.must(2 * time.Second)
		if _, isEvent := m["event"]; !isEvent {
			return m
		}
	}
}

func (c *client) subscribe(topics ...string) map[string]any {
	c.t.Helper()
	c.send("ipc.subscribe", map[string]any{"topics": topics})
	return c.reply()
}

func seqOf(m map[string]any) uint64 {
	f, _ := m["seq"].(float64)
	return uint64(f)
}

func TestSubscribeSnapshotThenState(t *testing.T) {
	topic := &fakeTopic{initial: "zero"}
	sock := startServerWith(t, Handlers{Topics: map[string]Provider{"fake": topic.provider()}}, nil)
	c := dial(t, sock)
	r := c.subscribe("fake")
	if r["ok"] != true || fmt.Sprint(r["result"]) != "map[topics:[fake]]" {
		t.Fatalf("subscribe reply = %v", r)
	}
	snap := c.must(time.Second)
	if snap["event"] != EventSnapshot || snap["topic"] != "fake" || snap["data"] != "zero" {
		t.Fatalf("first event = %v", snap)
	}
	n := seqOf(snap)
	for i, v := range []string{"one", "two"} {
		topic.pub(v)
		ev := c.must(time.Second)
		if ev["event"] != EventState || ev["data"] != v || seqOf(ev) != n+uint64(i)+1 {
			t.Fatalf("state %d = %v", i, ev)
		}
	}
	c.send("ipc.unsubscribe", map[string]any{"topics": []string{"fake"}})
	if r := c.must(time.Second); r["ok"] != true {
		t.Fatalf("unsubscribe reply = %v", r)
	}
	topic.pub("three")
	if ev, ok := c.next(150 * time.Millisecond); ok {
		t.Fatalf("event after unsubscribe: %v", ev)
	}
}

func TestSubscribeIsAtomicWithPublish(t *testing.T) {
	topic := &fakeTopic{initial: 0}
	sock := startServerWith(t, Handlers{Topics: map[string]Provider{"fake": topic.provider()}}, nil)
	// An anchor keeps the provider running for the whole race.
	anchor := dial(t, sock)
	anchor.subscribe("fake")
	base := seqOf(anchor.must(time.Second))

	const publishers, each, subscribers = 8, 500, 20
	final := base + publishers*each
	var wg sync.WaitGroup
	for p := 0; p < publishers; p++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < each; i++ {
				topic.pub(i)
			}
		}()
	}
	errs := make(chan error, subscribers)
	for s := 0; s < subscribers; s++ {
		go func() {
			nc, err := net.Dial("unix", sock)
			if err != nil {
				errs <- err
				return
			}
			defer nc.Close()
			_ = nc.SetDeadline(time.Now().Add(5 * time.Second))
			_, _ = nc.Write([]byte(`{"id":1,"method":"ipc.subscribe","params":{"topics":["fake"]}}` + "\n"))
			sc := bufio.NewScanner(nc)
			var last uint64
			for sc.Scan() {
				var ev Event
				if err := json.Unmarshal(sc.Bytes(), &ev); err != nil || ev.Event == "" {
					continue
				}
				if ev.Seq <= last {
					errs <- fmt.Errorf("seq went %d then %d", last, ev.Seq)
					return
				}
				last = ev.Seq
				if last == final {
					errs <- nil
					return
				}
			}
			errs <- fmt.Errorf("stream ended at seq %d, want %d", last, final)
		}()
	}
	wg.Wait()
	for s := 0; s < subscribers; s++ {
		if err := <-errs; err != nil {
			t.Fatal(err)
		}
	}
}

func TestProvidersAreRefcounted(t *testing.T) {
	topic := &fakeTopic{initial: 0}
	silent := &fakeTopic{silent: true}
	sock := startServerWith(t, Handlers{Topics: map[string]Provider{
		"fake": topic.provider(), "silent": silent.provider(),
	}}, nil)
	a, b := dial(t, sock), dial(t, sock)
	a.subscribe("fake")
	b.subscribe("fake")
	if got := topic.starts.Load(); got != 1 {
		t.Fatalf("starts after two subscribers = %d, want 1", got)
	}
	a.send("ipc.unsubscribe", map[string]any{"topics": []string{"fake"}})
	a.reply()
	if got := topic.stops.Load(); got != 0 {
		t.Fatalf("stopped with a subscriber left: %d", got)
	}
	_ = b.nc.Close()
	waitFor(t, "stop after the last disconnect", func() bool { return topic.stops.Load() == 1 })
	a.subscribe("fake")
	if got := topic.starts.Load(); got != 2 {
		t.Fatalf("starts after a fresh subscriber = %d, want 2", got)
	}

	// A provider that publishes nothing on Start fails the subscribe and is
	// stopped again.
	c := dial(t, sock)
	if r := c.subscribe("silent"); r["ok"] == true {
		t.Fatalf("silent provider subscribe = %v", r)
	}
	if silent.starts.Load() != 1 || silent.stops.Load() != 1 {
		t.Fatalf("silent starts/stops = %d/%d", silent.starts.Load(), silent.stops.Load())
	}
	// The failed subscribe left a command connection.
	c.send("ipc.capabilities", nil)
	if r := c.reply(); r["ok"] != true {
		t.Fatalf("command after a failed subscribe = %v", r)
	}
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestEventConnectionRefusesCommands(t *testing.T) {
	topic := &fakeTopic{initial: 0}
	var panels atomic.Int32
	sock := startServerWith(t, Handlers{
		Topics: map[string]Provider{"fake": topic.provider()},
		Panel:  func(string, string, string) error { panels.Add(1); return nil },
	}, nil)
	c := dial(t, sock)
	c.subscribe("fake")
	for _, method := range []string{"panel.open", "ipc.capabilities"} {
		c.send(method, map[string]any{"panel": "clock"})
		if r := c.reply(); r["code"] != CodeUnsupported {
			t.Fatalf("%s on an event connection = %v", method, r)
		}
	}
	if panels.Load() != 0 {
		t.Fatal("a command ran on an event connection")
	}
}

func TestConsumerAndTopicLimits(t *testing.T) {
	providers := map[string]Provider{}
	for i := 0; i <= maxTopicsPerConn; i++ {
		providers[fmt.Sprintf("t%02d", i)] = (&fakeTopic{initial: i}).provider()
	}
	sock := startServerWith(t, Handlers{Topics: providers, Status: func() map[string]any { return nil }}, nil)

	var consumers []*client
	for i := 0; i < maxConsumers; i++ {
		c := dial(t, sock)
		if r := c.subscribe("t00"); r["ok"] != true {
			t.Fatalf("consumer %d refused: %v", i+1, r)
		}
		consumers = append(consumers, c)
	}
	extra := dial(t, sock)
	if r := extra.subscribe("t00"); r["code"] != CodeLimitExceeded {
		t.Fatalf("consumer %d = %v, want limit_exceeded", maxConsumers+1, r)
	}
	extra.send("status", nil)
	if r := extra.reply(); r["ok"] != true {
		t.Fatalf("refused consumer lost its command role: %v", r)
	}
	_ = consumers[0].nc.Close()
	waitFor(t, "a released consumer slot", func() bool {
		c := dial(t, sock)
		return c.subscribe("t00")["ok"] == true
	})

	names := make([]string, 0, maxTopicsPerConn)
	for i := 1; i <= maxTopicsPerConn; i++ {
		names = append(names, fmt.Sprintf("t%02d", i-1))
	}
	c := consumers[1]
	if r := c.subscribe(names...); r["ok"] != true {
		t.Fatalf("%d topics refused: %v", maxTopicsPerConn, r)
	}
	if r := c.subscribe(fmt.Sprintf("t%02d", maxTopicsPerConn)); r["code"] != CodeLimitExceeded {
		t.Fatalf("topic %d = %v, want limit_exceeded", maxTopicsPerConn+1, r)
	}
	if r := c.subscribe("nope"); r["code"] != CodeUnsupported {
		t.Fatalf("unknown topic = %v, want unsupported", r)
	}
}

// fill is a state large enough that a few undelivered ones fill a socket
// buffer, so the writer really blocks.
type fill struct {
	N   int    `json:"n"`
	Pad string `json:"pad"`
}

func TestSlowConsumerDoesNotBlockCommands(t *testing.T) {
	topic := &fakeTopic{initial: fill{}}
	sock := startServerWith(t, Handlers{
		Topics: map[string]Provider{"fake": topic.provider()},
		Panel:  func(string, string, string) error { return nil },
	}, nil)
	stalled := dial(t, sock)
	stalled.subscribe("fake")
	// The stalled consumer never reads again.

	pad := strings.Repeat("x", 32<<10)
	start := time.Now()
	for i := 1; i <= 10000; i++ {
		topic.pub(fill{N: i, Pad: pad})
	}
	if d := time.Since(start); d > time.Second {
		t.Fatalf("10000 publishes took %v with a stalled consumer", d)
	}
	start = time.Now()
	out, err := Call(context.Background(), sock, "panel.toggle", map[string]string{"panel": "clock"})
	if d := time.Since(start); err != nil || d > 250*time.Millisecond {
		t.Fatalf("panel.toggle beside a stalled consumer: %v after %v (%s)", err, d, out)
	}
}

func TestSlowConsumerGetsResync(t *testing.T) {
	topic := &fakeTopic{initial: fill{}}
	sock := startServerWith(t, Handlers{Topics: map[string]Provider{"fake": topic.provider()}}, nil)
	c := dial(t, sock)
	c.subscribe("fake")
	pad := strings.Repeat("x", 32<<10)
	const total = 10000
	for i := 1; i <= total; i++ {
		topic.pub(fill{N: i, Pad: pad})
	}
	// Now read. States already in the socket arrive first; the one carrying
	// the last value must be a resync, and most states were never sent.
	var events int
	var last uint64
	for {
		m := c.must(2 * time.Second)
		if _, ok := m["event"]; !ok {
			continue
		}
		events++
		if seqOf(m) <= last {
			t.Fatalf("seq went %d then %d", last, seqOf(m))
		}
		last = seqOf(m)
		n := m["data"].(map[string]any)["n"].(float64)
		if int(n) == total {
			if m["event"] != EventResync {
				t.Fatalf("final state arrived as %v, want resync", m["event"])
			}
			break
		}
	}
	if events > 100 {
		t.Fatalf("%d events for %d states: the slow consumer was not coalesced", events, total)
	}
}

func TestOversizeStateEndsTopic(t *testing.T) {
	big := &fakeTopic{initial: ""}
	small := &fakeTopic{initial: 0}
	sock := startServerWith(t, Handlers{Topics: map[string]Provider{
		"big": big.provider(), "small": small.provider(),
	}}, nil)
	c := dial(t, sock)
	c.subscribe("big", "small")
	c.must(time.Second)
	c.must(time.Second)
	big.pub(strings.Repeat("x", pendingBytes))
	ev := c.must(2 * time.Second)
	if ev["event"] != EventError || ev["topic"] != "big" || ev["code"] != CodeLimitExceeded {
		t.Fatalf("oversize state = %v", ev)
	}
	waitFor(t, "the dropped topic's provider to stop", func() bool { return big.stops.Load() == 1 })
	small.pub(1)
	if ev := c.must(time.Second); ev["topic"] != "small" || ev["event"] != EventState {
		t.Fatalf("connection after a dropped topic = %v", ev)
	}
}

func TestStalledWriterIsClosed(t *testing.T) {
	topic := &fakeTopic{initial: fill{}}
	sock := startServerWith(t, Handlers{Topics: map[string]Provider{"fake": topic.provider()}},
		func(s *Server) { s.writeTimeout = 50 * time.Millisecond })
	c := dial(t, sock)
	c.send("ipc.subscribe", map[string]any{"topics": []string{"fake"}})
	pad := strings.Repeat("x", 64<<10)
	deadline := time.Now().Add(3 * time.Second)
	for i := 1; topic.stops.Load() == 0; i++ {
		if time.Now().After(deadline) {
			t.Fatal("a stalled writer kept its connection and its topic")
		}
		topic.pub(fill{N: i, Pad: pad})
		time.Sleep(time.Millisecond)
	}
	// The server closed the socket: draining it ends in EOF, not a timeout.
	_ = c.nc.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, err := c.rd.WriteTo(discard{})
	var ne net.Error
	if errors.As(err, &ne) && ne.Timeout() {
		t.Fatal("connection still open after the stalled write")
	}
}

type discard struct{}

func (discard) Write(p []byte) (int, error) { return len(p), nil }
