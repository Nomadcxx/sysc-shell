package render

import (
	"fmt"
	"math"
	"strings"

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
	if n != nil && n.Icon == "" && n.ValueText != "" &&
		(strings.ContainsRune(n.ValueText, '°') || n.ValueText == "temperature") {
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

func radialIconSize(size int) int {
	if size < 32 {
		// ponytail: keep the established 22px bar glyph proportion; larger
		// rings use the box-derived proportion below.
		return max(size/2, 1)
	}
	return max(size*7/20, 1)
}

func radialValueSize(size int, value string) int {
	if size < 32 {
		return max(size*4/11, 1)
	}
	valueSize := max(size/3, 1)
	if len([]rune(value)) >= 4 {
		valueSize = max(min(valueSize, size*3/10), 1)
	}
	return valueSize
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

	// A bar glyph stays a hairline on the bright on-surface track it has
	// always used, where a thin ring needs the contrast to register at all.
	// A panel-sized ring scales its band with the circle and drops the track
	// to a container tone: at that weight the old track outshouts the arc,
	// and the arc is what the eye is meant to land on.
	hairline := max(style.Scale120.Physical(2), 1)
	stroke := max(size/14, hairline)
	trackColor := style.Track
	if stroke > hairline {
		trackColor = style.ContainerHighest
	}
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
			col := trackColor
			if activeCoverage > 0 {
				progress := 0.0
				if limit > 0 && angle <= limit {
					progress = angle / limit
				} else if math.Hypot(dx-math.Sin(limit)*radius, dy+math.Cos(limit)*radius) < math.Hypot(dx, dy+radius) {
					progress = 1
				}
				active := radialArcColor(style, n, progress)
				col = LerpColor(trackColor, active, activeCoverage/coverage)
			}
			blendCoverage(c, x, y, col, coverage)
		}
	}

	centre := ui.Rect{X: box.X, Y: box.Y, W: box.W, H: box.H}
	if !n.Absent && n.Icon != "" {
		mask, err := text.RasterProjectIcon(n.Icon, radialIconSize(size))
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
	spec := textSpec(style, n).AtSize(radialValueSize(size, value))
	return paintCentredTextColor(c, value, centre, text, spec, true, textColor(style, n.Tone), false)
}
