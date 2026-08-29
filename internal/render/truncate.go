package render

import (
	"slices"

	"github.com/go-text/typesetting/shaping"
	"golang.org/x/image/math/fixed"
)

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
	fullGlyphs, fullAdvance, err := r.shapedGlyphs(text, size)
	if err != nil {
		return "", 0, err
	}
	if fullAdvance.Ceil() <= avail {
		return text, fullAdvance.Ceil(), nil
	}

	_, dotsAdvance, err := r.shapedGlyphs(ellipsis, size)
	if err != nil {
		return "", 0, err
	}
	dotsWidth := dotsAdvance.Ceil()
	if dotsWidth > avail {
		return "", 0, nil
	}

	budget := avail - dotsWidth
	runes := []rune(text)
	keep, _ := clusterPrefix(fullGlyphs, budget)
	if keep > len(runes) {
		keep = len(runes)
	}
	if keep <= 0 {
		return ellipsis, dotsWidth, nil
	}

	clusters := logicalClusters(fullGlyphs)
	for keep > 0 {
		out := string(runes[:keep]) + ellipsis
		_, advance, err := r.shapedGlyphs(out, size)
		if err != nil {
			return "", 0, err
		}
		if width := advance.Ceil(); width <= avail {
			return out, width, nil
		}
		for i := len(clusters) - 1; i >= 0; i-- {
			if clusters[i].end <= keep {
				keep = clusters[i].start
				break
			}
		}
	}
	return ellipsis, dotsWidth, nil
}

func (r *TextRenderer) shapedGlyphs(text string, size int) ([]shaping.Glyph, fixed.Int26_6, error) {
	runs, err := r.shapeRuns(text, size)
	if err != nil {
		return nil, 0, err
	}
	var glyphs []shaping.Glyph
	var advance fixed.Int26_6
	for _, run := range runs {
		for _, glyph := range run.output.Glyphs {
			glyph.ClusterIndex += run.runeStart
			glyphs = append(glyphs, glyph)
		}
		advance += run.output.Advance
	}
	return glyphs, advance, nil
}

type glyphCluster struct {
	start, end int
	advance    fixed.Int26_6
}

// logicalClusters groups every glyph in a shaping cluster and returns the
// clusters in logical text order. Shapers may emit RTL glyphs in visual order,
// so output slice order is not a safe prefix order.
func logicalClusters(glyphs []shaping.Glyph) []glyphCluster {
	byStart := make(map[int]glyphCluster, len(glyphs))
	for _, glyph := range glyphs {
		start := glyph.TextIndex()
		cluster := byStart[start]
		cluster.start = start
		cluster.end = max(cluster.end, start+glyph.RunesCount())
		cluster.advance += glyph.Advance
		byStart[start] = cluster
	}
	clusters := make([]glyphCluster, 0, len(byStart))
	for _, cluster := range byStart {
		clusters = append(clusters, cluster)
	}
	slices.SortFunc(clusters, func(a, b glyphCluster) int { return a.start - b.start })
	return clusters
}

// clusterPrefix reports the longest complete logical cluster prefix within a
// whole-pixel width budget.
func clusterPrefix(glyphs []shaping.Glyph, budget int) (keep int, used fixed.Int26_6) {
	for _, cluster := range logicalClusters(glyphs) {
		next := used + cluster.advance
		if next.Ceil() > budget {
			break
		}
		used = next
		keep = cluster.end
	}
	return keep, used
}
