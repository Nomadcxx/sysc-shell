package integration

import (
	"testing"
	"time"

	"github.com/Nomadcxx/sysc-shell/internal/config"
	"github.com/Nomadcxx/sysc-shell/internal/shell"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

func TestGateExclusiveZoneUnchangedByPanels(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.Accessibility.ReducedMotion = true
	reg := shell.NewRegistry(cfg)
	t.Cleanup(reg.Close)
	height := cfg.Bar.Height
	if err := reg.OpenPanel(shell.PanelSession, 7, shell.Trigger{BarEdge: "top", BarZone: 40}); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		req := <-reg.AuxRequests()
		if req.Open != nil && req.Open.ExclusiveZone != -1 {
			t.Fatalf("%s exclusive zone = %d, want -1", req.Open.ID, req.Open.ExclusiveZone)
		}
	}
	if cfg.Bar.Height != height {
		t.Fatal("opening a panel changed bar geometry")
	}
}

func TestGatePlacementWithinBounds(t *testing.T) {
	t.Parallel()
	outputs := []ui.Rect{
		{W: 1920, H: 1080},
		{W: 1080, H: 1920},
		{W: 3440, H: 1440},
		{W: 800, H: 600},
		{W: 100, H: 80},
	}
	for _, out := range outputs {
		p := shell.Placement{
			BarEdge: "top", Output: out, BarZone: 40,
			Gap: 8, Padding: 8, Panel: ui.Rect{W: 360, H: 420}, Align: "center",
		}
		w, h := p.FittedSize()
		p.Panel.W, p.Panel.H = w, h
		m := p.Margins()
		if m.Left < 0 || m.Top < 0 || m.Bottom < 0 || m.Right < 0 {
			t.Fatalf("negative margins %+v on %+v", m, out)
		}
		if w < 0 || h < 0 {
			t.Fatalf("negative fitted size %dx%d on %+v", w, h, out)
		}
		if out.W > 0 && m.Left+w > out.W {
			t.Fatalf("panel overflows width on %+v: left=%d w=%d", out, m.Left, w)
		}
	}
}

func TestGateReducedMotionInstant(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.Accessibility.ReducedMotion = true
	reg := shell.NewRegistry(cfg)
	t.Cleanup(reg.Close)
	if err := reg.OpenPanel(shell.PanelClock, 7, shell.Trigger{}); err != nil {
		t.Fatal(err)
	}
	<-reg.AuxRequests()
	<-reg.AuxRequests()
	n := 0
	timeout := time.After(50 * time.Millisecond)
	for {
		select {
		case inv := <-reg.Invalidations():
			if inv.SurfaceID != "" {
				n++
			}
		case <-timeout:
			if n != 1 {
				t.Fatalf("reduced motion invalidations = %d, want 1", n)
			}
			return
		}
	}
}
