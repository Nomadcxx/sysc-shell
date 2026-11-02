package settings

import (
	"strings"
	"testing"

	"github.com/Nomadcxx/sysc-shell/internal/config"
)

func TestInputPageValuesAndVisibility(t *testing.T) {
	speed := 0.3
	effective := config.Input{Touchpad: config.Pointer{AccelSpeed: &speed}}
	cfg := config.Default()
	r := DefaultFor(cfg, WithInput(effective, true, ""))
	e := r.ByPath("input.touchpad.accel-speed")
	if e == nil || e.Page != "Mouse & touchpad" || e.Get(cfg) != "30" {
		t.Fatalf("speed entry = %+v", e)
	}
	if err := e.Set(&cfg, "-25"); err != nil {
		t.Fatal(err)
	}
	if cfg.Input.Touchpad.AccelSpeed == nil || *cfg.Input.Touchpad.AccelSpeed != -0.25 {
		t.Fatalf("set speed = %+v", cfg.Input.Touchpad)
	}
	if cfg.Input.Touchpad.Tap != nil {
		t.Fatal("reading effective values marked another setting changed")
	}
	if DefaultFor(cfg, WithInput(effective, false, "")).ByPath("input.touchpad.tap") != nil {
		t.Fatal("touchpad shown without hardware")
	}
	if DefaultFor(cfg, WithInput(effective, false, "")).ByPath("input.mouse.natural-scroll") == nil {
		t.Fatal("mouse hidden")
	}
}

func TestInputPageRefusesUnavailableConfig(t *testing.T) {
	cfg := config.Default()
	reason := "Needs Niri 26.04 or sysc-compositor"
	e := DefaultFor(cfg, WithInput(config.Input{}, true, reason)).ByPath("input.touchpad.tap")
	if e == nil || !strings.Contains(e.Describe, reason) {
		t.Fatalf("missing refusal: %+v", e)
	}
	if err := e.Set(&cfg, "true"); err == nil {
		t.Fatal("blocked page accepted a write")
	}
	if cfg.Input.Touchpad.Tap != nil {
		t.Fatal("blocked page changed config")
	}
}
