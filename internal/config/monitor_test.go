package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMonitorDefaults(t *testing.T) {
	m := Default().Monitor
	want := Monitor{Refresh: 1, SortBackground: "surface_variant", SortColor: "on_surface_variant",
		HoverBackground: "surface_variant", HoverColor: "on_surface_variant", ShowApps: true, ShowProcesses: true}
	if m != want {
		t.Fatalf("Default().Monitor = %+v, want %+v", m, want)
	}
}

func TestMonitorBlockValidates(t *testing.T) {
	tests := []struct {
		name, json, errPart string
	}{
		{"refresh low", `{"monitor":{"refresh":0}}`, "monitor.refresh"},
		{"refresh high", `{"monitor":{"refresh":11}}`, "monitor.refresh"},
		{"unknown role", `{"monitor":{"hover_color":"magenta"}}`, "monitor.hover_color"},
		{"hex is not a role", `{"monitor":{"sort_column_background":"#ff00ff"}}`, "monitor.sort_column_background"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Parse([]byte(tt.json))
			if err == nil || !strings.Contains(err.Error(), tt.errPart) {
				t.Fatalf("err = %v, want mention of %s", err, tt.errPart)
			}
		})
	}
}

func TestMonitorBlockParses(t *testing.T) {
	c, err := Parse([]byte(`{"monitor":{"refresh":4,"hover_color":"primary","show_processes":false}}`))
	if err != nil {
		t.Fatal(err)
	}
	want := Default().Monitor
	want.Refresh, want.HoverColor, want.ShowProcesses = 4, "primary", false
	if c.Monitor != want {
		t.Fatalf("Monitor = %+v, want %+v", c.Monitor, want)
	}
}

func TestMonitorBlockRoundTrips(t *testing.T) {
	c := Default()
	c.Monitor.Refresh, c.Monitor.HoverColor, c.Monitor.ShowApps = 3, "on_primary_container", false
	path := filepath.Join(t.TempDir(), "config.json")
	if err := Write(path, c); err != nil {
		t.Fatal(err)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.Monitor != c.Monitor {
		t.Fatalf("round trip = %+v, want %+v", got.Monitor, c.Monitor)
	}
}

func TestDefaultMonitorWritesNoBlock(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := Write(path, Default()); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), `"monitor"`) {
		t.Fatalf("default config wrote a monitor block:\n%s", raw)
	}
}

func TestWriteRefusesAnUnknownMonitorRole(t *testing.T) {
	c := Default()
	c.Monitor.SortColor = "magenta"
	if err := Write(filepath.Join(t.TempDir(), "config.json"), c); err == nil {
		t.Fatal("Write accepted an unknown colour role")
	}
}

func TestAConfigWithoutAMonitorBlockGetsTheDefaults(t *testing.T) {
	c, err := Parse([]byte(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	if c.Monitor != Default().Monitor {
		t.Fatalf("Monitor = %+v", c.Monitor)
	}
}
