package shell

import (
	"fmt"
	"github.com/Nomadcxx/sysc-shell/internal/config"
	"github.com/Nomadcxx/sysc-shell/internal/platform/wayland"
	"github.com/Nomadcxx/sysc-shell/internal/render"
	"github.com/Nomadcxx/sysc-shell/internal/theme"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
	"testing"
)

func focusTestHost() (*PanelHost, *Registry) {
	n := &ui.Node{Kind: ui.KindButton, Action: "unknown", Focusable: true, Bounds: ui.Rect{X: 10, Y: 10, W: 80, H: 40}}
	h := &PanelHost{id: PanelClock, root: &ui.Node{Kind: ui.KindColumn, Bounds: ui.Rect{W: 100, H: 100}, Children: []*ui.Node{n}}, focus: []*ui.Node{n}, roving: ui.Roving{Count: 1}, pointer: interaction{stateLayer: true}}
	return h, &Registry{}
}

func TestPanelFocusVisibleModality(t *testing.T) {
	for _, tc := range []struct {
		name    string
		event   wayland.Event
		visible bool
		changed bool
	}{
		{"enter", wayland.Event{Kind: wayland.EventPointerEnter, X: 20, Y: 20}, true, false},
		{"same motion", wayland.Event{Kind: wayland.EventPointerMotion, X: 20, Y: 20}, true, false},
		{"real subpixel motion", wayland.Event{Kind: wayland.EventPointerMotion, X: 20.25, Y: 20}, false, true},
		{"press outside", wayland.Event{Kind: wayland.EventPointerPress, X: 200, Y: 200}, false, true},
		{"wheel at edge", wayland.Event{Kind: wayland.EventPointerAxis, AxisValue: .1}, false, true},
		{"zero wheel", wayland.Event{Kind: wayland.EventPointerAxis}, true, false},
		{"leave", wayland.Event{Kind: wayland.EventPointerLeave}, true, true},
		{"release", wayland.Event{Kind: wayland.EventKeyRelease, Key: 42}, true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h, r := focusTestHost()
			handle := h.handle(r)
			handle(wayland.Event{Kind: wayland.EventPointerEnter, X: 20, Y: 20})
			if h.focusVisible {
				t.Fatal("pointer-open host starts keyboard-visible")
			}
			if !h.keyPress(r, keyTab) || !h.focusVisible {
				t.Fatal("Tab did not enable keyboard focus")
			}
			if got := handle(tc.event); got != tc.changed {
				t.Errorf("invalidation=%v want %v", got, tc.changed)
			}
			if h.focusVisible != tc.visible {
				t.Fatalf("focus visibility=%v want %v", h.focusVisible, tc.visible)
			}
		})
	}
	for _, code := range []uint32{29, 42, 54, 56, 58, 69, 70, 97, 100, 125, 126, 127} {
		h, r := focusTestHost()
		h.keyPress(r, code)
		if h.focusVisible {
			t.Errorf("modifier/lock/compose %d enabled focus", code)
		}
	}
	h, r := focusTestHost()
	if !h.keyInput(r, ui.KeyInput{Code: 30, Sym: 'a', Mods: ui.ModCtrl}) || !h.focusVisible {
		t.Fatal("mode-only keyboard shortcut did not invalidate")
	}
}

func TestPanelFocusMarksPaintCopy(t *testing.T) {
	for _, tc := range []struct {
		name                               string
		virtual, disabled, aria, offscreen bool
	}{
		{name: "unkeyed"}, {name: "virtual", virtual: true}, {name: "disabled", disabled: true},
		{name: "aria disabled", disabled: true, aria: true}, {name: "offscreen", offscreen: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := &ui.Node{Kind: ui.KindTextField, Focusable: true, Bounds: ui.Rect{X: 10, Y: 10, W: 80, H: 30}}
			b := &ui.Node{Kind: ui.KindTextField, Focusable: true, Bounds: ui.Rect{X: 10, Y: 50, W: 80, H: 30}}
			if tc.disabled {
				b.State = ui.StateDisabled
				b.AriaDisabled = tc.aria
			}
			if tc.offscreen {
				b.Bounds.Y = 200
			}
			root := &ui.Node{Kind: ui.KindScroll, Bounds: ui.Rect{W: 100, H: 100}, Children: []*ui.Node{a, b}}
			focused := b
			if tc.virtual {
				root.Kind = ui.KindVirtualList
				b.Key = "row-8"
				fresh := *b
				fresh.Bounds = ui.Rect{}
				focused = &fresh
			}
			h := &PanelHost{root: root, focus: []*ui.Node{focused}, roving: ui.Roving{Count: 1}, focusVisible: true}
			cp := copyNodeVisible(root, ui.Rect{}, false)
			h.applyFocusView(cp)
			count := 0
			walkNodes(cp, func(n *ui.Node) {
				if n.State.Has(ui.StateFocused) {
					count++
				}
			})
			want := 1
			if tc.offscreen || tc.disabled && !tc.aria {
				want = 0
			}
			if count != want {
				t.Fatalf("focused paint nodes=%d want %d", count, want)
			}
			if a.State.Has(ui.StateFocused) || b.State.Has(ui.StateFocused) {
				t.Fatal("retained nodes mutated")
			}
			h.focusVisible = false
			cp = copyNode(root)
			h.applyFocusView(cp)
			walkNodes(cp, func(n *ui.Node) {
				if n.State.Has(ui.StateFocused) {
					t.Fatal("pointer mode painted focus")
				}
			})
		})
	}
	h, _ := focusTestHost()
	h.focusVisible = true
	h.focus = nil
	h.roving.Count = 0
	h.applyFocusView(copyNode(h.root))
}

func TestPanelDraggedStateRequiresMotion(t *testing.T) {
	for _, kind := range []ui.Kind{ui.KindSlider, ui.KindDragSource} {
		t.Run(kind.String(), func(t *testing.T) {
			h, r := focusTestHost()
			n := h.focus[0]
			n.Kind = kind
			n.Min = 0
			n.Max = 100
			handle := h.handle(r)
			handle(wayland.Event{Kind: wayland.EventPointerPress, X: 85, Y: 20})
			check := func(want bool) {
				t.Helper()
				cp := copyNode(h.root)
				h.pointer.apply(cp, nil)
				h.applyFocusView(cp)
				if got := cp.Children[0].State.Has(ui.StateDragged); got != want {
					t.Fatalf("dragged=%v want %v", got, want)
				}
			}
			check(false)
			handle(wayland.Event{Kind: wayland.EventPointerMotion, X: 85, Y: 20})
			check(false)
			handle(wayland.Event{Kind: wayland.EventPointerMotion, X: 86, Y: 20})
			check(kind == ui.KindSlider)
			handle(wayland.Event{Kind: wayland.EventPointerMotion, X: 100, Y: 20})
			check(true)
			handle(wayland.Event{Kind: wayland.EventPointerLeave})
			check(false)
		})
	}
}

func TestPanelFocusVisiblePixels(t *testing.T) {
	r := newPanelRegistry(t)
	if err := r.OpenPanel(PanelSession, 7, Trigger{}); err != nil {
		t.Fatal(err)
	}
	h := r.panelHosts[PanelSession]
	settleHostAnimation(r, h)
	r.mu.Lock()
	h.logicalW, h.logicalH, h.scale120 = 160, 140, 120
	h.theme.Palette.Secondary = render.Color{R: 227, G: 41, B: 119, A: 255}
	n := &ui.Node{Kind: ui.KindButton, Action: "unknown", Focusable: true, Bounds: ui.Rect{X: 35, Y: 25, W: 60, H: 30}}
	h.root = &ui.Node{Kind: ui.KindColumn, Bounds: ui.Rect{W: 160, H: 140}, Children: []*ui.Node{n}}
	h.focus = []*ui.Node{n}
	h.roving = ui.Roving{Count: 1}
	r.mu.Unlock()
	check := func(want bool) {
		t.Helper()
		r.mu.Lock()
		defer r.mu.Unlock()
		pix := make([]byte, 160*140*4)
		if err := h.render(false, pix, 160, 140, 160*4); err != nil {
			t.Fatal(err)
		}
		at := 21*160*4 + 65*4
		c := h.theme.Palette.Secondary
		got := pix[at] == c.B && pix[at+1] == c.G && pix[at+2] == c.R
		if got != want {
			t.Fatalf("Secondary ring=%v want %v, pixel%v", got, want, pix[at:at+4])
		}
		if !want {
			at = 25*160*4 + 65*4
			c = h.theme.Accent
			if pix[at] == c.B && pix[at+1] == c.G && pix[at+2] == c.R {
				t.Fatal("legacy host ring visible in pointer mode")
			}
		}
	}
	check(false)
	h.handle(r)(wayland.Event{Kind: wayland.EventKeyPress, Key: keyTab})
	check(true)
	h.handle(r)(wayland.Event{Kind: wayland.EventPointerPress, X: 150, Y: 130})
	check(false)
}

func TestSliderReleaseInvalidatesWithoutReceiver(t *testing.T) {
	h, r := focusTestHost()
	n := h.focus[0]
	n.Kind = ui.KindSlider
	n.Min = 0
	n.Max = 100
	n.Action = "plugin:test:volume"
	handle := h.handle(r)
	handle(wayland.Event{Kind: wayland.EventPointerPress, X: 30, Y: 20})
	handle(wayland.Event{Kind: wayland.EventPointerMotion, X: 40, Y: 20})
	if !handle(wayland.Event{Kind: wayland.EventPointerRelease, X: 40, Y: 20}) {
		t.Fatal("clearing captured press/drag did not invalidate without a receiver")
	}
}

func TestPanelFocusRingRevealsScroll(t *testing.T) {
	for _, tc := range []struct {
		name    string
		y, want int
	}{{"first", 12, 17}, {"last", 80, 25}} {
		t.Run(tc.name, func(t *testing.T) {
			n := &ui.Node{Kind: ui.KindButton, Focusable: true, Bounds: ui.Rect{X: 20, Y: tc.y, W: 60, H: 30}}
			view := &ui.Node{Kind: ui.KindScroll, Bounds: ui.Rect{X: 10, Y: 10, W: 100, H: 100}, ContentH: 200, ScrollOffset: 20, Children: []*ui.Node{n}}
			h := &PanelHost{root: view, focusVisible: true, focus: []*ui.Node{n}, roving: ui.Roving{Count: 1}}
			if !h.revealFocusRing() || view.ScrollOffset != tc.want {
				t.Fatalf("ring reveal offset=%d want%d", view.ScrollOffset, tc.want)
			}
		})
	}
	t.Run("fresh virtual item", func(t *testing.T) {
		item := func(i int) *ui.Node {
			return &ui.Node{Kind: ui.KindButton, Key: fmt.Sprint("item-", i), Focusable: true}
		}
		view := &ui.Node{Kind: ui.KindVirtualList, Bounds: ui.Rect{X: 10, Y: 10, W: 100, H: 100}, ContentH: 360, ItemCount: 12, ItemHeight: 30, Item: item}
		h := &PanelHost{root: view, focusVisible: true, focus: []*ui.Node{item(8)}, roving: ui.Roving{Count: 1}}
		if !h.revealFocusRing() || view.ScrollOffset != 175 {
			t.Fatalf("virtual reveal offset=%d want175", view.ScrollOffset)
		}
	})
}

func TestPanelFocusVisibleSurvivesReload(t *testing.T) {
	for _, visible := range []bool{false, true} {
		t.Run(fmt.Sprint(visible), func(t *testing.T) {
			r := newPanelRegistry(t)
			keepInvalidationsDrained(t, r)
			withTestBar(t, r, 7, r.cfg).setOutputSize(1536, 864)
			if err := r.OpenPanel(PanelSettings, 7, Trigger{BarEdge: "top", OutW: 1536, OutH: 864}); err != nil {
				t.Fatal(err)
			}
			r.mu.Lock()
			h := r.panelHosts[PanelSettings]
			h.query = "bar"
			h.search = ui.NewField("bar")
			r.rebuildPanel(h)
			h.focusByName("Search")
			h.search.SetCaret(1, false)
			h.search.SetCaret(3, true)
			editor := h.search
			h.focusVisible = visible
			scroll := keyedScroll(h.root, settingsSearchBodyKey)
			if scroll == nil {
				r.mu.Unlock()
				t.Fatal("missing search scroll")
			}
			scroll.ScrollOffset = 10
			ui.ScrollBy(scroll, 0)
			offset := scroll.ScrollOffset
			h.settingsSearchScroll = offset
			h.sliderDrag = &ui.Node{Kind: ui.KindSlider, Action: "detached"}
			h.sliderDragged = true
			h.pressed = "detached"
			h.pointer.setPress("detached")
			candidate := r.cfg.Clone()
			before := r.cfg.Clone()
			r.mu.Unlock()
			for _, density := range []theme.Density{theme.DensityMini, theme.DensitySpacious} {
				candidate.Theme.Density = density
				candidate.Bar.Edge = "bottom"
				config.RebaseDerivedBar(&candidate, before.Theme, before.Bar)
				prepared, err := r.PrepareConfig(candidate, []wayland.HostIdentity{{Global: 7, Connector: "DP-1"}})
				if err != nil {
					t.Fatal(err)
				}
				hooks := prepared.Hosts[7]
				configureCandidateBar(t, hooks, candidate.Bar, 1536, 864, 120)
				prepared.Commit()
				w, height := barSurfaceSize(candidate.Bar, 1536, 864)
				if err := hooks.Render(make([]byte, w*height*4), w, height, w*4); err != nil {
					t.Fatal(err)
				}
				r.mu.Lock()
				if r.panelHosts[PanelSettings] != h || h.focusVisible != visible {
					r.mu.Unlock()
					t.Fatal("adoption replaced host or mode")
				}
				if h.search != editor || editor.Cursor != 3 || editor.Anchor != 1 || editor.Text != "bar" {
					r.mu.Unlock()
					t.Fatal("adoption lost editor selection")
				}
				if f := h.focused(); f == nil || f.Name != "Search" {
					r.mu.Unlock()
					t.Fatal("adoption lost unkeyed focus")
				}
				if h.sliderDrag != nil || h.sliderDragged || h.pressed != "" || h.pointer.press != "" {
					r.mu.Unlock()
					t.Fatal("adoption retained detached interaction")
				}
				if got := keyedScroll(h.root, settingsSearchBodyKey).ScrollOffset; got != offset {
					r.mu.Unlock()
					t.Fatalf("scroll=%d want%d", got, offset)
				}
				before = r.cfg.Clone()
				candidate = r.cfg.Clone()
				r.mu.Unlock()
			}
			h.handle(r)(wayland.Event{Kind: wayland.EventPointerRelease, X: 1000, Y: 700})
			r.ClosePanel(PanelSettings)
			if err := r.OpenPanel(PanelSettings, 7, Trigger{}); err != nil {
				t.Fatal(err)
			}
			if r.panelHosts[PanelSettings].focusVisible {
				t.Fatal("reopened panel inherited keyboard mode")
			}
		})
	}
}

func TestEmptyPanelModalityDoesNotInvalidate(t *testing.T) {
	h, r := focusTestHost()
	h.focus = nil
	h.roving.Count = 0
	if h.keyInput(r, ui.KeyInput{Code: 30, Sym: 'a'}) {
		t.Fatal("empty panel requested a mode-only keyboard frame")
	}
	if !h.focusVisible {
		t.Fatal("empty panel lost keyboard modality")
	}
	if h.handle(r)(wayland.Event{Kind: wayland.EventPointerPress, X: 200, Y: 200}) {
		t.Fatal("empty panel requested a mode-only pointer frame")
	}
	if h.focusVisible {
		t.Fatal("empty panel kept keyboard modality after pointer press")
	}
}

func TestPluginStoreFocusRevealRetainsItsOffset(t *testing.T) {
	n := &ui.Node{Kind: ui.KindButton, Focusable: true, Bounds: ui.Rect{X: 20, Y: 12, W: 60, H: 30}}
	view := &ui.Node{Kind: ui.KindScroll, Key: "plugin-store-grid", Bounds: ui.Rect{X: 10, Y: 10, W: 100, H: 100}, ContentH: 200, ScrollOffset: 20, Children: []*ui.Node{n}}
	h := &PanelHost{id: PanelPluginStore, root: view, focusVisible: true, focus: []*ui.Node{n}, roving: ui.Roving{Count: 1}, pluginStoreScroll: 20}
	h.revealFocusRing()
	if h.pluginStoreScroll != view.ScrollOffset {
		t.Fatalf("retained offset=%d want%d", h.pluginStoreScroll, view.ScrollOffset)
	}
}

func TestSettingsFocusHasViewportRoom(t *testing.T) {
	for _, scale := range []ui.Scale120{120, 150} {
		t.Run(fmt.Sprint(scale), func(t *testing.T) {
			r := newPanelRegistry(t)
			if err := r.OpenPanel(PanelSettings, 7, Trigger{}); err != nil {
				t.Fatal(err)
			}
			h := r.panelHosts[PanelSettings]
			settleHostAnimation(r, h)
			r.mu.Lock()
			defer r.mu.Unlock()
			w, height := h.surfaceSize()
			if err := h.configure(w, height, int(scale)); err != nil {
				t.Fatal(err)
			}
			h.theme.Palette.Secondary = render.Color{R: 227, G: 41, B: 119, A: 255}
			for _, name := range []string{"Search", "Sounds"} {
				t.Run(name, func(t *testing.T) {
					h.focusByName(name)
					h.focusVisible = true
					h.revealFocusRing()
					rail, n := keyedScroll(h.root, settingsRailKey), h.focused()
					if n == nil || n.Name != name {
						t.Fatal("missing target")
					}
					if n.Bounds.Y-render.FocusOutset < rail.Bounds.Y || n.Bounds.Y+n.Bounds.H+render.FocusOutset > rail.Bounds.Y+rail.Bounds.H {
						t.Fatalf("%s ring clipped by rail: target%+v viewport%+v", name, n.Bounds, rail.Bounds)
					}
					pw, ph := scale.Physical(w), scale.Physical(height)
					pix := make([]byte, pw*ph*4)
					if err := h.render(false, pix, pw, ph, pw*4); err != nil {
						t.Fatal(err)
					}
					y := n.Bounds.Y - 3
					if name == "Sounds" {
						y = n.Bounds.Y + n.Bounds.H + 3
					}
					x := scale.Physical(n.Bounds.X + n.Bounds.W/2)
					at := scale.Physical(y)*pw*4 + x*4
					c := h.theme.Palette.Secondary
					if pix[at] != c.B || pix[at+1] != c.G || pix[at+2] != c.R {
						t.Fatalf("%s ring edge missing: %v", name, pix[at:at+4])
					}
				})
			}
			t.Run("body edges", func(t *testing.T) {
				body := keyedScroll(h.root, settingsBodyKey)
				if body == nil {
					t.Fatal("missing body")
				}
				targets := ui.Focusables(body)
				if len(targets) == 0 {
					t.Fatal("missing body controls")
				}
				n := targets[0]
				h.setFocus(n)
				h.revealFocusRing()
				if n.Bounds.X-render.FocusOutset < body.Bounds.X || n.Bounds.X+n.Bounds.W+render.FocusOutset > body.Bounds.X+body.Bounds.W {
					t.Fatalf("body ring has no side gutter: target%+v viewport%+v", n.Bounds, body.Bounds)
				}
			})
		})
	}
}
