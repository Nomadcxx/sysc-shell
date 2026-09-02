package plugin

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	v1 "github.com/Nomadcxx/sysc-shell/plugin/v1"
)

func TestHostCallStateIsNamespacedToThePlugin(t *testing.T) {
	t.Parallel()
	store := mustStore(t)
	d := NewDispatcher(CallEnv{
		PluginID: "org.sysc.timer",
		Granted:  []Capability{CapState},
		Store:    store,
	})

	set := d.Handle(context.Background(), &v1.HostCall{
		ID: "1", Call: v1.CallStateSet,
		Params: jsonOf(t, v1.StateSetParams{Key: "deadline", Value: json.RawMessage(`"soon"`)}),
	})
	if !set.OK {
		t.Fatalf("set: %+v", set)
	}

	get := d.Handle(context.Background(), &v1.HostCall{
		ID: "2", Call: v1.CallStateGet,
		Params: jsonOf(t, v1.StateGetParams{Key: "deadline"}),
	})
	if !get.OK {
		t.Fatalf("get: %+v", get)
	}
	var result v1.StateGetResult
	if err := json.Unmarshal(get.Result, &result); err != nil || !result.Found {
		t.Fatalf("get result = %s err=%v", get.Result, err)
	}

	list := d.Handle(context.Background(), &v1.HostCall{ID: "3", Call: v1.CallStateList})
	if !list.OK {
		t.Fatalf("list: %+v", list)
	}
	var keys v1.StateListResult
	if err := json.Unmarshal(list.Result, &keys); err != nil || len(keys.Keys) != 1 || keys.Keys[0] != "deadline" {
		t.Fatalf("keys = %+v err=%v", keys, err)
	}
}

func TestHostCallStateDeniedWithoutCapability(t *testing.T) {
	t.Parallel()
	d := NewDispatcher(CallEnv{PluginID: "org.sysc.timer", Store: mustStore(t)})
	reply := d.Handle(context.Background(), &v1.HostCall{
		ID: "1", Call: v1.CallStateGet,
		Params: jsonOf(t, v1.StateGetParams{Key: "deadline"}),
	})
	if reply.OK || reply.Error == "" {
		t.Fatalf("want a capability denial, got %+v", reply)
	}
}

func TestHostCallPanelOpenCloseUsesDeclaredEntry(t *testing.T) {
	t.Parallel()
	var opened, closed v1.PanelParams
	d := NewDispatcher(CallEnv{
		PluginID:       "org.sysc.timer",
		Granted:        []Capability{CapPanels},
		DeclaredPanels: []Panel{{ID: "panel", Width: 320, Height: 280}},
		OpenPanel: func(_ context.Context, p v1.PanelParams) (v1.PanelResult, error) {
			opened = p
			return v1.PanelResult{ViewID: "view-7"}, nil
		},
		ClosePanel: func(_ context.Context, p v1.PanelParams) error {
			closed = p
			return nil
		},
	})

	open := d.Handle(context.Background(), &v1.HostCall{
		ID: "1", Call: v1.CallPanelOpen,
		Params: jsonOf(t, v1.PanelParams{Entry: "panel", Output: "DP-1", Instance: "timer-1"}),
	})
	if !open.OK {
		t.Fatalf("open: %+v", open)
	}
	if opened.Entry != "panel" || opened.Output != "DP-1" {
		t.Fatalf("opened = %+v", opened)
	}

	unknown := d.Handle(context.Background(), &v1.HostCall{
		ID: "2", Call: v1.CallPanelOpen,
		Params: jsonOf(t, v1.PanelParams{Entry: "secret"}),
	})
	if unknown.OK {
		t.Fatal("an undeclared panel opened")
	}

	closeReply := d.Handle(context.Background(), &v1.HostCall{
		ID: "3", Call: v1.CallPanelClose,
		Params: jsonOf(t, v1.PanelParams{Entry: "panel"}),
	})
	if !closeReply.OK || closed.Entry != "panel" {
		t.Fatalf("close = %+v closed=%+v", closeReply, closed)
	}
}

func TestHostCallNotifyHonoursTheGrant(t *testing.T) {
	t.Parallel()
	var got v1.NotifyParams
	granted := NewDispatcher(CallEnv{
		PluginID: "org.sysc.timer",
		Granted:  []Capability{CapNotifications},
		Notify: func(_ context.Context, p v1.NotifyParams) (v1.NotifyResult, error) {
			got = p
			return v1.NotifyResult{ID: 9}, nil
		},
	})
	ok := granted.Handle(context.Background(), &v1.HostCall{
		ID: "1", Call: v1.CallNotify,
		Params: jsonOf(t, v1.NotifyParams{Summary: "done", Body: "tea"}),
	})
	if !ok.OK || got.Summary != "done" {
		t.Fatalf("granted = %+v params=%+v", ok, got)
	}

	denied := NewDispatcher(CallEnv{PluginID: "org.sysc.timer"})
	no := denied.Handle(context.Background(), &v1.HostCall{
		ID: "2", Call: v1.CallNotify,
		Params: jsonOf(t, v1.NotifyParams{Summary: "done"}),
	})
	if no.OK {
		t.Fatal("notify succeeded without the capability")
	}
}

func TestHostCallRejectsAFullPendingQueue(t *testing.T) {
	t.Parallel()
	started := make(chan struct{})
	release := make(chan struct{})
	d := NewDispatcher(CallEnv{
		PluginID:   "org.sysc.timer",
		Granted:    []Capability{CapState},
		MaxPending: 1,
		Store:      &blockingStore{started: started, release: release},
	})

	var first atomic.Pointer[v1.HostReply]
	go func() {
		r := d.Handle(context.Background(), &v1.HostCall{
			ID: "slow", Call: v1.CallStateGet,
			Params: jsonOf(t, v1.StateGetParams{Key: "deadline"}),
		})
		first.Store(&r)
	}()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("the first call never started")
	}

	busy := d.Handle(context.Background(), &v1.HostCall{
		ID: "fast", Call: v1.CallStateGet,
		Params: jsonOf(t, v1.StateGetParams{Key: "deadline"}),
	})
	if busy.OK || busy.ID != "fast" {
		t.Fatalf("busy = %+v", busy)
	}
	close(release)
	waitReply(t, &first)
}

func TestHostCallDeadlineAndCancellation(t *testing.T) {
	t.Parallel()
	started := make(chan struct{})
	d := NewDispatcher(CallEnv{
		PluginID:    "org.sysc.timer",
		Granted:     []Capability{CapState},
		CallTimeout: 20 * time.Millisecond,
		Store:       &blockingStore{started: started, release: make(chan struct{})},
	})
	reply := d.Handle(context.Background(), &v1.HostCall{
		ID: "late", Call: v1.CallStateGet,
		Params: jsonOf(t, v1.StateGetParams{Key: "deadline"}),
	})
	if reply.OK {
		t.Fatalf("deadline produced %+v", reply)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	cancelled := NewDispatcher(CallEnv{
		PluginID: "org.sysc.timer",
		Granted:  []Capability{CapState},
		Store:    mustStore(t),
	}).Handle(ctx, &v1.HostCall{
		ID: "gone", Call: v1.CallStateGet,
		Params: jsonOf(t, v1.StateGetParams{Key: "deadline"}),
	})
	if cancelled.OK {
		t.Fatal("a cancelled context succeeded")
	}
}

func TestHostCallFailureReplyKeepsTheCallID(t *testing.T) {
	t.Parallel()
	d := NewDispatcher(CallEnv{
		PluginID: "org.sysc.timer",
		Granted:  []Capability{CapNotifications},
		Notify: func(context.Context, v1.NotifyParams) (v1.NotifyResult, error) {
			return v1.NotifyResult{}, errors.New("service missing")
		},
	})
	reply := d.Handle(context.Background(), &v1.HostCall{
		ID: "n1", Call: v1.CallNotify,
		Params: jsonOf(t, v1.NotifyParams{Summary: "x"}),
	})
	if reply.OK || reply.ID != "n1" || reply.Error == "" {
		t.Fatalf("failure reply = %+v", reply)
	}
}

func TestHostCallNotifyBoundsFields(t *testing.T) {
	t.Parallel()
	var got v1.NotifyParams
	d := NewDispatcher(CallEnv{
		PluginID: "org.sysc.timer",
		Granted:  []Capability{CapNotifications},
		Notify: func(_ context.Context, p v1.NotifyParams) (v1.NotifyResult, error) {
			got = p
			return v1.NotifyResult{ID: 1}, nil
		},
	})
	ok := d.Handle(context.Background(), &v1.HostCall{
		ID: "1", Call: v1.CallNotify,
		Params: jsonOf(t, v1.NotifyParams{
			Summary:   "saved",
			Body:      "clip.mp4",
			Actions:   []v1.NotifyAction{{Key: "open", Label: "Open"}},
			TimeoutMS: 4000,
		}),
	})
	if !ok.OK || got.TimeoutMS != 4000 || len(got.Actions) != 1 {
		t.Fatalf("bounded notify = %+v params=%+v", ok, got)
	}
	huge := d.Handle(context.Background(), &v1.HostCall{
		ID: "2", Call: v1.CallNotify,
		Params: jsonOf(t, v1.NotifyParams{Summary: string(make([]byte, maxNotifyText+1))}),
	})
	if huge.OK || huge.Error == "" {
		t.Fatalf("oversize summary = %+v", huge)
	}
	tooMany := d.Handle(context.Background(), &v1.HostCall{
		ID: "3", Call: v1.CallNotify,
		Params: jsonOf(t, v1.NotifyParams{Summary: "x", Actions: make([]v1.NotifyAction, maxNotifyActions+1)}),
	})
	if tooMany.OK {
		t.Fatal("too many actions were accepted")
	}
}

func TestHostCallOutputContext(t *testing.T) {
	t.Parallel()
	d := NewDispatcher(CallEnv{
		PluginID: "org.sysc.timer",
		OutputContext: func(_ context.Context, p v1.OutputContextParams) (v1.OutputContextResult, error) {
			if p.Output == "HDMI-1" {
				return v1.OutputContextResult{}, errors.New("output HDMI-1 is not declared")
			}
			if p.Generation == 99 {
				return v1.OutputContextResult{}, errors.New("output DP-1 generation is stale")
			}
			if p.Output == "" {
				return v1.OutputContextResult{Output: "HDMI-1", Generation: 2}, nil
			}
			return v1.OutputContextResult{Output: p.Output, Generation: 1}, nil
		},
	})
	focused := d.Handle(context.Background(), &v1.HostCall{ID: "1", Call: v1.CallOutputContext})
	if !focused.OK {
		t.Fatalf("focused = %+v", focused)
	}
	var result v1.OutputContextResult
	if err := json.Unmarshal(focused.Result, &result); err != nil || result.Output != "HDMI-1" {
		t.Fatalf("focused result = %s err=%v", focused.Result, err)
	}
	stale := d.Handle(context.Background(), &v1.HostCall{
		ID: "2", Call: v1.CallOutputContext,
		Params: jsonOf(t, v1.OutputContextParams{Output: "DP-1", Generation: 99}),
	})
	if stale.OK || stale.Error == "" || !strings.Contains(stale.Error, "DP-1") {
		t.Fatalf("stale = %+v", stale)
	}
	unknown := d.Handle(context.Background(), &v1.HostCall{
		ID: "3", Call: v1.CallOutputContext,
		Params: jsonOf(t, v1.OutputContextParams{Output: "HDMI-1"}),
	})
	if unknown.OK || !strings.Contains(unknown.Error, "HDMI-1") {
		t.Fatalf("undeclared = %+v", unknown)
	}
}

func mustStore(t *testing.T) *Store {
	t.Helper()
	s, err := OpenStore(t.TempDir(), "org.sysc.timer")
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func waitReply(t *testing.T, p *atomic.Pointer[v1.HostReply]) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if p.Load() != nil {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("the blocking call never returned")
}

// blockingStore stalls Get until release is closed, so the pending-call
// ceiling can be proven without a fake clock.
type blockingStore struct {
	started chan struct{}
	release chan struct{}
	once    sync.Once
}

func (b *blockingStore) Get(string) (json.RawMessage, bool) {
	b.once.Do(func() { close(b.started) })
	<-b.release
	return nil, false
}
func (*blockingStore) Keys() []string { return nil }
func (*blockingStore) Set(context.Context, string, json.RawMessage) error {
	return errors.New("unused")
}
