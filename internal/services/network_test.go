package services

import (
	"testing"
	"time"
)

func TestSignalBandMatchesReferenceThresholds(t *testing.T) {
	cases := []struct {
		signal uint8
		band   int
	}{{100, 4}, {80, 4}, {79, 3}, {60, 3}, {59, 2}, {35, 2}, {34, 1}, {15, 1}, {14, 0}, {0, 0}}
	for _, c := range cases {
		if got := SignalBand(c.signal); got != c.band {
			t.Errorf("SignalBand(%d) = %d, want %d", c.signal, got, c.band)
		}
	}
}

// fakeBackend stands in for NetworkManager. Every service test runs against it,
// so no test needs a live system bus.
type fakeBackend struct {
	state  NetworkState
	aps    []AccessPoint
	scans  int
	wakeCh chan<- struct{}
}

func (f *fakeBackend) State() (NetworkState, error)         { return f.state, nil }
func (f *fakeBackend) AccessPoints() ([]AccessPoint, error) { return f.aps, nil }
func (f *fakeBackend) Scan() error                          { f.scans++; return nil }
func (f *fakeBackend) SetWirelessEnabled(on bool) error     { f.state.WirelessEnabled = on; return nil }
func (f *fakeBackend) Activate(ap AccessPoint) error        { return nil }
func (f *fakeBackend) Forget(ssid string) error             { return nil }
func (f *fakeBackend) Close() error                         { return nil }

func (f *fakeBackend) Watch(wake chan<- struct{}, stop <-chan struct{}) error {
	f.wakeCh = wake
	return nil
}

func TestAccessPointsSortByBandNotPercent(t *testing.T) {
	f := &fakeBackend{aps: []AccessPoint{
		{SSID: "weak", Strength: 20},
		{SSID: "beta", Strength: 81},
		{SSID: "alpha", Strength: 95}, // same band as beta; SSID breaks the tie
	}}
	n := NewNetwork(f)
	got := n.AccessPoints()
	want := []string{"alpha", "beta", "weak"}
	for i, w := range want {
		if got[i].SSID != w {
			t.Fatalf("position %d = %q, want %q", i, got[i].SSID, w)
		}
	}
}

func TestChangesPublishesOnBackendWake(t *testing.T) {
	f := &fakeBackend{state: NetworkState{WirelessEnabled: true}}
	n := NewNetwork(f)
	lease, err := n.Acquire()
	if err != nil {
		t.Fatal(err)
	}
	defer lease.Release()
	f.state.SSID = "LukeAP"
	f.wakeCh <- struct{}{}
	select {
	case st := <-n.Changes():
		if st.SSID != "LukeAP" {
			t.Fatalf("SSID = %q, want LukeAP", st.SSID)
		}
	case <-time.After(time.Second):
		t.Fatal("no change published")
	}
}
