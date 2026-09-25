package shell

import (
	"fmt"

	"github.com/Nomadcxx/sysc-shell/internal/render"
	"github.com/Nomadcxx/sysc-shell/internal/services"
	"github.com/Nomadcxx/sysc-shell/internal/theme"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

// The Monitor page's measured composition, on the same 355/228 grid as Home.
const (
	ccMonHeroH      = 168
	ccMonRowsH      = ccPageH - ccMonHeroH - theme.MarginL // 299
	ccMonRowH       = 50
	ccMonMarkH      = 28
	ccMonMarkW      = 240
	ccMonCPUGraphH  = 64
	ccMonMemMeterH  = 8
	ccMonSwapMeterH = 4
)

func ccMonitor(r *Registry, h *PanelHost) *ui.Node {
	m := h.metrics()
	snap := services.Snapshot{}
	history := map[services.Selector][]float64{}
	if r != nil {
		snap = r.sample
		if r.metrics != nil {
			history = r.historyLocked()
		}
	}
	iface, device := "", ""
	if h != nil {
		iface, device = h.ccIface, h.ccDevice
	}
	heroes := &ui.Node{Kind: ui.KindRow, Height: ccMonHeroH, Gap: theme.MarginL, Children: []*ui.Node{
		ccMonCPUHero(m, snap, history),
		ccMonMemoryHero(m, snap, history),
	}}
	rows := monitorCard(m, []*ui.Node{
		ccMonTemperatureRow(snap, history),
		ccMonGPURow(snap, history),
		ccMonStorageRow("Storage", snap),
		ccMonRateRow("network", "Network", iface, snap, history,
			services.Selector{Source: services.SourceNetwork, Subject: iface, Direction: "rx"},
			services.Selector{Source: services.SourceNetwork, Subject: iface, Direction: "tx"},
			"↓ ", "↑ ", networkRateFloor),
		ccMonRateRow("filesystem", "Disk I/O", device, snap, history,
			services.Selector{Source: services.SourceBlock, Subject: device},
			services.Selector{Source: services.SourceBlock, Subject: device, Direction: "write"},
			"R ", "W ", diskRateFloor),
	})
	rows.Height = ccMonRowsH
	rows.Children[0].Gap = theme.MarginS
	return &ui.Node{Kind: ui.KindColumn, Height: ccPageH, Gap: theme.MarginL, Children: []*ui.Node{heroes, rows}}
}

// ccMonValue is one tabular reading. name labels it for assistive tech and
// for tests; tone carries the threshold.
func ccMonValue(name, text string, tone ui.Tone, role theme.TextRole) *ui.Node {
	return &ui.Node{Kind: ui.KindText, Name: name, Text: text, Tone: tone, TextRole: role, Tabular: true}
}

func ccMonCaption(text string) *ui.Node {
	return &ui.Node{Kind: ui.KindText, Text: text, TextRole: theme.RoleCaption, Tone: ui.ToneSubtle, Tabular: true}
}

func ccMonGraph(width, height int, values, second []float64, tone ui.Tone) *ui.Node {
	return &ui.Node{Kind: ui.KindGraph, Width: width, Height: height, Values: values,
		SecondValues: second, Window: services.HistorySize, Tone: tone, Absent: len(values) == 0}
}

func ccMonCPUHero(m theme.Metrics, snap services.Snapshot, history map[services.Selector][]float64) *ui.Node {
	sel := services.Selector{Source: services.SourceCPU}
	value, tone := ccDash, ui.ToneNormal
	var samples []float64
	if snap.CPU != nil && snap.CPU.Usage.Valid {
		pct := snap.CPU.Usage.Fraction * 100
		value, tone, samples = fmt.Sprintf("%.0f%%", pct), thresholdTone(metricCPU, pct), history[sel]
	}
	caption := ""
	if snap.Thermal != nil && snap.Thermal.Valid {
		caption = fmt.Sprintf("%.0f°C", snap.Thermal.Celsius)
	}
	if ghz, ok := meanCoreGHz(snap); ok {
		caption = joinCaption(caption, fmt.Sprintf("%.2f GHz", ghz))
	}
	if snap.CPU != nil && snap.CPU.LoadValid {
		caption = joinCaption(caption, fmt.Sprintf("load %.2f %.2f %.2f", snap.CPU.Load1, snap.CPU.Load5, snap.CPU.Load15))
	}
	open := centreIconButton("chevron_right", panelMonitorAction, "Open system monitor")
	title := &ui.Node{Kind: ui.KindRow, PinEnd: true, Children: []*ui.Node{
		monitorCardTitle("CPU", monitorIconRune(sel)),
		{Kind: ui.KindRow, Gap: theme.MarginM, CenterY: true, Children: []*ui.Node{
			ccMonValue("CPU usage", value, tone, theme.RoleTitle), open,
		}},
	}}
	card := monitorCard(m, []*ui.Node{
		title,
		ccMonGraph(ccLeftColumnW-2*m.CardPadding, ccMonCPUGraphH, samples, nil, tone),
		ccMonCaption(ccText(caption)),
	})
	card.Width, card.Height = ccLeftColumnW, ccMonHeroH
	return card
}

func ccMonMemoryHero(m theme.Metrics, snap services.Snapshot, history map[services.Selector][]float64) *ui.Node {
	sel := services.Selector{Source: services.SourceMemory}
	inner := ccRightColumnW - 2*m.CardPadding
	rows := []*ui.Node{}
	value, tone, used, swap := ccDash, ui.ToneNormal, ccDash, ccDash
	var samples []float64
	fraction, swapFraction := 0.0, 0.0
	memOK, swapOK := false, false
	if snap.Memory != nil && snap.Memory.Memory.TotalBytes > 0 {
		c := snap.Memory.Memory
		fraction = float64(c.UsedBytes) / float64(c.TotalBytes)
		value, tone = fmt.Sprintf("%.0f%%", fraction*100), thresholdTone(metricMemory, fraction*100)
		used = formatBytes(float64(c.UsedBytes)) + " / " + formatBytes(float64(c.TotalBytes))
		samples, memOK = history[sel], true
		if s := snap.Memory.Swap; s.TotalBytes > 0 {
			swapFraction, swapOK = float64(s.UsedBytes)/float64(s.TotalBytes), true
			swap = "swap " + formatBytes(float64(s.UsedBytes)) + " / " + formatBytes(float64(s.TotalBytes))
		}
	}
	rows = append(rows,
		&ui.Node{Kind: ui.KindRow, PinEnd: true, Children: []*ui.Node{
			monitorCardTitle("Memory", monitorIconRune(sel)),
			ccMonValue("Memory used", value, tone, theme.RoleTitle),
		}},
		&ui.Node{Kind: ui.KindMeter, Width: inner, Height: ccMonMemMeterH, Value: fraction, Max: 1, Tone: tone, Absent: !memOK},
		ccMonCaption(used),
		&ui.Node{Kind: ui.KindMeter, Width: inner, Height: ccMonSwapMeterH, Value: swapFraction, Max: 1, Absent: !swapOK},
		ccMonCaption(swap),
		ccMonGraph(inner, ccMonMarkH, samples, nil, tone),
	)
	card := monitorCard(m, rows)
	card.Width, card.Height = ccRightColumnW, ccMonHeroH
	return card
}

// ccMonRow is one line of the rows card: icon and label, a value over a
// caption, and the mark on the trailing edge.
func ccMonRow(iconID, label string, value, caption *ui.Node, mark *ui.Node) *ui.Node {
	// MetricIconRune knows cpu, memory, filesystem/block and network. Other
	// IDs return zero, and a zero rune would paint as a missing glyph. The
	// glyph is painted, never named: the accessible name is the plain label.
	name, text := label, label
	if icon := render.MetricIconRune(iconID); icon != 0 {
		text = string(icon) + " " + label
	}
	return &ui.Node{Kind: ui.KindRow, Height: ccMonRowH, Gap: theme.MarginM, CenterY: true, PinEnd: true, Children: []*ui.Node{
		{Kind: ui.KindRow, Gap: theme.MarginM, CenterY: true, Children: []*ui.Node{
			{Kind: ui.KindText, Name: name, Text: text, MinWidthText: ccMonLabelFloor()},
			{Kind: ui.KindColumn, Gap: theme.MarginXXS, Children: []*ui.Node{value, caption}},
		}},
		mark,
	}}
}

// ccMonLabelFloor is the widest row label, glyph included. Every label is
// floored to it so the values after them start on one column; text nodes
// take their width from a measured sample, not a pixel count.
func ccMonLabelFloor() string {
	return string(render.MetricIconRune("cpu")) + " Temperature"
}

func ccMonTemperatureRow(snap services.Snapshot, history map[services.Selector][]float64) *ui.Node {
	sel := services.Selector{Source: services.SourceCPU, Subject: "temperature"}
	value, tone, source := ccDash, ui.ToneNormal, ""
	var samples []float64
	if snap.Thermal != nil && snap.Thermal.Valid {
		value = fmt.Sprintf("CPU %.0f°C", snap.Thermal.Celsius)
		tone, samples, source = thresholdTone(metricCPUTemp, snap.Thermal.Celsius), temperatureSeries(history[sel]), snap.Thermal.Source
		if gpuSel, ok := selectGPU(snap); ok {
			for _, g := range snap.GPU.GPUs {
				if g.PCIID == gpuSel.Subject && g.TempValid {
					value += fmt.Sprintf(" · GPU %.0f°C", g.Celsius)
				}
			}
		}
	}
	return ccMonRow("cpu", "Temperature", ccMonValue("Temperature reading", value, tone, theme.RoleBody),
		ccMonCaption(source), ccMonGraph(ccMonMarkW, ccMonMarkH, samples, nil, tone))
}

func ccMonGPURow(snap services.Snapshot, history map[services.Selector][]float64) *ui.Node {
	value, tone, name := ccDash, ui.ToneNormal, ""
	var samples []float64
	if sel, ok := selectGPU(snap); ok {
		for _, g := range snap.GPU.GPUs {
			if g.PCIID != sel.Subject {
				continue
			}
			name = g.Name
			// An nvidia-smi failure leaves usage invalid for this sample
			// (sysc-495): the row dashes rather than holding a stale value.
			if g.Usage.Valid {
				pct := g.Usage.Fraction * 100
				value, tone, samples = fmt.Sprintf("%.0f%%", pct), thresholdTone(metricGPU, pct), history[sel]
			}
			if g.TempValid {
				name = joinCaption(name, fmt.Sprintf("%.0f°C", g.Celsius))
			}
		}
	}
	return ccMonRow("gpu", "GPU", ccMonValue("GPU usage", value, tone, theme.RoleBody),
		ccMonCaption(name), ccMonGraph(ccMonMarkW, ccMonMarkH, samples, nil, tone))
}

func ccMonStorageRow(label string, snap services.Snapshot) *ui.Node {
	value, caption, tone, fraction, ok := ccDash, "", ui.ToneNormal, 0.0, false
	if snap.Filesystem != nil {
		for _, fs := range snap.Filesystem.Filesystems {
			if fs.MountPoint != "/" || fs.Capacity.TotalBytes == 0 {
				continue
			}
			fraction, ok = float64(fs.Capacity.UsedBytes)/float64(fs.Capacity.TotalBytes), true
			tone = thresholdTone(metricStorage, fraction*100)
			value = formatBytes(float64(fs.Capacity.UsedBytes)) + " / " + formatBytes(float64(fs.Capacity.TotalBytes))
			caption = fmt.Sprintf("/ · %.0f%%", fraction*100)
		}
	}
	mark := &ui.Node{Kind: ui.KindMeter, Width: ccMonMarkW, Height: ccMonMemMeterH, Value: fraction, Max: 1, Tone: tone, Absent: !ok}
	return ccMonRow("filesystem", label, ccMonValue("Storage used", value, tone, theme.RoleBody), ccMonCaption(caption), mark)
}

func ccMonRateRow(iconID, label, subject string, snap services.Snapshot, history map[services.Selector][]float64,
	in, out services.Selector, inPrefix, outPrefix string, floor float64) *ui.Node {
	value, caption := ccDash, subject
	var first, second []float64
	inRate, inOK := snap.Rate(in)
	outRate, outOK := snap.Rate(out)
	if subject != "" && inOK && outOK {
		value = inPrefix + formatRate(inRate) + "  " + outPrefix + formatRate(outRate)
		ceiling := rateCeiling(floor, history[in], history[out])
		first, second = scaleSeries(history[in], ceiling), scaleSeries(history[out], ceiling)
		caption = joinCaption(subject, peakCaption(history[in], history[out]))
	}
	return ccMonRow(iconID, label, ccMonValue(label+" rate", value, ui.ToneNormal, theme.RoleBody),
		ccMonCaption(caption), ccMonGraph(ccMonMarkW, ccMonMarkH, first, second, ui.ToneNormal))
}

func joinCaption(a, b string) string {
	switch {
	case a == "":
		return b
	case b == "":
		return a
	}
	return a + " · " + b
}

// ccMonCapacity is one used-of-total reading: a percent value, a bytes
// caption, the meter fraction and, when threshold is set, the metric's tone.
func ccMonCapacity(metric monitorMetric, threshold bool, used, total uint64) (value, caption string, fraction float64, tone ui.Tone, ok bool) {
	if total == 0 {
		return ccDash, "", 0, ui.ToneNormal, false
	}
	fraction = float64(used) / float64(total)
	tone = ui.ToneNormal
	if threshold {
		tone = thresholdTone(metric, fraction*100)
	}
	return fmt.Sprintf("%.0f%%", fraction*100), formatBytes(float64(used)) + " / " + formatBytes(float64(total)), fraction, tone, true
}
