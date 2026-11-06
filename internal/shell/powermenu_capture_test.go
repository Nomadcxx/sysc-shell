package shell

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/Nomadcxx/sysc-shell/internal/platform/wayland"
	"github.com/Nomadcxx/sysc-shell/internal/theme"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

func TestPowerMenuCaptures(t *testing.T) {
	dir := os.Getenv("SYSC_POWER_PNG_DIR")
	if dir == "" {
		t.Skip("set SYSC_POWER_PNG_DIR")
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"dark", "light"} {
		for _, density := range theme.Densities() {
			for _, scale := range []ui.Scale120{120, 150} {
				t.Run(fmt.Sprintf("%s-%s-%d", mode, density, scale), func(t *testing.T) {
					r := newPanelRegistry(t)
					r.cfg.Theme.Density = density
					r.cfg.Session.Locker = "swaylock"
					var ok bool
					r.tokens, ok = theme.NamedPalette("nord", mode, false)
					if !ok {
						t.Fatal("palette missing")
					}
					if err := r.OpenPanel(PanelPower, 7, Trigger{OutW: 1536, OutH: 864}); err != nil {
						t.Fatal(err)
					}
					drainAuxQueue(r)
					h := r.panelHosts[PanelPower]
					settleHostAnimation(r, h)
					handle := h.handle(r)
					capture := func(name string) {
						t.Helper()
						r.mu.Lock()
						defer r.mu.Unlock()
						w, ht := h.place.Panel.W, h.place.Panel.H
						if err := h.configure(w, ht, int(scale)); err != nil {
							t.Fatal(err)
						}
						pw, ph := scale.Physical(w), scale.Physical(ht)
						pix := make([]byte, pw*ph*4)
						if err := h.render(false, pix, pw, ph, pw*4); err != nil {
							t.Fatal(err)
						}
						writeCardPNG(t, filepath.Join(dir, fmt.Sprintf("%s-%s-%d-%s.png", mode, density, scale, name)), pix, pw, ph)
					}
					capture("compact-pointer")
					handle(wayland.Event{Kind: wayland.EventKeyPress, Key: 1, Sym: ui.SymTab})
					capture("compact-keyboard")
					r.mu.Lock()
					r.setPowerPresentationLocked(h, true)
					drainAuxQueue(r)
					r.mu.Unlock()
					capture("fullscreen-keyboard")
					handle(wayland.Event{Kind: wayland.EventPointerMotion, X: 1, Y: 1})
					capture("fullscreen-pointer")
					if density == theme.DensityDefault && scale == 120 {
						for _, full := range []bool{false, true} {
							r.mu.Lock()
							r.setPowerPresentationLocked(h, full)
							drainAuxQueue(r)
							r.mu.Unlock()
							suffix := "compact"
							if full {
								suffix = "fullscreen"
							}
							for _, a := range []string{"session-logout", "session-reboot", "session-poweroff"} {
								r.mu.Lock()
								h.activatePower(r, &ui.Node{Action: a})
								r.mu.Unlock()
								capture(suffix + "-" + a + "-confirm")
								r.mu.Lock()
								h.activatePower(r, &ui.Node{Action: "power:cancel"})
								r.mu.Unlock()
							}
							r.mu.Lock()
							h.powerFailed = "session-reboot"
							h.errLabel = "Access denied."
							r.rebuildPanel(h)
							r.mu.Unlock()
							capture(suffix + "-error")
							r.mu.Lock()
							h.powerFailed, h.errLabel = "", ""
							h.sessionPending = true
							r.rebuildPanel(h)
							r.mu.Unlock()
							capture(suffix + "-pending")
							r.mu.Lock()
							h.sessionPending = false
							h.powerFailed, h.errLabel = "", ""
							r.rebuildPanel(h)
							r.mu.Unlock()
						}
						r.mu.Lock()
						h.powerTrigger.OutW, h.powerTrigger.OutH = 320, 240
						h.theme.Type = ThemeFrom(r.cfg, r.cfg.Bar).Type
						r.setPowerPresentationLocked(h, false)
						r.mu.Unlock()
						capture("narrow-short")
					}
				})
			}
		}
	}
}
