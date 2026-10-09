package wayland

import (
	"errors"
	"testing"

	"github.com/Nomadcxx/sysc-shell/internal/platform/wayland/gamma"
	"github.com/Nomadcxx/sysc-wayland/client"
)

type gammaControlStub struct {
	setCalls      int
	destroyCalls  int
	setErr        error
	sizeHandler   gamma.ZwlrGammaControlV1GammaSizeHandlerFunc
	failedHandler gamma.ZwlrGammaControlV1FailedHandlerFunc
}

func (s *gammaControlStub) SetGamma(int) error {
	s.setCalls++
	return s.setErr
}

func (s *gammaControlStub) Destroy() error {
	s.destroyCalls++
	return nil
}

func (s *gammaControlStub) SetGammaSizeHandler(h gamma.ZwlrGammaControlV1GammaSizeHandlerFunc) {
	s.sizeHandler = h
}

func (s *gammaControlStub) SetFailedHandler(h gamma.ZwlrGammaControlV1FailedHandlerFunc) {
	s.failedHandler = h
}

func TestGammaFailureDestroysAndStopsUntilRetry(t *testing.T) {
	failed := &gammaControlStub{setErr: errors.New("write failed")}
	recovered := &gammaControlStub{}
	events := make(chan GammaEvent, 4)
	o := &owner{
		gammaOut:     map[uint32]*gammaOutput{3: {host: &OutputHost{global: 3, connector: "DP-1"}, ctl: failed, size: 4}},
		gammaCurrent: &GammaRequest{Kelvin: 4000},
		cb:           Callbacks{GammaEvents: events},
		gammaFactory: func(*client.Output) (gammaControl, error) { return recovered, nil },
	}
	o.applyGammaTo(3)
	if failed.setCalls != 1 || failed.destroyCalls != 1 {
		t.Fatalf("after failure set/destroy calls = %d/%d, want 1/1", failed.setCalls, failed.destroyCalls)
	}
	if ev := <-events; ev.State != GammaFailed || ev.Connector != "DP-1" {
		t.Fatalf("failure event = %+v", ev)
	}

	o.gammaCurrent = &GammaRequest{Kelvin: 3500}
	o.applyGammaTo(3)
	if failed.setCalls != 1 || failed.destroyCalls != 1 {
		t.Fatalf("unrequested retry set/destroy calls = %d/%d, want 1/1", failed.setCalls, failed.destroyCalls)
	}
	o.applyGamma(GammaRequest{Kelvin: 3500, Retry: true})
	if recovered.sizeHandler == nil {
		t.Fatal("explicit retry did not create a replacement control")
	}
	recovered.sizeHandler(gamma.ZwlrGammaControlV1GammaSizeEvent{Size: 4})
	if failed.setCalls != 1 || failed.destroyCalls != 1 || recovered.setCalls != 1 || recovered.destroyCalls != 0 {
		t.Fatalf("failed/retried controls set/destroy = %d/%d and %d/%d, want 1/1 and 1/0",
			failed.setCalls, failed.destroyCalls, recovered.setCalls, recovered.destroyCalls)
	}
}

func TestNeutralRetryRecreatesFailedControlAndRestoresNeutral(t *testing.T) {
	failed := &gammaControlStub{setErr: errors.New("write failed")}
	recovered := &gammaControlStub{}
	events := make(chan GammaEvent, 4)
	o := &owner{
		gammaOut:     map[uint32]*gammaOutput{3: {host: &OutputHost{global: 3, connector: "DP-1"}, ctl: failed, size: 4}},
		gammaCurrent: &GammaRequest{Kelvin: 4000},
		cb:           Callbacks{GammaEvents: events},
		gammaFactory: func(*client.Output) (gammaControl, error) { return recovered, nil },
	}
	o.applyGammaTo(3)
	if ev := <-events; ev.State != GammaFailed {
		t.Fatalf("failure event = %+v", ev)
	}

	o.applyGamma(GammaRequest{Neutral: true, Retry: true})
	if recovered.sizeHandler == nil {
		t.Fatal("neutral retry did not create a replacement control")
	}
	recovered.sizeHandler(gamma.ZwlrGammaControlV1GammaSizeEvent{Size: 4})
	if recovered.setCalls != 0 || recovered.destroyCalls != 1 {
		t.Fatalf("neutral replacement set/destroy = %d/%d, want 0/1", recovered.setCalls, recovered.destroyCalls)
	}
	if ev := <-events; ev.State != GammaReady {
		t.Fatalf("replacement readiness event = %+v", ev)
	}
}

func TestGammaReadyFollowsSuccessfulRampApplication(t *testing.T) {
	stub := &gammaControlStub{}
	events := make(chan GammaEvent, 2)
	o := &owner{
		gammaOut:     map[uint32]*gammaOutput{1: {host: &OutputHost{global: 1, connector: "DP-1"}, ctl: stub, size: 8}},
		gammaCurrent: &GammaRequest{Kelvin: 4000},
		cb:           Callbacks{GammaEvents: events},
	}
	o.applyGammaTo(1)
	if stub.setCalls != 1 {
		t.Fatalf("SetGamma calls = %d, want 1 before ready", stub.setCalls)
	}
	if ev := <-events; ev.State != GammaReady || ev.Size != 8 {
		t.Fatalf("ready event = %+v", ev)
	}
}

func TestGammaHotplugAppliesCurrentRampAndDestroysOnRemoval(t *testing.T) {
	stub := &gammaControlStub{}
	events := make(chan GammaEvent, 2)
	created := 0
	o := &owner{
		gammaCurrent: &GammaRequest{Kelvin: 4000},
		gammaFactory: func(*client.Output) (gammaControl, error) {
			created++
			return stub, nil
		},
		cb: Callbacks{GammaEvents: events},
	}

	o.attachGamma(&OutputHost{global: 8, connector: "DP-2"})
	if created != 1 || stub.sizeHandler == nil {
		t.Fatalf("hotplug created %d controls with size handler %v, want one control", created, stub.sizeHandler != nil)
	}
	stub.sizeHandler(gamma.ZwlrGammaControlV1GammaSizeEvent{Size: 4})
	if stub.setCalls != 1 {
		t.Fatalf("new output SetGamma calls = %d, want the current ramp applied once", stub.setCalls)
	}
	if ev := <-events; ev.State != GammaReady || ev.Global != 8 {
		t.Fatalf("hotplug event = %+v, want ready for output 8", ev)
	}

	o.destroyGamma(8)
	if stub.destroyCalls != 1 {
		t.Fatalf("removed output Destroy calls = %d, want 1", stub.destroyCalls)
	}
	if ev := <-events; ev.State != GammaRemoved || ev.Global != 8 {
		t.Fatalf("removal event = %+v, want removed for output 8", ev)
	}
}

func TestGammaSizeAboveBoundFailsBeforeRampAllocation(t *testing.T) {
	stub := &gammaControlStub{}
	events := make(chan GammaEvent, 1)
	o := &owner{
		gammaOut:     map[uint32]*gammaOutput{1: {host: &OutputHost{global: 1, connector: "DP-1"}}},
		gammaCurrent: &GammaRequest{Kelvin: 4000},
		cb:           Callbacks{GammaEvents: events},
		gammaFactory: func(*client.Output) (gammaControl, error) { return stub, nil },
	}
	o.gammaCreate(1)
	stub.sizeHandler(gamma.ZwlrGammaControlV1GammaSizeEvent{Size: uint32(gamma.MaxRampEntries + 1)})
	if stub.setCalls != 0 || stub.destroyCalls != 1 {
		t.Fatalf("oversized ramp set/destroy = %d/%d, want 0/1", stub.setCalls, stub.destroyCalls)
	}
	if ev := <-events; ev.State != GammaFailed {
		t.Fatalf("oversized ramp event = %+v, want failure", ev)
	}
}
