// Idle binding: the policy in internal/services decides which timeouts the
// compositor should watch; this file is the only place that holds
// ext_idle_notifier_v1 objects. Every request is applied on the Wayland
// goroutine, so notification create/destroy order matches arm/disarm order.
package wayland

import (
	"github.com/Nomadcxx/sysc-wayland/idle"
)

// IdleRequest arms or disarms one notification object. ID is an opaque policy
// id (the services package owns its meaning); TimeoutMS of zero destroys the
// object if one exists.
type IdleRequest struct {
	ID        uint64
	TimeoutMS uint32
}

// IdleEvent reports a compositor verdict for the object with the given ID.
type IdleEvent struct {
	ID    uint64
	Idled bool
}

// armIdle replaces the notification for id. It runs on the Wayland goroutine.
func (o *owner) armIdle(id uint64, timeoutMS uint32) {
	o.destroyIdle(id)
	if timeoutMS == 0 || o.idleNotifier == nil || o.seat == nil {
		return
	}
	n, err := o.idleNotifier.GetIdleNotification(timeoutMS, o.seat)
	if err != nil {
		return // a seat or notifier that vanished mid-flight is not retryable here
	}
	held := n
	n.SetIdledHandler(func(idle.ExtIdleNotificationV1IdledEvent) {
		o.emitIdle(IdleEvent{ID: id, Idled: true})
	})
	n.SetResumedHandler(func(idle.ExtIdleNotificationV1ResumedEvent) {
		o.emitIdle(IdleEvent{ID: id, Idled: false})
	})
	if o.idleNotes == nil {
		o.idleNotes = make(map[uint64]*idle.ExtIdleNotificationV1)
	}
	o.idleNotes[id] = held
}

func (o *owner) destroyIdle(id uint64) {
	if n, ok := o.idleNotes[id]; ok {
		delete(o.idleNotes, id)
		_ = n.Destroy()
	}
}

// emitIdle hands an event to the policy loop. It must never block: this runs
// on the Wayland dispatch goroutine, and a service that is momentarily busy
// must not stall the loop that would drain its arm requests. Anything that
// cannot be sent is queued and a wake schedules the retry.
func (o *owner) emitIdle(ev IdleEvent) {
	if o.cb.IdleEvents == nil {
		return
	}
	select {
	case o.cb.IdleEvents <- ev:
	default:
		o.idleEvQueue = append(o.idleEvQueue, ev)
		if o.wake != nil {
			o.wake.signal()
		}
	}
}

// deliverIdleEvents flushes the queue. Runs on the Wayland goroutine after
// arm requests are applied, so a service blocked on a full request queue has
// just been unstalled.
func (o *owner) deliverIdleEvents() {
	for len(o.idleEvQueue) > 0 && o.cb.IdleEvents != nil {
		select {
		case o.cb.IdleEvents <- o.idleEvQueue[0]:
			o.idleEvQueue = o.idleEvQueue[1:]
		default:
			if o.wake != nil {
				o.wake.signal()
			}
			return
		}
	}
}

func (o *owner) destroyIdleAll() {
	for id := range o.idleNotes {
		o.destroyIdle(id)
	}
}
