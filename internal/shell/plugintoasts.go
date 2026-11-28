package shell

import (
	"sync"

	"github.com/Nomadcxx/sysc-notify/protocol"
	"github.com/Nomadcxx/sysc-shell/internal/notifyclient"
	"github.com/Nomadcxx/sysc-shell/internal/plugin"
	v1 "github.com/Nomadcxx/sysc-shell/plugin/v1"
)

// pluginToastOwner hands a click on a plugin toast back to the plugin session
// that posted it. The plugin host builds one per notify call; it decides
// whether that session is still there to receive it.
type pluginToastOwner func(v1.NotifyActionEvent)

// maxPluginToasts bounds the notification ids remembered for routing. A toast
// past it still shows and closes; only its clicks stop reaching the plugin.
const maxPluginToasts = 256

// pluginToasts remembers which plugin posted each plugin toast, so a click on
// one reaches that plugin and no other. A notify call learns its request id
// at once and the notification id only from the service's reply, so an owner
// waits in pending until that reply arrives.
type pluginToasts struct {
	mu      sync.Mutex
	pending map[uint64]pluginToastOwner // client request id → owner
	owned   map[uint32]pluginToastOwner // notification id → owner
	order   []uint32                    // owned ids, oldest first
}

// publish runs send and remembers owner under the request id it returns. The
// lock spans send so the reply, applied on another goroutine, cannot arrive
// before the request id is recorded.
func (t *pluginToasts) publish(owner pluginToastOwner, send func() (uint64, error)) (uint64, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	requestID, err := send()
	if err != nil || owner == nil {
		return requestID, err
	}
	if t.pending == nil {
		t.pending = make(map[uint64]pluginToastOwner)
	}
	t.pending[requestID] = owner
	return requestID, nil
}

// apply follows the service: a reply names a pending toast's notification id,
// a close retires one, a snapshot keeps only the ids still active, and a
// disconnect drops the replies that will never come.
func (t *pluginToasts) apply(m notifyclient.Message) {
	t.mu.Lock()
	defer t.mu.Unlock()
	switch m.Kind {
	case notifyclient.KindReply:
		owner, ok := t.pending[m.RequestID]
		if !ok {
			return
		}
		delete(t.pending, m.RequestID)
		if m.Reply.OK && m.Reply.ID != 0 {
			t.ownLocked(m.Reply.ID, owner)
		}
	case notifyclient.KindDelta:
		if m.Delta.Kind == protocol.DeltaClosed {
			t.forgetLocked(func(id uint32) bool { return id == m.Delta.ID })
		}
	case notifyclient.KindSnapshot:
		active := make(map[uint32]bool, len(m.Snapshot.Active))
		for _, n := range m.Snapshot.Active {
			active[n.ID] = true
		}
		t.forgetLocked(func(id uint32) bool { return !active[id] })
	case notifyclient.KindDisconnected:
		clear(t.pending)
	}
}

func (t *pluginToasts) ownLocked(id uint32, owner pluginToastOwner) {
	if t.owned == nil {
		t.owned = make(map[uint32]pluginToastOwner)
	}
	if _, ok := t.owned[id]; !ok {
		t.order = append(t.order, id)
	}
	t.owned[id] = owner
	for len(t.order) > maxPluginToasts {
		delete(t.owned, t.order[0])
		t.order = t.order[1:]
	}
}

func (t *pluginToasts) forgetLocked(drop func(uint32) bool) {
	kept := t.order[:0]
	for _, id := range t.order {
		if drop(id) {
			delete(t.owned, id)
		} else {
			kept = append(kept, id)
		}
	}
	t.order = kept
}

// route hands a click on notification id to the plugin that posted it, if
// one did. The owner runs outside the lock: it takes the plugin host's.
func (t *pluginToasts) route(id uint32, key string) {
	t.mu.Lock()
	owner := t.owned[id]
	t.mu.Unlock()
	if owner != nil {
		owner(v1.NotifyActionEvent{ID: id, Key: key})
	}
}

// toastOwner is the delivery for clicks on a toast that plugin id posts now.
// It reaches the same runtime session or nothing: a plugin stopped, disabled
// or restarted since then drops the click, and so does one that negotiated a
// minor without notify.action.
func (h *pluginHost) toastOwner(id string, rt *plugin.Runtime) pluginToastOwner {
	h.mu.Lock()
	slot := h.slots[id]
	var session uint64
	if slot != nil && slot.rt == rt {
		session = slot.session
	}
	h.mu.Unlock()
	if session == 0 {
		return nil
	}
	return func(ev v1.NotifyActionEvent) {
		h.mu.Lock()
		current := h.slots[id] == slot && slot.session == session
		h.mu.Unlock()
		if !current {
			return
		}
		if p := rt.Protocol(); p.Major != v1.ProtocolMajor || p.Minor < v1.NotifyActionMinor {
			return
		}
		_ = rt.Send(&ev)
	}
}
