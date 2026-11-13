package settings

import (
	"testing"

	"github.com/Nomadcxx/sysc-shell/internal/config"
)

func TestOutputEntriesCreateAndResetOneField(t *testing.T) {
	cfg, err := config.Parse([]byte(`{"outputs":[{"connector":"DP-1","bar":{"edge":"left"}}]}`))
	if err != nil {
		t.Fatal(err)
	}
	r := DefaultFor(cfg, WithOutputs([]string{"DP-1", "HDMI-A-1"}))
	for _, conn := range []string{"DP-1", "HDMI-A-1"} {
		for _, field := range []string{"enabled", "edge", "height", "gap", "font-size"} {
			if r.ByPath("outputs."+conn+"."+field) == nil {
				t.Fatalf("missing %s/%s", conn, field)
			}
		}
	}
	e := r.ByPath("outputs.HDMI-A-1.enabled")
	if !e.IsDefault(cfg) {
		t.Fatal("unconfigured output is custom")
	}
	if len(cfg.Outputs) != 1 {
		t.Fatal("discovery wrote an override")
	}
	if err := e.Set(&cfg, "true"); err != nil {
		t.Fatal(err)
	}
	if e.IsDefault(cfg) {
		t.Fatal("explicit equal-base true is inherited")
	}
	if err := e.Reset(&cfg); err != nil {
		t.Fatal(err)
	}
	if !e.IsDefault(cfg) {
		t.Fatal("reset did not restore inheritance")
	}
	if cfg.ForConnector("DP-1").Edge != "left" {
		t.Fatal("another display changed")
	}
}

func TestOutputEntriesReuseSharedValidation(t *testing.T) {
	cfg := config.Default()
	r := DefaultFor(cfg, WithOutputs([]string{"DP-1"}))
	for _, tc := range []struct{ field, value string }{{"height", "1"}, {"gap", "-1"}, {"edge", "diagonal"}, {"font-size", "0"}, {"enabled", "yes"}} {
		e := r.ByPath("outputs.DP-1." + tc.field)
		if err := e.Set(&cfg, tc.value); err == nil {
			t.Errorf("accepted %s=%s", tc.field, tc.value)
		}
	}
	if len(cfg.Outputs) != 0 {
		t.Fatal("invalid edits created an override")
	}
}
