package render

import (
	"fmt"
	"math"

	"github.com/Nomadcxx/sysc-shell/internal/theme"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

func radialWarningAmber(style Style) Color {
	amber := theme.Color{R: 0xff, G: 0xb3, B: 0x00, A: style.Accent.A}
	track := theme.Color{R: style.Track.R, G: style.Track.G, B: style.Track.B, A: style.Track.A}
	amber = theme.EnsureContrast(amber, track, 3)
	return Color{R: amber.R, G: amber.G, B: amber.B, A: amber.A}
}

func radialArcColor(style Style, n *ui.Node, progress float64) Color {
	end := style.Secondary
	if n != nil && n.Icon == "" && n.ValueText != "" {
		temperature := n.Value * 100
		amber := radialWarningAmber(style)
		switch {
		case temperature < 60:
			end = style.Accent
		case temperature < 75:
			end = LerpColor(style.Accent, amber, (temperature-60)/15)
		case temperature < 85:
			end = LerpColor(amber, style.Error, (temperature-75)/10)
		default:
			end = style.Error
		}
	}
	return LerpColor(style.Accent, end, progress)
}

func radialBandCoverage(distance, radius, halfStroke float64) float64 {
	return min(max(halfStroke+.5-math.Abs(distance-radius), 0), 1)
}

func radialCapCoverage(dx, dy, capX, capY, radius float64) float64 {
	return min(max(radius+.5-math.Hypot(dx-capX, dy-capY), 0), 1)
}

func blendCoverage(c *Canvas, x, y int, col Color, coverage float64) {
	if coverage <= 0 || col.A == 0 {
		return
	}
	coverage = min(coverage, 1)
	src := col.premultiply()
	for i := range src {
		src[i] = byte(math.Round(float64(src[i]) * coverage))
	}
	alpha := uint32(math.Round(float64(col.A) * coverage))
	blendPixel(c.Pix[y*c.Stride+x*4:y*c.Stride+x*4+4], src, alpha)
}

func paintRadialGauge(c *Canvas, n *ui.Node, text *TextRenderer, style Style) error {
	box := style.Scale120.PhysicalRect(n.Bounds)
	size := min(box.W, box.H)
	if size <= 0 {
		return nil
	}
	box.X += (box.W - size) / 2
	box.Y += (box.H - size) / 2
	box.W, box.H = size, size

	stroke := max(style.Scale120.Physical(2), 1)
	outer := float64(size)/2 - 0.5
	inner := outer - float64(stroke)
	radius := (outer + inner) / 2
	halfStroke := float64(stroke) / 2
	cx, cy := float64(box.X)+float64(size)/2, float64(box.Y)+float64(size)/2
	fraction := min(max(n.Value, 0), 1)
	limit := fraction * 2 * math.Pi
	x0, y0, x1, y1 := c.clip(box)
	for y := y0; y < y1; y++ {
		for x := x0; x < x1; x++ {
			dx, dy := float64(x)+0.5-cx, float64(y)+0.5-cy
			distance := math.Hypot(dx, dy)
			trackCoverage := radialBandCoverage(distance, radius, halfStroke)
			activeCoverage := 0.0
			angle := math.Atan2(dx, -dy)
			if angle < 0 {
				angle += 2 * math.Pi
			}
			if !n.Absent && fraction > 0 {
				if fraction >= 1 || angle <= limit {
					activeCoverage = trackCoverage
				}
				if fraction < 1 {
					start := radialCapCoverage(dx, dy, 0, -radius, halfStroke)
					end := radialCapCoverage(dx, dy, math.Sin(limit)*radius, -math.Cos(limit)*radius, halfStroke)
					activeCoverage = max(activeCoverage, max(start, end))
				}
			}
			coverage := max(trackCoverage, activeCoverage)
			if coverage <= 0 {
				continue
			}
			col := style.Track
			if activeCoverage > 0 {
				progress := 0.0
				if limit > 0 && angle <= limit {
					progress = angle / limit
				} else if math.Hypot(dx-math.Sin(limit)*radius, dy+math.Cos(limit)*radius) < math.Hypot(dx, dy+radius) {
					progress = 1
				}
				active := radialArcColor(style, n, progress)
				col = LerpColor(style.Track, active, activeCoverage/coverage)
			}
			blendCoverage(c, x, y, col, coverage)
		}
	}

	centre := ui.Rect{X: box.X, Y: box.Y, W: box.W, H: box.H}
	if !n.Absent && n.Icon != "" {
		mask, err := text.RasterProjectIcon(n.Icon, style.Scale120.Physical(11))
		if err != nil {
			return err
		}
		paintCentredMask(c, mask, centre, textColor(style, n.Tone))
		return nil
	}
	value := n.ValueText
	if n.Absent {
		value = "—"
	} else if value == "" {
		value = fmt.Sprintf("%.0f%%", fraction*100)
	}
	spec := textSpec(style, n).AtSize(style.Scale120.Physical(8))
	return paintCentredTextColor(c, value, centre, text, spec, true, textColor(style, n.Tone), false)
}
