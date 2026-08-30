package shell

import "github.com/Nomadcxx/sysc-shell/internal/ui"

// PanelID identifies one process-wide auxiliary panel instance.
type PanelID uint8

const (
	PanelClock PanelID = iota
	PanelMonitor
	PanelSession
	PanelSettings
)

func (p PanelID) String() string {
	switch p {
	case PanelClock:
		return "clock"
	case PanelMonitor:
		return "monitor"
	case PanelSession:
		return "session"
	case PanelSettings:
		return "settings"
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
}

type Margins struct{ Top, Bottom, Left, Right int }

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
	x := clampAxis(alignX(p), p.Panel.W, p.Output.W, p.Padding)
	anchor := p.BarZone + p.Gap
	if p.BarEdge == "bottom" {
		return Margins{Bottom: anchor, Left: x}
	}
	return Margins{Top: anchor, Left: x}
}

func alignX(p Placement) int {
	switch p.Align {
	case "left":
		return p.Padding
	case "right":
		return p.Output.W - p.Panel.W - p.Padding
	default:
		return (p.Output.W - p.Panel.W) / 2
	}
}

// FittedSize returns the panel size after reserving the bar edge and output padding.
func (p Placement) FittedSize() (w, h int) {
	w, h = p.Panel.W, p.Panel.H
	if max := p.Output.W - 2*p.Padding; w > max {
		w = max
	}
	if max := p.Output.H - p.BarZone - p.Gap - p.Padding; h > max {
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

func (ps *PanelSet) Open(p PanelID) (PanelID, uint32) {
	return p, ps.open[p]
}

func (ps *PanelSet) Close(p PanelID) {
	delete(ps.open, p)
}
