package lockconfig

import (
	"context"
	"encoding/json"
	"errors"
	"os/exec"
	"reflect"
	"time"

	"github.com/Nomadcxx/sysc-terminal/renderer"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

func TestLockConfigPreservesUnknownFields(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.json")
	_ = os.WriteFile(p, []byte(`{"effect":"rain","palette":"nord","future":{"a":1}}`), 0600)
	c, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	c.ReducedMotion = true
	if err = Save(p, c); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(p)
	if !strings.Contains(string(data), `"future"`) || !strings.Contains(string(data), `"a": 1`) {
		t.Fatal("lost future fields", string(data))
	}
}
func TestLockConfigRejectsTextEffectAndOversize(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.json")
	for _, data := range []string{`{"effect":"fire-text"}`, `{"palette":"invalid"}`, `{"reduced_motion":null}`, strings.Repeat(" ", 65537)} {
		_ = os.WriteFile(p, []byte(data), 0600)
		if _, err := Load(p); err == nil {
			t.Fatal("accepted invalid config")
		}
	}
}
func TestLockConfigAtomicFailureKeepsOldFile(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.json")
	old := []byte(`{"effect":"rain","palette":"nord"}`)
	_ = os.WriteFile(p, old, 0600)
	c := Default()
	c.Effect = "invalid"
	if err := Save(p, c); err == nil {
		t.Fatal("invalid save")
	}
	got, _ := os.ReadFile(p)
	if string(got) != string(old) {
		t.Fatal("destroyed existing config")
	}
}

func TestLockConfigDoesNotFollowLinksOrBlockOnFIFO(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.json")
	target := filepath.Join(t.TempDir(), "other.json")
	if err := os.WriteFile(target, []byte(`{}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, p); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(p); err == nil {
		t.Fatal("followed config symlink")
	}
	if err := os.Remove(p); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Mkfifo(p, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(p); err == nil {
		t.Fatal("accepted nonregular configuration")
	}
}

func TestLockConfigReplaceFailureKeepsOldFile(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.json")
	old := []byte(`{"effect":"rain","palette":"nord","future":true}`)
	if err := os.WriteFile(p, old, 0600); err != nil {
		t.Fatal(err)
	}
	previous := atomicReplace
	t.Cleanup(func() { atomicReplace = previous })
	atomicReplace = func(string, string) error { return errors.New("replace failed") }
	if err := Save(p, Default()); err == nil {
		t.Fatal("accepted failed replacement")
	}
	got, err := os.ReadFile(p)
	if err != nil || string(got) != string(old) {
		t.Fatal("destroyed old configuration", err)
	}
	files, err := os.ReadDir(filepath.Dir(p))
	if err != nil || len(files) != 1 {
		t.Fatal("left temporary file", err)
	}
}

func TestLockConfigGpuKeysRoundTrip(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.json")
	body := `{"effect":"rain","palette":"nord","effect_backend":"gpu","effect_gpu_power_save":false,"blur_backdrop":false,"clock_24h":true,"effect_fps":60,"future":{"a":1}}`
	if err := os.WriteFile(p, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	c, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if c.Backend() != "gpu" || c.GpuPowerSave() || c.BlurBackdrop() || !c.Clock24h || c.EffectFPS != 60 {
		t.Fatalf("gpu keys not read: %+v", c)
	}
	if err = Save(p, c); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(p)
	if !strings.Contains(string(data), `"future"`) {
		t.Fatal("lost future fields", string(data))
	}
	again, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if again.Backend() != "gpu" || again.EffectFPS != 60 {
		t.Fatalf("round trip lost values: %+v", again)
	}
}

func TestLockConfigDefaultsForMissingKeys(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(p, []byte(`{}`), 0600); err != nil {
		t.Fatal(err)
	}
	c, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if c.Backend() != "auto" || !c.GpuPowerSave() || !c.BlurBackdrop() || c.Clock24h || c.EffectFPS != 20 {
		t.Fatalf("defaults not applied: %+v", c)
	}
}

func TestLockConfigClampsEffectFps(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.json")
	for body, want := range map[string]int{
		`{"effect_fps":200}`:  120,
		`{"effect_fps":1}`:    10,
		`{"effect_fps":0}`:    20,
		`{"effect_fps":null}`: 20,
	} {
		if err := os.WriteFile(p, []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
		c, err := Load(p)
		if err != nil {
			t.Fatalf("%s: %v", body, err)
		}
		if c.EffectFPS != want {
			t.Fatalf("%s: got %d want %d", body, c.EffectFPS, want)
		}
	}
}

func TestLockConfigClockStyleRoundTripsAndNormalizes(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(p, []byte(`{"clock_style":"phm_slanted"}`), 0600); err != nil {
		t.Fatal(err)
	}
	c, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if c.ClockStyle != "phm_slanted" {
		t.Fatalf("clock style not read: %+v", c)
	}
	if err = Save(p, c); err != nil {
		t.Fatal(err)
	}
	again, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if again.ClockStyle != "phm_slanted" {
		t.Fatalf("round trip lost clock style: %+v", again)
	}
	for body, want := range map[string]string{
		`{}`:                      DefaultClockStyle,
		`{"clock_style":""}`:      DefaultClockStyle,
		`{"clock_style":"nope"}`:  DefaultClockStyle,
		`{"clock_style":null}`:    DefaultClockStyle,
		`{"clock_style":"plain"}`: "plain",
	} {
		if err = os.WriteFile(p, []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
		c, err = Load(p)
		if err != nil {
			t.Fatalf("%s: %v", body, err)
		}
		if c.ClockStyle != want {
			t.Fatalf("%s: clock style = %q, want %q", body, c.ClockStyle, want)
		}
	}
}

func TestLockConfigClockStylesMatchTheLockerNames(t *testing.T) {
	want := []string{"kompaktblk", "phm_blocky_reverse", "phmvga", "phm_slanted", "plain"}
	got := ClockStyles()
	if len(got) != len(want) {
		t.Fatalf("clock styles = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] || ClockStyle(want[i]) != want[i] {
			t.Fatalf("clock style %d = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestLockConfigNoneMatchesLockerDefault(t *testing.T) {
	if got := Default().Effect; got != "none" {
		t.Fatalf("default effect = %q, want none", got)
	}
	p := filepath.Join(t.TempDir(), "config.json")
	for _, body := range []string{`{}`, `{"effect":"none","palette":"eldritch","future":{"a":1}}`} {
		if err := os.WriteFile(p, []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
		c, err := Load(p)
		if err != nil || c.Effect != "none" {
			t.Fatalf("load %s: %+v, %v", body, c, err)
		}
		if err := Save(p, c); err != nil {
			t.Fatal(err)
		}
		again, err := Load(p)
		if err != nil || again.Effect != "none" || again.Palette != c.Palette {
			t.Fatalf("round trip: %+v, %v", again, err)
		}
	}
	if err := os.WriteFile(p, []byte(`{"effect":"none","palette":"invalid"}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(p); err == nil {
		t.Fatal("none accepted invalid palette")
	}
}

// Run with SYSC_LOCK_CONTRACT_BINARY=/path/to/sysc-lock to check the native
// producer when it changes. No network or locker session is needed.
func TestLockConfigNativeDescriptionContract(t *testing.T) {
	binary := os.Getenv("SYSC_LOCK_CONTRACT_BINARY")
	if binary == "" {
		t.Skip("set SYSC_LOCK_CONTRACT_BINARY to verify the locker presentation contract")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	data, err := exec.CommandContext(ctx, binary, "--describe").Output()
	if err != nil {
		t.Fatal(err)
	}
	var description struct {
		ClockStyles []string `json:"clock_styles"`
		Effects     []string `json:"effects"`
		Palettes    []string `json:"palettes"`
		Defaults    Config   `json:"defaults"`
	}
	if err := json.Unmarshal(data, &description); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(description.ClockStyles, ClockStyles()) {
		t.Fatalf("clock style mirror drift: %v != %v", description.ClockStyles, ClockStyles())
	}
	if !reflect.DeepEqual(description.Effects, Effects()) {
		t.Fatalf("effect mirror drift: %v != %v", description.Effects, Effects())
	}
	if !reflect.DeepEqual(description.Palettes, renderer.Palettes()) {
		t.Fatal("palette mirror drift")
	}
	c, want := description.Defaults, Default()
	if c.Effect != want.Effect || c.Palette != want.Palette || c.ClockStyle != want.ClockStyle || c.ReducedMotion != want.ReducedMotion || c.Clock24h != want.Clock24h || c.EffectFPS != want.EffectFPS || c.BlurBackdrop() != want.BlurBackdrop() || c.Backend() != want.Backend() || c.GpuPowerSave() != want.GpuPowerSave() {
		t.Fatalf("default mirror drift: %+v != %+v", c, want)
	}
}
