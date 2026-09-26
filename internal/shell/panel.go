package shell

import "github.com/Nomadcxx/sysc-shell/internal/ui"

// PanelID identifies one process-wide auxiliary panel instance.
type PanelID uint8

const (
	PanelClock PanelID = iota
	PanelMonitor
	PanelSession
	PanelSettings
	PanelLauncher
	PanelPlugin
	PanelNotifications
	PanelWallpaper
	PanelAudio
	PanelControlCenter
	PanelNetwork
	PanelBluetooth
	PanelWeather
	PanelClipboard
)

func (p PanelID) String() string {
	switch p {
	case PanelClock:
		return "clock"
	case PanelMonitor:
		return "system-monitor"
	case PanelSession:
		return "session"
	case PanelSettings:
		return "settings"
	case PanelLauncher:
		return "launcher"
	case PanelPlugin:
		return "plugin"
	case PanelNotifications:
		return "notifications"
	case PanelWallpaper:
		return "wallpaper"
	case PanelAudio:
		return "audio"
	case PanelControlCenter:
		return "control-center"
	case PanelNetwork:
		return "network"
	case PanelBluetooth:
		return "bluetooth"
	case PanelWeather:
		return "weather"
	case PanelClipboard:
		return "clipboard"
	default:
		return "unknown"
	}
}

// Placement computes the logical margins for a panel anchored to a bar edge.
type Placement struct {
	BarEdge      string
	Output       ui.Rect
	BarZone      int
	Gap, Padding int
	Panel        ui.Rect
	Align        string
	// AnchorX is the logical centre of the triggering widget. Zero means
	// unset; Align then centres on the output.
	AnchorX int
	// CenterY centres the panel vertically inside the output minus the bar
	// zone and padding: Settings, the launcher and the clipboard float.
	CenterY bool
	// Detached keeps a panel off the bar, Gap away from it with no joints:
	// islands paint no ground to join, and an output without a bar has none.
	Detached bool
	// BarShape, BarGap and BarRadius describe the bar an attached panel joins,
	// and Fillet is the joint radius. Together they say where the bar's
	// straight edge ends on each side, which bounds each joint.
	BarShape          string
	BarGap, BarRadius int
	Fillet            int
	// Overlap is how far an attached panel tucks under the bar's edge, so
	// no seam of wallpaper opens between them at a fractional scale.
	Overlap int
}

// Joints is how an attached panel meets the bar: the concave wedge beside each
// side of its attached edge, and whether a side sits flush on the screen edge
// instead, curving into it past its far corner.
type Joints struct {
	Left, Right           int
	FlushLeft, FlushRight bool
}

// Flush reports whether either side sits on the screen edge.
func (j Joints) Flush() bool { return j.FlushLeft || j.FlushRight }

// Joints is the panel's joints at its placed position.
func (p Placement) Joints() Joints {
	_, j := p.layout()
	return j
}

// layout places the panel horizontally and derives its joints. A joint is as
// wide as the fillet, less where the bar's straight edge ends sooner: at the
// screen edge for an attached bar, at its rounded end for a floating one. On
// an attached bar a panel too close to the screen edge for a whole joint
// snaps flush to it instead.
func (p Placement) layout() (int, Joints) {
	x := clampAxis(alignX(p), p.Panel.W, p.Output.W, p.Padding)
	if !p.Attached() || p.Fillet <= 0 {
		return x, Joints{}
	}
	attachedBar := p.BarShape == "attached"
	lo, hi := p.BarGap+p.BarRadius, p.Output.W-p.BarGap-p.BarRadius
	var j Joints
	if attachedBar {
		lo, hi = 0, p.Output.W
		switch {
		case x-lo < p.Fillet:
			x, j.FlushLeft = 0, true
		case hi-(x+p.Panel.W) < p.Fillet:
			x, j.FlushRight = p.Output.W-p.Panel.W, true
		}
	}
	if !j.FlushLeft {
		j.Left = min(max(x-lo, 0), p.Fillet)
	}
	if !j.FlushRight {
		j.Right = min(max(hi-(x+p.Panel.W), 0), p.Fillet)
	}
	return x, j
}

// Attached reports whether the panel joins the bar along its edge.
func (p Placement) Attached() bool { return p.BarEdge != "" && !p.CenterY && !p.Detached }

type Margins struct{ Top, Bottom, Left, Right int }

// anchor is how far from the bar's screen edge the panel's near edge sits. An
// attached panel meets the bar, tucked under it by the overlap, whatever gap
// the others keep.
func (p Placement) anchor() int {
	if p.Attached() {
		return p.BarZone - p.Overlap
	}
	return p.BarZone + p.Gap
}

func clampAxis(desired, size, extent, pad int) int {
	if size+2*pad > extent {
		return pad
	}
	if desired < pad {
		return pad
	}
	if max := extent - size - pad; desired > max {
		return max
	}
	return desired
}

func (p Placement) Margins() Margins {
	x, _ := p.layout()
	anchor := p.anchor()
	if p.CenterY {
		// A zero anchor is a true modal: centre it in the whole output while
		// retaining the output padding on both sides. Other floating panels
		// centre in the region below the bar.
		if anchor == 0 {
			avail := p.Output.H - 2*p.Padding
			y := p.Padding + (avail-p.Panel.H)/2
			if p.BarEdge == "bottom" {
				return Margins{Bottom: y, Left: x}
			}
			return Margins{Top: y, Left: x}
		}
		avail := p.Output.H - anchor - p.Padding
		y := anchor + (avail-p.Panel.H)/2
		if p.BarEdge == "bottom" {
			return Margins{Bottom: y, Left: x}
		}
		return Margins{Top: y, Left: x}
	}
	if p.BarEdge == "bottom" {
		return Margins{Bottom: anchor, Left: x}
	}
	return Margins{Top: anchor, Left: x}
}

func alignX(p Placement) int {
	if p.AnchorX > 0 {
		return p.AnchorX - p.Panel.W/2
	}
	switch p.Align {
	case "left":
		return p.Padding
	case "right":
		return p.Output.W - p.Panel.W - p.Padding
	default:
		return (p.Output.W - p.Panel.W) / 2
	}
}

func exclusiveBarZone(bar *Bar) int {
	if bar != nil {
		if s, _, _ := bar.themeSnapshot().Geometry(); s > 0 {
			return s
		}
	}
	s, _, _ := DefaultTheme().Geometry()
	return s
}

// FittedSize returns the panel size after reserving the bar edge and output padding.
func (p Placement) FittedSize() (w, h int) {
	w, h = p.Panel.W, p.Panel.H
	if max := p.Output.W - 2*p.Padding; max < 0 {
		w = 0
	} else if w > max {
		w = max
	}
	room := p.Output.H - p.anchor() - p.Padding
	if p.Joints().Flush() {
		room -= p.Fillet // the screen-edge wedge hangs past the body
	}
	if max := room; max < 0 {
		h = 0
	} else if h > max {
		h = max
	}
	return w, h
}

type ToggleResult uint8

const (
	Opened ToggleResult = iota
	Closed
	Moved
)

// PanelSet tracks one open output per panel ID. Registry owns synchronization.
type PanelSet struct {
	open map[PanelID]uint32
}

func (ps *PanelSet) Toggle(p PanelID, output uint32) ToggleResult {
	if ps.open == nil {
		ps.open = make(map[PanelID]uint32)
	}
	where, ok := ps.open[p]
	if !ok {
		ps.open[p] = output
		return Opened
	}
	if where == output {
		delete(ps.open, p)
		return Closed
	}
	ps.open[p] = output
	return Moved
}

func (ps *PanelSet) Output(p PanelID) (uint32, bool) {
	where, ok := ps.open[p]
	return where, ok
}

func (ps *PanelSet) Close(p PanelID) {
	delete(ps.open, p)
}
