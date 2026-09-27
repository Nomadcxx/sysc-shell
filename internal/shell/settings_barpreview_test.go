package shell

import (
	"strings"
	"testing"

	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

func openSettingsForPreview(t *testing.T) (*Registry, *PanelHost) {
	t.Helper()
	reg := newPanelRegistry(t)
	withTestBar(t, reg, 7, reg.cfg)
	if err := reg.OpenPanel(PanelSettings, 7, Trigger{BarEdge: "top", BarZone: 40, OutW: 1536, OutH: 864}); err != nil {
		t.Fatal(err)
	}
	_ = drainAux(t, reg, 2)
	return reg, reg.panelHosts[PanelSettings]
}

func opaquePixels(img *ui.Image) int {
	n := 0
	for i := 3; i < len(img.Pix); i += 4 {
		if img.Pix[i] == 0xff {
			n++
		}
	}
	return n
}

func TestBarPreviewPaintsTheDraft(t *testing.T) {
	reg, h := openSettingsForPreview(t)
	reg.mu.Lock()
	defer reg.mu.Unlock()
	solid, islands := h.draft, h.draft
	solid.Bar.Style, islands.Bar.Style = "solid", "islands"
	a := reg.settingsBarImage(h, solid, 600)
	b := reg.settingsBarImage(h, islands, 600)
	if a == nil || b == nil || a.Width == 0 {
		t.Fatal("no preview image")
	}
	if opaquePixels(a) == 0 {
		t.Fatal("the solid preview painted nothing opaque")
	}
	if opaquePixels(b) >= opaquePixels(a) {
		t.Errorf("islands preview has %d opaque pixels, solid %d: islands should paint no ground", opaquePixels(b), opaquePixels(a))
	}
}

func TestStyleRendersOnePictureCardPerOption(t *testing.T) {
	reg, h := openSettingsForPreview(t)
	reg.mu.Lock()
	defer reg.mu.Unlock()
	cards := settingsPictureCards(reg, h, *h.set.ByPath("bar.style"))
	if len(cards.Children) != 3 {
		t.Fatalf("%d style cards, want 3", len(cards.Children))
	}
	for i, opt := range []string{"frosted", "solid", "islands"} {
		c := cards.Children[i]
		imgs := findAllKind(c, ui.KindImage)
		if c.Action != "pick:bar.style="+opt || len(imgs) == 0 || imgs[0].Image == nil {
			t.Errorf("card %d = %q without a picture", i, c.Action)
		}
		if (c.State&ui.StateSelected != 0) != (opt == h.draft.Bar.Style) {
			t.Errorf("card %s selection is wrong", opt)
		}
	}
	for _, out := range [][2]int{{1280, 720}, {1536, 864}} {
		h.place.Panel = settingsPanelSize(out[0], out[1])
		for _, scale := range []int{120, 150, 180} {
			h.scale120 = scale
			for _, path := range []string{"bar.style", "bar.shape"} {
				row := settingsPictureCards(reg, h, *h.set.ByPath(path))
				if err := ui.LayoutColumn(&ui.Node{Kind: ui.KindColumn, Children: []*ui.Node{row}},
					ui.Rect{W: settingsCardInner(h), H: 400}, h.measureText()); err != nil {
					t.Errorf("%v %s @%d: %v", out, path, scale, err)
				}
			}
		}
	}
}

func TestPreviewKeepsTheLastGoodImageOnABadDraft(t *testing.T) {
	reg, h := openSettingsForPreview(t)
	reg.mu.Lock()
	defer reg.mu.Unlock()
	good := settingsBarPreview(reg, h)
	h.draft.Bar.Gap = -5 // fails theme validation
	bad := settingsBarPreview(reg, h)
	gi, bi := findAllKind(good, ui.KindImage), findAllKind(bad, ui.KindImage)
	if len(gi) == 0 || gi[0].Image == nil {
		t.Fatal("no good preview to start from")
	}
	if len(bi) == 0 || bi[0].Image != gi[0].Image {
		t.Fatal("a failing draft replaced the preview instead of keeping the last good image")
	}
}

func TestBarAppearanceLeadsWithPreviewAndCards(t *testing.T) {
	reg := newPanelRegistry(t)
	withTestBar(t, reg, 7, reg.cfg)
	if err := reg.OpenPanel(PanelSettings, 7, Trigger{BarEdge: "top", BarZone: 40, OutW: 1536, OutH: 864}); err != nil {
		t.Fatal(err)
	}
	reqs := drainAux(t, reg, 2)
	reg.mu.Lock()
	defer reg.mu.Unlock()
	h := reg.panelHosts[PanelSettings]
	if err := h.configure(int(reqs[1].Open.Width), int(reqs[1].Open.Height), 150); err != nil {
		t.Fatal(err)
	}
	body := findScroll(h.root)
	if body == nil || len(body.Children) < 3 {
		t.Fatal("Appearance body is missing its blocks")
	}
	if len(findAllKind(body.Children[0], ui.KindImage)) == 0 {
		t.Error("the first block is not the preview")
	}
	if findAction(body, "pick:bar.style=islands") == nil || findAction(body, "pick:bar.shape=floating") == nil {
		t.Fatal("Style or Shape picture cards are missing")
	}
	style := findAction(body, "pick:bar.style=frosted")
	frost := findAction(body, "set:bar.frost-opacity")
	if style == nil || frost == nil || style.Bounds.Y > frost.Bounds.Y {
		t.Errorf("Style (%v) does not sit above the frost sliders (%v)", style, frost)
	}
	if view := body.Bounds.H; style != nil && style.Bounds.Y > body.Bounds.Y+view {
		t.Errorf("Style at y=%d is below the %d-tall viewport", style.Bounds.Y, view)
	}
}

func TestFrostDimsWhenTheStyleIsSolidAndAllDimWhenTheBarIsOff(t *testing.T) {
	t.Parallel()
	h := newSettingsHost()
	h.draft.Bar.Style = "solid"
	h.root = settingsTree(nil, h)
	if frost := findAction(h.root, "set:bar.frost-opacity"); frost == nil || frost.State&ui.StateDisabled == 0 {
		t.Fatal("frost opacity is live under a solid bar")
	}
	if hgt := findAction(h.root, "set:bar.height"); hgt != nil && hgt.State&ui.StateDisabled != 0 {
		t.Fatal("bar height dimmed under a solid bar")
	}
	h.draft.Bar.Style = "frosted"
	h.draft.Bar.Enabled = false
	h.root = settingsTree(nil, h)
	dimmed := 0
	for _, n := range walk(h.root) {
		if strings.HasPrefix(n.Action, "set:bar.") && n.Action != "set:bar.enabled" {
			if n.State&ui.StateDisabled == 0 {
				t.Errorf("%s is live with the bar disabled", n.Action)
			}
			dimmed++
		}
	}
	if dimmed == 0 {
		t.Fatal("no bar rows found to dim")
	}
	if n := findAction(h.root, "set:bar.enabled"); n == nil || n.State&ui.StateDisabled != 0 {
		t.Fatal("the Enabled toggle itself was disabled")
	}
}
