package shell

import (
	"fmt"

	"github.com/Nomadcxx/sysc-shell/internal/config"
	"github.com/Nomadcxx/sysc-shell/internal/services"
	"github.com/Nomadcxx/sysc-shell/internal/theme"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

// The reference's proportions at 1.53 scale, in logical pixels.
const (
	monitorHeaderH   = 36
	monitorInfoH     = 142
	monitorLogoSize  = 110
	monitorGaugeSize = 84
	monitorFactIcon  = 18
	monitorFactMaxW  = 360
	monitorOptionW   = 320
	monitorOwnerH    = 28
	monitorSearchW   = 220
)

type monitorView struct {
	Metrics  theme.Metrics
	Snap     services.Snapshot
	History  map[services.Selector][]float64
	Facts    machineFacts
	Apps     []runningAppSlot
	Config   config.Monitor
	UID      uint32
	Iface    string
	Device   string
	Icon     func(name string, size int) *ui.Image
	Username func(uint32) string
	AppIcon  func(name string) string
}

// monitorHeader is the one header row both pages share: title, page pills,
// the Processes page's search and options, then settings and close.
func monitorHeader(h *PanelHost, page string) *ui.Node {
	title := "Processes"
	if page == monitorPageMetrics {
		title = "System"
	}
	pill := func(id, label, icon string) *ui.Node {
		n := &ui.Node{Kind: ui.KindButton, Action: "monitor:page:" + id, Name: label, Role: "tab",
			Focusable: true, Height: monitorHeaderH, Fill: ui.FillContainerHighest, Shape: ui.ShapeSmall,
			Children: []*ui.Node{{Kind: ui.KindRow, Gap: theme.MarginS, CenterY: true, Children: []*ui.Node{
				{Kind: ui.KindIcon, Icon: icon, IconSize: monitorFactIcon},
				{Kind: ui.KindText, Text: label},
			}}}}
		if page == id {
			n.State |= ui.StateSelected
		}
		return n
	}
	left := &ui.Node{Kind: ui.KindRow, Gap: theme.MarginL, CenterY: true, Children: []*ui.Node{
		{Kind: ui.KindRow, Gap: theme.MarginS, CenterY: true, Children: []*ui.Node{
			{Kind: ui.KindIcon, Icon: "desktop_windows", IconSize: centreIconSize},
			{Kind: ui.KindText, Text: title, TextRole: theme.RoleTitle, Role: "heading", Name: title},
		}},
		{Kind: ui.KindRow, Gap: theme.MarginXS, Children: []*ui.Node{
			pill(monitorPageProcesses, "Processes", "apps"),
			pill(monitorPageMetrics, "System", "memory"),
		}},
	}}
	if page == monitorPageProcesses {
		if h.search == nil {
			h.search = ui.NewField("")
		}
		field := h.search.Node("Search")
		field.Width, field.Height, field.Placeholder = monitorSearchW, monitorHeaderH, "type to search"
		left.Children = append(left.Children, &ui.Node{Kind: ui.KindRow, Gap: theme.MarginS, CenterY: true, Children: []*ui.Node{
			{Kind: ui.KindIcon, Icon: "search", IconSize: centreIconSize},
			field,
			{Kind: ui.KindButton, Action: "monitor:clear", Name: "Clear search", Role: "button", Focusable: true,
				Width: monitorHeaderH, Height: monitorHeaderH, Fill: ui.FillContainerHighest, Shape: ui.ShapeSmall,
				Children: []*ui.Node{{Kind: ui.KindIcon, Icon: "close", IconSize: monitorFactIcon}}},
		}})
	}
	right := &ui.Node{Kind: ui.KindRow, Gap: theme.MarginS, CenterY: true}
	if page == monitorPageProcesses {
		options := centreIconButton("tune", "monitor:options", "View options")
		if h.monitorOptions {
			options.State |= ui.StateSelected
		}
		right.Children = append(right.Children, options)
	}
	right.Children = append(right.Children,
		centreIconButton("settings", "monitor:settings", "Monitor settings"),
		centreIconButton("close", "monitor:close", "Close"))
	return &ui.Node{Kind: ui.KindRow, Height: monitorHeaderH, PinEnd: true, CenterY: true, Children: []*ui.Node{left, right}}
}

// monitorInfoCard is the logo, a middle column the caller chooses (facts,
// view options, or the process detail replaces the whole card), and the CPU
// and memory rings.
func monitorInfoCard(in monitorView, middle *ui.Node) *ui.Node {
	var logo *ui.Node
	if in.Icon != nil && in.Facts.Logo != "" {
		if img := in.Icon(in.Facts.Logo, monitorLogoSize); img != nil {
			logo = &ui.Node{Kind: ui.KindImage, Image: img, Width: monitorLogoSize, Height: monitorLogoSize}
		}
	}
	if logo == nil {
		logo = &ui.Node{Kind: ui.KindCapsule, Width: monitorLogoSize, Height: monitorLogoSize,
			Fill: ui.FillContainer, Shape: ui.ShapeLarge, Children: []*ui.Node{
				{Kind: ui.KindText, Text: in.Facts.LogoLetter, TextRole: theme.RoleDisplay, CenterX: true, CenterY: true},
			}}
	}
	return &ui.Node{Kind: ui.KindCapsule, Height: monitorInfoH, Padding: in.Metrics.CardPadding,
		Fill: ui.FillContainerHigh, Shape: ui.ShapeCard, Children: []*ui.Node{
			{Kind: ui.KindRow, Gap: theme.MarginL, CenterY: true, PinEnd: true, Children: []*ui.Node{
				{Kind: ui.KindRow, Gap: theme.MarginL, CenterY: true, Children: []*ui.Node{logo, middle}},
				{Kind: ui.KindRow, Gap: theme.MarginL, CenterY: true, Children: monitorGauges(in.Snap)},
			}},
		}}
}

func monitorFactsColumn(f machineFacts) *ui.Node {
	row := func(icon, text string) *ui.Node {
		if text == "" {
			text = ccDash
		}
		return &ui.Node{Kind: ui.KindRow, Gap: theme.MarginS, CenterY: true, Children: []*ui.Node{
			{Kind: ui.KindIcon, Icon: icon, IconSize: monitorFactIcon, Tone: ui.ToneSubtle},
			{Kind: ui.KindText, Text: text, MaxWidth: monitorFactMaxW},
		}}
	}
	return &ui.Node{Kind: ui.KindColumn, Gap: theme.MarginXS, Children: []*ui.Node{
		row("desktop_windows", f.Distro),
		row("content_copy", f.Kernel),
		row("memory", f.CPU),
		row("developer_board", f.Board),
		row("schedule", f.UptimeLong),
	}}
}

// monitorOptionsColumn replaces the facts while the options button is on,
// as the reference's view options do: two section toggles and the owner
// filter the header pills gave up their place to.
func monitorOptionsColumn(h *PanelHost, in monitorView) *ui.Node {
	toggle := func(action, label string, on bool) *ui.Node {
		return &ui.Node{Kind: ui.KindRow, Gap: theme.MarginL, CenterY: true, PinEnd: true, Width: monitorOptionW, Children: []*ui.Node{
			{Kind: ui.KindText, Text: label, Tone: ui.ToneSubtle},
			{Kind: ui.KindToggle, Width: ui.ToggleWidth, Height: ui.ToggleHeight, Action: action, Name: label,
				Role: "switch", Focusable: true, Value: toggleValue(on)},
		}}
	}
	owners := &ui.Node{Kind: ui.KindSegmented, Key: "process-owner", Height: monitorOwnerH}
	for _, o := range []struct{ id, label string }{{"all", "All"}, {"user", "User"}, {"system", "System"}} {
		n := &ui.Node{Kind: ui.KindButton, Action: "monitor:owner:" + o.id, Name: o.label, Role: "tab",
			Focusable: true, Height: monitorOwnerH, Children: []*ui.Node{{Kind: ui.KindText, Text: o.label}}}
		if h.processFilter == o.id {
			n.State |= ui.StateSelected
		}
		owners.Children = append(owners.Children, n)
	}
	return &ui.Node{Kind: ui.KindColumn, Gap: theme.MarginS, Children: []*ui.Node{
		toggle("monitor:show:apps", "Show applications", in.Config.ShowApps),
		toggle("monitor:show:procs", "Show processes", in.Config.ShowProcesses),
		owners,
	}}
}

// monitorGauges are the reference's two rings: CPU usage over temperature,
// memory used over memory available.
func monitorGauges(snap services.Snapshot) []*ui.Node {
	gauge := func(name, label, value, under string, fraction float64, ok bool) *ui.Node {
		return &ui.Node{Kind: ui.KindColumn, Width: monitorGaugeSize, Gap: theme.MarginXXS, Name: name, Role: "group", Children: []*ui.Node{
			{Kind: ui.KindRadialGauge, Width: monitorGaugeSize, Height: monitorGaugeSize, Value: fraction,
				ValueText: value, Absent: !ok, Name: name, Role: "img", Tooltip: label + ": " + value},
			{Kind: ui.KindText, Text: label, Tone: ui.ToneAccent, CenterX: true},
			{Kind: ui.KindText, Text: under, TextRole: theme.RoleCaption, Tone: ui.ToneSubtle, CenterX: true, Tabular: true},
		}}
	}
	cpuValue, cpuUnder, cpuFrac, cpuOK := ccDash, "", 0.0, false
	if snap.CPU != nil && snap.CPU.Usage.Valid {
		cpuFrac, cpuOK = snap.CPU.Usage.Fraction, true
		cpuValue = fmt.Sprintf("%.0f%%", cpuFrac*100)
	}
	if snap.Thermal != nil && snap.Thermal.Valid {
		cpuUnder = fmt.Sprintf("%.0f°", snap.Thermal.Celsius)
	}
	memValue, memUnder, memFrac, memOK := ccDash, "", 0.0, false
	if snap.Memory != nil && snap.Memory.Memory.TotalBytes > 0 {
		c := snap.Memory.Memory
		memFrac, memOK = float64(c.UsedBytes)/float64(c.TotalBytes), true
		memValue = formatProcessBytes(c.UsedBytes)
		memUnder = "+" + formatProcessBytes(c.AvailableBytes)
	}
	return []*ui.Node{
		gauge("CPU gauge", "CPU", cpuValue, cpuUnder, cpuFrac, cpuOK),
		gauge("Memory gauge", "Memory", memValue, memUnder, memFrac, memOK),
	}
}

// formatProcessBytes is the reference's compact size: "2.4G", "651.1M".
func formatProcessBytes(b uint64) string {
	const k = 1024
	switch {
	case b >= k*k*k:
		return fmt.Sprintf("%.1fG", float64(b)/(k*k*k))
	case b >= k*k:
		return fmt.Sprintf("%.1fM", float64(b)/(k*k))
	case b >= k:
		return fmt.Sprintf("%.1fK", float64(b)/k)
	}
	return fmt.Sprintf("%dB", b)
}
