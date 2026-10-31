package render

import (
	"fmt"
	"math"
	"strings"

	"github.com/Nomadcxx/sysc-shell/internal/theme"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

func radialWarningAmber(style Style, track Color) Color {
	amber := theme.Color{R: 0xff, G: 0xb3, B: 0x00, A: style.Accent.A}
	background := theme.Color{R: track.R, G: track.G, B: track.B, A: track.A}
	amber = theme.EnsureContrast(amber, background, 3)
	return Color{R: amber.R, G: amber.G, B: amber.B, A: amber.A}
}

func radialArcColor(style Style, n *ui.Node, progress float64, track Color) Color {
	end := style.Secondary
	if n != nil && n.Icon == "" && n.ValueText != "" &&
		(strings.ContainsRune(n.ValueText, '°') || n.ValueText == "temperature") {
		temperature := n.Value * 100
		amber := radialWarningAmber(style, track)
		error := theme.EnsureContrast(
			theme.Color{R: style.Error.R, G: style.Error.G, B: style.Error.B, A: style.Error.A},
			theme.Color{R: track.R, G: track.G, B: track.B, A: track.A}, 3,
		)
		errorColor := Color{R: error.R, G: error.G, B: error.B, A: error.A}
		switch {
		case temperature < 60:
			end = style.Accent
		case temperature < 75:
			end = LerpColor(style.Accent, amber, (temperature-60)/15)
		case temperature < 85:
			end = LerpColor(amber, errorColor, (temperature-75)/10)
		default:
			end = errorColor
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
	if c.restrict.W > 0 && c.restrict.H > 0 &&
		(x < c.restrict.X || y < c.restrict.Y || x >= c.restrict.X+c.restrict.W || y >= c.restrict.Y+c.restrict.H) {
		return
	}
	if !c.contains(x, y) {
		return
	}
	coverage = min(coverage, 1)
	src := col.premultiply()
	for i := range src {
		src[i] = byte(math.Round(float64(src[i]) * coverage))
	}
	alpha := uint32(math.Round(float64(col.A) * coverage))
	at := c.at(x, y)
	blendPixel(c.Pix[at:at+4], src, alpha)
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
	// Contrast repair depends on the gauge and track, not the arc pixel.
	arcEnd := radialArcColor(style, n, 1, trackColor)
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
				active := LerpColor(style.Accent, arcEnd, progress)
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

// paintBandedGauge paints a gauge whose track is a health scale rather than a
// progress ring: severity zones in fixed colours, a needle at the reading, and
// optionally only half the circle.
//
// It is a separate path from paintRadialGauge on purpose. A gauge with neither
// bands nor the half form must keep painting exactly the bytes it always has,
// so nothing here runs for it. The two share their coverage helpers and their
// centre-painting rules, which is where the real consistency risk would lie.
func paintBandedGauge(c *Canvas, n *ui.Node, text *TextRenderer, style Style) error {
	box := style.Scale120.PhysicalRect(n.Bounds)
	size := min(box.W, box.H)
	if size <= 0 {
		return nil
	}

	half := n.Form == ui.GaugeHalf
	var cx, cy float64
	if half {
		// A half gauge spends its box on the arc plus the label beneath it.
		// The arc is a semicircle, so it needs size/2 vertically and size
		// horizontally; a box two-thirds arc to one-third label means the
		// circle may be wider than the box allows, and the box wins.
		size = min(box.W, max(box.H*4/3, 1))
		cx = float64(box.X) + float64(box.W)/2
		cy = float64(box.Y) + float64(size)/2
	} else {
		box.X += (box.W - size) / 2
		box.Y += (box.H - size) / 2
		cx = float64(box.X) + float64(size)/2
		cy = float64(box.Y) + float64(size)/2
	}

	hairline := max(style.Scale120.Physical(2), 1)
	stroke := max(size/14, hairline)
	outer := float64(size)/2 - 0.5
	inner := outer - float64(stroke)
	radius := (outer + inner) / 2
	halfStroke := float64(stroke) / 2
	fraction := min(max(n.Value, 0), 1)

	// A full gauge sweeps clockwise from twelve o'clock. A half sweeps from
	// nine o'clock to three, which in this convention (clockwise from straight
	// up) is -pi/2 through +pi/2.
	start, sweep := 0.0, 2*math.Pi
	if half {
		start, sweep = -math.Pi/2, math.Pi
	}

	// A bar-sized glyph stays a hairline on the bright track it has always
	// used; a panel-sized ring drops the track to a container tone so the
	// bands are what the eye lands on.
	track := style.ContainerHighest
	if stroke <= hairline {
		track = style.Track
	}
	bandColors := make([]Color, len(n.Bands))
	for i, band := range n.Bands {
		bandColors[i] = severityPaint(band.Severity, track)
	}

	paintBox := ui.Rect{
		X: int(cx - float64(size)/2 - 1), Y: int(cy - float64(size)/2 - 1),
		W: size + 2, H: size + 2,
	}
	if half {
		paintBox.H = size/2 + 2
	}
	x0, y0, x1, y1 := c.clip(paintBox)

	for y := y0; y < y1; y++ {
		for x := x0; x < x1; x++ {
			dx, dy := float64(x)+0.5-cx, float64(y)+0.5-cy
			coverage := radialBandCoverage(math.Hypot(dx, dy), radius, halfStroke)
			if coverage <= 0 {
				continue
			}
			angle := math.Atan2(dx, -dy)
			if angle < start {
				angle += 2 * math.Pi
			}
			progress := (angle - start) / sweep
			if progress < 0 || progress > 1 {
				continue
			}
			// The zones are the scale, so they paint whether or not the value
			// has reached them: the reader has to see where high begins.
			col := track
			if !n.Absent {
				if bandCol, ok := bandColorAt(n.Bands, bandColors, progress); ok {
					col = bandCol
				}
			}
			blendCoverage(c, x, y, col, coverage)
		}
	}

	// The needle answers "where is it now". A stopped arc would instead read as
	// progress toward a goal, and a threshold reading is not a goal.
	if !n.Absent {
		paintNeedle(c, cx, cy, radius, inner, outer, halfStroke, stroke, hairline, start+fraction*sweep, style.Foreground)
	}

	centre := ui.Rect{X: box.X, Y: box.Y, W: box.W, H: box.H}
	if half {
		centre = ui.Rect{
			X: box.X, Y: int(cy),
			W: box.W, H: max(box.Y+box.H-int(cy), 1),
		}
	}
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

// paintNeedle draws a radial hairline at one angle of a gauge.
func paintNeedle(c *Canvas, cx, cy, radius, inner, outer, halfStroke float64, stroke int, hairline int, angle float64, col Color) {
	width := max(float64(stroke)/3, float64(hairline)*1.5)
	from := inner - halfStroke*0.6
	to := outer + halfStroke*0.6
	ux, uy := math.Sin(angle), -math.Cos(angle)

	box := ui.Rect{
		X: int(cx - radius - 1), Y: int(cy - radius - 1),
		W: int(2*radius) + 3, H: int(2*radius) + 3,
	}
	x0, y0, x1, y1 := c.clip(box)
	for y := y0; y < y1; y++ {
		for x := x0; x < x1; x++ {
			dx, dy := float64(x)+0.5-cx, float64(y)+0.5-cy
			along := dx*ux + dy*uy
			if along < from || along > to {
				continue
			}
			perp := math.Abs(-dx*uy + dy*ux)
			if cov := min(max(width/2+.5-perp, 0), 1); cov > 0 {
				blendCoverage(c, x, y, col, cov)
			}
		}
	}
}

// bandColorAt returns the band covering a fraction of the sweep. Bands stack as
// exclusive ceilings, so the first ceiling above progress names the band; a
// progress past every ceiling takes the last band.
func bandColorAt(bands []ui.GaugeBand, colors []Color, progress float64) (Color, bool) {
	if len(bands) == 0 {
		return Color{}, false
	}
	for i, band := range bands {
		if progress < band.Until || i == len(bands)-1 {
			return colors[i], true
		}
	}
	return Color{}, false
}

// severityPaint resolves a band colour against the track it is painted on, so
// the zone stays legible in a theme whose surface sits close to the ramp.
func severityPaint(s ui.Severity, surface Color) Color {
	painted := theme.SeverityColor(themeSeverity(s), theme.Color{
		R: surface.R, G: surface.G, B: surface.B, A: surface.A,
	})
	return Color{R: painted.R, G: painted.G, B: painted.B, A: painted.A}
}

func themeSeverity(s ui.Severity) theme.Severity {
	switch s {
	case ui.SeverityModerate:
		return theme.SeverityModerate
	case ui.SeverityHigh:
		return theme.SeverityHigh
	}
	return theme.SeverityGood
}
