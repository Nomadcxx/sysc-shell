package integration

import (
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/Nomadcxx/sysc-shell/internal/config"
	"github.com/Nomadcxx/sysc-shell/internal/plugin"
	"github.com/Nomadcxx/sysc-shell/internal/shell"
)

// The screen-recorder plugin and its protocol gate live in the sysc-plugins
// repository. This gate keeps the host-side coverage (missing dependency and
// disable lifecycle) and builds the plugin from the sibling checkout, which
// defaults to ../sysc-plugins and can be overridden with SYSC_PLUGINS_DIR.

func pluginsRepoRoot(t *testing.T) string {
	t.Helper()
	root := os.Getenv("SYSC_PLUGINS_DIR")
	if root == "" {
		root = filepath.Join(repoRoot(t), "..", "sysc-plugins")
	}
	if _, err := os.Stat(filepath.Join(root, "plugins/screen-recorder/manifest.json")); err != nil {
		t.Skipf("sysc-plugins checkout not found at %s (set SYSC_PLUGINS_DIR): %v", root, err)
	}
	return root
}

func TestPluginRecorderGateMissingDependencyAndHostDisable(t *testing.T) {
	_ = builtRecorder(t)
	origPATH := os.Getenv("PATH")
	pluginsRoot := pluginsRepoRoot(t)
	pluginDir := filepath.Join(t.TempDir(), "org.sysc.screen-recorder")
	if err := os.MkdirAll(filepath.Join(pluginDir, "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	manifest, err := os.ReadFile(filepath.Join(pluginsRoot, "plugins/screen-recorder/manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pluginDir, "manifest.json"), manifest, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pluginDir, "bin", "sysc-plugin-screen-recorder"), []byte("#!/bin/true\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", t.TempDir())

	cfg := config.Default()
	cfg.Accessibility.ReducedMotion = true
	cfg.Bar.Left = nil
	cfg.Bar.Center = []config.Item{{ID: "clock", Format: "15:04", Boundary: time.Minute}}
	cfg.Bar.Right = []config.Item{{ID: "plugin", Plugin: "org.sysc.screen-recorder", Entry: "bar", Instance: "rec-1"}}
	cfg.Plugins.Enabled = []string{"org.sysc.screen-recorder"}
	reg := shell.NewRegistry(cfg)
	t.Cleanup(reg.Close)
	if err := reg.BindPlugins(shell.PluginHostOptions{
		Roots:    []plugin.Root{{Path: filepath.Dir(pluginDir), Source: plugin.SourceUser}},
		StateDir: t.TempDir(),
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := reg.NewHost(1, "DP-1"); err != nil {
		t.Fatal(err)
	}
	if got := reg.PluginPID("org.sysc.screen-recorder"); got != 0 {
		t.Fatalf("missing dependency started pid %d", got)
	}

	binDir := t.TempDir()
	installFakeGSR(t, binDir)
	pluginDir2 := filepath.Join(t.TempDir(), "org.sysc.screen-recorder")
	if err := installBuiltRecorder(t, pluginDir2); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+origPATH)
	cfg2 := config.Default()
	cfg2.Accessibility.ReducedMotion = true
	cfg2.Bar.Left = nil
	cfg2.Bar.Center = []config.Item{{ID: "clock", Format: "15:04", Boundary: time.Minute}}
	cfg2.Bar.Right = []config.Item{{ID: "plugin", Plugin: "org.sysc.screen-recorder", Entry: "bar", Instance: "rec-1"}}
	cfg2.Plugins.Enabled = []string{"org.sysc.screen-recorder"}
	cfg2.Plugins.Settings = map[string]map[string]any{
		"org.sysc.screen-recorder": {"directory": t.TempDir()},
	}
	reg2 := shell.NewRegistry(cfg2)
	t.Cleanup(reg2.Close)
	if err := reg2.BindPlugins(shell.PluginHostOptions{
		Roots:    []plugin.Root{{Path: filepath.Dir(pluginDir2), Source: plugin.SourceUser}},
		StateDir: t.TempDir(),
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := reg2.NewHost(1, "DP-1"); err != nil {
		t.Fatal(err)
	}
	if _, err := reg2.NewHost(2, "HDMI-1"); err != nil {
		t.Fatal(err)
	}
	waitViews(t, reg2, "DP-1", 1)
	waitViews(t, reg2, "HDMI-1", 1)
	pid := reg2.PluginPID("org.sysc.screen-recorder")
	if pid == 0 {
		t.Fatal("no plugin process")
	}
	if err := reg2.SetPluginEnabled("org.sysc.screen-recorder", false); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if reg2.PluginPID("org.sysc.screen-recorder") == 0 && reg2.PluginBarViews("DP-1") == 0 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("disable left pid=%d views=%d", reg2.PluginPID("org.sysc.screen-recorder"), reg2.PluginBarViews("DP-1"))
}

var (
	recorderBinOnce sync.Once
	recorderBinPath string
	recorderBinErr  error
)

func builtRecorder(t *testing.T) string {
	t.Helper()
	recorderBinOnce.Do(func() {
		dir, err := os.MkdirTemp("", "sysc-plugin-screen-recorder-")
		if err != nil {
			recorderBinErr = err
			return
		}
		recorderBinPath = filepath.Join(dir, "sysc-plugin-screen-recorder")
		cmd := exec.Command("go", "build", "-o", recorderBinPath, "./cmd/sysc-plugin-screen-recorder")
		cmd.Dir = pluginsRepoRoot(t)
		if out, err := cmd.CombinedOutput(); err != nil {
			recorderBinErr = err
			t.Logf("build recorder: %s", out)
		}
	})
	if recorderBinErr != nil {
		t.Fatal(recorderBinErr)
	}
	return recorderBinPath
}

func installBuiltRecorder(t *testing.T, pluginDir string) error {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(pluginDir, "bin"), 0o755); err != nil {
		return err
	}
	manifest, err := os.ReadFile(filepath.Join(pluginsRepoRoot(t), "plugins/screen-recorder/manifest.json"))
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(pluginDir, "manifest.json"), manifest, 0o644); err != nil {
		return err
	}
	data, err := os.ReadFile(builtRecorder(t))
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(pluginDir, "bin", "sysc-plugin-screen-recorder"), data, 0o755)
}

func installFakeGSR(t *testing.T, binDir string) {
	t.Helper()
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(binDir, "gpu-screen-recorder")
	if err := os.Symlink(self, dst); err != nil {
		t.Fatal(err)
	}
}
