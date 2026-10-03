package shell

import (
	"github.com/Nomadcxx/sysc-shell/internal/theme"
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

// TestAppearanceFillsThePaneAndCardsHoldTheirContent is the live gate's
// finding on the desktop: the scrolling body sat inside a column, which gave it
// the layout's 240 fallback and cut the page off after Shape; and a picture
// card was a control's height, so its picture spilled out and its label had
// no room.
func TestAppearanceFillsThePaneAndCardsHoldTheirContent(t *testing.T) {
	reg := newPanelRegistry(t)
	withTestBar(t, reg, 7, reg.cfg)
	if err := reg.OpenPanel(PanelSettings, 7, Trigger{BarEdge: "top", BarZone: 40, OutW: 3440, OutH: 1440}); err != nil {
		t.Fatal(err)
	}
	open := drainAux(t, reg, 2)[1].Open
	reg.mu.Lock()
	defer reg.mu.Unlock()
	h := reg.panelHosts[PanelSettings]
	if err := h.configure(int(open.Width), int(open.Height), 120); err != nil {
		t.Fatal(err)
	}
	body := findScroll(h.root)
	if body == nil || body.Bounds.H < int(open.Height)/2 {
		t.Fatalf("scrolling body is %+v in a %d-tall pane", body.Bounds, open.Height)
	}
	for _, opt := range []string{"frosted", "solid", "islands"} {
		card := findAction(h.root, "pick:bar.style="+opt)
		img := findAllKind(card, ui.KindImage)
		label := findNode(card, func(n *ui.Node) bool { return n.Kind == ui.KindText })
		if len(img) == 0 || label == nil {
			t.Fatalf("%s card lacks a picture or a label", opt)
		}
		inside := func(r ui.Rect) bool {
			b := card.Bounds
			return r.W > 0 && r.H > 0 && r.X >= b.X && r.Y >= b.Y && r.X+r.W <= b.X+b.W && r.Y+r.H <= b.Y+b.H
		}
		if !inside(img[0].Bounds) || !inside(label.Bounds) {
			t.Errorf("%s card %+v does not hold its picture %+v and label %+v", opt, card.Bounds, img[0].Bounds, label.Bounds)
		}
	}
}

// Found on the laptop at 1.25: the preview laid out at the card's width
// crowded the centre into the window title, Edge drew as a clipped one-row
// menu, and sliders showed no value.
func TestLaptopGateFixes(t *testing.T) {
	reg, h := openSettingsForPreview(t)
	reg.mu.Lock()
	defer reg.mu.Unlock()
	if h.place.Output.W != 1536 {
		t.Fatalf("settings placed against a %d-wide output", h.place.Output.W)
	}
	if got := settingsPreviewLayoutW(h, settingsCardInner(h)); got != 1536 {
		t.Errorf("preview lays out at %d, want the output's 1536", got)
	}
	preview := findAllKind(settingsBarPreview(reg, h), ui.KindImage)[0]
	if preview.Image == nil || preview.Image.Width < 1536*h.scale120/120 {
		t.Errorf("preview raster %+v is narrower than the output", preview.Image)
	}
	if edge := settingsControl(h, *h.set.ByPath("bar.edge"), 200); edge.Kind != ui.KindText || edge.Text != "Top" {
		t.Errorf("one-option Edge renders as %v %q, want the text Top", edge.Kind, edge.Text)
	}
	frost := settingsControl(h, *h.set.ByPath("bar.frost-opacity"), 300)
	if frost.Kind != ui.KindRow || len(frost.Children) != 2 || frost.Children[1].Kind != ui.KindText || frost.Children[1].Text == "" {
		t.Fatalf("frost opacity control has no value cell: %+v", frost)
	}
	if frost.Children[0].Width+theme.MarginS+frost.Children[1].Width > 300 {
		t.Errorf("slider and value overrun their 300 column")
	}
}

func TestSideBarPreviewUsesUprightOutputGeometry(t *testing.T) {
	reg, h := openSettingsForPreview(t)
	reg.mu.Lock()
	defer reg.mu.Unlock()
	h.place.Output = ui.Rect{W: 1536, H: 864}
	h.draft.Bar.Edge = "left"

	preview := settingsBarPreview(reg, h)
	n := findAllKind(preview, ui.KindImage)[0]
	wantW := h.draft.Bar.SurfaceExtent() * h.scale120 / 120
	wantH := h.place.Output.H * h.scale120 / 120
	const maxSidePreviewHeight = 240
	if n.Image == nil || n.Image.Width != wantW || n.Image.Height != wantH {
		if n.Image == nil {
			t.Fatal("side preview has no raster")
		}
		t.Fatalf("side preview raster = %dx%d, want %dx%d from (SurfaceExtent, output.H)", n.Image.Width, n.Image.Height, wantW, wantH)
	}
	if n.ImageH <= 0 || n.ImageH > maxSidePreviewHeight {
		t.Fatalf("side preview display height = %d, want a positive height at most %d", n.ImageH, maxSidePreviewHeight)
	}
	delta := n.ImageW*h.place.Output.H - n.ImageH*h.draft.Bar.SurfaceExtent()
	if delta < 0 {
		delta = -delta
	}
	if delta > h.place.Output.H/2 {
		t.Errorf("side preview display %dx%d distorts the %dx%d strip", n.ImageW, n.ImageH, h.draft.Bar.SurfaceExtent(), h.place.Output.H)
	}

	cards := settingsPictureCards(reg, h, *h.set.ByPath("bar.style"))
	cardImage := findAllKind(cards.Children[0], ui.KindImage)[0].Image
	if cardImage == nil || cardImage.Width != wantW || cardImage.Height >= wantH {
		t.Fatalf("side style card image = %v, want a top-end crop of the upright strip", cardImage)
	}
	if len(cardImage.Pix) == 0 || &cardImage.Pix[0] != &n.Image.Pix[0] {
		t.Error("style card did not crop the top end of the cached side image")
	}

	good := n.Image
	h.place.Output.H = 1000
	resized := reg.settingsBarImage(h, h.draft, settingsPreviewLayoutW(h, settingsCardInner(h)))
	if resized == nil || resized == good || resized.Height != 1000*h.scale120/120 {
		if resized == nil {
			t.Error("output-height change produced no side preview")
		} else {
			t.Errorf("output-height change reused an incompatible %dx%d preview", resized.Width, resized.Height)
		}
	}
	h.scale120 = 150
	scaled := reg.settingsBarImage(h, h.draft, settingsPreviewLayoutW(h, settingsCardInner(h)))
	if scaled == nil || scaled == resized || scaled.Width != wantW*150/120 || scaled.Height != 1000*150/120 {
		if scaled == nil {
			t.Error("scale change produced no side preview")
		} else {
			t.Errorf("scale change reused an incompatible %dx%d preview", scaled.Width, scaled.Height)
		}
	}

	h.barPreview = scaled
	h.draft.Bar.Gap = -5
	bad := findAllKind(settingsBarPreview(reg, h), ui.KindImage)[0].Image
	if bad != scaled {
		t.Error("an invalid side draft replaced the last good image")
	}
}
