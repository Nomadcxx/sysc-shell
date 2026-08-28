package render

import (
	"fmt"

	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

// ProofStyle carries the proof's colours, text size, render scale, and the one
// piece of model state that changes colour.
type ProofStyle struct {
	// Size is the logical font size; shaping happens at the physical size.
	Size int
	// Scale120 is the fractional render scale as a numerator over 120.
	Scale120 int

	Background Color
	Foreground Color
	Track      Color
	Accent     Color
	AccentOn   Color

	// Toggled swaps the accent used by the meter fill and the button.
	Toggled bool
}

// accent returns the colour the meter fill and button share.
func (s ProofStyle) accent() Color {
	if s.Toggled {
		return s.AccentOn
	}
	return s.Accent
}

// physical converts a logical length to buffer pixels, rounding to nearest.
func (s ProofStyle) physical(logical int) int {
	return (logical*s.Scale120 + 60) / 120
}

// physicalRect maps a logical rectangle by its edges, so adjacent rectangles
// stay adjacent after scaling.
func (s ProofStyle) physicalRect(r ui.Rect) ui.Rect {
	x0, y0 := s.physical(r.X), s.physical(r.Y)
	return ui.Rect{X: x0, Y: y0, W: s.physical(r.X+r.W) - x0, H: s.physical(r.Y+r.H) - y0}
}

// Paint draws the arranged row into the canvas. Node bounds are logical; every
// write is converted to buffer pixels and clipped to the canvas.
func Paint(c *Canvas, root *ui.Node, text *TextRenderer, style ProofStyle) error {
	switch {
	case c == nil:
		return fmt.Errorf("render: nil canvas")
	case root == nil:
		return fmt.Errorf("render: nil root")
	case text == nil:
		return fmt.Errorf("render: nil text renderer")
	case style.Scale120 <= 0:
		return fmt.Errorf("render: scale120 %d is not positive", style.Scale120)
	case style.Size <= 0:
		return fmt.Errorf("render: text size %d is not positive", style.Size)
	}

	fillRect(c, ui.Rect{W: c.Width, H: c.Height}, style.Background)

	size := style.physical(style.Size)
	for i, child := range root.Children {
		if child == nil {
			return fmt.Errorf("render: nil child %d", i)
		}
		if err := paintNode(c, child, text, style, size); err != nil {
			return fmt.Errorf("render: child %d: %w", i, err)
		}
	}
	return nil
}

func paintNode(c *Canvas, n *ui.Node, text *TextRenderer, style ProofStyle, size int) error {
	switch n.Kind {
	case ui.KindText:
		return paintText(c, n.Text, style.physicalRect(n.Bounds), text, style, size)

	case ui.KindMeter:
		box := style.physicalRect(n.Bounds)
		fillRect(c, box, style.Track)
		filled := box
		filled.W = style.physical(n.Bounds.X+int(float64(n.Bounds.W)*n.Value+0.5)) - box.X
		fillRect(c, filled, style.accent())
		return nil

	case ui.KindButton:
		fillRect(c, style.physicalRect(n.Bounds), style.accent())
		label := ui.Rect{
			X: n.Bounds.X + n.Padding,
			Y: n.Bounds.Y + n.Padding,
			W: n.Bounds.W - 2*n.Padding,
			H: n.Bounds.H - 2*n.Padding,
		}
		return paintText(c, n.Text, style.physicalRect(label), text, style, size)

	default:
		return fmt.Errorf("unsupported kind %d", n.Kind)
	}
}

// paintText shapes at the physical size and blends the mask at the box origin.
func paintText(c *Canvas, s string, box ui.Rect, text *TextRenderer, style ProofStyle, size int) error {
	if s == "" {
		return nil
	}
	mask, err := text.Raster(s, size)
	if err != nil {
		return err
	}
	blendMask(c, mask.Alpha, box.X, box.Y, style.Foreground)
	return nil
}
