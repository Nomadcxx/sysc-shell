package shell

import (
	"fmt"
	"os/exec"
	"testing"

	"github.com/Nomadcxx/sysc-shell/internal/config"
	"github.com/Nomadcxx/sysc-shell/internal/platform/wayland"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

// The control-centre Media page wraps its card content in one `Opacity: 80`
// node (mediabody.go), and that single wrapper used to cost +6.3 MiB and
// +1.5-2 ms per frame at 2x: the group-opacity path allocated an offscreen as
// large as the whole output surface and painted the subtree into it. This
// benchmark renders the real page through the real panel callbacks, the path
// the compositor drives, so the change is measured where a user feels it.
//
// BenchmarkControlCentreMediaFrame in frame_bench_test.go paints the same
// frame with the group in place. This one adds the cleared baseline beside
// it, so the gap between the two runs is what the group costs.
//
// What to watch in the output: B/op at 2x should be a small fraction of the
// output surface (3440x1440x4 is about 18 MiB), and ns/op should sit close to
// the same run with the group's opacity cleared.

// mediaBenchScale120 lists the scales the issue measured at; 240 is 2x.
var mediaBenchScale120 = []int{120, 240}

func BenchmarkControlCentreMediaFrameOpacity(b *testing.B) {
	for _, scale120 := range mediaBenchScale120 {
		b.Run(fmt.Sprintf("scale=%d", scale120/120), func(b *testing.B) {
			benchmarkControlCentreMediaFrame(b, scale120, false)
		})
		b.Run(fmt.Sprintf("scale=%d/opacity-cleared", scale120/120), func(b *testing.B) {
			benchmarkControlCentreMediaFrame(b, scale120, true)
		})
	}
}

// benchRegistry builds a panel registry whose invalidations a stand-in bridge
// keeps drained, like the test fixture newPanelRegistry.
func benchRegistry(b *testing.B) *Registry {
	b.Helper()
	cfg := config.Default()
	cfg.Accessibility.ReducedMotion = true
	reg := NewRegistry(cfg)
	reg.lookPath = func(string) (string, error) { return "", exec.ErrNotFound }
	b.Cleanup(reg.Close)
	benchInvalidationsDrained(b, reg)
	return reg
}

// benchOpenPanel opens id and returns the panel's own spec, whose callbacks
// are the paths the compositor drives.
func benchOpenPanel(b *testing.B, reg *Registry, id PanelID) *wayland.AuxSpec {
	b.Helper()
	if err := reg.OpenPanel(id, 7, Trigger{BarEdge: "top", BarZone: 40, OutW: assetOutW, OutH: assetOutH}); err != nil {
		b.Fatal(err)
	}
	reqs := benchDrainAux(b, reg)
	spec := reqs[1].Open
	if spec == nil {
		b.Fatal("no panel spec arrived")
	}
	return spec
}

// benchmarkControlCentreMediaFrame opens the control centre on its Media page
// with a playing track and renders it. clearOpacity strips the group-opacity
// fields from the page so the same frame can be measured without the feature.
//
// The order below is the capture tests' order: settle the host's animations
// first, so its frame loop rests instead of rebuilding the tree underneath the
// benchmark, and only then touch the panel state under Registry.mu.
func benchmarkControlCentreMediaFrame(b *testing.B, scale120 int, clearOpacity bool) {
	b.Helper()
	reg := benchRegistry(b)
	reg.now, reg.sample, reg.mediaState, reg.reading = assetNow, assetSample(), assetMedia, assetWeather()
	reg.setMedia(nil)
	panel := benchOpenPanel(b, reg, PanelControlCenter)
	h := reg.panelHosts[PanelControlCenter]
	if h == nil {
		b.Fatal("control-centre host is missing")
	}
	settleHostAnimation(reg, h)

	reg.mu.Lock()
	h.section = "media"
	reg.rebuildPanel(h)
	if clearOpacity {
		clearTreeOpacity(h.root)
	}
	reg.mu.Unlock()

	s := ui.Scale120(scale120)
	w, hgt := int(panel.Width), int(panel.Height)
	if err := panel.Callbacks.Configure(w, hgt, scale120); err != nil {
		b.Fatal(err)
	}
	pw, ph := s.Physical(w), s.Physical(hgt)
	pixels := make([]byte, pw*ph*4)
	// One frame warms the scratch free list and the geometry caches; the loop
	// measures what a steady-state repaint costs.
	if err := panel.Callbacks.Render(pixels, pw, ph, pw*4); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := panel.Callbacks.Render(pixels, pw, ph, pw*4); err != nil {
			b.Fatal(err)
		}
	}
}

// clearTreeOpacity removes the Opacity field from every node, which is the
// "opacity removed" baseline the issue compares against.
func clearTreeOpacity(n *ui.Node) {
	if n == nil {
		return
	}
	n.Opacity = 0
	for _, c := range n.Children {
		clearTreeOpacity(c)
	}
}
