package plugin

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	v1 "github.com/Nomadcxx/sysc-shell/plugin/v1"
)

// StateStore is the namespaced persistent store a plugin may read and write.
// *Store is the production implementation; tests substitute a stalling one.
type StateStore interface {
	Get(key string) (json.RawMessage, bool)
	Keys() []string
	Set(ctx context.Context, key string, value json.RawMessage) error
}

// CallEnv is the host surface one plugin is allowed to use.
type CallEnv struct {
	PluginID       string
	Granted        []Capability
	DeclaredPanels []Panel
	Store          StateStore
	OpenPanel      func(context.Context, v1.PanelParams) (v1.PanelResult, error)
	ClosePanel     func(context.Context, v1.PanelParams) error
	Notify         func(context.Context, v1.NotifyParams) (v1.NotifyResult, error)
	OutputContext  func(context.Context, v1.OutputContextParams) (v1.OutputContextResult, error)
	MaxPending     int
	CallTimeout    time.Duration
}

func (e CallEnv) maxPending() int {
	if e.MaxPending > 0 {
		return e.MaxPending
	}
	return v1.DefaultLimits.PendingCalls
}

func (e CallEnv) allows(c Capability) bool {
	for _, have := range e.Granted {
		if have == c {
			return true
		}
	}
	return false
}

func (e CallEnv) hasPanel(id string) bool {
	for _, p := range e.DeclaredPanels {
		if p.ID == id {
			return true
		}
	}
	return false
}

// Dispatcher answers host.call messages for one plugin.
//
// Capabilities are checked here, not in the store or the notification client:
// a plugin that was not granted a class of call must not reach the service,
// even if the service would have accepted it.
type Dispatcher struct {
	env     CallEnv
	mu      sync.Mutex
	pending int
}

// NewDispatcher returns a dispatcher for one plugin's host calls.
func NewDispatcher(env CallEnv) *Dispatcher {
	return &Dispatcher{env: env}
}

// Handle answers one call. It always produces a reply so the plugin can pair
// it with the id it sent; a missing service or a denied grant is an error
// reply, not a dropped line.
func (d *Dispatcher) Handle(ctx context.Context, call *v1.HostCall) v1.HostReply {
	if call == nil {
		return failReply("", "empty call")
	}
	if d.env.CallTimeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, d.env.CallTimeout)
		defer cancel()
	}
	if err := ctx.Err(); err != nil {
		return failReply(call.ID, err.Error())
	}
	if !d.begin() {
		return failReply(call.ID, "too many pending calls")
	}
	defer d.end()

	done := make(chan v1.HostReply, 1)
	go func() { done <- d.dispatch(ctx, call) }()
	select {
	case reply := <-done:
		return reply
	case <-ctx.Done():
		return failReply(call.ID, ctx.Err().Error())
	}
}

func (d *Dispatcher) begin() bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.pending >= d.env.maxPending() {
		return false
	}
	d.pending++
	return true
}

func (d *Dispatcher) end() {
	d.mu.Lock()
	d.pending--
	d.mu.Unlock()
}

func (d *Dispatcher) dispatch(ctx context.Context, call *v1.HostCall) v1.HostReply {
	if err := ctx.Err(); err != nil {
		return failReply(call.ID, err.Error())
	}
	switch call.Call {
	case v1.CallStateGet, v1.CallStateSet, v1.CallStateList:
		if !d.env.allows(CapState) {
			return failReply(call.ID, "capability state is not granted")
		}
		if d.env.Store == nil {
			return failReply(call.ID, "no state store")
		}
		return d.state(ctx, call)
	case v1.CallPanelOpen, v1.CallPanelClose:
		if !d.env.allows(CapPanels) {
			return failReply(call.ID, "capability panels is not granted")
		}
		return d.panel(ctx, call)
	case v1.CallNotify:
		if !d.env.allows(CapNotifications) {
			return failReply(call.ID, "capability notifications is not granted")
		}
		return d.notify(ctx, call)
	case v1.CallOutputContext:
		return d.output(ctx, call)
	default:
		return failReply(call.ID, fmt.Sprintf("unknown call %q", call.Call))
	}
}

func (d *Dispatcher) state(ctx context.Context, call *v1.HostCall) v1.HostReply {
	switch call.Call {
	case v1.CallStateGet:
		var p v1.StateGetParams
		if err := decodeParams(call.Params, &p); err != nil {
			return failReply(call.ID, err.Error())
		}
		value, found := d.env.Store.Get(p.Key)
		return okReply(call.ID, v1.StateGetResult{Found: found, Value: value})
	case v1.CallStateSet:
		var p v1.StateSetParams
		if err := decodeParams(call.Params, &p); err != nil {
			return failReply(call.ID, err.Error())
		}
		if err := d.env.Store.Set(ctx, p.Key, p.Value); err != nil {
			return failReply(call.ID, err.Error())
		}
		return okReply(call.ID, nil)
	case v1.CallStateList:
		return okReply(call.ID, v1.StateListResult{Keys: d.env.Store.Keys()})
	}
	return failReply(call.ID, "unknown state call")
}

func (d *Dispatcher) panel(ctx context.Context, call *v1.HostCall) v1.HostReply {
	var p v1.PanelParams
	if err := decodeParams(call.Params, &p); err != nil {
		return failReply(call.ID, err.Error())
	}
	if p.Entry == "" || !d.env.hasPanel(p.Entry) {
		return failReply(call.ID, fmt.Sprintf("panel %q is not declared", p.Entry))
	}
	switch call.Call {
	case v1.CallPanelOpen:
		if d.env.OpenPanel == nil {
			return failReply(call.ID, "panel open is not available")
		}
		result, err := d.env.OpenPanel(ctx, p)
		if err != nil {
			return failReply(call.ID, err.Error())
		}
		return okReply(call.ID, result)
	case v1.CallPanelClose:
		if d.env.ClosePanel == nil {
			return failReply(call.ID, "panel close is not available")
		}
		if err := d.env.ClosePanel(ctx, p); err != nil {
			return failReply(call.ID, err.Error())
		}
		return okReply(call.ID, nil)
	}
	return failReply(call.ID, "unknown panel call")
}

func (d *Dispatcher) notify(ctx context.Context, call *v1.HostCall) v1.HostReply {
	var p v1.NotifyParams
	if err := decodeParams(call.Params, &p); err != nil {
		return failReply(call.ID, err.Error())
	}
	if err := boundNotify(&p); err != nil {
		return failReply(call.ID, err.Error())
	}
	if d.env.Notify == nil {
		return failReply(call.ID, "notifications are not available")
	}
	result, err := d.env.Notify(ctx, p)
	if err != nil {
		return failReply(call.ID, err.Error())
	}
	return okReply(call.ID, result)
}

func (d *Dispatcher) output(ctx context.Context, call *v1.HostCall) v1.HostReply {
	var p v1.OutputContextParams
	if err := decodeParams(call.Params, &p); err != nil {
		return failReply(call.ID, err.Error())
	}
	if d.env.OutputContext == nil {
		return failReply(call.ID, "output context is not available")
	}
	result, err := d.env.OutputContext(ctx, p)
	if err != nil {
		return failReply(call.ID, err.Error())
	}
	return okReply(call.ID, result)
}

const (
	maxNotifyText    = 16 << 10
	maxNotifyActions = 6
)

func boundNotify(p *v1.NotifyParams) error {
	if len(p.Summary) > maxNotifyText {
		return fmt.Errorf("notify summary exceeds %d bytes", maxNotifyText)
	}
	if len(p.Body) > maxNotifyText {
		return fmt.Errorf("notify body exceeds %d bytes", maxNotifyText)
	}
	if len(p.Actions) > maxNotifyActions {
		return fmt.Errorf("notify actions exceed %d", maxNotifyActions)
	}
	for _, a := range p.Actions {
		if len(a.Key)+len(a.Label) > maxNotifyText {
			return fmt.Errorf("notify action exceeds %d bytes", maxNotifyText)
		}
	}
	if p.TimeoutMS < 0 {
		return fmt.Errorf("notify timeout %d is negative", p.TimeoutMS)
	}
	return nil
}

func decodeParams(raw json.RawMessage, dest any) error {
	if len(raw) == 0 {
		return nil
	}
	if err := json.Unmarshal(raw, dest); err != nil {
		return fmt.Errorf("params: %w", err)
	}
	return nil
}

func okReply(id string, result any) v1.HostReply {
	reply := v1.HostReply{ID: id, OK: true}
	if result == nil {
		return reply
	}
	data, err := json.Marshal(result)
	if err != nil {
		return failReply(id, err.Error())
	}
	reply.Result = data
	return reply
}

func failReply(id, err string) v1.HostReply {
	return v1.HostReply{ID: id, Error: err}
}
