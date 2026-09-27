package shell

import (
	"github.com/Nomadcxx/sysc-shell/internal/config"
	"github.com/Nomadcxx/sysc-shell/internal/settings"
	"github.com/Nomadcxx/sysc-shell/internal/theme"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

// settingsBarImage paints the bar cfg describes, width logical pixels wide, at
// the panel's scale (settings redesign D6). The bar's own painter draws it, so
// the picture cannot drift from what the setting does. It resolves as if the
// compositor blurs, so a translucent style shows as translucent. Nil when cfg
// does not resolve. Callers hold r.mu.
func (r *Registry) settingsBarImage(h *PanelHost, cfg config.Config, width int) *ui.Image {
	if width <= 0 {
		return nil
	}
	connector := ""
	if bar, ok := r.bars[h.output]; ok {
		connector = bar.connector()
	}
	policy := cfg.ForConnector(connector)
	t, err := resolveOutputTheme(cfg, connector, r.tokens, true)
	if err != nil {
		return nil
	}
	bar, err := NewWithTheme(t, policy, connector)
	if err != nil {
		return nil
	}
	defer bar.stopAnimation()
	bar.apply(r.viewLocked(connector))
	height := policy.SurfaceExtent()
	scale := h.scale120
	if !ui.Scale120(scale).Valid() {
		scale = int(ui.ScaleUnit)
	}
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
	if img := r.settingsBarImage(h, h.draft, layoutW); img != nil {
		h.barPreview = img
	}
	extent := h.draft.ForConnector("").SurfaceExtent()
	return settingsGroupCard(h, "Preview", []*ui.Node{{
		Kind: ui.KindImage, Image: h.barPreview, ImageW: w,
		ImageH: max(extent*w/max(layoutW, 1), 1),
		Name:   "Bar preview", Role: "img",
	}})
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
		imgH := cfg.ForConnector("").SurfaceExtent()
		_, labelH := measure(label, ui.TextAttrs{})
		// A button sizes to a control, not to its content, so the card states
		// the height its picture and label need.
		card := &ui.Node{
			Kind: ui.KindButton, Action: "pick:" + e.Path + "=" + opt,
			Name: label, Role: "radio", Focusable: true, Shape: ui.ShapeCard,
			Fill: ui.FillContainerHighest, Padding: pad, Width: cardW,
			Height: imgH + theme.MarginS + labelH + 2*pad,
			Children: []*ui.Node{{Kind: ui.KindColumn, Gap: theme.MarginS, Children: []*ui.Node{
				// The left end of the bar at the output's width: where the
				// ground, the pills and an attached or floating end differ.
				{Kind: ui.KindImage, Image: settingsBarEnd(r.settingsBarImage(h, cfg, settingsPreviewLayoutW(h, imgW)), imgW, h.scale120), ImageW: imgW, ImageH: imgH},
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
