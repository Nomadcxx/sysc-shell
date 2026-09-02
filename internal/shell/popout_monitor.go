package shell

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/Nomadcxx/sysc-shell/internal/render"
	"github.com/Nomadcxx/sysc-shell/internal/services"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

const (
	// monitorCardPadding is the inset between a card's fill and its content.
	monitorCardPadding = 12
	monitorCardGap     = 8
	// monitorPanelPadding insets the card grid inside the panel body.
	monitorPanelPadding = 16
)

// monitorTree builds one titled card per metric, stacked and all visible.
//
// The panel used to show a tab strip over a single unlabelled number and one
// sparkline, so a reader had to know which tab was selected to know what the
// number meant. Cards state their own subject, carry their value with its
// unit, and end with a resources card projecting what the metrics release
// supplies and no bar widget shows.
//
// Cards are laid two to a row, as the reference does. A lone trailing card
// spans the full width rather than sitting in a half-empty row.
func monitorTree(sels []services.Selector, snap services.Snapshot, history map[services.Selector][]float64, facts machineFacts) *ui.Node {
	var metrics []*ui.Node
	for _, sel := range sels {
		metrics = append(metrics, monitorMetricCard(sel, snap, history[sel]))
	}
	var info []*ui.Node
	if system := monitorSystemCard(factsWithGPU(facts, snap)); system != nil {
		info = append(info, system)
	}
	if resources := monitorResourcesCard(snap); resources != nil {
		info = append(info, resources)
	}
	cards := append(metrics, info...)
	if len(cards) == 0 {
		return &ui.Node{Kind: ui.KindColumn, Padding: 12, Children: []*ui.Node{
			{Kind: ui.KindText, Text: "No metrics"},
		}}
	}
	return &ui.Node{
		Kind: ui.KindColumn, Gap: monitorCardGap, Padding: monitorPanelPadding,
		Children: append(monitorRows(metrics), monitorRows(info)...),
	}
}

// monitorRows pairs cards into rows of two and gives each cell an explicit
// half width, so a row's two cards are the same size whichever holds the
// longer figure. An odd final card takes the whole content width.
func monitorRows(cards []*ui.Node) []*ui.Node {
	content := panelTargetSize(PanelMonitor).W - 2*monitorPanelPadding
	cell := (content - monitorCardGap) / 2
	rows := make([]*ui.Node, 0, (len(cards)+1)/2)
	for i := 0; i < len(cards); i += 2 {
		if i == len(cards)-1 {
			cards[i].Width = content
			rows = append(rows, cards[i])
			continue
		}
		cards[i].Width = cell
		cards[i+1].Width = cell
		rows = append(rows, &ui.Node{
			Kind: ui.KindRow, Gap: monitorCardGap,
			Children: []*ui.Node{cards[i], cards[i+1]},
		})
	}
	return rows
}

// monitorMetricCard is one metric: its subject, its history, and its current
// value with the unit that value is in.
func monitorMetricCard(sel services.Selector, snap services.Snapshot, history []float64) *ui.Node {
	label, absent := formatMonitorMetric(sel, snap)
	rows := []*ui.Node{monitorCardTitle(selectorLabel(sel), monitorIconRune(sel))}
	rows = append(rows, &ui.Node{Kind: ui.KindGraph, Values: monitorGraphValues(sel, history), Absent: absent})
	rows = append(rows, monitorLegend(sel, snap, label))
	return monitorCard(rows)
}

func monitorLegend(sel services.Selector, snap services.Snapshot, label string) *ui.Node {
	chips := []*ui.Node{{Kind: ui.KindText, Text: label, Tabular: true}}
	if sel.Source == services.SourceCPU && snap.Thermal != nil && snap.Thermal.Valid {
		chips = append(chips, &ui.Node{
			Kind: ui.KindText, Text: fmt.Sprintf("%.0f°C", snap.Thermal.Celsius), Tabular: true,
		})
	}
	if sel.Source == services.SourceGPU && snap.GPU != nil {
		for _, g := range snap.GPU.GPUs {
			if sel.Subject != "" && g.PCIID != sel.Subject {
				continue
			}
			if g.TempValid {
				chips = append(chips, &ui.Node{
					Kind: ui.KindText, Text: fmt.Sprintf("%.0f°C", g.Celsius), Tabular: true,
				})
			}
			break
		}
	}
	if sel.Source == services.SourceNetwork && sel.Direction == "rx" && snap.Network != nil {
		for _, iface := range snap.Network.Interfaces {
			if sel.Subject != "" && iface.Name != sel.Subject {
				continue
			}
			if iface.Rates.TransmitBytesPerSecond > 0 {
				chips = append(chips, &ui.Node{
					Kind: ui.KindText, Text: formatRate(iface.Rates.TransmitBytesPerSecond), Tabular: true,
				})
			}
			break
		}
	}
	return &ui.Node{Kind: ui.KindRow, Gap: 12, Children: chips}
}

// machineFacts is the System card: the six identity rows Noctalia draws on
// the sysmon pane. GPU usage and CPU temperature live on the metric cards.
// GPU model is filled from the GPU snapshot when a name exists.
type machineFacts struct {
	CPU, GPU, OS, Kernel, WM, Uptime string
}

func monitorSystemCard(facts machineFacts) *ui.Node {
	rows := []*ui.Node{monitorCardTitle("System", 0)}
	before := len(rows)
	for _, kv := range [][2]string{
		{"CPU", facts.CPU},
		{"GPU", facts.GPU},
		{"OS", facts.OS},
		{"Kernel", facts.Kernel},
		{"WM", facts.WM},
		{"Uptime", facts.Uptime},
	} {
		if kv[1] != "" {
			rows = append(rows, monitorKeyValue(kv[0], kv[1]))
		}
	}
	if len(rows) == before {
		return nil
	}
	return monitorCard(rows)
}

// readMachineFacts is a one-shot identity read. Uptime is the only field
// that moves, and /proc/uptime is cheap enough to reread when the panel
// rebuilds rather than joining the leased sample loop.
func readMachineFacts() machineFacts {
	cpu, _ := os.ReadFile("/proc/cpuinfo")
	osrel, _ := os.ReadFile("/etc/os-release")
	ostype, _ := os.ReadFile("/proc/sys/kernel/ostype")
	osrelk, _ := os.ReadFile("/proc/sys/kernel/osrelease")
	uptime := ""
	if d, ok := services.ReadUptime(); ok {
		uptime = formatUptime(d)
	}
	return machineFacts{
		CPU:    parseCPUModel(string(cpu)),
		OS:     parseOSRelease(string(osrel)),
		Kernel: kernelLabel(strings.TrimSpace(string(ostype)), strings.TrimSpace(string(osrelk))),
		WM:     compositorLabel(),
		Uptime: uptime,
	}
}

func factsWithGPU(facts machineFacts, snap services.Snapshot) machineFacts {
	if facts.GPU != "" || snap.GPU == nil {
		return facts
	}
	for _, g := range snap.GPU.GPUs {
		if g.Name != "" {
			facts.GPU = g.Name
			return facts
		}
	}
	return facts
}

func kernelLabel(sysname, release string) string {
	switch {
	case sysname != "" && release != "":
		return sysname + " " + release
	case release != "":
		return release
	default:
		return sysname
	}
}

func compositorLabel() string {
	if d := strings.TrimSpace(os.Getenv("XDG_CURRENT_DESKTOP")); d != "" {
		return d
	}
	return "niri"
}

func parseCPUModel(text string) string {
	fallback := ""
	for _, line := range strings.Split(text, "\n") {
		key, val, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		key, val = strings.TrimSpace(key), strings.TrimSpace(val)
		switch key {
		case "model name":
			return val
		case "Hardware", "cpu model":
			if fallback == "" {
				fallback = val
			}
		}
	}
	return fallback
}

func parseOSRelease(text string) string {
	for _, line := range strings.Split(text, "\n") {
		key, val, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		if strings.TrimSpace(key) == "PRETTY_NAME" {
			return strings.Trim(strings.TrimSpace(val), `"`)
		}
	}
	return ""
}

func formatUptime(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	days := int64(d / (24 * time.Hour))
	rem := d % (24 * time.Hour)
	hours := int64(rem / time.Hour)
	mins := int64((rem % time.Hour) / time.Minute)
	switch {
	case days > 0 && hours > 0:
		return fmt.Sprintf("%s %s", countUnit(days, "day", "days"), countUnit(hours, "hour", "hours"))
	case days > 0:
		return countUnit(days, "day", "days")
	case hours > 0 && mins > 0:
		return fmt.Sprintf("%s %s", countUnit(hours, "hour", "hours"), countUnit(mins, "minute", "minutes"))
	case hours > 0:
		return countUnit(hours, "hour", "hours")
	default:
		return countUnit(mins, "minute", "minutes")
	}
}

func countUnit(n int64, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return fmt.Sprintf("%d %s", n, many)
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

// monitorKeyValue is one labelled figure. The value is pinned to the trailing
// edge at layout, matching the reference's right-aligned facts.
func monitorKeyValue(label, value string) *ui.Node {
	return &ui.Node{Kind: ui.KindRow, Gap: 6, Children: []*ui.Node{
		{Kind: ui.KindText, Text: label},
		{Kind: ui.KindText, Text: value, Tabular: true},
	}}
}

// monitorGraphValues scales one history against the right ceiling.
//
// A fraction source is already zero through one and graphs against that, so a
// steady 31 per cent draws a flat line a third of the way up. Normalising it
// against its own maximum instead made every sample full height, which reads
// as a machine at its limit. A rate has no ceiling, so its own maximum is the
// only scale there is.
func monitorGraphValues(sel services.Selector, history []float64) []float64 {
	if monitorSourceIsFraction(sel.Source) {
		return append([]float64(nil), history...)
	}
	return normalise(history)
}

func monitorSourceIsFraction(source services.Source) bool {
	switch source {
	case services.SourceCPU, services.SourceMemory, services.SourceFilesystem, services.SourceGPU:
		return true
	}
	return false
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
	case services.SourceGPU:
		return "GPU"
	default:
		return sel.String()
	}
}

func formatMonitorMetric(sel services.Selector, snap services.Snapshot) (string, bool) {
	if sel.Source == services.SourceMemory && snap.Memory != nil && snap.Memory.Memory.TotalBytes > 0 {
		frac, ok := snap.Fraction(sel)
		if !ok {
			return "collecting", true
		}
		return fmt.Sprintf("%s · %.0f%%", formatBytes(float64(snap.Memory.Memory.UsedBytes)), frac*100), false
	}
	if fraction, ok := snap.Fraction(sel); ok {
		return fmt.Sprintf("%.0f%%", fraction*100), false
	}
	if rate, ok := snap.Rate(sel); ok {
		return formatRate(rate), false
	}
	if sel.Source == services.SourceGPU && snap.GPU != nil && len(snap.GPU.GPUs) > 0 {
		return "--", true
	}
	return "collecting", true
}
