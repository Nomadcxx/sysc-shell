package render

import (
	"math"

	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

// spinnerSweep is the arc's length in turns: long enough to read as a stroke
// at an inline size, short enough that its turning is unmistakable.
const spinnerSweep = 0.28

// paintSpinner draws an indeterminate activity ring: a faint track and an
// arc that starts at the node's phase (Value, in turns clockwise from the
// top). The animator advances the phase; the painter only draws one pose,
// so reduced motion leaves a still arc at the top.
func paintSpinner(c *Canvas, n *ui.Node, style Style) error {
	box := style.Scale120.PhysicalRect(n.Bounds)
	size := min(box.W, box.H)
	if size <= 0 {
		return nil
	}
	box.X += (box.W - size) / 2
	box.Y += (box.H - size) / 2
	box.W, box.H = size, size

	stroke := max(size/10, max(style.Scale120.Physical(2), 1))
	outer := float64(size)/2 - 0.5
	inner := outer - float64(stroke)
	radius := (outer + inner) / 2
	halfStroke := float64(stroke) / 2
	cx, cy := float64(box.X)+float64(size)/2, float64(box.Y)+float64(size)/2

	phase := n.Value - math.Floor(n.Value)
	start := phase * 2 * math.Pi
	sweep := spinnerSweep * 2 * math.Pi
	end := start + sweep
	startX, startY := math.Sin(start)*radius, -math.Cos(start)*radius
	endX, endY := math.Sin(end)*radius, -math.Cos(end)*radius

	arcColor := style.accent()
	if n.Tone != ui.ToneNormal {
		arcColor = textColor(style, n.Tone)
	}
	track := style.ContainerHighest

	x0, y0, x1, y1 := c.clip(box)
	for y := y0; y < y1; y++ {
		for x := x0; x < x1; x++ {
			dx, dy := float64(x)+0.5-cx, float64(y)+0.5-cy
			band := radialBandCoverage(math.Hypot(dx, dy), radius, halfStroke)
			arc := 0.0
			angle := math.Atan2(dx, -dy) - start
			angle -= 2 * math.Pi * math.Floor(angle/(2*math.Pi))
			if angle <= sweep {
				arc = band
			}
			arc = max(arc, radialCapCoverage(dx, dy, startX, startY, halfStroke),
				radialCapCoverage(dx, dy, endX, endY, halfStroke))
			coverage := max(band, arc)
			if coverage <= 0 {
				continue
			}
			col := track
			if arc > 0 {
				col = LerpColor(track, arcColor, arc/coverage)
			}
			blendCoverage(c, x, y, col, coverage)
		}
	}
	return nil
}
