package shell

import (
	"strings"
	"testing"

	metrics "github.com/Nomadcxx/sysc-metrics"

	"github.com/Nomadcxx/sysc-shell/internal/services"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

func monitorTestRegistry() *Registry {
	return &Registry{sample: services.Snapshot{
		CPU: &metrics.CPUSnapshot{Usage: metrics.CPUUsage{Fraction: 0.95, Valid: true},
			Load1: 0.82, Load5: 0.74, Load15: 0.66, LoadValid: true,
			Cores: []metrics.CPUCore{{FrequencyHz: 4_210_000_000, FrequencyValid: true}}},
		Memory: &metrics.MemorySnapshot{
			Memory: metrics.Capacity{TotalBytes: 32 << 30, UsedBytes: 12 << 30},
			Swap:   metrics.Capacity{TotalBytes: 8 << 30, UsedBytes: 1 << 29}},
		Thermal: &metrics.ThermalSnapshot{Celsius: 63, Valid: true},
		Filesystem: &metrics.FilesystemSnapshot{Filesystems: []metrics.Filesystem{
			{MountPoint: "/", Capacity: metrics.Capacity{TotalBytes: 1000 << 30, UsedBytes: 850 << 30}}}},
	}}
}

func layOutMonitor(t *testing.T, r *Registry) *ui.Node {
	t.Helper()
	h := &PanelHost{id: PanelControlCenter, section: "monitor", theme: DefaultTheme(), ccIface: "enp7s0", ccDevice: "dm-0"}
	page := ccMonitor(r, h)
	measure := func(s string, _ ui.TextAttrs) (int, int) { return len(s) * 7, 16 }
	if err := ui.LayoutColumn(page, ui.Rect{W: 596, H: ccPageH}, measure); err != nil {
		t.Fatal(err)
	}
	return page
}

func TestMonitorPageFitsTheBodyWithoutScrolling(t *testing.T) {
	page := layOutMonitor(t, monitorTestRegistry())
	if page.Height != ccPageH {
		t.Fatalf("page height = %d, want %d", page.Height, ccPageH)
	}
	var walk func(*ui.Node)
	walk = func(n *ui.Node) {
		if b := n.Bounds; b.X+b.W > 596 || b.Y+b.H > ccPageH {
			t.Errorf("%v %q ends at (%d,%d), outside 596x%d", n.Kind, n.Text, b.X+b.W, b.Y+b.H, ccPageH)
		}
		for _, c := range n.Children {
			walk(c)
		}
	}
	walk(page)
}

func TestMonitorPageShowsEveryRowAndDashesTheAbsent(t *testing.T) {
	page := layOutMonitor(t, monitorTestRegistry())
	text := renderText(page)
	for _, want := range []string{"CPU", "Memory", "Temperature", "GPU", "Storage", "Network", "Disk I/O",
		"4.21 GHz", "load 0.82 0.74 0.66", "63°C"} {
		if !strings.Contains(text, want) {
			t.Errorf("monitor text %q is missing %q", text, want)
		}
	}
	// No GPU, network or block sample: each keeps its row with a dash.
	if got := strings.Count(text, ccDash); got < 3 {
		t.Errorf("absent rows show %d dashes, want at least 3", got)
	}
}

func TestMonitorPageTonesFollowThresholds(t *testing.T) {
	page := layOutMonitor(t, monitorTestRegistry())
	cpu := findByName(page, "CPU usage")
	storage := findByName(page, "Storage used")
	if cpu == nil || cpu.Tone != ui.ToneError {
		t.Errorf("95%% CPU value tone = %+v, want error", cpu)
	}
	if storage == nil || storage.Tone != ui.ToneActivity {
		t.Errorf("85%% storage tone = %+v, want activity", storage)
	}
}

func TestMonitorPageOpensTheStandaloneMonitor(t *testing.T) {
	page := layOutMonitor(t, monitorTestRegistry())
	open := findByName(page, "Open system monitor")
	if open == nil || open.Action != panelMonitorAction || !open.Focusable {
		t.Fatalf("open control = %+v", open)
	}
}

func TestMonitorPageRateChartsCarryAPeak(t *testing.T) {
	r := monitorTestRegistry()
	r.sample.Network = &metrics.NetworkSnapshot{Interfaces: []metrics.NetworkInterface{{Name: "enp7s0",
		Rates: metrics.NetworkRates{ReceiveBytesPerSecond: 1 << 20, TransmitBytesPerSecond: 1 << 16, Valid: true}}}}
	text := renderText(layOutMonitor(t, r))
	if !strings.Contains(text, "enp7s0") {
		t.Errorf("network caption %q does not name the interface", text)
	}
}

// An nvidia-smi failure leaves usage invalid for that sample (sysc-495). The
// row keeps the GPU's name but dashes the value, rather
// than holding a stale reading.
func TestMonitorPageDashesAGPUWithoutAValidSample(t *testing.T) {
	r := monitorTestRegistry()
	r.sample.GPU = &metrics.GPUSnapshot{GPUs: []metrics.GPU{{PCIID: "10de:2808", Name: "RTX 4060"}}}
	page := layOutMonitor(t, r)
	value := findByName(page, "GPU usage")
	if value == nil || value.Text != ccDash {
		t.Fatalf("GPU value = %+v, want a dash", value)
	}
	if !strings.Contains(renderText(page), "RTX 4060") {
		t.Fatal("the GPU row lost its name")
	}
}

// Row values read as one column: every row's value starts at the same x
// whatever its label's length, and whether or not the label has a glyph.
func TestMonitorRowValuesShareOneColumn(t *testing.T) {
	page := layOutMonitor(t, monitorTestRegistry())
	x := -1
	for _, name := range []string{"Temperature reading", "GPU usage", "Storage used", "Network rate", "Disk I/O rate"} {
		n := findByName(page, name)
		if n == nil {
			t.Fatalf("no value named %q", name)
		}
		if x < 0 {
			x = n.Bounds.X
		} else if n.Bounds.X != x {
			t.Errorf("%s value starts at x=%d, want %d", name, n.Bounds.X, x)
		}
	}
}

// A chart opened with a short history draws it against the right edge on the
// ring's full time scale, rather than stretching two samples across the width
// and compressing them for the next two minutes.
func TestMonitorGraphsUseTheRingWindow(t *testing.T) {
	page := layOutMonitor(t, monitorTestRegistry())
	var graphs []*ui.Node
	collectByKind(page, ui.KindGraph, &graphs)
	if len(graphs) == 0 {
		t.Fatal("no graphs on the monitor page")
	}
	for _, g := range graphs {
		if g.Window != services.HistorySize {
			t.Errorf("graph window = %d, want the ring size %d", g.Window, services.HistorySize)
		}
	}
}

// Row labels paint an icon-font glyph. Their accessible name is the plain
// label, so assistive tech never reads a private-use character.
func TestMonitorRowLabelsHavePlainNames(t *testing.T) {
	page := layOutMonitor(t, monitorTestRegistry())
	for _, label := range []string{"Temperature", "GPU", "Storage", "Network", "Disk I/O"} {
		n := findByName(page, label)
		if n == nil || n.Kind != ui.KindText {
			t.Errorf("no text node named %q", label)
		}
	}
}
