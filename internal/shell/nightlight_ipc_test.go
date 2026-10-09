package shell

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/Nomadcxx/sysc-shell/internal/config"
	"github.com/Nomadcxx/sysc-shell/internal/platform/wayland"
	"github.com/Nomadcxx/sysc-shell/internal/settings"
)

func TestNightLightCallStatusSetAndToggle(t *testing.T) {
	cfg := config.Default()
	cfg.NightLight.Mode = config.NightLightModeAlways
	cfg.Accessibility.ReducedMotion = true
	r := NewRegistry(cfg)
	t.Cleanup(r.Close)
	r.NightLight().GammaEvent(wayland.GammaEvent{Global: 1, State: wayland.GammaReady})

	got, err := r.NightLightCall("nightlight.status", json.RawMessage(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	if got["mode"] != config.NightLightModeAlways || got["active"] != true || got["kelvin"] != 4000 || got["available"] != true {
		t.Fatalf("status = %#v", got)
	}
	if got["next_change"] != "" || got["override"] != false {
		t.Fatalf("status boundary and override = %#v", got)
	}

	got, err = r.NightLightCall("nightlight.set", json.RawMessage(`{"on":false}`))
	if err != nil {
		t.Fatal(err)
	}
	if got["on"] != false || r.NightLight().State().Target != 0 {
		t.Fatalf("set off = %#v, state %+v", got, r.NightLight().State())
	}
	if _, err := r.NightLightCall("nightlight.toggle", nil); err != nil {
		t.Fatal(err)
	}
	if state := r.NightLight().State(); !state.Override || state.Target != 4000 {
		t.Fatalf("toggle on = %+v", state)
	}
}

func TestNightLightTemperatureIPCValidatesAndPersists(t *testing.T) {
	cfg := config.Default()
	path := t.TempDir() + "/config.json"
	if err := config.Write(path, cfg); err != nil {
		t.Fatal(err)
	}
	r := NewRegistry(cfg)
	t.Cleanup(r.Close)
	reloads := make(chan struct{}, 1)
	r.BindPersist(path, reloads)

	got, err := r.NightLightCall("nightlight.temperature", json.RawMessage(`{"kelvin":3500}`))
	if err != nil || got["kelvin"] != 3500 {
		t.Fatalf("temperature = %#v, %v", got, err)
	}
	saved, err := config.Load(path)
	if err != nil || saved.NightLight.NightKelvin != 3500 {
		t.Fatalf("saved night temperature = %d, %v", saved.NightLight.NightKelvin, err)
	}
	select {
	case <-reloads:
	default:
		t.Fatal("temperature did not request config reload")
	}

	for _, params := range []string{`{"kelvin":2400}`, `{"kelvin":7000}`, `{}`, `not-json`} {
		if _, err := r.NightLightCall("nightlight.temperature", json.RawMessage(params)); err == nil {
			t.Errorf("temperature %s was accepted", params)
		}
	}
	if _, err := r.NightLightCall("nightlight.set", json.RawMessage(`{"on":"yes"}`)); err == nil {
		t.Error("set with a non-boolean on value was accepted")
	}
	if _, err := r.NightLightCall("nightlight.unknown", nil); err == nil || !strings.Contains(err.Error(), "unknown method") {
		t.Errorf("unknown method error = %v", err)
	}
}

func TestPersistedConfigUpdatesSerializeReadModifyWrite(t *testing.T) {
	path := t.TempDir() + "/config.json"
	if err := config.Write(path, config.Default()); err != nil {
		t.Fatal(err)
	}
	r := NewRegistry(config.Default())
	t.Cleanup(r.Close)
	r.BindPersist(path, nil)

	firstEntered := make(chan struct{})
	releaseFirst := make(chan struct{})
	secondEntered := make(chan struct{})
	firstDone := make(chan error, 1)
	secondDone := make(chan error, 1)
	go func() {
		_, err := r.updateConfig(func(cfg *config.Config, _ func(config.Config) *settings.Registry) error {
			close(firstEntered)
			<-releaseFirst
			cfg.ThemeGen.Mode = "light"
			return nil
		})
		firstDone <- err
	}()
	<-firstEntered
	go func() {
		_, err := r.updateConfig(func(cfg *config.Config, _ func(config.Config) *settings.Registry) error {
			close(secondEntered)
			cfg.NightLight.NightKelvin = 3500
			return nil
		})
		secondDone <- err
	}()
	select {
	case <-secondEntered:
		t.Error("second update entered before the first read-modify-write finished")
	case <-time.After(100 * time.Millisecond):
	}
	close(releaseFirst)
	if err := <-firstDone; err != nil {
		t.Fatal(err)
	}
	if err := <-secondDone; err != nil {
		t.Fatal(err)
	}
	got, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.ThemeGen.Mode != "light" || got.NightLight.NightKelvin != 3500 {
		t.Fatalf("concurrent updates lost a field: mode=%q night=%d", got.ThemeGen.Mode, got.NightLight.NightKelvin)
	}
}
