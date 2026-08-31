package shell

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Nomadcxx/sysc-shell/internal/services"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

func TestOsdPositionMarginsBottomCenter(t *testing.T) {
	t.Parallel()
	m := osdMargins("bottom-center", ui.Rect{W: 1920, H: 1080}, ui.Rect{W: 220, H: 64}, 40, 8)
	if m.Bottom != 48 {
		t.Fatalf("bottom = %d, want 48", m.Bottom)
	}
	if m.Left != (1920-220)/2 {
		t.Fatalf("left = %d, want centred", m.Left)
	}
}

func TestOsdPositionAllNineTokens(t *testing.T) {
	t.Parallel()
	output := ui.Rect{W: 1920, H: 1080}
	size := ui.Rect{W: 220, H: 64}
	tokens := []string{
		"top-left", "top-center", "top-right",
		"center-left", "center", "center-right",
		"bottom-left", "bottom-center", "bottom-right",
	}
	for _, pos := range tokens {
		m := osdMargins(pos, output, size, 40, 8)
		box := osdBox(pos, output, size, m)
		if box.X < 0 || box.Y < 0 || box.X+box.W > output.W || box.Y+box.H > output.H {
			t.Fatalf("%s box %+v escapes output", pos, box)
		}
	}
}

func TestOsdTimerResetsOnRepeat(t *testing.T) {
	t.Parallel()
	reg := newPanelRegistry(t)
	reg.osd.hideFor = 50 * time.Millisecond
	reg.bars[1] = &Bar{conn: "eDP-1"}
	reg.OSD().Show(OSDView{Kind: "audio", Level: 40})
	_ = drainAux(t, reg, 1)
	time.Sleep(30 * time.Millisecond)
	reg.OSD().Show(OSDView{Kind: "audio", Level: 45})
	if !reg.OSD().Visible() {
		t.Fatal("timer must reset on repeated change")
	}
}

func TestOsdShownOnEveryOutputWithBar(t *testing.T) {
	t.Parallel()
	reg := newPanelRegistry(t)
	reg.bars[1] = &Bar{conn: "DP-1"}
	reg.bars[2] = &Bar{conn: "DP-2"}
	reg.OSD().Show(OSDView{Kind: "audio", Level: 50})
	reqs := drainAux(t, reg, 2)
	seen := map[string]bool{}
	for _, req := range reqs {
		if req.Open == nil {
			t.Fatalf("expected open, got %+v", req)
		}
		seen[req.Open.ID] = true
	}
	if !seen["osd:1"] || !seen["osd:2"] {
		t.Fatalf("osd ids = %v", seen)
	}
}

func TestOsdStepStepsAndShows(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	log := filepath.Join(dir, "log")
	script := `#!/bin/sh
echo "$*" >> "` + log + `"
if [ "$1" = get-volume ]; then
  echo "Volume: 0.40"
fi
`
	bin := filepath.Join(dir, "wpctl")
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	reg := newPanelRegistry(t)
	reg.audio = services.NewAudio(time.Second, bin)
	reg.bars[1] = &Bar{conn: "eDP-1"}
	if err := reg.OSDStep("audio", "up"); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "set-volume @DEFAULT_AUDIO_SINK@ +5%") {
		t.Fatalf("wpctl log = %q", raw)
	}
	reqs := drainAux(t, reg, 1)
	if reqs[0].Open == nil || reqs[0].Open.ID != "osd:1" {
		t.Fatalf("osd aux = %+v", reqs[0])
	}
}

func TestOsdReducedMotionNoAnimation(t *testing.T) {
	t.Parallel()
	reg := newPanelRegistry(t) // already reduced-motion
	reg.bars[1] = &Bar{conn: "eDP-1"}
	drainInvalidations(reg)
	reg.OSD().Show(OSDView{Kind: "audio", Level: 20})
	_ = drainAux(t, reg, 1)
	if got := countSurfaceInvalidations(reg, 50*time.Millisecond); got != 1 {
		t.Fatalf("reduced motion produced %d invalidations, want 1", got)
	}
}

func osdBox(pos string, output, size ui.Rect, m Margins) ui.Rect {
	box := ui.Rect{X: m.Left, W: size.W, H: size.H, Y: m.Top}
	if strings.HasPrefix(pos, "bottom") {
		box.Y = output.H - m.Bottom - size.H
	}
	return box
}
