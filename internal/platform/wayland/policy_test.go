package wayland

import (
	"errors"
	"testing"

	"github.com/Nomadcxx/sysc-shell/internal/config"
)

func TestDisabledHostDoesNotBuildABar(t *testing.T) {
	t.Parallel()

	cfg := config.Default()
	cfg.Outputs = []config.OutputOverride{{
		Connector: "DP-1",
		Bar: func() config.Bar {
			bar := cfg.Bar
			bar.Enabled = false
			return bar
		}(),
	}}
	built := false
	o := owner{
		cfg: &cfg,
		cb: Callbacks{NewHost: func(uint32, string) (HostCallbacks, error) {
			built = true
			return HostCallbacks{}, errors.New("disabled host requested callbacks")
		}},
	}
	h := newHost(7, nil)
	h.connector = "DP-1"
	h.doneSeen = true
	h.state = hostReady

	if err := o.hostBecameReady(h); err != nil {
		t.Fatalf("hostBecameReady: %v", err)
	}
	if built {
		t.Fatal("disabled host built a bar")
	}
	if h.state != hostIdle {
		t.Fatalf("state = %v, want hostIdle", h.state)
	}
}

// NewHost already acquired the bar's leases. A later validation failure must
// still DropHost, or the clock goroutine outlives the host.
func TestHostBecameReadyDropsAHostThatFailsValidation(t *testing.T) {
	t.Parallel()

	cfg := config.Default()
	var dropped uint32
	o := owner{
		cfg: &cfg,
		cb: Callbacks{
			NewHost: func(uint32, string) (HostCallbacks, error) {
				return HostCallbacks{}, nil
			},
			DropHost: func(global uint32) { dropped = global },
		},
	}
	h := newHost(7, nil)
	h.connector = "DP-9"
	h.doneSeen = true
	h.state = hostReady

	if err := o.hostBecameReady(h); err == nil {
		t.Fatal("hostBecameReady accepted callbacks with no Configure")
	}
	if dropped != 7 {
		t.Fatalf("DropHost(%d), want 7; NewHost succeeded and the leases leaked", dropped)
	}
}

func TestHostGeometryUsesItsOwnPolicy(t *testing.T) {
	t.Parallel()

	first := newHost(7, nil)
	first.policy = config.Default().Bar
	second := newHost(8, nil)
	second.policy = config.Default().Bar
	second.policy.Height = 56
	second.policy.Gap = 6

	if got := first.surfaceHeight(); got != 44 {
		t.Fatalf("first surface height = %d, want 44", got)
	}
	if got := second.surfaceHeight(); got != 50 {
		t.Fatalf("second surface height = %d, want 50", got)
	}
}

func TestPrepareConfigDoesNotMutateLiveHosts(t *testing.T) {
	t.Parallel()

	current := config.Default()
	h := newHost(7, nil)
	h.connector = "DP-1"
	h.doneSeen = true
	h.state = hostMapped
	h.policy = current.Bar
	hosts := newHostSet()
	hosts.hosts[h.global] = h
	hosts.arrival = append(hosts.arrival, h.global)

	candidate := config.Default()
	candidate.Theme.Background = "#10141880"
	override := candidate.Bar
	override.Height = 56
	override.Gap = 6
	candidate.Outputs = []config.OutputOverride{{Connector: "DP-1", Bar: override}}
	committed := false
	o := owner{
		cfg:   &current,
		hosts: hosts,
		cb: Callbacks{PrepareConfig: func(_ config.Config, identities []HostIdentity) (PreparedConfig, error) {
			if len(identities) != 1 || identities[0] != (HostIdentity{Global: 7, Connector: "DP-1"}) {
				t.Fatalf("identities = %v, want global 7 / DP-1", identities)
			}
			return PreparedConfig{
				Hosts:    map[uint32]HostCallbacks{7: validHostCallbacks()},
				Commit:   func() { committed = true },
				Rollback: func() {},
			}, nil
		}},
	}

	prepared, err := o.prepareConfig(candidate)
	if err != nil {
		t.Fatalf("prepareConfig: %v", err)
	}
	if committed {
		t.Fatal("prepareConfig committed shell state")
	}
	if h.policy.Height != current.Bar.Height || h.policy.Gap != current.Bar.Gap {
		t.Fatalf("live policy changed to height=%d gap=%d", h.policy.Height, h.policy.Gap)
	}
	if o.cfg.Bar.Height != current.Bar.Height {
		t.Fatalf("owner config height = %d, want %d", o.cfg.Bar.Height, current.Bar.Height)
	}
	if len(prepared.hosts) != 1 || prepared.hosts[0].policy.Height != 56 || prepared.hosts[0].policy.Gap != 6 {
		t.Fatalf("prepared hosts = %+v, want DP-1 policy 56/6", prepared.hosts)
	}
	if prepared.hosts[0].opaqueBackground {
		t.Fatal("translucent candidate prepared an opaque host")
	}
}

func TestPrepareConfigConfiguresMappedReplacementBeforePublishing(t *testing.T) {
	t.Parallel()

	current := config.Default()
	h := newHost(7, nil)
	h.connector = "DP-1"
	h.doneSeen = true
	h.state = hostMapped
	h.policy = current.Bar
	h.ss.configure(1200, h.surfaceHeight())
	h.ss.acknowledge()
	h.ss.preferredScale(180)
	hosts := newHostSet()
	hosts.hosts[h.global] = h
	hosts.arrival = append(hosts.arrival, h.global)

	configured := false
	o := owner{
		cfg:   &current,
		hosts: hosts,
		cb: Callbacks{PrepareConfig: func(_ config.Config, _ []HostIdentity) (PreparedConfig, error) {
			callbacks := validHostCallbacks()
			callbacks.Configure = func(width, height, scale120 int) error {
				configured = true
				if width != 1200 || height != 44 || scale120 != 180 {
					t.Fatalf("Configure(%d, %d, %d), want (1200, 44, 180)",
						width, height, scale120)
				}
				return nil
			}
			return PreparedConfig{
				Hosts:    map[uint32]HostCallbacks{7: callbacks},
				Commit:   func() {},
				Rollback: func() {},
			}, nil
		}},
	}

	if _, err := o.prepareConfig(config.Default()); err != nil {
		t.Fatalf("prepareConfig: %v", err)
	}
	if !configured {
		t.Fatal("mapped replacement was not configured during preparation")
	}
}

func TestReloadBeforeFirstConfigureDefersRegionRefresh(t *testing.T) {
	t.Parallel()

	h := newHost(7, nil)
	h.state = hostCreating
	if refreshRegionsOnReload(h) {
		t.Fatal("creating host refreshed regions before it had accepted configure geometry")
	}

	h.state = hostMapped
	if !refreshRegionsOnReload(h) {
		t.Fatal("mapped host did not refresh regions from its accepted configure geometry")
	}
}

func TestApplyConfigCommitsDisabledPoliciesTogether(t *testing.T) {
	t.Parallel()

	current := config.Default()
	hosts := newHostSet()
	for global, connector := range map[uint32]string{7: "DP-1", 8: "HDMI-A-1"} {
		h := newHost(global, nil)
		h.connector = connector
		h.doneSeen = true
		h.state = hostIdle
		h.policy = current.Bar
		hosts.hosts[global] = h
		hosts.arrival = append(hosts.arrival, global)
	}
	candidate := config.Default()
	candidate.Bar.Enabled = false
	committed := false
	o := owner{
		cfg:   &current,
		hosts: hosts,
		cb: Callbacks{PrepareConfig: func(_ config.Config, identities []HostIdentity) (PreparedConfig, error) {
			if len(identities) != 0 {
				t.Fatalf("enabled identities = %v, want none", identities)
			}
			return PreparedConfig{
				Hosts:    map[uint32]HostCallbacks{},
				Commit:   func() { committed = true },
				Rollback: func() {},
			}, nil
		}},
	}

	if err := o.applyConfig(candidate); err != nil {
		t.Fatalf("applyConfig: %v", err)
	}
	if !committed {
		t.Fatal("applyConfig did not commit the prepared shell state")
	}
	if o.cfg.Bar.Enabled {
		t.Fatal("owner retained the enabled configuration")
	}
	for _, h := range hosts.each() {
		if h.policy.Enabled {
			t.Fatalf("%s retained its enabled policy", h.connector)
		}
		if h.state != hostIdle {
			t.Fatalf("%s state = %v, want hostIdle", h.connector, h.state)
		}
	}
}

func TestHotplugUsesTheAcceptedOutputPolicy(t *testing.T) {
	t.Parallel()

	current := config.Default()
	candidate := config.Default()
	override := candidate.Bar
	override.Enabled = false
	override.Height = 56
	override.Gap = 6
	candidate.Outputs = []config.OutputOverride{{Connector: "DP-1", Bar: override}}
	committed := false
	built := false
	o := owner{
		cfg:   &current,
		hosts: newHostSet(),
		cb: Callbacks{
			NewHost: func(uint32, string) (HostCallbacks, error) {
				built = true
				return validHostCallbacks(), nil
			},
			PrepareConfig: func(_ config.Config, _ []HostIdentity) (PreparedConfig, error) {
				return PreparedConfig{
					Hosts:    map[uint32]HostCallbacks{},
					Commit:   func() { committed = true },
					Rollback: func() {},
				}, nil
			},
		},
	}

	if err := o.applyConfig(candidate); err != nil {
		t.Fatalf("applyConfig: %v", err)
	}
	if !committed {
		t.Fatal("candidate was not committed")
	}
	h, _ := o.hosts.add(9, nil)
	h.connector = "DP-1"
	h.doneSeen = true
	h.state = hostReady
	if err := o.hostBecameReady(h); err != nil {
		t.Fatalf("hostBecameReady: %v", err)
	}
	if built {
		t.Fatal("hotplug built a bar disabled by the accepted override")
	}
	if h.policy.Height != 56 || h.policy.Gap != 6 || h.policy.Enabled {
		t.Fatalf("hotplug policy = %+v, want disabled 56/6 override", h.policy)
	}
}

func validHostCallbacks() HostCallbacks {
	return HostCallbacks{
		Configure: func(int, int, int) error { return nil },
		Render:    func([]byte, int, int, int) error { return nil },
		Handle:    func(Event) bool { return false },
	}
}
