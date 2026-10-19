package wayland

import (
	"errors"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Nomadcxx/sysc-shell/internal/config"
	"github.com/Nomadcxx/sysc-shell/internal/platform/wayland/layershell"
	"github.com/Nomadcxx/sysc-wayland/client"
)

func TestReloadEqualConfigSkipsPrepareAndReportsRecovery(t *testing.T) {
	current := config.Default()
	path := filepath.Join(t.TempDir(), "config.json")
	if err := config.Write(path, current); err != nil {
		t.Fatal(err)
	}
	calls := 0
	o := owner{cfg: &current, hosts: newHostSet(), cb: Callbacks{
		ConfigPath: path,
		PrepareConfig: func(config.Config, []HostIdentity, uint64) (PreparedConfig, error) {
			t.Fatal("equal config prepared replacement resources")
			return PreparedConfig{}, nil
		},
		ReloadResult: func(err error) {
			calls++
			if err != nil {
				t.Errorf("equal config result: %v", err)
			}
		},
	}}
	if err := o.reloadConfig(); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("outcomes = %d, want one recovery", calls)
	}
}

func TestReloadConfigSnapshotsGenerationBeforeRead(t *testing.T) {
	current := config.Default()
	candidate := current.Clone()
	candidate.Bar.Height++
	path := filepath.Join(t.TempDir(), "config.json")
	const wantGeneration = 19
	generationCalls, prepareCalls := 0, 0
	o := owner{cfg: &current, hosts: newHostSet(), cb: Callbacks{
		ConfigPath: path,
		ConfigGeneration: func() uint64 {
			generationCalls++
			if err := config.Write(path, candidate); err != nil {
				t.Fatalf("write config from generation callback: %v", err)
			}
			return wantGeneration
		},
		PrepareConfig: func(got config.Config, _ []HostIdentity, generation uint64) (PreparedConfig, error) {
			prepareCalls++
			if !reflect.DeepEqual(got, candidate) {
				t.Fatalf("prepared config = %+v, want %+v", got, candidate)
			}
			if generation != wantGeneration {
				t.Fatalf("prepared generation = %d, want %d", generation, wantGeneration)
			}
			return PreparedConfig{Hosts: map[uint32]HostCallbacks{}, Commit: func() {}, Rollback: func() {}}, nil
		},
	}}

	if err := o.reloadConfig(); err != nil {
		t.Fatalf("reloadConfig: %v", err)
	}
	if generationCalls != 1 || prepareCalls != 1 {
		t.Fatalf("generation calls=%d, prepare calls=%d, want one each", generationCalls, prepareCalls)
	}
}

func TestReloadRefusesUnavailableOrInvalidFilesWithoutPreparing(t *testing.T) {
	for _, test := range []struct {
		name      string
		writeFile bool
		contents  []byte
		wantError string
	}{
		{name: "missing"},
		{name: "malformed", writeFile: true, contents: []byte("{\n  \"secret\":\"do-not-publish\",\n  @\n}")},
		{name: "invalid frame cap", writeFile: true, contents: []byte(`{"theme":{"frame-cap":"30"}}`), wantError: "theme.frame-cap"},
	} {
		t.Run(test.name, func(t *testing.T) {
			current := config.Default()
			cfgPointer := &current
			path := filepath.Join(t.TempDir(), "config.json")
			if test.writeFile {
				if err := os.WriteFile(path, test.contents, 0600); err != nil {
					t.Fatal(err)
				}
			}
			hosts := newHostSet()
			h := newHost(7, nil)
			h.connector, h.policy = "DP-1", current.Bar
			app := validHostCallbacks()
			h.bar.app = app
			hosts.hosts[7] = h
			hosts.arrival = append(hosts.arrival, 7)
			prepareCalls := 0
			var outcomes []error
			o := owner{cfg: cfgPointer, hosts: hosts, cb: Callbacks{
				ConfigPath: path,
				PrepareConfig: func(config.Config, []HostIdentity, uint64) (PreparedConfig, error) {
					prepareCalls++
					return PreparedConfig{Hosts: map[uint32]HostCallbacks{}, Commit: func() {}, Rollback: func() {}}, nil
				},
				ReloadResult: func(err error) { outcomes = append(outcomes, err) },
			}}

			if err := o.reloadConfig(); err != nil {
				t.Fatalf("refused reload returned fatal error: %v", err)
			}
			if prepareCalls != 0 {
				t.Fatalf("prepared %d candidates after a refused read", prepareCalls)
			}
			if o.cfg != cfgPointer || o.hosts != hosts || !reflect.DeepEqual(h.policy, current.Bar) {
				t.Fatal("refused reload changed live config, hosts, or host policy")
			}
			if reflect.ValueOf(h.bar.app.Configure).Pointer() != reflect.ValueOf(app.Configure).Pointer() {
				t.Fatal("refused reload replaced host callbacks")
			}
			if len(outcomes) != 1 || outcomes[0] == nil {
				t.Fatalf("outcomes = %v, want one refusal", outcomes)
			}
			if test.wantError != "" && !strings.Contains(outcomes[0].Error(), test.wantError) {
				t.Fatalf("refusal = %v, want %q error", outcomes[0], test.wantError)
			}
			if strings.Contains(outcomes[0].Error(), "do-not-publish") {
				t.Fatalf("source leaked through refusal: %v", outcomes[0])
			}
			if test.name == "missing" && !errors.Is(outcomes[0], os.ErrNotExist) {
				t.Fatalf("missing-file cause lost: %v", outcomes[0])
			}
		})
	}
}

func TestReloadPreparationFailureRollsBackAndReportsOneRefusal(t *testing.T) {
	current := config.Default()
	candidate := current.Clone()
	candidate.Bar.Edge = "left"
	path := filepath.Join(t.TempDir(), "config.json")
	if err := config.Write(path, candidate); err != nil {
		t.Fatal(err)
	}
	hosts := newHostSet()
	for _, global := range []uint32{7, 8} {
		h := newHost(global, nil)
		h.connector, h.doneSeen, h.state, h.policy = "DP-1", true, hostMapped, current.Bar
		h.applyMode(1920, 1080)
		h.bar.ss.configure(1536, current.Bar.SurfaceExtent())
		h.bar.ss.acknowledge()
		hosts.hosts[global] = h
		hosts.arrival = append(hosts.arrival, global)
	}
	rolledBack := 0
	var outcomes []error
	o := owner{cfg: &current, hosts: hosts, cb: Callbacks{
		ConfigPath: path,
		PrepareConfig: func(_ config.Config, identities []HostIdentity, _ uint64) (PreparedConfig, error) {
			callbacks := map[uint32]HostCallbacks{}
			for _, identity := range identities {
				callbacks[identity.Global] = validHostCallbacks()
			}
			failed := callbacks[8]
			failed.Configure = func(int, int, int) error { return errors.New("private-service-payload") }
			callbacks[8] = failed
			return PreparedConfig{
				Hosts: callbacks,
				Commit: func() {
					t.Error("failed preparation committed")
				},
				Rollback: func() { rolledBack++ },
			}, nil
		},
		ReloadResult: func(err error) { outcomes = append(outcomes, err) },
	}}
	if err := o.reloadConfig(); err != nil {
		t.Fatalf("preparation refusal became fatal: %v", err)
	}
	if rolledBack != 1 {
		t.Fatalf("candidate rollback count = %d", rolledBack)
	}
	if len(outcomes) != 1 || outcomes[0] == nil || strings.Contains(outcomes[0].Error(), "private-service-payload") {
		t.Fatalf("unsafe or missing preparation refusal: %v", outcomes)
	}
	if o.cfg != &current {
		t.Fatal("preparation refusal replaced current config")
	}
	for _, h := range hosts.each() {
		if !reflect.DeepEqual(h.policy, current.Bar) {
			t.Fatalf("preparation refusal changed %s policy: %+v", h.connector, h.policy)
		}
	}
}

func TestReloadSuccessReportsAfterCommitAndEmptyPathDoesNothing(t *testing.T) {
	current := config.Default()
	candidate := current.Clone()
	candidate.Bar.Height++
	path := filepath.Join(t.TempDir(), "config.json")
	if err := config.Write(path, candidate); err != nil {
		t.Fatal(err)
	}
	committed := false
	var outcomes []error
	o := owner{cfg: &current, hosts: newHostSet(), cb: Callbacks{
		ConfigPath: path,
		PrepareConfig: func(config.Config, []HostIdentity, uint64) (PreparedConfig, error) {
			return PreparedConfig{Hosts: map[uint32]HostCallbacks{}, Commit: func() { committed = true }, Rollback: func() {}}, nil
		},
		ReloadResult: func(err error) {
			if err == nil && !committed {
				t.Error("success was reported before commit")
			}
			outcomes = append(outcomes, err)
		},
	}}
	if err := o.reloadConfig(); err != nil {
		t.Fatal(err)
	}
	if !committed || !reflect.DeepEqual(*o.cfg, candidate) || len(outcomes) != 1 || outcomes[0] != nil {
		t.Fatalf("commit=%v config=%+v outcomes=%v", committed, o.cfg, outcomes)
	}

	emptyCalls := 0
	empty := owner{cfg: &current, hosts: newHostSet(), cb: Callbacks{ReloadResult: func(error) { emptyCalls++ }}}
	if err := empty.reloadConfig(); err != nil || emptyCalls != 0 {
		t.Fatalf("empty path reload = %v, callbacks=%d", err, emptyCalls)
	}
	withoutCallback := owner{cfg: &current, hosts: newHostSet(), cb: Callbacks{
		ConfigPath: path,
		PrepareConfig: func(config.Config, []HostIdentity, uint64) (PreparedConfig, error) {
			return PreparedConfig{Hosts: map[uint32]HostCallbacks{}, Commit: func() {}, Rollback: func() {}}, nil
		},
	}}
	if err := withoutCallback.reloadConfig(); err != nil || !reflect.DeepEqual(*withoutCallback.cfg, candidate) {
		t.Fatalf("reload without optional callback = %v, config=%+v", err, withoutCallback.cfg)
	}
}

func TestReloadApplyTransportFailureDoesNotReportSuccess(t *testing.T) {
	path := filepath.Join(t.TempDir(), "wayland.sock")
	listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	accepted := make(chan *net.UnixConn, 1)
	go func() {
		conn, err := listener.AcceptUnix()
		if err == nil {
			accepted <- conn
		}
	}()
	display, err := client.Connect(path)
	if err != nil {
		t.Fatal(err)
	}
	serverConn := <-accepted
	t.Cleanup(func() {
		_ = display.Context().Close()
		_ = serverConn.Close()
	})

	current := config.Default()
	candidate := current.Clone()
	candidate.Bar.Padding++
	configPath := filepath.Join(t.TempDir(), "config.json")
	if err := config.Write(configPath, candidate); err != nil {
		t.Fatal(err)
	}
	h := newHost(7, nil)
	h.connector, h.doneSeen, h.state, h.policy = "DP-1", true, hostIdle, current.Bar
	h.bar.surface = client.NewSurface(display.Context())
	h.bar.layer = layershell.NewZwlrLayerSurfaceV1(display.Context())
	hosts := newHostSet()
	hosts.hosts[7] = h
	hosts.arrival = append(hosts.arrival, 7)
	var outcomes []error
	o := owner{cfg: &current, hosts: hosts, cb: Callbacks{
		ConfigPath: configPath,
		PrepareConfig: func(_ config.Config, identities []HostIdentity, _ uint64) (PreparedConfig, error) {
			callbacks := make(map[uint32]HostCallbacks, len(identities))
			for _, identity := range identities {
				callbacks[identity.Global] = validHostCallbacks()
			}
			return PreparedConfig{Hosts: callbacks, Commit: func() {}, Rollback: func() {}}, nil
		},
		ReloadResult: func(err error) { outcomes = append(outcomes, err) },
	}}
	if err := serverConn.Close(); err != nil {
		t.Fatal(err)
	}
	if err := o.reloadConfig(); err == nil {
		t.Fatal("broken transport was not fatal")
	}
	if len(outcomes) != 0 {
		t.Fatalf("transport failure reported reload results: %v", outcomes)
	}
}
