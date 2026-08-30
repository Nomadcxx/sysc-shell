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
	// Error paints text that reports a failure. It is a distinct field rather
	// than reusing AccentOn, which the bar already uses for a toggled control.
	Error Color

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
		return paintText(c, n.Text, style.Scale120.PhysicalRect(n.Bounds), text, style, size, n.Tabular, n.Tone)

	case ui.KindMeter:
		if n.Absent {
			return nil
		}
		box := style.Scale120.PhysicalRect(n.Bounds)
		fillRect(c, box, style.Track)
		filled := box
		filled.W = style.Scale120.Physical(n.Bounds.X+int(float64(n.Bounds.W)*n.Value+0.5)) - box.X
		fillRect(c, filled, style.accent())
		return nil

	case ui.KindGraph:
		return paintGraph(c, n, style.Scale120.PhysicalRect(n.Bounds), style)

	case ui.KindButton:
		fillRect(c, style.Scale120.PhysicalRect(n.Bounds), style.accent())
		label := ui.Rect{
			X: n.Bounds.X + n.Padding,
			Y: n.Bounds.Y + n.Padding,
			W: n.Bounds.W - 2*n.Padding,
			H: n.Bounds.H - 2*n.Padding,
		}
		return paintText(c, n.Text, style.Scale120.PhysicalRect(label), text, style, size, n.Tabular, n.Tone)

	case ui.KindToggle:
		paintToggle(c, n, style)
		return nil

	case ui.KindSlider:
		paintSlider(c, n, style)
		return nil

	case ui.KindMenu:
		paintMenu(c, n, text, style, size)
		return nil

	case ui.KindRow, ui.KindColumn:
		for i, child := range n.Children {
			if child == nil {
				return fmt.Errorf("nil child %d", i)
			}
			if err := paintNode(c, child, text, style, size); err != nil {
				return err
			}
		}
		return nil

	case ui.KindSeparator:
		box := style.Scale120.PhysicalRect(n.Bounds)
		box.H = max(box.H, 1)
		fillRect(c, box, style.Track)
		return nil

	case ui.KindTab:
		return paintText(c, n.Text, style.Scale120.PhysicalRect(n.Bounds), text, style, size, n.Tabular, n.Tone)

	default:
		return fmt.Errorf("unsupported kind %d", n.Kind)
	}
}

func paintToggle(c *Canvas, n *ui.Node, style ProofStyle) {
	box := style.Scale120.PhysicalRect(n.Bounds)
	track := style.Track
	knob := style.Foreground
	if n.Value != 0 {
		track = style.accent()
		if style.AccentOn.A != 0 {
			knob = style.AccentOn
		}
	}
	c.FillRounded(box, min(box.H/2, style.Scale120.Physical(10)), track)
	knobH := style.Scale120.Physical(ui.ToggleKnob)
	if knobH > box.H {
		knobH = box.H
	}
	pad := (box.H - knobH) / 2
	x := box.X + pad
	if n.Value != 0 {
		x = box.X + box.W - pad - knobH
	}
	c.FillRounded(ui.Rect{X: x, Y: box.Y + pad, W: knobH, H: knobH}, knobH/2, knob)
}

func paintSlider(c *Canvas, n *ui.Node, style ProofStyle) {
	box := style.Scale120.PhysicalRect(n.Bounds)
	trackH := max(style.Scale120.Physical(ui.SliderTrack), 1)
	y := box.Y + (box.H-trackH)/2
	c.FillRounded(ui.Rect{X: box.X, Y: y, W: box.W, H: trackH}, trackH/2, style.Track)
	span := n.Max - n.Min
	frac := 0.0
	if span > 0 {
		frac = (n.Value - n.Min) / span
	}
	frac = min(max(frac, 0), 1)
	fillW := int(float64(box.W) * frac)
	if fillW > 0 {
		c.FillRounded(ui.Rect{X: box.X, Y: y, W: fillW, H: trackH}, trackH/2, style.accent())
	}
	knob := style.Scale120.Physical(ui.SliderKnob)
	if knob > box.H {
		knob = box.H
	}
	kx := box.X + fillW - knob/2
	if kx < box.X {
		kx = box.X
	}
	if kx+knob > box.X+box.W {
		kx = box.X + box.W - knob
	}
	c.FillRounded(ui.Rect{X: kx, Y: box.Y + (box.H-knob)/2, W: knob, H: knob}, knob/2, style.accent())
}

func paintMenu(c *Canvas, n *ui.Node, text *TextRenderer, style ProofStyle, size int) {
	box := style.Scale120.PhysicalRect(n.Bounds)
	field := box
	if len(n.Children) > 0 {
		first := style.Scale120.PhysicalRect(n.Children[0].Bounds)
		if first.Y > box.Y {
			field.H = first.Y - box.Y
		}
	}
	c.FillRounded(field, style.Scale120.Physical(6), style.Track)
	_ = paintText(c, n.Text, field, text, style, size, n.Tabular, n.Tone)
	if len(n.Children) == 0 {
		return
	}
	last := style.Scale120.PhysicalRect(n.Children[len(n.Children)-1].Bounds)
	list := ui.Rect{X: box.X, Y: field.Y + field.H, W: box.W, H: last.Y + last.H - (field.Y + field.H)}
	c.DrawShadow(list, style.Scale120.Physical(6), ElevMenu, Color{A: 0x73})
	c.FillRounded(list, style.Scale120.Physical(6), style.Background)
	for _, child := range n.Children {
		cb := style.Scale120.PhysicalRect(child.Bounds)
		if child.Value != 0 {
			c.FillRounded(cb, style.Scale120.Physical(4), style.accent())
		}
		_ = paintText(c, child.Text, cb, text, style, size, child.Tabular, child.Tone)
	}
}

// paintGraph fills one column per sample, newest at the right, using the same
// rectangle fill the meter uses. There is no path rasteriser and no
// anti-aliasing: a bar-height sparkline needs neither.
//
// Values are already normalised to zero through one by the widget, so this
// applies no scale of its own.
func paintGraph(c *Canvas, n *ui.Node, box ui.Rect, style ProofStyle) error {
	if n.Absent || box.W <= 0 || box.H <= 0 || len(n.Values) == 0 {
		return nil
	}

	// Columns are laid out newest-last. When there are more samples than
	// pixels, the oldest are dropped rather than averaged: the recent shape is
	// what a glanceable bar graph is for.
	values := n.Values
	if len(values) > box.W {
		values = values[len(values)-box.W:]
	}
	width := box.W / len(values)
	if width < 1 {
		width = 1
	}

	for i, v := range values {
		if v < 0 {
			v = 0
		}
		if v > 1 {
			v = 1
		}
		height := int(float64(box.H) * v)
		if height <= 0 {
			continue
		}
		x := box.X + box.W - (len(values)-i)*width
		if x < box.X {
			continue
		}
		fillRect(c, ui.Rect{X: x, Y: box.Y + box.H - height, W: width, H: height}, style.Accent)
	}
	return nil
}

// paintText shapes at the physical size and blends the mask at the box origin.
//
// Truncation happens here rather than in layout because it needs cluster
// measurement, which the text renderer owns. The box is already physical, so
// the available width is compared in the same units the shaper reports.
func paintText(c *Canvas, s string, box ui.Rect, text *TextRenderer, style ProofStyle, size int, tabular bool, tone ui.Tone) error {
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
	blendMask(c, mask.Alpha, box.X, box.Y, textColor(style, tone))
	return nil
}

// textColor picks the colour a tone paints in.
func textColor(style ProofStyle, tone ui.Tone) Color {
	if tone == ui.ToneError {
		return style.Error
	}
	return style.Foreground
}
