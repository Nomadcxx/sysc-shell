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
	Scale120 ui.Scale120
	// Body is the logical painted bar body inside the transparent layer
	// surface. Radius is its logical corner radius.
	Body   ui.Rect
	Radius int

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
	case !style.Scale120.Valid():
		return fmt.Errorf("render: scale120 %d is not positive", style.Scale120)
	case style.Size <= 0:
		return fmt.Errorf("render: text size %d is not positive", style.Size)
	case style.Body.W <= 0 || style.Body.H <= 0:
		return fmt.Errorf("render: body %dx%d has a non-positive dimension", style.Body.W, style.Body.H)
	}

	clear(c.Pix)
	fillRoundedRect(c, style.Scale120.PhysicalRect(style.Body),
		style.Scale120.Physical(style.Radius), style.Background)

	size := style.Scale120.Physical(style.Size)
	for i, child := range root.Children {
		if child == nil {
			return fmt.Errorf("render: nil child %d", i)
		}
		if err := paintNode(c, child, text, style, size); err != nil {
			return fmt.Errorf("render: child %d: %w", i, err)
		}
	}
	clearOutsideRoundedRect(c, style.Scale120.PhysicalRect(style.Body),
		style.Scale120.Physical(style.Radius))
	return nil
}

func paintNode(c *Canvas, n *ui.Node, text *TextRenderer, style ProofStyle, size int) error {
	switch n.Kind {
	case ui.KindText:
		return paintText(c, n.Text, style.Scale120.PhysicalRect(n.Bounds), text, style, size, n.Tabular)

	case ui.KindMeter:
		box := style.Scale120.PhysicalRect(n.Bounds)
		fillRect(c, box, style.Track)
		filled := box
		filled.W = style.Scale120.Physical(n.Bounds.X+int(float64(n.Bounds.W)*n.Value+0.5)) - box.X
		fillRect(c, filled, style.accent())
		return nil

	case ui.KindButton:
		fillRect(c, style.Scale120.PhysicalRect(n.Bounds), style.accent())
		label := ui.Rect{
			X: n.Bounds.X + n.Padding,
			Y: n.Bounds.Y + n.Padding,
			W: n.Bounds.W - 2*n.Padding,
			H: n.Bounds.H - 2*n.Padding,
		}
		return paintText(c, n.Text, style.Scale120.PhysicalRect(label), text, style, size, n.Tabular)

	default:
		return fmt.Errorf("unsupported kind %d", n.Kind)
	}
}

// paintText shapes at the physical size and blends the mask at the box origin.
//
// Truncation happens here rather than in layout because it needs cluster
// measurement, which the text renderer owns. The box is already physical, so
// the available width is compared in the same units the shaper reports.
func paintText(c *Canvas, s string, box ui.Rect, text *TextRenderer, style ProofStyle, size int, tabular bool) error {
	if s == "" || box.W <= 0 {
		return nil
	}
	fitted, _, err := text.Truncate(s, size, box.W, tabular)
	if err != nil {
		return err
	}
	if fitted == "" {
		return nil // not even an ellipsis fits
	}
	// Raster with the same flag measurement used, or the drawn run and the
	// space reserved for it would disagree.
	mask, err := text.Raster(fitted, size, tabular)
	if err != nil {
		return err
	}
	blendMask(c, mask.Alpha, box.X, box.Y, style.Foreground)
	return nil
}
