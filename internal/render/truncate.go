package render

import "golang.org/x/image/math/fixed"

// ellipsis ends a truncated run. It is shaped as its own single-cluster run.
const ellipsis = "…"

// Truncate fits text into avail pixels, appending an ellipsis when it does not
// fit whole. It reports the text to draw and that text's advance.
//
// Cutting happens on cluster boundaries taken from the shaping output, never on
// runes or bytes, so a ligature or a mark sequence is never split. Advances
// accumulate in 26.6 fixed point and round once at the end, because rounding
// every glyph would drift wider than the run actually is.
//
// When even the ellipsis does not fit the result is empty: an overflowing
// ellipsis is never drawn.
func (r *TextRenderer) Truncate(text string, size, avail int) (string, int, error) {
	if avail <= 0 || text == "" {
		return "", 0, nil
	}
	full, err := r.Shape(text, size)
	if err != nil {
		return "", 0, err
	}
	if full.Advance.Ceil() <= avail {
		return text, full.Advance.Ceil(), nil
	}

	dots, err := r.Shape(ellipsis, size)
	if err != nil {
		return "", 0, err
	}
	dotsWidth := dots.Advance.Ceil()
	if dotsWidth > avail {
		return "", 0, nil
	}

	budget := avail - dotsWidth
	runes := []rune(text)
	var used fixed.Int26_6
	keep := 0
	for _, g := range full.Glyphs {
		next := used + g.Advance
		if next.Ceil() > budget {
			break
		}
		used = next
		if end := g.TextIndex() + g.RunesCount(); end > keep {
			keep = end
		}
	}
	if keep > len(runes) {
		keep = len(runes)
	}
	if keep <= 0 {
		return ellipsis, dotsWidth, nil
	}

	out := string(runes[:keep]) + ellipsis
	shaped, err := r.Shape(out, size)
	if err != nil {
		return "", 0, err
	}
	return out, shaped.Advance.Ceil(), nil
}
