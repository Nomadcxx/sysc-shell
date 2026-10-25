package shell

import (
	"fmt"
	"testing"
	"time"

	launcher "github.com/Nomadcxx/sysc-launch"
	"github.com/Nomadcxx/sysc-shell/internal/config"
	"github.com/Nomadcxx/sysc-shell/internal/platform/wayland"
)

// syntheticAppCatalog builds n installed applications shaped like a real
// desktop's: most carry the comment line that adds a second label row and one
// in seventeen is a terminal whose row grows a third control, so the rows are
// the mixed heights the Applications page actually lays out.
func syntheticAppCatalog(n int) []launcher.Entry {
	entries := make([]launcher.Entry, n)
	for i := 0; i < n; i++ {
		e := launcher.Entry{
			ID:   fmt.Sprintf("app-%04d.desktop", i),
			Name: fmt.Sprintf("Application %04d", i),
		}
		if i%3 != 0 {
			e.Comment = fmt.Sprintf("A synthetic application number %d used to exercise the settings list", i)
		}
		if i%17 == 0 {
			e.TerminalApp = true
			e.Name = fmt.Sprintf("Terminal %04d", i)
		}
		entries[i] = e
	}
	return entries
}

// benchSyntheticApps opens Settings > Applications over a synthetic catalog of
// n apps and returns the placement's render spec, so the frame a compositor
// callback drives is the same one the regression budget measures. The snapshot
// is stamped now so refreshLauncherApplicationsLocked leaves it alone rather
// than racing a background rescan.
func benchSyntheticApps(b *testing.B, n int) *wayland.AuxSpec {
	b.Helper()
	apps := syntheticAppCatalog(n)
	cfg := config.Default()
	cfg.Theme.BlurBehind = false
	cfg.Accessibility.ReducedMotion = true
	reg := NewRegistry(cfg)
	b.Cleanup(reg.Close)
	benchInvalidationsDrained(b, reg)
	reg.launcherSvc = launcher.NewService(launcher.ServiceConfig{Scan: func() []launcher.Entry { return apps }})
	reg.launcherApplications.Store(&launcherApplicationSnapshot{
		entries: apps, attemptedAt: time.Now(), available: true,
	})
	if err := reg.OpenPanel(PanelSettings, 7, Trigger{BarEdge: "top", BarZone: 40, OutW: 3440, OutH: 1440}); err != nil {
		b.Fatal(err)
	}
	reqs := benchDrainAux(b, reg)
	reg.mu.Lock()
	err := reg.selectPanelSectionLocked(PanelSettings, "Applications")
	reg.mu.Unlock()
	if err != nil {
		b.Fatal(err)
	}
	h := reg.panelHosts[PanelSettings]
	benchSettleReveal(b, reg, h)
	spec := reqs[1].Open
	if err := spec.Callbacks.Configure(int(spec.Width), int(spec.Height), 240); err != nil {
		b.Fatal(err)
	}
	return spec
}

// BenchmarkSettingsApplicationsSynthetic paints the Applications page at 2x
// over a synthetic catalog of 1,000 apps. This is the frame the perf report
// profiles and the budget regression check sizes.
func BenchmarkSettingsApplicationsSynthetic(b *testing.B) {
	b.Run("1000apps", func(b *testing.B) {
		spec := benchSyntheticApps(b, 1000)
		benchFrames(b, spec, 240)
	})
}
