package shell

import (
	"fmt"

	"github.com/Nomadcxx/sysc-shell/internal/services"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

func monitorTree(sels []services.Selector, snap services.Snapshot, history map[services.Selector][]float64, active int) *ui.Node {
	if len(sels) == 0 {
		return &ui.Node{Kind: ui.KindColumn, Padding: 12, Children: []*ui.Node{
			{Kind: ui.KindText, Text: "No metrics"},
		}}
	}
	if active < 0 || active >= len(sels) {
		active = 0
	}
	tabs := &ui.Node{Kind: ui.KindRow, Gap: 8}
	for i, sel := range sels {
		tabs.Children = append(tabs.Children, &ui.Node{
			Kind:      ui.KindTab,
			Text:      selectorLabel(sel),
			Name:      selectorLabel(sel),
			Role:      "tab",
			Focusable: true,
			Action:    "monitor-tab",
			Value:     float64(i),
		})
	}
	sel := sels[active]
	label, absent := formatMonitorMetric(sel, snap)
	return &ui.Node{Kind: ui.KindColumn, Gap: 8, Padding: 12, Children: []*ui.Node{
		tabs,
		{Kind: ui.KindText, Text: label, Tabular: true},
		{Kind: ui.KindGraph, Width: 240, Values: normalise(history[sel]), Absent: absent},
	}}
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
