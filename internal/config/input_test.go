package config

import (
	"bytes"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestInputConfigRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	cfg := Default()
	if err := Write(path, cfg); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), `"input"`) {
		t.Fatalf("unset input was written: %s", data)
	}
	tap := true
	cfg.Input.Touchpad.Tap = &tap
	if err := Write(path, cfg); err != nil {
		t.Fatal(err)
	}
	data, err = os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var wire map[string]json.RawMessage
	if err := json.Unmarshal(data, &wire); err != nil {
		t.Fatal(err)
	}
	var compact bytes.Buffer
	if err := json.Compact(&compact, wire["input"]); err != nil {
		t.Fatal(err)
	}
	if compact.String() != `{"touchpad":{"tap":true}}` {
		t.Fatalf("input = %s", compact.String())
	}
	got, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got.Input, cfg.Input) {
		t.Fatalf("input round trip: got %+v want %+v", got.Input, cfg.Input)
	}
	falseValue := false
	cfg.Input.Mouse.NaturalScroll = &falseValue
	if err := Write(path, cfg); err != nil {
		t.Fatal(err)
	}
	got, err = Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.Input.Mouse.NaturalScroll == nil || *got.Input.Mouse.NaturalScroll {
		t.Fatal("explicit false was lost")
	}
	speed := 1.5
	cfg.Input.Touchpad.AccelSpeed = &speed
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := Write(path, cfg); err == nil {
		t.Fatal("out-of-range acceleration was written")
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatal("invalid input replaced config")
	}
}

func TestInputConfigValidation(t *testing.T) {
	for _, raw := range []string{
		`{"touchpad":{"accel-speed":1.5}}`,
		`{"mouse":{"accel-speed":-1.01}}`,
		`{"touchpad":{"scroll-factor":-1}}`,
		`{"mouse":{"scroll-factor":101}}`,
		`{"touchpad":{"accel-profile":"bad"}}`,
		`{"touchpad":{"scroll-method":"bad"}}`,
		`{"touchpad":{"click-method":"bad"}}`,
		`{"touchpad":{"tap":1}}`,
		`{"touchpad":{"unknown":true}}`,
		`{"mouse":{"tap":true}}`,
		`{"mouse":{"tap":false}}`,
		`{"mouse":{"dwt":true}}`,
		`{"mouse":{"disabled-on-external-mouse":false}}`,
		`{"mouse":{"click-method":"button-areas"}}`,
	} {
		t.Run(raw, func(t *testing.T) {
			if _, err := Parse([]byte(`{"input":` + raw + `}`)); err == nil {
				t.Fatalf("accepted %s", raw)
			}
		})
	}
	for _, value := range []float64{math.NaN(), math.Inf(1), math.Inf(-1)} {
		if err := ValidateInput(Input{Touchpad: Pointer{AccelSpeed: &value}}); err == nil {
			t.Fatalf("accepted accel-speed %v", value)
		}
		if err := ValidateInput(Input{Mouse: Pointer{ScrollFactor: &value}}); err == nil {
			t.Fatalf("accepted scroll-factor %v", value)
		}
	}
	for _, raw := range []string{
		`{"touchpad":{"accel-speed":-1,"scroll-factor":0,"accel-profile":"flat","scroll-method":"two-finger","click-method":"clickfinger"}}`,
		`{"mouse":{"accel-speed":1,"scroll-factor":100,"accel-profile":"adaptive","scroll-method":"on-button-down"}}`,
		`{"touchpad":{"click-method":"button-areas"}}`,
		`{"touchpad":{"scroll-method":"no-scroll"}}`,
		`{"touchpad":{"scroll-method":"edge"}}`,
	} {
		if _, err := Parse([]byte(`{"input":` + raw + `}`)); err != nil {
			t.Fatalf("valid input %s: %v", raw, err)
		}
	}
}

func TestInputConfigClone(t *testing.T) {
	b, profile, method, click, f := true, "flat", "two-finger", "clickfinger", 0.2
	cfg := Default()
	cfg.Input.Touchpad = Pointer{NaturalScroll: &b, LeftHanded: &b, Tap: &b, DWT: &b, DisabledOnExternalMouse: &b, AccelSpeed: &f, ScrollFactor: &f, AccelProfile: &profile, ScrollMethod: &method, ClickMethod: &click}
	cfg.Input.Mouse = Pointer{NaturalScroll: &b, LeftHanded: &b, AccelSpeed: &f, ScrollFactor: &f, AccelProfile: &profile, ScrollMethod: &method}
	copy := cfg.Clone()
	original, cloned := map[string]uintptr{}, map[string]uintptr{}
	collectMutable(reflect.ValueOf(cfg.Input), "Input", original)
	collectMutable(reflect.ValueOf(copy.Input), "Input", cloned)
	for path, ptr := range original {
		if ptr == cloned[path] {
			t.Errorf("%s shares storage", path)
		}
	}
	if !reflect.DeepEqual(copy.Input, cfg.Input) {
		t.Fatal("clone changed input")
	}
}
