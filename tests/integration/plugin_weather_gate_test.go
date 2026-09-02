package integration

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Nomadcxx/sysc-shell/internal/config"
	"github.com/Nomadcxx/sysc-shell/internal/plugin"
	"github.com/Nomadcxx/sysc-shell/internal/shell"
	v1 "github.com/Nomadcxx/sysc-shell/plugin/v1"
)

const weatherForecastBody = `{
  "current":{"temperature_2m":18.4,"weather_code":3},
  "daily":{
    "time":["2026-09-02","2026-09-03","2026-09-04","2026-09-05","2026-09-06","2026-09-07","2026-09-08"],
    "weather_code":[3,61,71,95,0,2,45],
    "temperature_2m_max":[22.1,19.0,8.5,17.0,24.0,21.0,16.0],
    "temperature_2m_min":[12.0,11.0,-1.5,9.0,13.0,12.5,10.0],
    "sunrise":["2026-09-02T06:12","2026-09-03T06:14","2026-09-04T06:16","2026-09-05T06:18","2026-09-06T06:20","2026-09-07T06:22","2026-09-08T06:24"],
    "sunset":["2026-09-02T18:44","2026-09-03T18:42","2026-09-04T18:40","2026-09-05T18:38","2026-09-06T18:36","2026-09-07T18:34","2026-09-08T18:32"]
  }
}`

func TestPluginWeatherGateNetworkAndViews(t *testing.T) {
	var calls atomic.Int32
	var mode atomic.Int32 // 0 success, 1 fail, 2 malformed, 3 stall, 4 recovered
	server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		switch mode.Load() {
		case 1:
			http.Error(rw, "unavailable", http.StatusServiceUnavailable)
		case 2:
			rw.Write([]byte("{"))
		case 3:
			<-r.Context().Done()
		case 4:
			fmt.Fprint(rw, `{"current":{"temperature_2m":21.4,"weather_code":0},"daily":{"time":["2026-09-02"],"weather_code":[0],"temperature_2m_max":[24],"temperature_2m_min":[12],"sunrise":["2026-09-02T06:12"],"sunset":["2026-09-02T18:44"]}}`)
		default:
			fmt.Fprint(rw, weatherForecastBody)
		}
	}))
	t.Cleanup(server.Close)

	h := startWeather(t, server.URL)
	h.open("bar-dp1", v1.ViewBar, "DP-1")
	h.open("bar-hdmi", v1.ViewBar, "HDMI-1")
	h.open("tip-dp1", v1.ViewTooltip, "DP-1")
	h.open("panel-dp1", v1.ViewPanel, "DP-1")

	h.waitView("bar-dp1", func(n *v1.Node) bool { return strings.Contains(treeText(n), "18") })
	h.waitView("bar-hdmi", func(n *v1.Node) bool { return strings.Contains(treeText(n), "18") })
	h.waitView("tip-dp1", func(n *v1.Node) bool { return strings.Contains(treeText(n), "Cloudy") })
	h.waitView("panel-dp1", func(n *v1.Node) bool {
		return strings.Contains(treeText(n), "06:12") && strings.Contains(treeText(n), "22")
	})
	if got := calls.Load(); got != 1 {
		t.Fatalf("fetches = %d, want 1 owner for every view", got)
	}

	if err := h.send(&v1.ViewClose{ViewID: "tip-dp1"}); err != nil {
		t.Fatal(err)
	}
	h.waitView("bar-dp1", func(n *v1.Node) bool { return strings.Contains(treeText(n), "18") })

	before := h.snapCount.Load()
	time.Sleep(200 * time.Millisecond)
	if got := h.snapCount.Load(); got != before {
		t.Fatalf("unchanged weather redrew: snapshots %d -> %d", before, got)
	}

	mode.Store(1)
	h.waitView("bar-dp1", func(n *v1.Node) bool {
		return strings.Contains(treeText(n), "18") && strings.Contains(treeText(n), "(")
	})

	mode.Store(2)
	time.Sleep(150 * time.Millisecond)
	if !strings.Contains(treeText(h.rootOf("bar-dp1")), "18") {
		t.Fatalf("malformed response dropped last good: %s", treeText(h.rootOf("bar-dp1")))
	}

	mode.Store(4)
	h.waitView("bar-dp1", func(n *v1.Node) bool { return strings.Contains(treeText(n), "21") })
}

func TestPluginWeatherGateTimeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	t.Cleanup(server.Close)
	h := startWeather(t, server.URL)
	h.open("bar-1", v1.ViewBar, "DP-1")
	h.waitView("bar-1", func(n *v1.Node) bool {
		return strings.Contains(treeText(n), "unavailable")
	})
}

func TestPluginWeatherGateTwoOutputsShareOneProcess(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		fmt.Fprint(rw, weatherForecastBody)
	}))
	t.Cleanup(server.Close)
	t.Setenv("SYSC_WEATHER_ENDPOINT", server.URL)

	root := repoRoot(t)
	pluginDir := filepath.Join(t.TempDir(), "org.sysc.weather")
	bin := filepath.Join(pluginDir, "bin", "sysc-plugin-weather")
	if err := os.MkdirAll(filepath.Dir(bin), 0o755); err != nil {
		t.Fatal(err)
	}
	manifest, err := os.ReadFile(filepath.Join(root, "plugins/reference/weather/manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pluginDir, "manifest.json"), manifest, 0o644); err != nil {
		t.Fatal(err)
	}
	build := exec.Command("go", "build", "-o", bin, "./cmd/sysc-plugin-weather")
	build.Dir = root
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build weather: %v\n%s", err, out)
	}

	cfg := config.Default()
	cfg.Accessibility.ReducedMotion = true
	cfg.Bar.Left = nil
	cfg.Bar.Center = []config.Item{{ID: "clock", Format: "15:04", Boundary: time.Minute}}
	cfg.Bar.Right = []config.Item{{ID: "plugin", Plugin: "org.sysc.weather", Entry: "bar", Instance: "weather-1"}}
	cfg.Plugins.Enabled = []string{"org.sysc.weather"}
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
	if _, err := reg.NewHost(2, "HDMI-1"); err != nil {
		t.Fatal(err)
	}
	waitViews(t, reg, "DP-1", 1)
	waitViews(t, reg, "HDMI-1", 1)
	pid := reg.PluginPID("org.sysc.weather")
	if pid == 0 {
		t.Fatal("no plugin process")
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) && calls.Load() == 0 {
		time.Sleep(20 * time.Millisecond)
	}
	time.Sleep(100 * time.Millisecond)
	if got := calls.Load(); got != 1 {
		t.Fatalf("fetches = %d, want 1 shared owner", got)
	}
	if got := reg.PluginPID("org.sysc.weather"); got != pid {
		t.Fatalf("pid changed from %d to %d", pid, got)
	}
}

type weatherHost struct {
	t         *testing.T
	enc       *v1.Encoder
	mu        sync.Mutex
	roots     map[string]*v1.Node
	snapCount atomic.Int32
	wake      chan struct{}
}

func startWeather(t *testing.T, endpoint string) *weatherHost {
	t.Helper()
	root := repoRoot(t)
	pluginDir := filepath.Join(t.TempDir(), "org.sysc.weather")
	bin := filepath.Join(pluginDir, "bin", "sysc-plugin-weather")
	if err := os.MkdirAll(filepath.Dir(bin), 0o755); err != nil {
		t.Fatal(err)
	}
	manifest, err := os.ReadFile(filepath.Join(root, "plugins/reference/weather/manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pluginDir, "manifest.json"), manifest, 0o644); err != nil {
		t.Fatal(err)
	}
	build := exec.Command("go", "build", "-o", bin, "./cmd/sysc-plugin-weather")
	build.Dir = root
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build weather: %v\n%s", err, out)
	}
	cmd := exec.Command(bin)
	cmd.Env = append(os.Environ(), "SYSC_WEATHER_ENDPOINT="+endpoint)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if t.Failed() {
			t.Logf("plugin stderr:\n%s", stderr.String())
		}
	})
	h := &weatherHost{t: t, enc: v1.NewEncoder(stdin), roots: map[string]*v1.Node{}, wake: make(chan struct{}, 1)}
	dec := v1.NewDecoder(stdout, v1.ToHost)
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	t.Cleanup(func() {
		_ = h.send(&v1.HostShutdown{})
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			_ = cmd.Process.Kill()
			<-done
		}
		_ = stdin.Close()
	})
	go func() {
		for {
			msg, err := dec.Decode()
			if err != nil {
				return
			}
			switch m := msg.(type) {
			case *v1.HostCall:
				_ = h.send(&v1.HostReply{ID: m.ID, OK: true})
			case *v1.ViewSnapshot:
				h.mu.Lock()
				h.roots[m.ViewID] = m.Root
				h.mu.Unlock()
				h.snapCount.Add(1)
				select {
				case h.wake <- struct{}{}:
				default:
				}
			}
		}
	}()
	if err := h.send(&v1.HostHello{
		Supported:    []v1.Version{{Major: 1, Minor: 0}},
		Plugin:       v1.Identity{ID: "org.sysc.weather", Name: "Weather", Version: "1.0.0"},
		Capabilities: []string{"panels", "settings"},
		Limits:       v1.DefaultLimits,
	}); err != nil {
		t.Fatal(err)
	}
	if err := h.send(&v1.SettingsChanged{Scope: v1.ScopePlugin, Values: map[string]any{
		"enabled": true, "latitude": 51.5, "longitude": -0.13, "unit": "celsius", "interval": "50ms",
		"bar_temperature": true, "bar_unit": true, "bar_icon": true, "bar_condition": true,
		"tooltip_mode": "current",
	}}); err != nil {
		t.Fatal(err)
	}
	return h
}

func (h *weatherHost) send(m v1.Message) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.enc.Encode(m)
}

func (h *weatherHost) open(id string, kind v1.ViewKind, output string) {
	h.t.Helper()
	if err := h.send(&v1.ViewOpen{ViewID: id, View: kind, Entry: "bar", Output: output, Width: 280, Height: 200}); err != nil {
		h.t.Fatal(err)
	}
}

func (h *weatherHost) rootOf(id string) *v1.Node {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.roots[id]
}

func (h *weatherHost) waitView(id string, ok func(*v1.Node) bool) *v1.Node {
	h.t.Helper()
	deadline := time.Now().Add(12 * time.Second)
	for time.Now().Before(deadline) {
		root := h.rootOf(id)
		if root != nil && ok(root) {
			return root
		}
		wait := time.Until(deadline)
		if wait > 50*time.Millisecond {
			wait = 50 * time.Millisecond
		}
		select {
		case <-h.wake:
		case <-time.After(wait):
		}
	}
	h.t.Fatalf("view %s never matched\n%s", id, dumpTree(h.rootOf(id)))
	return nil
}

func treeText(n *v1.Node) string {
	if n == nil {
		return ""
	}
	parts := []string{n.Text, n.Icon, n.Name}
	for _, c := range n.Children {
		parts = append(parts, treeText(c))
	}
	return strings.Join(parts, " ")
}
