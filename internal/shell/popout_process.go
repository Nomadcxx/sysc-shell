package shell

import (
	"errors"
	"fmt"
	"math"
	"os"
	"strconv"
	"strings"
	"syscall"

	"github.com/Nomadcxx/sysc-shell/internal/config"
	"github.com/Nomadcxx/sysc-shell/internal/icons"
	"github.com/Nomadcxx/sysc-shell/internal/services"
	"github.com/Nomadcxx/sysc-shell/internal/theme"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

const (
	monitorPageProcesses = "processes"
	monitorPageMetrics   = "monitor"
)

func (r *Registry) monitorViewLocked(h *PanelHost) monitorView {
	users := r.usernameCache()
	entries := r.runningIndex
	return monitorView{
		Snap: r.sample, History: r.historyLocked(), Facts: r.machineFacts, Apps: r.running,
		Config: r.cfg.Monitor, UID: uint32(os.Getuid()), Iface: h.ccIface, Device: h.ccDevice,
		Icon:     func(name string, size int) *ui.Image { return monitorLookupIcon(r, h, name, size) },
		Username: users.Name,
		AppIcon: func(name string) string {
			if e, ok := lookupRunningApp(name, entries); ok {
				return e.Icon
			}
			return ""
		},
	}
}

// monitorLookupIcon is launcherLookupIcon at an arbitrary logical size.
func monitorLookupIcon(r *Registry, h *PanelHost, name string, logical int) *ui.Image {
	if r == nil || r.trayIcons == nil || name == "" {
		return nil
	}
	size := logical
	if scale := ui.Scale120(h.scale120); scale.Valid() {
		size = max(scale.Physical(logical), 1)
	}
	key := icons.Square(name, size)
	if img, ok := r.trayIcons.Lookup(key); ok {
		return img
	}
	_, _, _ = r.trayIcons.Request(key)
	return nil
}

const (
	processRowPitch     = 32
	processRowHeight    = 30
	processHeaderHeight = 32
	processTablePadding = 6
	processIconSize     = 20
	processIndent       = 20
	processFooterHeight = 20
	processRowPadding   = 2
	// processHeaderRowH is the header cells plus the insets that line them
	// up with the list rows below.
	processHeaderRowH = processHeaderHeight + 2*(processTablePadding+processRowPadding)
)

// processColumn is one table column after Name. Widths are the reference's
// proportions at 800 logical; Name takes what is left.
type processColumn struct {
	key, label string
	width      int
}

var processColumnsAfterName = []processColumn{
	{"cpu", "CPU", 72}, {"mem", "MEM", 100}, {"swap", "SWAP", 84},
	{"io", "DISK", 96}, {"pid", "PID", 72}, {"user", "USER", 88},
}

func processNameWidth(h *PanelHost) int {
	// The table card and the list well each inset by processTablePadding, and
	// a row by processRowPadding; the header row carries the same total.
	w := h.place.Panel.W - 2*h.metrics().PanelPadding - 4*processTablePadding - 2*processRowPadding
	for _, c := range processColumnsAfterName {
		w -= c.width + theme.MarginM
	}
	return max(w, 120)
}

func monitorPanelTree(h *PanelHost, in monitorView) *ui.Node {
	if h.monitorPage == "" {
		h.monitorPage = monitorPageProcesses
	}
	if h.monitorPage == monitorPageMetrics {
		return legacySystemPage(h, in)
	}
	return processTableTree(h, in)
}

func processTableTree(h *PanelHost, in monitorView) *ui.Node {
	if h.processFilter == "" {
		h.processFilter = "all"
	}
	if h.processSort == "" {
		h.processSort, h.processDesc = "mem", true
	}
	if h.processExpanded == nil {
		h.processExpanded = map[string]bool{}
	}
	if h.processCollapsed == nil {
		h.processCollapsed = map[string]bool{}
	}
	var snapshot services.ProcessSnapshot
	if in.Snap.Processes != nil {
		snapshot = *in.Snap.Processes
	}
	lines := projectProcessLines(processLineInput{
		Processes: snapshot.Processes, Apps: in.Apps, CurrentUID: in.UID,
		Query: h.query, Owner: h.processFilter, Sort: h.processSort, Desc: h.processDesc,
		Expanded: h.processExpanded, Collapsed: h.processCollapsed,
		ShowApps: in.Config.ShowApps, ShowProcesses: in.Config.ShowProcesses,
		Username: in.Username, AppIcon: in.AppIcon,
	})

	middle := monitorFactsColumn(in.Facts)
	if h.monitorOptions {
		middle = monitorOptionsColumn(h, in)
	}
	info := monitorInfoCard(in, middle)
	if detail := processDetailCard(h, in, snapshot); detail != nil {
		info = detail
	}

	pad := h.metrics().PanelPadding
	used := 2*pad + monitorHeaderH + monitorInfoH + processHeaderRowH + 2*processTablePadding + processFooterHeight + 4*theme.MarginM
	tableH := max(h.place.Panel.H-used, processRowPitch+2*processTablePadding)
	rows := make([]*ui.Node, len(lines))
	list := &ui.Node{
		Kind: ui.KindVirtualList, Height: tableH - 2*processTablePadding,
		ItemCount: len(lines), ItemHeight: processRowPitch, HideScrollbar: true,
		Item: func(i int) *ui.Node {
			if i < 0 || i >= len(lines) {
				return nil
			}
			if rows[i] == nil {
				rows[i] = processLineRow(h, in, lines[i])
			}
			return rows[i]
		},
	}
	table := &ui.Node{Kind: ui.KindCapsule, Padding: processTablePadding, Fill: ui.FillContainerHigh, Shape: ui.ShapeCard,
		Children: []*ui.Node{{Kind: ui.KindColumn, Gap: theme.MarginM, Children: []*ui.Node{
			processTableHeader(h, in),
			{Kind: ui.KindCapsule, Height: tableH, Fill: ui.FillContainerHighest, Shape: ui.ShapeCard,
				Padding: processTablePadding, Children: []*ui.Node{list}},
		}}}}
	return &ui.Node{Kind: ui.KindColumn, Gap: theme.MarginM, Padding: pad, Children: []*ui.Node{
		monitorHeader(h, monitorPageProcesses), info, table, processFooter(h, snapshot, in.UID),
	}}
}

func monitorRole(name string, fallback ui.PaintRole) ui.PaintRole {
	if r, ok := ui.PaintRoleFor(name); ok {
		return r
	}
	return fallback
}

func processTableHeader(h *PanelHost, in monitorView) *ui.Node {
	cell := func(key, label string, width int) *ui.Node {
		text := label
		n := &ui.Node{Kind: ui.KindButton, Action: "monitor:sort:" + key, Name: "Sort by " + label,
			Role: "button", Focusable: true, Width: width, Height: processHeaderHeight, Shape: ui.ShapeSmall,
			Children: []*ui.Node{{Kind: ui.KindText, Text: text, CenterX: true}}}
		if h.processSort == key {
			arrow := "▲ "
			if h.processDesc {
				arrow = "▼ "
			}
			n.Children[0].Text = arrow + label
			n.Fill = ui.FillRole
			n.FillRole = monitorRole(in.Config.SortBackground, ui.PaintSurfaceVariant)
			n.InkRole = monitorRole(in.Config.SortColor, ui.PaintOnSurfaceVariant)
		}
		return n
	}
	row := &ui.Node{Kind: ui.KindRow, Gap: theme.MarginM, Padding: processTablePadding + processRowPadding, Height: processHeaderRowH,
		Children: []*ui.Node{cell("name", "Name", processNameWidth(h))}}
	for _, c := range processColumnsAfterName {
		row.Children = append(row.Children, cell(c.key, c.label, c.width))
	}
	return row
}

func processLineRow(h *PanelHost, in monitorView, l processLine) *ui.Node {
	if l.Kind == lineSection {
		chevron := "chevron_right"
		if l.Expanded {
			chevron = "expand_more"
		}
		return &ui.Node{Kind: ui.KindRow, Height: processRowHeight, Gap: theme.MarginS, CenterY: true,
			Action: "monitor:toggle:" + l.Key, Name: l.Name, Role: "button", Focusable: true, Children: []*ui.Node{
				{Kind: ui.KindIcon, Icon: chevron, IconSize: processIconSize},
				{Kind: ui.KindText, Text: l.Name, TextRole: theme.RoleTitle, Tone: ui.ToneActivity},
			}}
	}
	nameCell := &ui.Node{Kind: ui.KindRow, Width: processNameWidth(h), Gap: theme.MarginS, CenterY: true}
	nameCell.Children = append(nameCell.Children, &ui.Node{Kind: ui.KindColumn, Width: processIndent * (l.Depth + 1)})
	if l.Expandable {
		chevron := "chevron_right"
		if l.Expanded {
			chevron = "expand_more"
		}
		nameCell.Children[0] = &ui.Node{Kind: ui.KindRow, Width: processIndent * (l.Depth + 1), PinEnd: true,
			Children: []*ui.Node{{Kind: ui.KindColumn}, {Kind: ui.KindIcon, Icon: chevron, IconSize: processIconSize - 4}}}
	}
	nameCell.Children = append(nameCell.Children, processLineIcon(in, l), &ui.Node{Kind: ui.KindText, Text: l.Name,
		MaxWidth: processNameWidth(h) - processIndent*(l.Depth+1) - processIconSize - 2*theme.MarginS})
	row := &ui.Node{Kind: ui.KindRow, Height: processRowHeight, Padding: processRowPadding, Gap: theme.MarginM, CenterY: true,
		Shape: ui.ShapeSmall, Role: "row", Focusable: true, Name: l.Name,
		HoverFill: monitorRole(in.Config.HoverBackground, ui.PaintSurfaceVariant),
		HoverInk:  monitorRole(in.Config.HoverColor, ui.PaintOnSurfaceVariant),
		Children:  []*ui.Node{nameCell}}
	if l.Kind == lineGroup {
		row.Action = "monitor:toggle:" + l.Key
	} else {
		row.Action = fmt.Sprintf("monitor:select:%d:%d", l.Identity.PID, l.Identity.StartTimeTicks)
		if h.processSelected == l.Identity {
			row.Fill = ui.FillSoft
		}
	}
	for _, c := range processColumnsAfterName {
		var text string
		switch c.key {
		case "pid":
			text = l.PIDText
		case "user":
			text = l.User
		default:
			text = formatProcessCell(c.key, l.Totals)
		}
		cell := &ui.Node{Kind: ui.KindCapsule, Width: c.width, Height: processRowHeight - 4, Name: c.label + " value",
			Shape: ui.ShapeSmall, Children: []*ui.Node{{Kind: ui.KindText, Text: text, Tabular: true, MaxWidth: c.width - 8, CenterY: true}}}
		if c.key == "cpu" || c.key == "mem" || c.key == "swap" || c.key == "io" {
			cell.Children[0].PinEnd = true
		}
		if h.processSort == c.key {
			cell.Fill = ui.FillRole
			cell.FillRole = monitorRole(in.Config.SortBackground, ui.PaintSurfaceVariant)
			cell.InkRole = monitorRole(in.Config.SortColor, ui.PaintOnSurfaceVariant)
		}
		row.Children = append(row.Children, cell)
	}
	return row
}

func processLineIcon(in monitorView, l processLine) *ui.Node {
	if in.Icon != nil && l.Icon != "" {
		if img := in.Icon(l.Icon, processIconSize); img != nil {
			return &ui.Node{Kind: ui.KindImage, Image: img, Width: processIconSize, Height: processIconSize}
		}
	}
	return &ui.Node{Kind: ui.KindCapsule, Width: processIconSize, Height: processIconSize, Fill: ui.FillContainer,
		Shape: ui.ShapeSmall, Children: []*ui.Node{{Kind: ui.KindText, Text: launcherGlyph(l.Name),
			TextRole: theme.RoleCaption, CenterX: true, CenterY: true}}}
}

// formatProcessCell is one numeric cell in the reference's form. Zero and
// unavailable are the same em dash, as they are there.
func formatProcessCell(kind string, t processTotals) string {
	spaced := func(b uint64) string {
		s := formatProcessBytes(b)
		return s[:len(s)-1] + " " + s[len(s)-1:]
	}
	switch kind {
	case "cpu":
		if !t.CPUValid || t.CPU*100 < 0.05 {
			return ccDash
		}
		pct := t.CPU * 100
		if pct == math.Trunc(pct) {
			return fmt.Sprintf("%.0f%%", pct)
		}
		return strings.TrimSuffix(strings.TrimSuffix(fmt.Sprintf("%.1f", pct), "0"), ".") + "%"
	case "mem":
		if !t.ResidentValid || t.Resident == 0 {
			return ccDash
		}
		return spaced(t.Resident)
	case "swap":
		if !t.SwapValid || t.Swap == 0 {
			return ccDash
		}
		return spaced(t.Swap)
	case "io":
		if !t.IOValid || t.IO < 1 {
			return ccDash
		}
		return spaced(uint64(t.IO)) + "/s"
	}
	return ccDash
}

func processFooter(h *PanelHost, snapshot services.ProcessSnapshot, uid uint32) *ui.Node {
	users := 0
	for _, p := range snapshot.Processes {
		if p.UIDValid && p.UID == uid {
			users++
		}
	}
	text := fmt.Sprintf("Total processes: %d (user: %d  system: %d)", len(snapshot.Processes), users, len(snapshot.Processes)-users)
	tone := ui.ToneSubtle
	switch {
	case h.processStatus != "":
		text, tone = h.processStatus, ui.ToneNormal
		if h.processStatusErr != nil {
			tone = ui.ToneError
		}
	case len(snapshot.Issues) > 0:
		text += fmt.Sprintf(" · %d could not be read", len(snapshot.Issues))
	}
	return &ui.Node{Kind: ui.KindText, Text: text, Tone: tone, TextRole: theme.RoleCaption, Height: processFooterHeight}
}

func validProcessSort(key string) bool {
	switch key {
	case "name", "cpu", "mem", "swap", "io", "pid", "user":
		return true
	}
	return false
}

// legacySystemPage is the previous metric cards, kept only until the System
// page is rebuilt on the shared frame.
func legacySystemPage(h *PanelHost, in monitorView) *ui.Node {
	bodyH := max(h.place.Panel.H-2*h.metrics().PanelPadding-monitorHeaderH-theme.MarginL, processRowHeight)
	body := monitorTree(h.metrics(), monitorSelectors(config.Bar{}), in.Snap, in.History, in.Facts)
	body.Padding = 0
	return &ui.Node{Kind: ui.KindColumn, Gap: theme.MarginL, Padding: h.metrics().PanelPadding, Children: []*ui.Node{
		monitorHeader(h, monitorPageMetrics),
		{Kind: ui.KindScroll, Height: bodyH, Children: []*ui.Node{body}},
	}}
}

func revealFocusedProcess(h *PanelHost) bool {
	if h == nil || h.monitorPage != monitorPageProcesses {
		return false
	}
	target := h.focused()
	list := processVirtualList(h.root)
	if target == nil || list == nil || list.Item == nil || list.ItemHeight <= 0 {
		return false
	}
	view := list.Height
	if view <= 0 {
		view = list.Bounds.H
	}
	for i := 0; i < list.ItemCount; i++ {
		if !nodeContains(list.Item(i), target) {
			continue
		}
		top, bottom := i*list.ItemHeight, (i+1)*list.ItemHeight
		before := list.ScrollOffset
		if top < list.ScrollOffset {
			list.ScrollOffset = top
		} else if bottom > list.ScrollOffset+view {
			list.ScrollOffset = bottom - view
		}
		return list.ScrollOffset != before
	}
	return false
}

func processVirtualList(n *ui.Node) *ui.Node {
	if n == nil {
		return nil
	}
	if n.Kind == ui.KindVirtualList {
		return n
	}
	for _, child := range n.Children {
		if list := processVirtualList(child); list != nil {
			return list
		}
	}
	return nil
}

func nodeContains(root, target *ui.Node) bool {
	if root == nil || target == nil {
		return false
	}
	if root == target {
		return true
	}
	for _, child := range root.Children {
		if nodeContains(child, target) {
			return true
		}
	}
	return false
}

// parseProcessAction reads the detail view's two signal actions: Kill is
// SIGINT, Force kill is SIGKILL, as the reference sends them.
func parseProcessAction(action string) (services.ProcessIdentity, syscall.Signal, bool) {
	if id, ok := parseProcessIdentityAction(action, "process:int"); ok {
		return id, syscall.SIGINT, true
	}
	if id, ok := parseProcessIdentityAction(action, "process:kill"); ok {
		return id, syscall.SIGKILL, true
	}
	return services.ProcessIdentity{}, 0, false
}

func parseProcessIdentityAction(action, prefix string) (services.ProcessIdentity, bool) {
	parts := strings.Split(strings.TrimPrefix(action, prefix+":"), ":")
	if !strings.HasPrefix(action, prefix+":") || len(parts) != 2 {
		return services.ProcessIdentity{}, false
	}
	pid, err := strconv.Atoi(parts[0])
	if err != nil || pid <= 0 {
		return services.ProcessIdentity{}, false
	}
	start, err := strconv.ParseUint(parts[1], 10, 64)
	if err != nil || start == 0 {
		return services.ProcessIdentity{}, false
	}
	return services.ProcessIdentity{PID: pid, StartTimeTicks: start}, true
}

func (h *PanelHost) activateMonitor(r *Registry, n *ui.Node) bool {
	rebuild := func() bool { r.rebuildPanel(h); return true }
	switch a := n.Action; {
	case a == "monitor:close":
		r.closePanelLocked(h.id)
		return true
	case a == "monitor:settings":
		return r.openSettingsAtLocked(h.output, "Monitor")
	case a == "monitor:options":
		h.monitorOptions = !h.monitorOptions
		return rebuild()
	case a == "monitor:clear":
		h.query, h.search = "", ui.NewField("")
		return rebuild()
	case strings.HasPrefix(a, "monitor:page:"):
		page := strings.TrimPrefix(a, "monitor:page:")
		if page != monitorPageProcesses && page != monitorPageMetrics {
			return false
		}
		h.monitorPage = page
		return rebuild()
	case strings.HasPrefix(a, "monitor:owner:"):
		o := strings.TrimPrefix(a, "monitor:owner:")
		if o != "all" && o != "user" && o != "system" {
			return false
		}
		h.processFilter = o
		return rebuild()
	case strings.HasPrefix(a, "monitor:sort:"):
		key := strings.TrimPrefix(a, "monitor:sort:")
		if !validProcessSort(key) {
			return false
		}
		if h.processSort == key {
			h.processDesc = !h.processDesc
		} else {
			h.processSort, h.processDesc = key, key != "name" && key != "user" && key != "pid"
		}
		return rebuild()
	case strings.HasPrefix(a, "monitor:toggle:"):
		key := strings.TrimPrefix(a, "monitor:toggle:")
		if strings.HasPrefix(key, "section:") {
			h.processCollapsed[key] = !h.processCollapsed[key]
		} else {
			h.processExpanded[key] = !h.processExpanded[key]
		}
		return rebuild()
	case a == "monitor:show:apps" || a == "monitor:show:procs":
		c := r.cfg
		if a == "monitor:show:apps" {
			c.Monitor.ShowApps = !c.Monitor.ShowApps
		} else {
			c.Monitor.ShowProcesses = !c.Monitor.ShowProcesses
		}
		if err := r.writeConfig(c); err != nil {
			h.processStatus, h.processStatusErr = "Could not save view options: "+err.Error(), err
		} else {
			// writeConfig only signals the reload; apply the view option now
			// so the toggle and the table agree on this rebuild.
			r.cfg.Monitor = c.Monitor
		}
		return rebuild()
	}
	if n.Action == "monitor:detail:close" {
		h.processSelected = services.ProcessIdentity{}
		return rebuild()
	}
	if identity, ok := parseProcessIdentityAction(n.Action, "monitor:select"); ok {
		h.processSelected = identity
		r.rebuildPanel(h)
		return true
	}
	identity, signal, ok := parseProcessAction(n.Action)
	if !ok {
		return false
	}
	h.processStatus, h.processStatusErr = "", nil
	r.scheduleProcessSignal(h, identity, signal)
	return true
}

func signalProcessDefault(identity services.ProcessIdentity, signal syscall.Signal) error {
	if err := services.ValidateProcessIdentity(identity); err != nil {
		return err
	}
	process, err := os.FindProcess(identity.PID)
	if err != nil {
		return err
	}
	return process.Signal(signal)
}

func (r *Registry) scheduleProcessSignal(h *PanelHost, identity services.ProcessIdentity, signal syscall.Signal) {
	run := r.signalProcess
	go func() {
		err := run(identity, signal)
		r.mu.Lock()
		current := r.panelHosts[PanelMonitor]
		if current != h {
			r.mu.Unlock()
			return
		}
		if err == nil {
			name := "INT"
			if signal == syscall.SIGKILL {
				name = "KILL"
			}
			current.processStatus = fmt.Sprintf("Sent %s to PID %d", name, identity.PID)
			current.processSelected = services.ProcessIdentity{}
			current.processStatusErr = nil
		} else {
			current.processStatus = processSignalError(identity.PID, err)
			current.processStatusErr = err
		}
		r.rebuildPanel(current)
		output := current.output
		r.mu.Unlock()
		r.publishSurface(output, panelSurfaceID(PanelMonitor))
	}()
}

func processSignalError(pid int, err error) string {
	switch {
	case errors.Is(err, services.ErrProcessIdentityChanged):
		return fmt.Sprintf("PID %d now belongs to another process", pid)
	case errors.Is(err, os.ErrNotExist), errors.Is(err, syscall.ESRCH):
		return fmt.Sprintf("PID %d has exited", pid)
	case errors.Is(err, os.ErrPermission), errors.Is(err, syscall.EPERM):
		return fmt.Sprintf("Permission denied for PID %d", pid)
	default:
		return fmt.Sprintf("Could not signal PID %d: %v", pid, err)
	}
}
