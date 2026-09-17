package shell

import (
	"errors"
	"math"
	"sync"
	"testing"

	metrics "github.com/Nomadcxx/sysc-metrics"
	"github.com/Nomadcxx/sysc-notify/protocol"
	"github.com/Nomadcxx/sysc-shell/internal/config"
	"github.com/Nomadcxx/sysc-shell/internal/notifyclient"
	"github.com/Nomadcxx/sysc-shell/internal/services"
)

func TestBatteryWarningPublishesOnceAndUsesHysteresis(t *testing.T) {
	r := NewRegistry(warningConfig())
	sender := &batteryProducerRecorder{}
	r.producerSender = sender
	r.applyNotify(notifySnapshot(1, true))

	r.UpdateMetrics(warningBattery(0.15, metrics.BatteryDischarging))
	if got := sender.commands(); len(got) != 1 {
		t.Fatalf("publish commands = %d, want one", len(got))
	} else if got[0].Kind != protocol.CommandProducerPublish || got[0].Producer == nil || *got[0].Producer.Value != 15 || got[0].Producer.Body != "Battery is at 15%." {
		t.Fatalf("publish command = %#v", got[0])
	}

	r.UpdateMetrics(warningBattery(0.15, metrics.BatteryDischarging))
	if got := len(sender.commands()); got != 1 {
		t.Fatalf("repeated low sample sent %d commands", got)
	}
	r.applyNotify(notifyReply(1, 1, protocol.Reply{OK: true, ID: 7}))
	r.UpdateMetrics(warningBattery(0.21, metrics.BatteryDischarging))
	if got := len(sender.commands()); got != 1 {
		t.Fatalf("hysteresis sample sent %d commands", got)
	}
	r.UpdateMetrics(warningBattery(0.26, metrics.BatteryDischarging))
	if got := sender.commands(); len(got) != 2 || got[1].Kind != protocol.CommandProducerClose {
		t.Fatalf("recovery commands = %#v", got)
	}
}

func TestBatteryWarningIgnoresInvalidReadingsAndChargingRecovers(t *testing.T) {
	r := NewRegistry(warningConfig())
	sender := &batteryProducerRecorder{}
	r.producerSender = sender
	r.applyNotify(notifySnapshot(1, true))
	r.UpdateMetrics(warningBattery(0.15, metrics.BatteryDischarging))
	r.applyNotify(notifyReply(1, 1, protocol.Reply{OK: true, ID: 7}))

	invalid := []services.Snapshot{
		warningBattery(math.NaN(), metrics.BatteryDischarging),
		warningBattery(math.Inf(1), metrics.BatteryDischarging),
		warningBattery(-0.1, metrics.BatteryDischarging),
		warningBattery(1.1, metrics.BatteryDischarging),
		{Battery: &metrics.BatterySnapshot{Present: false}},
		{Battery: &metrics.BatterySnapshot{Present: true, Charge: 0.15, State: metrics.BatteryDischarging}},
		warningBattery(0.15, metrics.BatteryUnknown),
	}
	for _, sample := range invalid {
		r.UpdateMetrics(sample)
	}
	if got := len(sender.commands()); got != 1 {
		t.Fatalf("invalid readings changed producer state with %d commands", got)
	}

	r.UpdateMetrics(warningBattery(0.15, metrics.BatteryCharging))
	if got := sender.commands(); len(got) != 2 || got[1].Kind != protocol.CommandProducerClose {
		t.Fatalf("charging recovery = %#v", got)
	}
}

func TestBatteryWarningRetriesAfterSendError(t *testing.T) {
	r := NewRegistry(warningConfig())
	sender := &batteryProducerRecorder{err: errors.New("queue full")}
	r.producerSender = sender
	r.applyNotify(notifySnapshot(1, true))
	r.UpdateMetrics(warningBattery(0.15, metrics.BatteryDischarging))
	if got := len(sender.commands()); got != 1 {
		t.Fatalf("failed send attempts = %d", got)
	}
	sender.err = nil
	r.UpdateMetrics(warningBattery(0.15, metrics.BatteryDischarging))
	if got := len(sender.commands()); got != 2 {
		t.Fatalf("retry attempts = %d, want two", got)
	}
}

func TestBatteryWarningDropsPendingSendForInvalidReading(t *testing.T) {
	r := NewRegistry(warningConfig())
	sender := &batteryProducerRecorder{}
	r.producerSender = sender
	r.applyNotify(notifySnapshot(1, true))
	r.UpdateMetrics(warningBattery(0.15, metrics.BatteryDischarging))
	r.UpdateMetrics(services.Snapshot{Battery: &metrics.BatterySnapshot{Present: false}})
	r.UpdateMetrics(warningBattery(0.15, metrics.BatteryDischarging))
	if got := len(sender.commands()); got != 2 {
		t.Fatalf("valid sample after invalid reading made %d publish attempts, want two", got)
	}
}

func TestBatteryWarningConsumesReplyThatRacesSend(t *testing.T) {
	r := NewRegistry(warningConfig())
	sender := &batteryProducerRecorder{}
	sender.onSend = func(id uint64) {
		r.applyNotify(notifyReply(1, id, protocol.Reply{OK: true, ID: 7}))
	}
	r.producerSender = sender
	r.applyNotify(notifySnapshot(1, true))
	r.UpdateMetrics(warningBattery(0.15, metrics.BatteryDischarging))
	r.UpdateMetrics(warningBattery(0.15, metrics.BatteryDischarging))
	if got := len(sender.commands()); got != 1 {
		t.Fatalf("early reply caused %d publishes", got)
	}
}

func TestBatteryWarningReconcilesQualifiedReconnect(t *testing.T) {
	r := NewRegistry(warningConfig())
	sender := &batteryProducerRecorder{}
	r.producerSender = sender
	r.applyNotify(notifySnapshot(1, true))
	r.UpdateMetrics(warningBattery(0.15, metrics.BatteryDischarging))
	r.applyNotify(notifyReply(1, 1, protocol.Reply{OK: true, ID: 7}))
	r.applyNotify(notifyclient.Message{Generation: 1, Kind: notifyclient.KindDisconnected})
	r.UpdateMetrics(warningBattery(0.50, metrics.BatteryDischarging))
	r.applyNotify(notifySnapshot(2, true))
	if got := sender.commands(); len(got) != 2 || got[1].Kind != protocol.CommandProducerClose {
		t.Fatalf("reconnect reconciliation = %#v", got)
	}
}

func TestBatteryWarningDoesNotSendToUnqualifiedService(t *testing.T) {
	r := NewRegistry(warningConfig())
	sender := &batteryProducerRecorder{}
	r.producerSender = sender
	r.applyNotify(notifySnapshot(1, false))
	r.UpdateMetrics(warningBattery(0.15, metrics.BatteryDischarging))
	if got := len(sender.commands()); got != 0 {
		t.Fatalf("unqualified service received %d producer commands", got)
	}
}

func TestBatteryWarningUsesBaseThresholdAndNestedFirstBattery(t *testing.T) {
	bar := config.Bar{
		Left:  []config.Item{{ID: "group", Items: []config.Item{{ID: "battery", WarnBelow: 20}}}},
		Right: []config.Item{{ID: "battery", WarnBelow: 5}},
	}
	if got, ok := batteryWarningThreshold(bar); !ok || got != 20 {
		t.Fatalf("nested threshold = %d, %v", got, ok)
	}

	cfg := config.Default()
	cfg.Bar.Left = []config.Item{{ID: "battery", WarnBelow: 20}}
	cfg.Bar.Center, cfg.Bar.Right = nil, nil
	cfg.Outputs = []config.OutputOverride{{Connector: "DP-1", Bar: config.Bar{Left: []config.Item{{ID: "battery", WarnBelow: 5}}}}}
	r := NewRegistry(cfg)
	sender := &batteryProducerRecorder{}
	r.producerSender = sender
	r.applyNotify(notifySnapshot(1, true))
	r.UpdateMetrics(warningBattery(0.10, metrics.BatteryDischarging))
	if got := len(sender.commands()); got != 1 {
		t.Fatalf("base threshold did not publish: %d", got)
	}
}

func notifySnapshot(generation uint64, qualified bool) notifyclient.Message {
	capabilities := []string{}
	if qualified {
		capabilities = []string{protocol.CapabilityBatteryProducer}
	}
	return notifyclient.Message{Generation: generation, Kind: notifyclient.KindSnapshot,
		Capabilities: capabilities, Snapshot: protocol.Snapshot{}}
}

func notifyReply(generation, requestID uint64, reply protocol.Reply) notifyclient.Message {
	return notifyclient.Message{Generation: generation, Kind: notifyclient.KindReply, RequestID: requestID, Reply: reply}
}

func warningBattery(charge float64, state metrics.BatteryState) services.Snapshot {
	return services.Snapshot{Battery: &metrics.BatterySnapshot{Present: true, Charge: charge, ChargeValid: true, State: state}}
}

func warningConfig() config.Config {
	cfg := config.Default()
	cfg.Bar.Left = []config.Item{{ID: "battery", WarnBelow: 20}}
	cfg.Bar.Center, cfg.Bar.Right = nil, nil
	return cfg
}

type batteryProducerRecorder struct {
	mu     sync.Mutex
	items  []protocol.Command
	next   uint64
	err    error
	onSend func(uint64)
}

func (s *batteryProducerRecorder) SendProducer(command protocol.Command) (uint64, error) {
	s.mu.Lock()
	s.next++
	id := s.next
	s.items = append(s.items, command)
	err := s.err
	onSend := s.onSend
	s.mu.Unlock()
	if onSend != nil {
		onSend(id)
	}
	return id, err
}

func (s *batteryProducerRecorder) commands() []protocol.Command {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]protocol.Command(nil), s.items...)
}
