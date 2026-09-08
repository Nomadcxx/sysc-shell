package render

import (
	"math"

	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

func paintRadialGauge(c *Canvas, n *ui.Node, text *TextRenderer, style Style) error {
	box := style.Scale120.PhysicalRect(n.Bounds)
	size := min(box.W, box.H)
	if size <= 0 {
		return nil
	}
	box.X += (box.W - size) / 2
	box.Y += (box.H - size) / 2
	box.W, box.H = size, size

	stroke := max(style.Scale120.Physical(3), 2)
	outer := float64(size)/2 - 0.5
	inner := outer - float64(stroke)
	cx, cy := float64(box.X)+float64(size)/2, float64(box.Y)+float64(size)/2
	fraction := min(max(n.Value, 0), 1)
	limit := fraction * 2 * math.Pi
	for y := box.Y; y < box.Y+size; y++ {
		for x := box.X; x < box.X+size; x++ {
			dx, dy := float64(x)+0.5-cx, float64(y)+0.5-cy
			distance := math.Hypot(dx, dy)
			if distance < inner || distance > outer {
				continue
			}
			col := style.Track
			angle := math.Atan2(dx, -dy)
			if angle < 0 {
				angle += 2 * math.Pi
			}
			if !n.Absent && angle <= limit {
				col = style.accent()
			}
			fillRect(c, ui.Rect{X: x, Y: y, W: 1, H: 1}, col)
		}
	}
	return paintText(c, n.Text, box, text, style, textSpec(style, n), true, n.Tone, false)
}
