package shell

import (
	"fmt"
	"hash/fnv"
	"strconv"

	"github.com/Nomadcxx/sysc-shell/internal/config"
	"github.com/Nomadcxx/sysc-shell/internal/settings"
	"github.com/Nomadcxx/sysc-shell/internal/theme"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

const settingsSidePreviewMaxH = 240

// settingsBarImage paints the bar cfg describes, width logical pixels wide, at
// the panel's scale (settings redesign D6). The bar's own painter draws it, so
// the picture cannot drift from what the setting does. It resolves as if the
// compositor blurs, so a translucent style shows as translucent. Nil when cfg
// does not resolve. Callers hold r.mu.
func (r *Registry) settingsBarImage(h *PanelHost, cfg config.Config, width int) *ui.Image {
	layoutW, layoutH := settingsBarLayoutSize(h, cfg, width)
	if layoutW <= 0 || layoutH <= 0 {
		return nil
	}
	scale := h.scale120
	if !ui.Scale120(scale).Valid() {
		scale = int(ui.ScaleUnit)
	}
	key := settingsBarImageKey(cfg, r.tokens, layoutW, layoutH, scale)
	if img, ok := h.barImages[key]; ok {
		return img
	}
	img := r.paintSettingsBar(h, cfg, layoutW, layoutH, scale)
	if img == nil {
		return nil
	}
	// A handful of pictures per page; a long session of edits would
	// otherwise keep every one it ever drew.
	if h.barImages == nil || len(h.barImages) >= 32 {
		h.barImages = map[string]*ui.Image{}
	}
	h.barImages[key] = img
	return img
}

// settingsBarImageKey names a picture by everything it is painted from except
// the live widget data: the draft, palette, layout dimensions and scale. A
// clock in a cached picture keeps the minute it was drawn at.
func settingsBarImageKey(cfg config.Config, tok theme.Tokens, width, height, scale int) string {
	f := fnv.New64a()
	fmt.Fprintf(f, "%v|%v|%d|%d|%d", cfg, tok, width, height, scale)
	return strconv.FormatUint(f.Sum64(), 16)
}

func settingsBarLayoutSize(h *PanelHost, cfg config.Config, width int) (int, int) {
	if cfg.Bar.Edge == "left" || cfg.Bar.Edge == "right" {
		return cfg.Bar.SurfaceExtent(), h.place.Output.H
	}
	return width, cfg.Bar.SurfaceExtent()
}

// paintSettingsBar draws the shared bar policy, cfg.Bar, which is the one
// Appearance edits. The output's own override would ignore the draft's Style
// and Shape on an output that has one.
func (r *Registry) paintSettingsBar(h *PanelHost, cfg config.Config, width, height, scale int) *ui.Image {
	connector := ""
	if bar, ok := r.bars[h.output]; ok {
		connector = bar.connector()
	}
	policy := cfg.Bar
	resolved, err := ResolveTheme(cfg, policy, r.tokens)
	if err != nil {
		return nil
	}
	t := resolved.WithCompositor(true)
	bar, err := NewWithTheme(t, policy, connector)
	if err != nil {
		return nil
	}
	defer bar.stopAnimation()
	bar.apply(r.viewLocked(connector))
	if err := bar.Configure(width, height, scale); err != nil {
		return nil
	}
	pw, ph := width*scale/120, height*scale/120
	pix := make([]byte, pw*ph*4)
	if err := bar.Render(pix, pw, ph, pw*4); err != nil {
		return nil
	}
	return &ui.Image{Width: pw, Height: ph, Stride: pw * 4, Pix: pix}
}

// settingsPreviewLayoutW is the width the preview bar lays out at: the
// output's, so its widgets sit as they do on screen. Laid out at the card's
// width instead, the centre crowded into the window title on the laptop.
func settingsPreviewLayoutW(h *PanelHost, least int) int {
	return max(h.place.Output.W, least)
}

// settingsBarPreview is the Appearance page's lead card: the draft's bar laid
// out at the output's width and scaled down to the card. A draft that does not
// resolve keeps the last good image, so a value that is mid-edit does not
// blank the preview.
func settingsBarPreview(r *Registry, h *PanelHost) *ui.Node {
	w := settingsCardInner(h)
	layoutW := settingsPreviewLayoutW(h, w)
	layoutW, layoutH := settingsBarLayoutSize(h, h.draft, layoutW)
	if img := r.settingsBarImage(h, h.draft, layoutW); img != nil {
		h.barPreview = img
	}
	imageW, imageH := w, max(h.draft.Bar.SurfaceExtent()*w/max(layoutW, 1), 1)
	if h.draft.Bar.Edge == "left" || h.draft.Bar.Edge == "right" {
		imageW, imageH = fitSettingsPreview(layoutW, layoutH, w, settingsSidePreviewMaxH)
	}
	return settingsGroupCard(h, "Preview", []*ui.Node{{
		Kind: ui.KindRow, CenterX: true, Children: []*ui.Node{{
			Kind: ui.KindImage, Image: h.barPreview, ImageW: imageW, ImageH: imageH,
			Name: "Bar preview", Role: "img",
		}},
	}})
}

func fitSettingsPreview(width, height, maxWidth, maxHeight int) (int, int) {
	if width <= 0 || height <= 0 || maxWidth <= 0 || maxHeight <= 0 {
		return 0, 0
	}
	if width*maxHeight > height*maxWidth {
		return maxWidth, max(1, height*maxWidth/width)
	}
	return max(1, width*maxHeight/height), maxHeight
}

// settingsBarEnd is the left end of a full-width bar image, width logical
// pixels of it, without copying: the painter reads rows by stride, so a
// narrower Width over the same pixels is a crop.
func settingsBarEnd(img *ui.Image, width, scale120 int) *ui.Image {
	if img == nil {
		return nil
	}
	if !ui.Scale120(scale120).Valid() {
		scale120 = int(ui.ScaleUnit)
	}
	crop := *img
	crop.Width = min(img.Width, width*scale120/120)
	return &crop
}

// settingsBarTop is the vertical counterpart to settingsBarEnd: cards show the
// first, top-end segment of an upright side bar without copying its pixels.
func settingsBarTop(img *ui.Image, height, scale120 int) *ui.Image {
	if img == nil {
		return nil
	}
	if !ui.Scale120(scale120).Valid() {
		scale120 = int(ui.ScaleUnit)
	}
	crop := *img
	crop.Height = min(img.Height, height*scale120/120)
	return &crop
}

// settingsPictureCardW is the widest a picture card's bar segment gets.
const settingsPictureCardW = 240

// settingsPictureCards shows every option of e as a card with its own
// preview: the draft with that one value substituted (design D2, D6). The
// cards share the row, so on a narrow pane each narrows rather than
// overflowing.
func settingsPictureCards(r *Registry, h *PanelHost, e settings.Entry) *ui.Node {
	raw := ""
	if e.Get != nil {
		raw = e.Get(h.draft)
	}
	pad := h.metrics().CardPadding
	n := max(len(e.Options), 1)
	cardW := (settingsCardInner(h) - (n-1)*theme.MarginM) / n
	imgW := min(settingsPictureCardW, max(cardW-2*pad, 0))
	measure := settingsMeasure(h)
	row := &ui.Node{Kind: ui.KindRow, Gap: theme.MarginM, Name: e.Label, Role: "radiogroup"}
	for _, opt := range e.Options {
		cfg := h.draft
		if e.Set == nil || e.Set(&cfg, opt) != nil {
			continue
		}
		label := settingsOptionLabel(opt)
		imgH := cfg.Bar.SurfaceExtent()
		imgWForNode := imgW
		img := r.settingsBarImage(h, cfg, settingsPreviewLayoutW(h, imgW))
		if cfg.Bar.Edge == "left" || cfg.Bar.Edge == "right" {
			imgH = min(imgW, h.place.Output.H)
			imgWForNode = cfg.Bar.SurfaceExtent()
			img = settingsBarTop(img, imgH, h.scale120)
		} else {
			img = settingsBarEnd(img, imgW, h.scale120)
		}
		_, labelH := measure(label, ui.TextAttrs{})
		// A button sizes to a control, not to its content, so the card states
		// the height its picture and label need.
		card := &ui.Node{
			Kind: ui.KindButton, Action: "pick:" + e.Path + "=" + opt,
			Name: label, Role: "radio", Focusable: true, Shape: ui.ShapeCard,
			Fill: ui.FillContainerHighest, Padding: pad, Width: cardW,
			Height: imgH + theme.MarginS + labelH + 2*pad,
			Children: []*ui.Node{{Kind: ui.KindColumn, Gap: theme.MarginS, Children: []*ui.Node{
				// Horizontal cards crop the left end; side cards crop the top
				// end while keeping the rendered strip upright.
				{Kind: ui.KindRow, CenterX: true, Children: []*ui.Node{{Kind: ui.KindImage, Image: img, ImageW: imgWForNode, ImageH: imgH}}},
				{Kind: ui.KindText, Text: label},
			}}},
		}
		if opt == raw {
			card.State |= ui.StateSelected
		}
		row.Children = append(row.Children, card)
	}
	return row
}
