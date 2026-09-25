package shell

import (
	"fmt"

	"github.com/Nomadcxx/sysc-shell/internal/services"
	"github.com/Nomadcxx/sysc-shell/internal/theme"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

// systemPageTree is the System page: the shared header and info card over one
// card of collapsible sections in the process table's style. Row content is
// the Control Centre Monitor page's, so the two surfaces never disagree.
func systemPageTree(h *PanelHost, in monitorView) *ui.Node {
	snap, history := in.Snap, in.History
	if history == nil {
		history = map[services.Selector][]float64{}
	}
	cpuSel := services.Selector{Source: services.SourceCPU}
	cpuValue, cpuTone, cpuCaption := ccDash, ui.ToneNormal, ""
	var cpuSamples []float64
	if snap.CPU != nil && snap.CPU.Usage.Valid {
		pct := snap.CPU.Usage.Fraction * 100
		cpuValue, cpuTone, cpuSamples = fmt.Sprintf("%.0f%%", pct), thresholdTone(metricCPU, pct), history[cpuSel]
	}
	if snap.Thermal != nil && snap.Thermal.Valid {
		cpuCaption = fmt.Sprintf("%.0f°C", snap.Thermal.Celsius)
	}
	if ghz, ok := meanCoreGHz(snap); ok {
		cpuCaption = joinCaption(cpuCaption, fmt.Sprintf("%.2f GHz", ghz))
	}
	if snap.CPU != nil && snap.CPU.LoadValid {
		cpuCaption = joinCaption(cpuCaption, fmt.Sprintf("load %.2f %.2f %.2f", snap.CPU.Load1, snap.CPU.Load5, snap.CPU.Load15))
	}
	cpu := ccMonRow("cpu", "CPU", ccMonValue("CPU usage", cpuValue, cpuTone, theme.RoleBody),
		ccMonCaption(cpuCaption), ccMonGraph(ccMonMarkW, ccMonMarkH, cpuSamples, nil, cpuTone))

	var ram, swap *ui.Node
	{
		var used, total, sUsed, sTotal uint64
		if snap.Memory != nil {
			used, total = snap.Memory.Memory.UsedBytes, snap.Memory.Memory.TotalBytes
			sUsed, sTotal = snap.Memory.Swap.UsedBytes, snap.Memory.Swap.TotalBytes
		}
		v, c, f, tone, ok := ccMonCapacity(metricMemory, true, used, total)
		ram = ccMonRow("memory", "RAM", ccMonValue("RAM used", v, tone, theme.RoleBody), ccMonCaption(c),
			&ui.Node{Kind: ui.KindMeter, Width: ccMonMarkW, Height: ccMonMemMeterH, Value: f, Max: 1, Tone: tone, Absent: !ok})
		v, c, f, tone, ok = ccMonCapacity(metricMemory, false, sUsed, sTotal)
		swap = ccMonRow("memory", "Swap", ccMonValue("Swap used", v, tone, theme.RoleBody), ccMonCaption(c),
			&ui.Node{Kind: ui.KindMeter, Width: ccMonMarkW, Height: ccMonMemMeterH, Value: f, Max: 1, Tone: tone, Absent: !ok})
	}

	sections := []struct {
		key, title string
		rows       []*ui.Node
	}{
		{"section:compute", "Compute", []*ui.Node{cpu, ccMonGPURow(snap, history, true)}},
		{"section:memory", "Memory", []*ui.Node{ram, swap}},
		{"section:storage", "Storage & network", []*ui.Node{
			ccMonStorageRow("/", snap),
			ccMonRateRow("filesystem", "Disk I/O", in.Device, snap, history,
				services.Selector{Source: services.SourceBlock, Subject: in.Device},
				services.Selector{Source: services.SourceBlock, Subject: in.Device, Direction: "write"},
				"R ", "W ", diskRateFloor),
			ccMonRateRow("network", "Network", in.Iface, snap, history,
				services.Selector{Source: services.SourceNetwork, Subject: in.Iface, Direction: "rx"},
				services.Selector{Source: services.SourceNetwork, Subject: in.Iface, Direction: "tx"},
				"↓ ", "↑ ", networkRateFloor),
		}},
	}
	if h.processCollapsed == nil {
		h.processCollapsed = map[string]bool{}
	}
	body := &ui.Node{Kind: ui.KindColumn, Gap: theme.MarginXS}
	for _, s := range sections {
		open := !h.processCollapsed[s.key]
		body.Children = append(body.Children, processLineRow(h, in, processLine{Kind: lineSection, Key: s.key, Name: s.title, Expanded: open}))
		if open {
			body.Children = append(body.Children, s.rows...)
		}
	}
	pad := h.metrics().PanelPadding
	bodyH := max(h.place.Panel.H-2*pad-monitorHeaderH-monitorInfoH-2*theme.MarginM, processRowPitch)
	card := &ui.Node{Kind: ui.KindCapsule, Padding: h.metrics().CardPadding, Fill: ui.FillContainerHigh, Shape: ui.ShapeCard,
		Children: []*ui.Node{{Kind: ui.KindScroll, Height: bodyH - 2*h.metrics().CardPadding, Children: []*ui.Node{body}}}}
	return &ui.Node{Kind: ui.KindColumn, Gap: theme.MarginM, Padding: pad, Children: []*ui.Node{
		monitorHeader(h, monitorPageMetrics), monitorInfoCard(in, monitorFactsColumn(in.Facts)), card,
	}}
}
