package shell

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/Nomadcxx/sysc-shell/internal/platform/wayland"
	"github.com/Nomadcxx/sysc-shell/internal/plugin"
	"github.com/Nomadcxx/sysc-shell/internal/theme"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
	v1 "github.com/Nomadcxx/sysc-shell/plugin/v1"
)

// TestM3FocusContactSheet captures real host input callbacks. Columns are
// pointer, Tab, arrow/edit (last scrolled plugin row), then pointer again.
func TestM3FocusContactSheet(t *testing.T) {
	dir := os.Getenv("SYSC_FOCUS_PNG_DIR")
	if dir == "" {
		t.Skip("set SYSC_FOCUS_PNG_DIR to save focus scenes")
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"dark", "light"} {
		for _, density := range theme.Densities() {
			for _, scale := range []ui.Scale120{120, 150} {
				for _, scene := range []struct {
					name string
					id   PanelID
				}{{"control-centre", PanelControlCenter}, {"audio", PanelControlCenter}, {"settings-search", PanelSettings}, {"plugin-scroll", PanelSession}} {
					name := fmt.Sprintf("%s-%s-%d-%s", mode, density, scale, scene.name)
					t.Run(name, func(t *testing.T) {
						r := newPanelRegistry(t)
						withState := func(fn func()) { r.mu.Lock(); defer r.mu.Unlock(); fn() }
						withState(func() {
							r.cfg.Theme.Density = density
							var ok bool
							r.tokens, ok = theme.NamedPalette("nord", mode, false)
							if !ok {
								t.Fatal("missing palette")
							}
							assetBase(t, r)
						})
						if err := r.OpenPanel(scene.id, 7, Trigger{BarEdge: "top", OutW: 1536, OutH: 1080}); err != nil {
							t.Fatal(err)
						}
						h := r.panelHosts[scene.id]
						settleHostAnimation(r, h)
						var w, height int
						withState(func() {
							if scene.name == "audio" {
								h.section = "audio"
								r.rebuildPanel(h)
							}
							if scene.id == PanelSettings {
								h.query = "bar"
								h.search = ui.NewField("bar")
								r.rebuildPanel(h)
								h.focusByName("Search")
							}
							w, height = h.surfaceSize()
							if scene.name == "plugin-scroll" {
								wire := &v1.Node{Kind: v1.KindColumn, Padding: 12, Gap: 12, Children: []*v1.Node{
									{Kind: v1.KindTextInput, ID: "query", Key: "query", Name: "Search", Role: "textbox", Events: []v1.EventKind{v1.EventChange}, Placeholder: "Search", Height: 44},
									{Kind: v1.KindSlider, ID: "level", Key: "level", Name: "Volume", Role: "slider", Events: []v1.EventKind{v1.EventChange}, Min: 0, Max: 100, Step: 1, Value: 50},
								}}
								for i := 0; i < 8; i++ {
									wire.Children = append(wire.Children, &v1.Node{Kind: v1.KindButton, ID: fmt.Sprint("row-", i), Key: fmt.Sprint("row-", i), Name: fmt.Sprint("Destination ", i), Role: "button", Events: []v1.EventKind{v1.EventActivate}, Text: fmt.Sprint("Destination ", i), Height: 44})
								}
								root, err := plugin.Convert(wire, v1.ViewPanel)
								if err != nil {
									t.Fatal(err)
								}
								h.root = &ui.Node{Kind: ui.KindScroll, Padding: 8, Children: []*ui.Node{root}}
								w, height = 440, 360
							}
							if err := h.configure(w, height, int(scale)); err != nil {
								t.Fatal(err)
							}
							h.focus = ui.Focusables(h.root)
							h.roving = ui.Roving{Count: len(h.focus)}
						})
						pw, ph := scale.Physical(w), scale.Physical(height)
						sheet := make([]byte, 4*pw*ph*4)
						handle := h.handle(r)
						handle(wayland.Event{Kind: wayland.EventPointerEnter, X: -1, Y: -1})
						for column := 0; column < 4; column++ {
							switch column {
							case 1:
								handle(wayland.Event{Kind: wayland.EventKeyPress, Key: keyTab})
							case 2:
								handle(wayland.Event{Kind: wayland.EventKeyPress, Key: keyRight})
								withState(func() {
									if scene.id == PanelSettings {
										h.focusByName("Search")
										h.keyPress(r, keyHome)
									}
									if scene.name == "plugin-scroll" {
										h.roving.Set(h.roving.Count - 1)
										h.afterFocusChange(r)
									}
								})
							case 3:
								handle(wayland.Event{Kind: wayland.EventPointerMotion, X: -2, Y: -1})
							}
							pix := make([]byte, pw*ph*4)
							withState(func() {
								if h.focusVisible != (column == 1 || column == 2) {
									t.Fatalf("column%d has wrong input mode", column)
								}
								if err := h.render(false, pix, pw, ph, pw*4); err != nil {
									t.Fatal(err)
								}
							})
							for y := 0; y < ph; y++ {
								copy(sheet[(y*4*pw+column*pw)*4:], pix[y*pw*4:(y+1)*pw*4])
							}
						}
						writeCardPNG(t, filepath.Join(dir, name+".png"), sheet, 4*pw, ph)
					})
				}
			}
		}
	}
}
