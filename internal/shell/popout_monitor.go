package shell

import (
	"fmt"

	"github.com/Nomadcxx/sysc-shell/internal/render"
	"github.com/Nomadcxx/sysc-shell/internal/services"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

const (
	// monitorCardPadding is the inset between a card's fill and its content.
	monitorCardPadding = 10
	monitorCardGap     = 8
)

// monitorTree builds one titled card per metric, stacked and all visible.
//
// The panel used to show a tab strip over a single unlabelled number and one
// sparkline, so a reader had to know which tab was selected to know what the
// number meant. Cards state their own subject, carry their value with its
// unit, and end with a resources card projecting what the metrics release
// supplies and no bar widget shows.
//
// The reference lays these out two to a row. That needs a capsule inside a row
// inside a column to measure to its content rather than the row's unbounded
// height, which the measure path does not do yet; a single column of
// full-width cards is the same content in the width this panel has.
func monitorTree(sels []services.Selector, snap services.Snapshot, history map[services.Selector][]float64, _ int) *ui.Node {
	cards := make([]*ui.Node, 0, len(sels)+1)
	for _, sel := range sels {
		cards = append(cards, monitorMetricCard(sel, snap, history[sel]))
	}
	if resources := monitorResourcesCard(snap); resources != nil {
		cards = append(cards, resources)
	}
	if len(cards) == 0 {
		return &ui.Node{Kind: ui.KindColumn, Padding: 12, Children: []*ui.Node{
			{Kind: ui.KindText, Text: "No metrics"},
		}}
	}
	return &ui.Node{Kind: ui.KindColumn, Gap: monitorCardGap, Padding: 12, Children: cards}
}

// monitorMetricCard is one metric: its subject, its history, and its current
// value with the unit that value is in.
func monitorMetricCard(sel services.Selector, snap services.Snapshot, history []float64) *ui.Node {
	label, absent := formatMonitorMetric(sel, snap)
	rows := []*ui.Node{monitorCardTitle(selectorLabel(sel), monitorIconRune(sel))}
	rows = append(rows,
		&ui.Node{Kind: ui.KindGraph, Values: normalise(history), Absent: absent},
		&ui.Node{Kind: ui.KindText, Text: label, Tabular: true},
	)
	return monitorCard(rows)
}

// monitorResourcesCard reports the capacities and averages the sampler already
// collects and nothing projects: the load average, memory and swap as bytes
// rather than a bare percentage.
//
// It is omitted entirely when none of them is available, so an unsampled
// machine shows no empty card.
func monitorResourcesCard(snap services.Snapshot) *ui.Node {
	rows := []*ui.Node{monitorCardTitle("Resources", 0)}
	before := len(rows)

	if snap.CPU != nil && snap.CPU.LoadValid {
		rows = append(rows, monitorKeyValue("Load",
			fmt.Sprintf("%.2f / %.2f / %.2f", snap.CPU.Load1, snap.CPU.Load5, snap.CPU.Load15)))
	}
	if snap.Memory != nil {
		if row := monitorCapacityRow("Memory",
			snap.Memory.Memory.UsedBytes, snap.Memory.Memory.TotalBytes); row != nil {
			rows = append(rows, row)
		}
		if row := monitorCapacityRow("Swap",
			snap.Memory.Swap.UsedBytes, snap.Memory.Swap.TotalBytes); row != nil {
			rows = append(rows, row)
		}
	}
	if snap.Filesystem != nil {
		for _, fs := range snap.Filesystem.Filesystems {
			if row := monitorCapacityRow(fs.MountPoint,
				fs.Capacity.UsedBytes, fs.Capacity.TotalBytes); row != nil {
				rows = append(rows, row)
			}
		}
	}
	if len(rows) == before {
		return nil
	}
	return monitorCard(rows)
}

// monitorCapacityRow renders one used-of-total pair. A zero total is a
// capacity the machine does not have, such as swap on a system without it,
// and renders nothing rather than "0 B / 0 B".
//
// It takes the two counts rather than the upstream capacity type: this file
// projects what internal/services already resolved, and importing the metrics
// release here would put the panel back on the sampler's vocabulary.
func monitorCapacityRow(label string, used, total uint64) *ui.Node {
	if total == 0 {
		return nil
	}
	return monitorKeyValue(label,
		fmt.Sprintf("%s / %s", formatBytes(float64(used)), formatBytes(float64(total))))
}

// monitorCard wraps content in the same capsule the bar uses, so a panel card
// and a bar widget are visibly the same surface.
func monitorCard(rows []*ui.Node) *ui.Node {
	return &ui.Node{
		Kind: ui.KindCapsule, Padding: monitorCardPadding,
		Children: []*ui.Node{{Kind: ui.KindColumn, Gap: 4, Children: rows}},
	}
}

// monitorCardTitle names the card. The icon is the one the bar widget for that
// metric already uses, so a card and its widget read as the same subject.
//
// The glyph goes in the painted text and not in the accessible name: it is a
// private-use codepoint from the shell's own icon font and means nothing when
// it is read out.
func monitorCardTitle(label string, icon rune) *ui.Node {
	text := label
	if icon != 0 {
		text = string(icon) + " " + label
	}
	return &ui.Node{Kind: ui.KindText, Text: text, Bold: true, Name: label, Role: "heading"}
}

// monitorKeyValue is one labelled figure. The reference right-aligns the value
// against the card's trailing edge; a row places its children left to right
// and the tree has no alignment of its own, so the label and the value sit
// together for now.
func monitorKeyValue(label, value string) *ui.Node {
	return &ui.Node{Kind: ui.KindRow, Gap: 6, Children: []*ui.Node{
		{Kind: ui.KindText, Text: label},
		{Kind: ui.KindText, Text: value, Tabular: true},
	}}
}

func monitorIconRune(sel services.Selector) rune {
	switch sel.Source {
	case services.SourceCPU:
		return render.MetricIconRune("cpu")
	case services.SourceMemory:
		return render.MetricIconRune("memory")
	case services.SourceFilesystem, services.SourceBlock:
		return render.MetricIconRune("filesystem")
	case services.SourceNetwork:
		return render.MetricIconRune("network")
	}
	return 0
}

func selectorLabel(sel services.Selector) string {
	switch sel.Source {
	case services.SourceCPU:
		return "CPU"
	case services.SourceMemory:
		return "Memory"
	case services.SourceFilesystem:
		if sel.Subject != "" {
			return sel.Subject
		}
		return "Disk"
	case services.SourceBlock:
		if sel.Subject != "" {
			return sel.Subject
		}
		return "Block"
	case services.SourceNetwork:
		if sel.Subject != "" {
			return sel.Subject
		}
		return "Net"
	default:
		return sel.String()
	}
}

func formatMonitorMetric(sel services.Selector, snap services.Snapshot) (string, bool) {
	if fraction, ok := snap.Fraction(sel); ok {
		return fmt.Sprintf("%.0f%%", fraction*100), false
	}
	if rate, ok := snap.Rate(sel); ok {
		return formatRate(rate), false
	}
	return "collecting", true
}
