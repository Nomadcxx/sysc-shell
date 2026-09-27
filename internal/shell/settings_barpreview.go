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

// settingsBarPreview is the Appearance page's lead card: the draft's bar at
// the page's width. A draft that does not resolve keeps the last good image,
// so a value that is mid-edit does not blank the preview.
func settingsBarPreview(r *Registry, h *PanelHost) *ui.Node {
	w := settingsCardInner(h)
	if img := r.settingsBarImage(h, h.draft, w); img != nil {
		h.barPreview = img
	}
	return settingsGroupCard(h, "Preview", []*ui.Node{{
		Kind: ui.KindImage, Image: h.barPreview, ImageW: w,
		ImageH: h.draft.ForConnector("").SurfaceExtent(),
		Name:   "Bar preview", Role: "img",
	}})
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
	row := &ui.Node{Kind: ui.KindRow, Gap: theme.MarginM, Name: e.Label, Role: "radiogroup"}
	for _, opt := range e.Options {
		cfg := h.draft
		if e.Set == nil || e.Set(&cfg, opt) != nil {
			continue
		}
		label := settingsOptionLabel(opt)
		card := &ui.Node{
			Kind: ui.KindButton, Action: "pick:" + e.Path + "=" + opt,
			Name: label, Role: "radio", Focusable: true, Shape: ui.ShapeCard,
			Fill: ui.FillContainerHighest, Padding: pad, Width: cardW,
			Children: []*ui.Node{{Kind: ui.KindColumn, Gap: theme.MarginS, Children: []*ui.Node{
				{
					Kind: ui.KindImage, Image: r.settingsBarImage(h, cfg, imgW),
					ImageW: imgW, ImageH: cfg.ForConnector("").SurfaceExtent(),
				},
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
