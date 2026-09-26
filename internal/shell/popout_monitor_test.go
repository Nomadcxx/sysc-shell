package shell

import (
	"bytes"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/Nomadcxx/sysc-metrics"

	"github.com/Nomadcxx/sysc-shell/internal/services"
	"github.com/Nomadcxx/sysc-shell/internal/theme"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

func TestSelectGPUUsesAStableIdentity(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		gpus   []metrics.GPU
		want   services.Selector
		wantOK bool
	}{
		{
			name:   "one named device",
			gpus:   []metrics.GPU{{PCIID: "0000:02:00.0", Name: "GPU B"}},
			want:   services.Selector{Source: services.SourceGPU, Subject: "0000:02:00.0"},
			wantOK: true,
		},
		{
			name:   "one unnamed device",
			gpus:   []metrics.GPU{{Name: "integrated"}},
			want:   services.Selector{Source: services.SourceGPU},
			wantOK: true,
		},
		{
			name: "multiple devices sort by PCI ID",
			gpus: []metrics.GPU{
				{PCIID: "0000:03:00.0", Name: "GPU C"},
				{PCIID: "0000:01:00.0", Name: "GPU A"},
			},
			want:   services.Selector{Source: services.SourceGPU, Subject: "0000:01:00.0"},
			wantOK: true,
		},
		{
			name: "unique PCI identity beats an unidentified device",
			gpus: []metrics.GPU{
				{PCIID: "0000:02:00.0", Name: "GPU B"},
				{Name: "integrated"},
			},
			want:   services.Selector{Source: services.SourceGPU, Subject: "0000:02:00.0"},
			wantOK: true,
		},
		{
			name:   "multiple unnamed devices are ambiguous",
			gpus:   []metrics.GPU{{Name: "integrated A"}, {Name: "integrated B"}},
			want:   services.Selector{Source: services.SourceGPU},
			wantOK: false,
		},
		{
			name: "duplicate identities are ambiguous",
			gpus: []metrics.GPU{
				{PCIID: "0000:01:00.0", Name: "GPU A"},
				{PCIID: "0000:01:00.0", Name: "GPU B"},
			},
			want:   services.Selector{Source: services.SourceGPU},
			wantOK: false,
		},
		{
			name: "duplicate non-primary identity is ambiguous",
			gpus: []metrics.GPU{
				{PCIID: "0000:01:00.0", Name: "GPU A"},
				{PCIID: "0000:02:00.0", Name: "GPU B"},
				{PCIID: "0000:02:00.0", Name: "GPU B duplicate"},
			},
			want:   services.Selector{Source: services.SourceGPU},
			wantOK: false,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			gpus := append([]metrics.GPU(nil), test.gpus...)
			snap := services.Snapshot{GPU: &metrics.GPUSnapshot{GPUs: gpus}}
			got, ok := selectGPU(snap)
			if ok != test.wantOK || got != test.want {
				t.Fatalf("selectGPU = %v/%v, want %v/%v", got, ok, test.want, test.wantOK)
			}
			for i := range gpus {
				if snap.GPU.GPUs[i] != gpus[i] {
					t.Fatalf("selection reordered snapshot at %d: got %+v, want %+v", i, snap.GPU.GPUs[i], gpus[i])
				}
			}
		})
	}
}

func TestSelectedGPUFractionPreservesValidity(t *testing.T) {
	t.Parallel()
	snap := services.Snapshot{GPU: &metrics.GPUSnapshot{GPUs: []metrics.GPU{
		{PCIID: "0000:01:00.0", Usage: metrics.GPUUsage{Fraction: 0, Valid: true}},
		{PCIID: "0000:02:00.0", Usage: metrics.GPUUsage{Fraction: .75, Valid: true}},
	}}}
	sel, ok := selectGPU(snap)
	if !ok {
		t.Fatal("selectGPU reported a valid multi-GPU snapshot as unavailable")
	}
	value, ok := snap.Fraction(sel)
	if !ok || value != 0 {
		t.Fatalf("selected zero GPU fraction = %v/%v, want 0/true", value, ok)
	}
	snap.GPU.GPUs[0].Usage.Valid = false
	if _, ok := snap.Fraction(sel); ok {
		t.Fatal("invalid GPU usage reported as valid")
	}
}

func TestMonitorConfigureAcceptsTabsAndGraph(t *testing.T) {
	t.Parallel()
	reg := newPanelRegistry(t)
	if err := reg.OpenPanel(PanelMonitor, 7, Trigger{}); err != nil {
		t.Fatal(err)
	}
	_ = drainAux(t, reg, 2)
	h := reg.panelHosts[PanelMonitor]
	size := panelTargetSize(PanelMonitor)
	if err := h.configure(size.W, size.H, 120); err != nil {
		t.Fatal(err)
	}
}

func TestMonitorLeaseReusesM3Service(t *testing.T) {
	t.Parallel()
	src, err := os.ReadFile("popout_monitor.go")
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(src, []byte("sysc-metrics")) {
		t.Fatal("popout_monitor.go must not import sysc-metrics")
	}
	reg := newPanelRegistry(t)
	if err := reg.OpenPanel(PanelMonitor, 7, Trigger{}); err != nil {
		t.Fatal(err)
	}
	_ = drainAux(t, reg, 2)
	if !reg.Metrics().Running() {
		t.Fatal("opening the monitor did not lease Registry.Metrics")
	}
	starts := reg.Metrics().Starts()
	reg.ClosePanel(PanelMonitor)
	if reg.Metrics().Running() {
		t.Fatal("closing left a second sampler running")
	}
	if starts != 1 {
		t.Fatalf("starts = %d, want 1 shared service", starts)
	}
}

func treeHasText(n *ui.Node, text string) bool {
	if n == nil {
		return false
	}
	if n.Text == text {
		return true
	}
	if n.ValueText == text {
		return true
	}
	for _, c := range n.Children {
		if treeHasText(c, text) {
			return true
		}
	}
	return false
}

func findKind(n *ui.Node, k ui.Kind) *ui.Node {
	if n == nil {
		return nil
	}
	if n.Kind == k {
		return n
	}
	for _, c := range n.Children {
		if got := findKind(c, k); got != nil {
			return got
		}
	}
	return nil
}

func floatSliceEqual(a, b []float64) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestParseCPUModel(t *testing.T) {
	t.Parallel()
	got := parseCPUModel("processor\t: 0\nvendor_id\t: AuthenticAMD\nmodel name\t: AMD Ryzen 9 9950X 16-Core Processor\n")
	if got != "AMD Ryzen 9 9950X 16-Core Processor" {
		t.Fatalf("got %q", got)
	}
}

func TestParseOSReleasePrettyName(t *testing.T) {
	t.Parallel()
	got := parseOSRelease(`NAME="Arch Linux"
PRETTY_NAME="Arch Linux"
ID=arch
`)
	if got != "Arch Linux" {
		t.Fatalf("got %q", got)
	}
}

func TestFormatUptime(t *testing.T) {
	t.Parallel()
	cases := []struct {
		d    time.Duration
		want string
	}{
		{3 * time.Minute, "3 minutes"},
		{2*time.Hour + 15*time.Minute, "2 hours 15 minutes"},
		{50*time.Hour + 10*time.Minute, "2 days 2 hours"},
		{4*time.Hour + 59*time.Minute, "4 hours 59 minutes"},
	}
	for _, tc := range cases {
		if got := formatUptime(tc.d); got != tc.want {
			t.Fatalf("formatUptime(%v) = %q, want %q", tc.d, got, tc.want)
		}
	}
}

func legendRowHolds(n *ui.Node, text string) bool {
	for _, row := range findAllKind(n, ui.KindRow) {
		if len(row.Children) == 0 {
			continue
		}
		// A legend sits under a graph: every child is a figure, none a card.
		if row.Children[0].Kind == ui.KindCapsule {
			continue
		}
		if treeHasText(row, text) {
			return true
		}
	}
	return false
}

func treeHasName(n *ui.Node, name string) bool {
	if n == nil {
		return false
	}
	if n.Name == name {
		return true
	}
	for _, c := range n.Children {
		if treeHasName(c, name) {
			return true
		}
	}
	return false
}

func findAllKind(n *ui.Node, kind ui.Kind) []*ui.Node {
	if n == nil {
		return nil
	}
	var out []*ui.Node
	if n.Kind == kind {
		out = append(out, n)
	}
	for _, c := range n.Children {
		out = append(out, findAllKind(c, kind)...)
	}
	for i := 0; n.Item != nil && i < n.ItemCount; i++ {
		out = append(out, findAllKind(n.Item(i), kind)...)
	}
	return out
}

func TestMonitorSurfaceHeightCoversATallTree(t *testing.T) {
	t.Parallel()
	tree := &ui.Node{Kind: ui.KindColumn, Gap: theme.MarginS}
	for i := 0; i < 30; i++ {
		tree.Children = append(tree.Children, &ui.Node{Kind: ui.KindText, Text: fmt.Sprintf("row %d", i)})
	}
	measure := func(s string, _ ui.TextAttrs) (int, int) { return len([]rune(s)) * 8, 20 }
	content, err := ui.ContentHeight(tree, 640, measure)
	if err != nil {
		t.Fatal(err)
	}
	if content <= 480 {
		t.Fatalf("content height %d still fits the old 480 guess, fixture is too short", content)
	}
	got := monitorSurfaceHeight(tree, 640, 12, measure)
	if got < content+24 {
		t.Fatalf("surface height %d, want at least content %d plus two radii", got, content)
	}
}
