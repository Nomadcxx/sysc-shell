package integration

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Nomadcxx/sysc-shell/internal/config"
	"github.com/Nomadcxx/sysc-shell/internal/plugin"
	"github.com/Nomadcxx/sysc-shell/internal/shell"
)

func TestMain(m *testing.M) {
	if os.Getenv("SYSC_FAKE_RECORDER") == "1" {
		os.Exit(runGateFakeRecorder())
	}
	if len(os.Args) > 1 && os.Args[1] == plugin.HelperFlag {
		os.Exit(plugin.HelperServe(os.Args[2:]))
	}
	os.Exit(m.Run())
}

const gateTimerManifest = `{
  "schema": 1,
  "id": "org.sysc.timer",
  "name": "Timer",
  "version": "1.0.0",
  "protocol": {"major": 1, "minor": 0},
  "exec": "bin/sysc-plugin-timer",
  "capabilities": ["notifications", "panels", "settings", "state"],
  "requires": {"commands": []},
  "services": [{"id": "timer"}],
  "widgets": [{"id": "bar", "settings": []}],
  "panels": [{"id": "panel", "width": 320, "height": 280, "placement": "attached"}],
  "settings": []
}`

func bindGate(t *testing.T, mode string, enable bool) *shell.Registry {
	t.Helper()
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	if _, err := plugin.WriteHelperPlugin(filepath.Join(root, "org.sysc.timer"), self, mode, gateTimerManifest); err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	cfg.Accessibility.ReducedMotion = true
	cfg.Bar.Left = nil
	cfg.Bar.Center = []config.Item{{ID: "clock", Format: "15:04", Boundary: time.Minute}}
	cfg.Bar.Right = []config.Item{{
		ID: "plugin", Plugin: "org.sysc.timer", Entry: "bar", Instance: "timer-1",
	}}
	if enable {
		cfg.Plugins.Enabled = []string{"org.sysc.timer"}
	}
	reg := shell.NewRegistry(cfg)
	t.Cleanup(reg.Close)
	if err := reg.BindPlugins(shell.PluginHostOptions{
		Roots:    []plugin.Root{{Path: root, Source: plugin.SourceUser}},
		StateDir: t.TempDir(),
	}); err != nil {
		t.Fatal(err)
	}
	return reg
}

func waitViews(t *testing.T, reg *shell.Registry, output string, n int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if reg.PluginBarViews(output) == n {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("views on %s = %d, want %d", output, reg.PluginBarViews(output), n)
}

func TestPluginHostGateTwoOutputsShareOneProcess(t *testing.T) {
	reg := bindGate(t, "ok", true)
	if _, err := reg.NewHost(1, "DP-1"); err != nil {
		t.Fatal(err)
	}
	if _, err := reg.NewHost(2, "HDMI-1"); err != nil {
		t.Fatal(err)
	}
	waitViews(t, reg, "DP-1", 1)
	waitViews(t, reg, "HDMI-1", 1)
	pid := reg.PluginPID("org.sysc.timer")
	if pid == 0 {
		t.Fatal("no plugin process")
	}
	reg.DropHost(2)
	waitViews(t, reg, "HDMI-1", 0)
	if got := reg.PluginPID("org.sysc.timer"); got != pid {
		t.Fatalf("pid after dropping one output = %d, want %d", got, pid)
	}
	if err := reg.SetPluginEnabled("org.sysc.timer", false); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if reg.PluginPID("org.sysc.timer") == 0 && reg.PluginBarViews("DP-1") == 0 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("disable left pid=%d views=%d", reg.PluginPID("org.sysc.timer"), reg.PluginBarViews("DP-1"))
}

func TestPluginHostGateFailuresLeaveClockPlacement(t *testing.T) {
	now := time.Date(2026, 9, 1, 15, 4, 0, 0, time.UTC)
	for _, mode := range []string{"bad-view", "crash-after-hello", "garbage", "silent"} {
		t.Run(mode, func(t *testing.T) {
			reg := bindGate(t, mode, true)
			if _, err := reg.NewHost(1, "DP-1"); err != nil {
				t.Fatal(err)
			}
			if changed := reg.UpdateClock(now); len(changed) == 0 {
				t.Fatal("clock update produced no bar change")
			}
			if _, err := reg.NewHost(2, "HDMI-1"); err != nil {
				t.Fatal(err)
			}
			if changed := reg.UpdateClock(now.Add(time.Minute)); len(changed) == 0 {
				t.Fatal("second clock update produced no bar change")
			}
		})
	}
}
