package render

import (
	"fmt"
	"math"
	"strings"

	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

// buttonText returns the label colour over a Primary fill, falling back to
// Foreground for a style assembled before the token existed.
func (s Style) buttonText() Color {
	if s.OnPrimary.A == 0 {
		return s.Foreground
	}
	return s.OnPrimary
}

// onError falls back to the button text colour for a style assembled before
// the token existed.
func (s Style) onError() Color {
	if s.OnError.A == 0 {
		return s.buttonText()
	}
	return s.OnError
}

// containerHighest falls back to the capsule level when a style predates the
// token; a control then reads as flat rather than invisible.
func (s Style) containerHighest() Color {
	if s.ContainerHighest.A == 0 {
		return s.Capsule
	}
	return s.ContainerHighest
}

// outline falls back to the foreground, which always separates from its own
// paired fill.
func (s Style) outline() Color {
	if s.Outline.A == 0 {
		return s.Foreground
	}
	return s.Outline
}

// accent returns the colour the meter fill and button share.
func (s Style) accent() Color {
	if s.Toggled {
		return s.AccentOn
	}
	return s.Accent
}

// Paint draws the arranged row into the canvas. Node bounds are logical; every
// write is converted to buffer pixels and clipped to the canvas.
func Paint(c *Canvas, root *ui.Node, text *TextRenderer, style Style) error {
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
	box := style.Scale120.PhysicalRect(style.Body)
	radius := style.Scale120.Physical(style.Radius)
	if style.Rim.A > 0 {
		fillRoundedRect(c, box, radius, style.Rim)
		inset := max(style.Scale120.Physical(1), 1)
		inner := ui.Rect{X: box.X + inset, Y: box.Y + inset, W: box.W - 2*inset, H: box.H - 2*inset}
		fillRoundedRect(c, inner, max(radius-inset, 0), style.Background)
	} else {
		fillRoundedRect(c, box, radius, style.Background)
	}
	squareAttachedEdge(c, box, radius, style.AttachEdge, style.Background)

	size := style.Scale120.Physical(style.Size)
	if root.Kind == ui.KindScroll || root.Kind == ui.KindVirtualList {
		if err := paintNode(c, root, text, style, size); err != nil {
			return err
		}
	} else {
		for i, child := range root.Children {
			if child == nil {
				return fmt.Errorf("render: nil child %d", i)
			}
			if err := paintNode(c, child, text, style, size); err != nil {
				return fmt.Errorf("render: child %d: %w", i, err)
			}
		}
	}
	clearOutsideRoundedRect(c, box, radius, style.AttachEdge)
	return nil
}

func squareAttachedEdge(c *Canvas, box ui.Rect, radius int, edge string, col Color) {
	if radius <= 0 {
		return
	}
	h := min(radius, box.H)
	switch edge {
	case "top":
		fillRect(c, ui.Rect{X: box.X, Y: box.Y, W: box.W, H: h}, col)
	case "bottom":
		fillRect(c, ui.Rect{X: box.X, Y: box.Y + box.H - h, W: box.W, H: h}, col)
	}
}

func paintNode(c *Canvas, n *ui.Node, text *TextRenderer, style Style, size int) error {
	switch n.Kind {
	case ui.KindText:
		return paintText(c, n.Text, style.Scale120.PhysicalRect(n.Bounds), text, style, textSpec(style, n), n.Tabular, n.Tone, n.Underline)

	case ui.KindMeter:
		if n.Absent {
			return nil
		}
		box := style.Scale120.PhysicalRect(n.Bounds)
		fillRect(c, box, style.Track)
		filled := box
		filled.W = style.Scale120.Physical(n.Bounds.X+int(float64(n.Bounds.W)*n.Value+0.5)) - box.X
		fill := style.accent()
		if n.Tone == ui.ToneError {
			fill = style.Error
		}
		fillRect(c, filled, fill)
		return nil

	case ui.KindCapsule:
		// A bar pill and a panel card are the same chrome on the same
		// background: same fill resolution, same state layers, differing only
		// in radius. A capsule with no explicit Radius stays a stadium, so an
		// empty dot is a circle; a card sets the theme's card radius.
		radius := style.Radius
		if n.Fill == ui.FillContainerHigh && style.CardRadius > 0 {
			radius = style.CardRadius
		}
		return paintChrome(c, n, text, style, size, style.Capsule, radius)

	case ui.KindGraph:
		return paintGraph(c, n, style.Scale120.PhysicalRect(n.Bounds), style)

	case ui.KindImage:
		// A node whose raster has not resolved paints nothing but keeps the
		// box it measured, so the card does not reflow when it arrives.
		paintImage(c, style.Scale120.PhysicalRect(n.Bounds), n.Image)
		return nil

	case ui.KindButton, ui.KindDragSource:
		return paintButton(c, n, text, style, size)

	case ui.KindIcon:
		return paintIcon(c, n, text, style)

	case ui.KindToggle:
		paintToggle(c, n, style)
		return nil

	case ui.KindSlider:
		paintSlider(c, n, style)
		return nil

	case ui.KindMenu:
		paintMenu(c, n, text, style, size)
		return nil

	case ui.KindTextField:
		return paintTextField(c, n, text, style, size)

	case ui.KindScroll, ui.KindVirtualList:
		prev := c.restrict
		c.restrict = style.Scale120.PhysicalRect(n.Bounds)
		defer func() { c.restrict = prev }()
		for i, child := range n.Children {
			if child == nil {
				return fmt.Errorf("nil child %d", i)
			}
			if err := paintNode(c, child, text, style, size); err != nil {
				return err
			}
		}
		paintScrollThumb(c, n, style)
		return nil

	// A segmented row owns allocation, not chrome: each segment paints its own
	// fill through paintButton, Primary when selected and quiet container
	// otherwise, so the container only dispatches to its children.
	case ui.KindRow, ui.KindColumn, ui.KindDropZone, ui.KindSegmented:
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
		return paintText(c, n.Text, style.Scale120.PhysicalRect(n.Bounds), text, style, textSpec(style, n), n.Tabular, n.Tone, n.Underline)

	default:
		return fmt.Errorf("unsupported kind %d", n.Kind)
	}
}

func paintScrollThumb(c *Canvas, n *ui.Node, style Style) {
	inner := n.Bounds.H - 2*n.Padding
	if inner <= 0 || n.ContentH <= inner {
		return
	}
	trackW := style.Scale120.Physical(4)
	if trackW < 2 {
		trackW = 2
	}
	box := style.Scale120.PhysicalRect(n.Bounds)
	pad := style.Scale120.Physical(n.Padding)
	if pad < 2 {
		pad = 2
	}
	trackH := box.H - 2*pad
	if trackH <= 0 {
		return
	}
	track := ui.Rect{
		X: box.X + box.W - pad - trackW,
		Y: box.Y + pad,
		W: trackW,
		H: trackH,
	}
	fillRoundedRect(c, track, trackW/2, style.Track)
	thumbH := track.H * inner / n.ContentH
	minThumb := style.Scale120.Physical(16)
	if thumbH < minThumb {
		thumbH = minThumb
	}
	if thumbH > track.H {
		thumbH = track.H
	}
	maxOff := n.ContentH - inner
	thumbY := track.Y
	if maxOff > 0 {
		thumbY += (track.H - thumbH) * n.ScrollOffset / maxOff
	}
	fillRoundedRect(c, ui.Rect{X: track.X, Y: thumbY, W: trackW, H: thumbH}, trackW/2, style.Foreground)
}

func paintToggle(c *Canvas, n *ui.Node, style Style) {
	if n.Role == "checkbox" {
		paintCheckbox(c, n, style)
		return
	}
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

func paintCheckbox(c *Canvas, n *ui.Node, style Style) {
	box := style.Scale120.PhysicalRect(n.Bounds)
	radius := style.Scale120.Physical(3)
	c.FillRounded(box, radius, style.Track)
	if n.Value == 0 {
		return
	}
	inset := style.Scale120.Physical(4)
	if inset*2 >= box.W || inset*2 >= box.H {
		inset = 1
	}
	inner := ui.Rect{X: box.X + inset, Y: box.Y + inset, W: box.W - 2*inset, H: box.H - 2*inset}
	c.FillRounded(inner, radius, style.accent())
}

func paintSlider(c *Canvas, n *ui.Node, style Style) {
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

func paintMenu(c *Canvas, n *ui.Node, text *TextRenderer, style Style, size int) {
	box := style.Scale120.PhysicalRect(n.Bounds)
	field := box
	if len(n.Children) > 0 {
		first := style.Scale120.PhysicalRect(n.Children[0].Bounds)
		if first.Y > box.Y {
			field.H = first.Y - box.Y
		}
	}
	c.FillRounded(field, style.Scale120.Physical(6), style.Track)
	_ = paintText(c, n.Text, field, text, style, textSpec(style, n), n.Tabular, n.Tone, n.Underline)
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
		_ = paintText(c, child.Text, cb, text, style, textSpec(style, child), child.Tabular, child.Tone, child.Underline)
	}
}

func paintTextField(c *Canvas, n *ui.Node, text *TextRenderer, style Style, size int) error {
	box := style.Scale120.PhysicalRect(n.Bounds)
	// Stadium: a search well is a pill, not a 6px-radius box.
	radius := box.H / 2
	well := style.Capsule
	if well.A == 0 {
		well = style.Track
	}
	if n.Name == "Search" && style.Rim.A > 0 {
		fillRoundedRect(c, box, radius, style.Rim)
		inset := max(style.Scale120.Physical(1), 1)
		fillRoundedRect(c, ui.Rect{X: box.X + inset, Y: box.Y + inset, W: box.W - 2*inset, H: box.H - 2*inset}, max(radius-inset, 0), well)
	} else {
		fillRoundedRect(c, box, radius, well)
	}
	mark := 0
	if n.Name == "Search" && !n.Multiline {
		mark = 26
		paintSearchMark(c, box, style.Scale120.Physical(mark), style.Foreground, well)
	}
	inner := ui.Rect{
		X: n.Bounds.X + n.Padding + mark,
		Y: n.Bounds.Y + n.Padding,
		W: max(n.Bounds.W-2*n.Padding-mark, 0),
		H: n.Bounds.H - 2*n.Padding,
	}
	phys := style.Scale120.PhysicalRect(inner)
	prev := c.restrict
	c.restrict = phys
	defer func() { c.restrict = prev }()
	if n.Multiline {
		return paintMultilineField(c, n, text, style, size, phys)
	}
	// A Search field's glass is the affordance; painting Name as a
	// placeholder put bright body text in the well.
	if n.Text == "" && n.Preedit == "" && n.Name != "" && mark == 0 {
		_ = paintText(c, n.Name, phys, text, style, textSpec(style, n).Italicised(), n.Tabular, n.Tone, false)
	}
	committed := n.Text
	if n.Cursor >= 0 && n.Cursor <= len(n.Text) {
		committed = n.Text[:n.Cursor]
	}
	if err := paintText(c, n.Text, phys, text, style, textSpec(style, n), n.Tabular, n.Tone, n.Underline); err != nil {
		return err
	}
	prefixW := 0
	if text != nil && committed != "" {
		if w, _, err := text.Measure(committed, textSpec(style, n), n.Tabular); err == nil {
			prefixW = w
		}
	}
	if n.Preedit != "" {
		pre := phys
		pre.X += prefixW
		pre.W -= prefixW
		if err := paintText(c, n.Preedit, pre, text, style, textSpec(style, n), n.Tabular, n.Tone, n.Underline); err != nil {
			return err
		}
		if pw, _, err := text.Measure(n.Preedit, textSpec(style, n), n.Tabular); err == nil {
			underline := ui.Rect{X: pre.X, Y: pre.Y + pre.H - 1, W: pw, H: 1}
			fillRect(c, underline, style.Foreground)
			prefixW += pw
		}
	}
	caret := ui.Rect{X: phys.X + prefixW, Y: phys.Y, W: 1, H: phys.H}
	fillRect(c, caret, style.accent())
	return nil
}

func paintMultilineField(c *Canvas, n *ui.Node, text *TextRenderer, style Style, size int, phys ui.Rect) error {
	lineH := size
	if text != nil {
		if _, h, err := text.Measure(" ", textSpec(style, n), n.Tabular); err == nil && h > 0 {
			lineH = h
		}
	}
	lines := strings.Split(n.Text, "\n")
	off := 0
	caretLine, caretCol := 0, 0
	cursor := n.Cursor
	if cursor < 0 {
		cursor = 0
	}
	if cursor > len(n.Text) {
		cursor = len(n.Text)
	}
	for i, line := range lines {
		end := off + len(line)
		if cursor >= off && cursor <= end {
			caretLine, caretCol = i, cursor-off
		}
		box := phys
		box.Y += i * lineH
		box.H = lineH
		if err := paintText(c, line, box, text, style, textSpec(style, n), n.Tabular, n.Tone, n.Underline); err != nil {
			return err
		}
		off = end + 1
	}
	prefixW := 0
	if text != nil && caretCol > 0 && caretLine < len(lines) {
		prefix := lines[caretLine]
		if caretCol < len(prefix) {
			prefix = prefix[:caretCol]
		}
		if w, _, err := text.Measure(prefix, textSpec(style, n), n.Tabular); err == nil {
			prefixW = w
		}
	}
	caret := ui.Rect{X: phys.X + prefixW, Y: phys.Y + caretLine*lineH, W: 1, H: lineH}
	fillRect(c, caret, style.accent())
	return nil
}

// paintGraph fills one column per sample, newest at the right, using the same
// rectangle fill the meter uses. There is no path rasteriser and no
// anti-aliasing: a bar-height sparkline needs neither.
//
// Values are already normalised to zero through one by the widget, so this
// applies no scale of its own.
func paintGraph(c *Canvas, n *ui.Node, box ui.Rect, style Style) error {
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
func paintText(c *Canvas, s string, box ui.Rect, text *TextRenderer, style Style, spec TextSpec, tabular bool, tone ui.Tone, underline bool) error {
	return paintTextColor(c, s, box, text, style, spec, tabular,
		textColor(style, tone), underline)
}

// textSpec resolves the one spec a node's text is measured and painted with:
// its semantic role, the weight and slant the node asks for on top of that
// role, and the physical size for the surface's render scale.
//
// Measurement and paint both come through here, so a label cannot be measured
// at one weight and drawn at another.
func textSpec(style Style, n *ui.Node) TextSpec {
	return SpecFor(style, ui.TextAttrsOf(n))
}

// SpecFor resolves the spec for one measurement or paint request. Layout
// measures through it and paint resolves through it, which is what keeps a
// medium-weight label from being measured as regular body text and then drawn
// wider than the space reserved for it.
func SpecFor(style Style, attrs ui.TextAttrs) TextSpec {
	spec := style.Type.Spec(attrs.Role)
	if spec.Size <= 0 {
		// A style assembled without a role table -- a surface painting before
		// its theme resolved -- still measures at the one size it carries.
		spec = TextSpec{Family: style.Type.Family, Size: style.Size, Weight: 400}
	}
	if attrs.Bold {
		spec = spec.Bolder()
	}
	if attrs.Italic {
		spec = spec.Italicised()
	}
	return spec.AtSize(style.Scale120.Physical(spec.Size))
}

func paintTextColor(c *Canvas, s string, box ui.Rect, text *TextRenderer, style Style, spec TextSpec, tabular bool, fg Color, underline bool) error {
	if s == "" || box.W <= 0 {
		return nil
	}
	fitted, _, err := text.Truncate(s, spec, box.W, tabular)
	if err != nil {
		return err
	}
	if fitted == "" {
		return nil // not even an ellipsis fits
	}
	// Raster with the same flag measurement used, or the drawn run and the
	// space reserved for it would disagree.
	mask, err := text.Raster(fitted, spec, tabular)
	if err != nil {
		return err
	}
	// Weight and slant are resolved faces now, not a re-blend at an offset
	// and a sheared mask. The scanner returns the closest cut it has, so a
	// family with no bold degrades to its regular rather than to a smear.
	blendMask(c, mask.Alpha, box.X, box.Y, fg)
	if mask.Color != nil {
		paintImage(c, ui.Rect{X: box.X, Y: box.Y, W: mask.Color.Width, H: mask.Color.Height}, mask.Color)
	}
	if underline {
		th := max(spec.Size/16, 1)
		rule := ui.Rect{X: box.X, Y: box.Y + box.H - th, W: min(mask.Advance, box.W), H: th}
		fillRect(c, rule, fg)
	}
	return nil
}

// State-layer opacities from the catalogue's colour recipe. The layer is the
// resolved fill's own paired foreground, so one recipe covers every fill in
// light and dark palettes without inventing hover RGB.
const (
	hoverLayerAlpha    = 0.08
	pressedLayerAlpha  = 0.12
	disabledForeground = 0.38
)

// chromeFill resolves what a filled node paints and the foreground its contents
// must use to stay legible. Fill and foreground travel together: every fill
// carries the only label colour that reads on it.
//
// base is the kind's resting fill, which differs by kind rather than by token:
// a capsule or card rests on the high container, a control resting on one of
// those needs the level above it.
func chromeFill(style Style, n *ui.Node, base Color) (fill, fg Color) {
	// Selection outranks the declared fill: a selected segment is Primary
	// whatever it rests in.
	if n.State.Has(ui.StateSelected) {
		return style.accent(), style.buttonText()
	}
	switch n.Fill {
	case ui.FillAccent:
		return style.accent(), style.buttonText()
	case ui.FillContainer:
		return style.Container, style.OnContainer
	case ui.FillContainerHigh:
		return style.Capsule, style.Foreground
	case ui.FillSoft:
		// A muted accent wash. Contents keep the surface foreground, so a
		// selected launcher row does not read as a primary-on-white chip.
		return wash(style.Accent, style.Capsule), style.Foreground
	case ui.FillError:
		return style.Error, style.onError()
	case ui.FillOutline:
		// Outlined chrome keeps whatever its parent painted; only the boundary
		// and the label mark it. A destructive control is error-toned here
		// rather than a solid red block.
		if n.Tone == ui.ToneError {
			return Color{}, style.Error
		}
		return Color{}, style.Foreground
	}
	return base, style.Foreground
}

// chromeRadius clamps a logical radius to half the node's short side, so a
// stadium is the most any chrome can round to and a square icon button is a
// circle. A logical radius of zero asks for that stadium outright.
func chromeRadius(style Style, logical int, box ui.Rect) int {
	half := min(box.W, box.H) / 2
	if logical <= 0 {
		return half
	}
	return min(style.Scale120.Physical(logical), half)
}

// stateLayer returns the overlay a node's resolved interaction state composites
// over its fill, or a zero colour when it is at rest.
func stateLayer(fg Color, state ui.Interaction) Color {
	var alpha float64
	switch {
	case state.Has(ui.StateDisabled):
		return Color{}
	case state.Has(ui.StatePressed):
		alpha = pressedLayerAlpha
	case state.Has(ui.StateHovered):
		alpha = hoverLayerAlpha
	default:
		return Color{}
	}
	return Color{R: fg.R, G: fg.G, B: fg.B, A: uint8(math.Round(float64(fg.A) * alpha))}
}

// paintChrome draws one filled, optionally outlined, optionally state-layered
// rounded node and then its contents. Buttons, capsules, and cards share it so
// a control cannot acquire chrome that differs from the pill beside it.
// radius is the logical corner radius to use when the node does not carry one;
// zero asks for a stadium.
func paintChrome(c *Canvas, n *ui.Node, text *TextRenderer, style Style, size int, base Color, radiusLogical int) error {
	box := style.Scale120.PhysicalRect(n.Bounds)
	if n.Radius > 0 {
		radiusLogical = n.Radius
	}
	radius := chromeRadius(style, radiusLogical, box)
	fill, fg := chromeFill(style, n, base)
	fillRoundedRect(c, box, radius, fill)
	// An explicit stroke marks a card that has to stand out from its
	// neighbours -- a critical notification -- independently of the fill and of
	// any interaction state.
	if n.Stroke > 0 {
		strokeCol := capsuleFill(style, n.StrokeFill)
		if n.StrokeFill == ui.FillNone {
			strokeCol = style.Accent
		}
		strokeRoundedRect(c, box, radius, max(1, style.Scale120.Physical(n.Stroke)), strokeCol)
	}
	if n.Fill == ui.FillOutline {
		boundary := style.outline()
		if n.Tone == ui.ToneError {
			boundary = style.Error
		}
		strokeRoundedRect(c, box, radius, max(1, style.Scale120.Physical(1)), boundary)
	}
	// The layer sits over the resolved fill and under the contents, so a label
	// never dims along with its own hover wash.
	fillRoundedRect(c, box, radius, stateLayer(fg, n.State))
	if n.State.Has(ui.StateDisabled) {
		fg = Color{R: fg.R, G: fg.G, B: fg.B, A: uint8(math.Round(float64(fg.A) * disabledForeground))}
	}

	inner := style
	inner.Foreground = fg
	if len(n.Children) > 0 {
		for i, child := range n.Children {
			if child == nil {
				return fmt.Errorf("chrome child %d is nil", i)
			}
			if err := paintNode(c, child, text, inner, size); err != nil {
				return err
			}
		}
		return nil
	}
	if n.Text == "" {
		return nil
	}
	label := ui.Rect{
		X: n.Bounds.X + n.Padding,
		Y: n.Bounds.Y + n.Padding,
		W: n.Bounds.W - 2*n.Padding,
		H: n.Bounds.H - 2*n.Padding,
	}
	return paintTextColor(c, n.Text, style.Scale120.PhysicalRect(label), text, inner,
		textSpec(inner, n), n.Tabular, fg, n.Underline)
}

// paintIcon draws one named glyph from the embedded Material subset, centred in
// the node's box and tinted with the foreground it inherited from the chrome it
// sits in. An icon takes no fill of its own: the control around it already
// resolved one.
func paintIcon(c *Canvas, n *ui.Node, text *TextRenderer, style Style) error {
	if n.Icon == "" {
		return nil
	}
	size := style.Scale120.Physical(ui.IconSize(n))
	if size <= 0 {
		return fmt.Errorf("render: icon %q has no size", n.Icon)
	}
	mask, err := text.RasterMaterialIcon(n.Icon, size)
	if err != nil {
		return err
	}
	box := style.Scale120.PhysicalRect(n.Bounds)
	b := mask.Alpha.Bounds()
	x := box.X + (box.W-b.Dx())/2
	y := box.Y + (box.H-b.Dy())/2
	blendMask(c, mask.Alpha, x, y, style.Foreground)
	return nil
}

// A button is a stadium unless it carries an explicit card radius.
// A button is a stadium unless it carries an explicit card radius.
func paintButton(c *Canvas, n *ui.Node, text *TextRenderer, style Style, size int) error {
	return paintChrome(c, n, text, style, size, style.containerHighest(), 0)
}

func capsuleFill(style Style, fill ui.Fill) Color {
	switch fill {
	case ui.FillAccent:
		return style.Accent
	case ui.FillContainer:
		return style.Container
	case ui.FillError:
		return style.Error
	case ui.FillSoft:
		return wash(style.Accent, style.Capsule)
	}
	return style.Capsule
}

func capsuleForeground(style Style, fill ui.Fill) Color {
	switch fill {
	case ui.FillAccent:
		return style.OnAccent
	case ui.FillContainer:
		return style.OnContainer
	}
	return style.Foreground
}

// wash tints surface with accent at ~31% so a selected row stays dark with
// ordinary text instead of a primary chip.
func wash(accent, surface Color) Color {
	const a uint32 = 40
	ia := uint32(255 - a)
	mix := func(over, under uint8) uint8 {
		return uint8((uint32(over)*a + uint32(under)*ia) / 255)
	}
	return Color{R: mix(accent.R, surface.R), G: mix(accent.G, surface.G), B: mix(accent.B, surface.B), A: 0xff}
}

// paintSearchMark draws a magnifying glass in the leading well. There is no
// SVG rasterizer on this path; the glyph is two rounded fills.
func paintSearchMark(c *Canvas, field ui.Rect, slot int, fg, well Color) {
	if slot <= 0 || field.H <= 0 {
		return
	}
	cx := field.X + slot/2
	// The handle hangs SE of the lens, so the midline of the field is
	// below the visual centre of the glyph unless the lens sits a little high.
	cy := field.Y + field.H/2 - 3
	r := min(field.H/5, slot/3)
	if r < 3 {
		r = 3
	}
	outer := ui.Rect{X: cx - r, Y: cy - r, W: 2*r + 1, H: 2*r + 1}
	fillRoundedRect(c, outer, r, fg)
	hole := r - 2
	if hole >= 2 {
		fillRoundedRect(c, ui.Rect{X: cx - hole, Y: cy - hole, W: 2*hole + 1, H: 2*hole + 1}, hole, well)
	}
	handle := max(r, 5)
	for i := 0; i < handle; i++ {
		fillRect(c, ui.Rect{X: cx + r - 1 + i, Y: cy + r - 1 + i, W: 3, H: 3}, fg)
	}
}

func textColor(style Style, tone ui.Tone) Color {
	if tone == ui.ToneError {
		return style.Error
	}
	return style.Foreground
}
