package shell

import (
	"errors"
	"fmt"
	"math"
	"slices"
	"sync"

	metrics "github.com/Nomadcxx/sysc-metrics"
	"github.com/Nomadcxx/sysc-notify/protocol"
	"github.com/Nomadcxx/sysc-shell/internal/config"
	"github.com/Nomadcxx/sysc-shell/internal/notifyclient"
	"github.com/Nomadcxx/sysc-shell/internal/services"
)

const (
	batteryWarningHysteresis = 5
	maxEarlyProducerReplies  = 8
)

type batteryProducerOperation struct {
	publish bool
	ready   bool
	request uint64
}

type batteryProducerAction struct {
	operation *batteryProducerOperation
	command   protocol.Command
}

// batteryWarning owns the producer's desired state. Service-owned notification
// state remains in notifyState; this reducer only decides which keyed command
// is needed to make that state agree with the latest valid battery sample.
type batteryWarning struct {
	mu sync.Mutex

	generation uint64
	connected  bool
	qualified  bool
	reconcile  bool

	valid      bool
	desired    bool
	charge     int32
	liveKnown  bool
	live       bool
	pending    *batteryProducerOperation
	earlyReply map[uint64]protocol.Reply
}

func newBatteryWarning() *batteryWarning {
	return &batteryWarning{earlyReply: make(map[uint64]protocol.Reply)}
}

func (b *batteryWarning) observe(snapshot services.Snapshot, threshold int) *batteryProducerAction {
	b.mu.Lock()
	defer b.mu.Unlock()
	if threshold <= 0 || snapshot.Battery == nil {
		b.pending = nil
		b.earlyReply = make(map[uint64]protocol.Reply)
		return nil
	}
	desired, ok := batteryWarningDesired(snapshot.Battery, threshold, b.desired)
	if !ok {
		b.pending = nil
		b.earlyReply = make(map[uint64]protocol.Reply)
		return nil
	}
	b.valid = true
	b.desired = desired
	b.charge = batteryPercent(snapshot.Battery.Charge)
	return b.startLocked()
}

func (b *batteryWarning) snapshot(generation uint64, capabilities []string) *batteryProducerAction {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.connected && b.generation != generation {
		b.reconcile = b.valid
	}
	b.generation = generation
	b.connected = true
	b.qualified = slices.Contains(capabilities, protocol.CapabilityBatteryProducer)
	b.pending = nil
	b.earlyReply = make(map[uint64]protocol.Reply)
	b.liveKnown = false
	if !b.qualified {
		return nil
	}
	return b.startLocked()
}

func (b *batteryWarning) disconnect(generation uint64) *batteryProducerAction {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.connected && b.generation != generation {
		return nil
	}
	b.connected = false
	b.qualified = false
	b.liveKnown = false
	b.reconcile = b.valid
	b.pending = nil
	b.earlyReply = make(map[uint64]protocol.Reply)
	return nil
}

func (b *batteryWarning) reply(generation, requestID uint64, reply protocol.Reply) *batteryProducerAction {
	b.mu.Lock()
	defer b.mu.Unlock()
	if !b.connected || b.generation != generation || b.pending == nil {
		return nil
	}
	if !b.pending.ready {
		if len(b.earlyReply) < maxEarlyProducerReplies {
			b.earlyReply[requestID] = reply
		}
		return nil
	}
	if b.pending.request != requestID {
		return nil
	}
	return b.finishLocked(b.pending, reply)
}

// sent records the request ID after the transport has accepted a command.
// A sender can deliver a reply synchronously, so an unmatched early reply is
// consumed here after the ID becomes known.
func (b *batteryWarning) sent(operation *batteryProducerOperation, requestID uint64, err error) *batteryProducerAction {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.pending != operation {
		return nil
	}
	if err != nil || requestID == 0 {
		b.pending = nil
		if errors.Is(err, notifyclient.ErrUnsupported) {
			b.qualified = false
		}
		return nil
	}
	operation.request = requestID
	operation.ready = true
	if reply, ok := b.earlyReply[requestID]; ok {
		delete(b.earlyReply, requestID)
		return b.finishLocked(operation, reply)
	}
	return nil
}

func (b *batteryWarning) sendFailed(operation *batteryProducerOperation) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.pending == operation {
		b.pending = nil
	}
}

func (r *Registry) dispatchBatteryWarning(action *batteryProducerAction) {
	for action != nil {
		if r == nil || r.producerSender == nil || r.batteryWarning == nil {
			if r == nil || r.batteryWarning == nil {
				return
			}
			r.batteryWarning.sendFailed(action.operation)
			return
		}
		requestID, err := r.producerSender.SendProducer(action.command)
		action = r.batteryWarning.sent(action.operation, requestID, err)
	}
}

func (b *batteryWarning) finishLocked(operation *batteryProducerOperation, reply protocol.Reply) *batteryProducerAction {
	b.pending = nil
	if reply.OK {
		b.liveKnown = true
		b.live = operation.publish
		b.reconcile = false
	} else if reply.Error != nil && reply.Error.Code == protocol.ErrorUnavailable {
		b.qualified = false
		b.liveKnown = false
	}
	if b.qualified && b.valid && ((b.liveKnown && b.live != b.desired) || (b.reconcile && !b.liveKnown)) {
		return b.startLocked()
	}
	return nil
}

func (b *batteryWarning) startLocked() *batteryProducerAction {
	if !b.connected || !b.qualified || !b.valid || b.pending != nil {
		return nil
	}
	if b.liveKnown && b.live == b.desired {
		return nil
	}
	if !b.liveKnown && !b.desired && !b.reconcile {
		return nil
	}
	op := &batteryProducerOperation{publish: b.desired}
	b.pending = op
	return &batteryProducerAction{operation: op, command: batteryProducerCommand(b.desired, b.charge)}
}

func batteryWarningDesired(battery *metrics.BatterySnapshot, threshold int, wasWarning bool) (bool, bool) {
	if battery == nil || !battery.Present || !battery.ChargeValid ||
		math.IsNaN(battery.Charge) || math.IsInf(battery.Charge, 0) || battery.Charge < 0 || battery.Charge > 1 {
		return false, false
	}
	switch battery.State {
	case metrics.BatteryCharging, metrics.BatteryFull:
		return false, true
	case metrics.BatteryDischarging:
		limit := float64(threshold)
		if wasWarning {
			limit += batteryWarningHysteresis
		}
		return battery.Charge*100 <= limit, true
	default:
		return false, false
	}
}

func batteryProducerCommand(publish bool, value int32) protocol.Command {
	request := &protocol.ProducerRequest{Key: "sysc-shell:battery-low"}
	if publish {
		request.AppName = "sysc-shell"
		request.Summary = "Battery low"
		request.Body = fmt.Sprintf("Battery is at %d%%.", value)
		request.Urgency = protocol.UrgencyCritical
		request.Value = &value
	}
	kind := protocol.CommandProducerClose
	if publish {
		kind = protocol.CommandProducerPublish
	}
	return protocol.Command{Kind: kind, Producer: request}
}

func batteryPercent(charge float64) int32 {
	value := int32(math.Round(charge * 100))
	if value < 0 {
		return 0
	}
	if value > 100 {
		return 100
	}
	return value
}

func batteryWarningThreshold(bar config.Bar) (int, bool) {
	var find func([]config.Item) (int, bool)
	find = func(items []config.Item) (int, bool) {
		for _, item := range items {
			if item.ID == "battery" {
				return item.WarnBelow, item.WarnBelow > 0
			}
			if item.ID == "group" {
				if threshold, ok := find(item.Items); ok {
					return threshold, true
				}
			}
		}
		return 0, false
	}
	for _, section := range [][]config.Item{bar.Left, bar.Center, bar.Right} {
		if threshold, ok := find(section); ok {
			return threshold, true
		}
	}
	return 0, false
}
