package shell

import (
	"cmp"
	"errors"
	"fmt"
	"os"
	"slices"
	"strconv"
	"strings"
	"syscall"

	"github.com/Nomadcxx/sysc-shell/internal/services"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

const (
	monitorPageProcesses = "processes"
	monitorPageMetrics   = "monitor"
	processRowHeight     = 26
	processRowPitch      = 32
	processHeaderHeight  = 22
	monitorControlHeight = 28
	processTablePadding  = 6
)

func monitorPanelTree(h *PanelHost, sels []services.Selector, snap services.Snapshot, history map[services.Selector][]float64, facts machineFacts) *ui.Node {
	if h.monitorPage == "" {
		h.monitorPage = monitorPageProcesses
	}
	if h.monitorPage == monitorPageMetrics {
		bodyH := max(h.place.Panel.H-2*h.metrics().PanelPadding-monitorControlHeight-12, processRowHeight)
		body := monitorTree(h.metrics(), sels, snap, history, facts)
		body.Padding = 0
		return &ui.Node{Kind: ui.KindColumn, Gap: 12, Padding: h.metrics().PanelPadding, Children: []*ui.Node{
			monitorPageSwitcher(h),
			{Kind: ui.KindScroll, Height: bodyH, Children: []*ui.Node{body}},
		}}
	}
	var processes services.ProcessSnapshot
	if snap.Processes != nil {
		processes = *snap.Processes
	}
	return processMonitorTree(h, processes, uint32(os.Getuid()))
}

func monitorPageSwitcher(h *PanelHost) *ui.Node {
	segment := func(page, label string) *ui.Node {
		n := &ui.Node{
			Kind: ui.KindButton, Action: "monitor:page:" + page, Name: label, Role: "tab",
			Focusable: true, Height: monitorControlHeight, Fill: ui.FillOutline,
			Children: []*ui.Node{{Kind: ui.KindText, Text: label}},
		}
		if h.monitorPage == page || h.monitorPage == "" && page == monitorPageProcesses {
			n.State |= ui.StateSelected
		}
		return n
	}
	return &ui.Node{Kind: ui.KindSegmented, Key: "monitor-page", Height: monitorControlHeight,
		Children: []*ui.Node{
			segment(monitorPageProcesses, "System Processes"),
			segment(monitorPageMetrics, "System Monitor"),
		}}
}

func processMonitorTree(h *PanelHost, snapshot services.ProcessSnapshot, currentUID uint32) *ui.Node {
	if h.search == nil {
		h.search = ui.NewField("")
	}
	if h.processFilter == "" {
		h.processFilter = "all"
	}
	if h.processSort == "" {
		h.processSort, h.processDesc = "cpu", true
	}
	field := h.search.Node("Search")
	field.Width, field.Height = 300, monitorControlHeight
	filters := processFilterSwitcher(h)
	filters.Width = 264
	tools := &ui.Node{Kind: ui.KindRow, Gap: 12, Height: monitorControlHeight, PinEnd: true,
		Children: []*ui.Node{field, filters}}
	processes := projectProcesses(snapshot.Processes, h.query, h.processFilter, h.processSort, h.processDesc, currentUID)
	header := processHeader(h)

	used := 2*h.metrics().PanelPadding + monitorControlHeight + 12 + monitorControlHeight + 8 + processHeaderHeight + 8
	children := []*ui.Node{monitorPageSwitcher(h), tools, header}
	if h.processStatus != "" {
		tone := ui.ToneNormal
		if h.processStatusErr != nil {
			tone = ui.ToneError
		}
		children = append(children, &ui.Node{Kind: ui.KindText, Text: h.processStatus, Tone: tone, Height: 24})
		used += 24 + 8
	} else if len(snapshot.Issues) > 0 {
		children = append(children, &ui.Node{
			Kind: ui.KindText, Text: fmt.Sprintf("%d processes could not be read", len(snapshot.Issues)),
			Tone: ui.ToneError, Height: 24,
		})
		used += 24 + 8
	}
	rows := make([]*ui.Node, len(processes))
	tableHeight := max(h.place.Panel.H-used, processRowPitch+2*processTablePadding)
	list := &ui.Node{
		Kind: ui.KindVirtualList, Height: tableHeight - 2*processTablePadding,
		ItemCount: len(processes), ItemHeight: processRowPitch,
		Item: func(i int) *ui.Node {
			if i < 0 || i >= len(processes) {
				return nil
			}
			if rows[i] == nil {
				rows[i] = processRow(h, processes[i])
			}
			return rows[i]
		},
	}
	children = append(children, &ui.Node{
		Kind: ui.KindCapsule, Height: tableHeight, Padding: processTablePadding,
		Fill: ui.FillContainerHigh, Shape: ui.ShapeCard, Children: []*ui.Node{list},
	})
	return &ui.Node{Kind: ui.KindColumn, Gap: 8, Padding: h.metrics().PanelPadding, Children: children}
}

func processFilterSwitcher(h *PanelHost) *ui.Node {
	segments := make([]*ui.Node, 0, 3)
	for _, filter := range []struct{ id, label string }{{"all", "All"}, {"user", "User"}, {"system", "System"}} {
		n := &ui.Node{
			Kind: ui.KindButton, Action: "monitor:filter:" + filter.id, Name: filter.label,
			Role: "tab", Focusable: true, Height: monitorControlHeight,
			Children: []*ui.Node{{Kind: ui.KindText, Text: filter.label}},
		}
		if h.processFilter == filter.id {
			n.State |= ui.StateSelected
		}
		segments = append(segments, n)
	}
	return &ui.Node{Kind: ui.KindSegmented, Key: "process-filter",
		Height: monitorControlHeight, Children: segments}
}

func processColumns(h *PanelHost) (name, cpu, memory, pid, action int) {
	action = 48
	cpu, memory, pid = 84, 112, 72
	// ponytail: fixed columns keep compositor downsizing safe; widen Name only
	// when the table gains a measured-column layout.
	name = 150
	return
}

func processHeader(h *PanelHost) *ui.Node {
	nameW, cpuW, memoryW, pidW, actionW := processColumns(h)
	button := func(key, label string, width int) *ui.Node {
		if key == "" {
			return &ui.Node{Kind: ui.KindText, Text: label, Width: width}
		}
		if h.processSort == key {
			if h.processDesc {
				label += " ↓"
			} else {
				label += " ↑"
			}
		}
		return &ui.Node{Kind: ui.KindText, Text: label, Action: "monitor:sort:" + key,
			Name: "Sort by " + label, Role: "button", Focusable: true, CenterX: true,
			Width: width, Height: processHeaderHeight}
	}
	return &ui.Node{Kind: ui.KindRow, Gap: 8, Height: processHeaderHeight, Children: []*ui.Node{
		button("name", "Name", nameW), button("cpu", "CPU", cpuW),
		button("memory", "Memory", memoryW), button("pid", "PID", pidW),
		button("", "", actionW),
	}}
}

func processRow(h *PanelHost, process services.Process) *ui.Node {
	nameW, cpuW, memoryW, pidW, actionW := processColumns(h)
	cell := func(text string, width int, tabular bool) *ui.Node {
		return &ui.Node{Kind: ui.KindColumn, Width: width, Children: []*ui.Node{
			{Kind: ui.KindText, Text: text, MaxWidth: width, Tabular: tabular},
		}}
	}
	cpu, memory := "—", "—"
	if process.CPU.Valid {
		cpu = fmt.Sprintf("%.1f%%", process.CPU.Fraction*100)
	}
	if process.ResidentValid {
		memory = formatBytes(float64(process.ResidentBytes))
	}
	identity := fmt.Sprintf(":%d:%d", process.Identity.PID, process.Identity.StartTimeTicks)
	data := &ui.Node{
		Kind: ui.KindRow, Gap: 8, Height: 22, Children: []*ui.Node{
			cell(process.Name, nameW, false), cell(cpu, cpuW, true), cell(memory, memoryW, true),
			cell(strconv.Itoa(process.Identity.PID), pidW, true),
		},
	}
	kill := &ui.Node{
		Kind: ui.KindButton, Text: "Kill", Action: "process:term" + identity,
		Name: fmt.Sprintf("Kill %s", process.Name), Role: "button", Focusable: true,
		Width: actionW, Height: 22, Padding: 4, Fill: ui.FillOutline, Tone: ui.ToneError,
	}
	row := &ui.Node{
		Kind: ui.KindRow, Height: processRowHeight, Padding: 2, Gap: 8,
		Action: "monitor:select" + identity, Name: fmt.Sprintf("Select %s", process.Name),
		Role: "row", Focusable: true, Children: []*ui.Node{data, kill},
	}
	if h.processSelected == process.Identity {
		row.Fill = ui.FillSoft
	}
	return row
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

func projectProcesses(processes []services.Process, query, filter, sortKey string, desc bool, currentUID uint32) []services.Process {
	query = strings.ToLower(strings.TrimSpace(query))
	out := make([]services.Process, 0, len(processes))
	for _, process := range processes {
		switch filter {
		case "user":
			if !process.UIDValid || process.UID != currentUID {
				continue
			}
		case "system":
			if !process.UIDValid || process.UID == currentUID {
				continue
			}
		}
		if query != "" {
			haystack := strings.ToLower(process.Name + "\x00" + strings.Join(process.Args, "\x00"))
			if !strings.Contains(haystack, query) {
				continue
			}
		}
		out = append(out, process)
	}
	slices.SortStableFunc(out, func(a, b services.Process) int {
		var order int
		switch sortKey {
		case "name":
			order = cmp.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name))
		case "cpu":
			if a.CPU.Valid != b.CPU.Valid {
				if a.CPU.Valid {
					return -1
				}
				return 1
			}
			order = cmp.Compare(a.CPU.Fraction, b.CPU.Fraction)
		case "memory":
			if a.ResidentValid != b.ResidentValid {
				if a.ResidentValid {
					return -1
				}
				return 1
			}
			order = cmp.Compare(a.ResidentBytes, b.ResidentBytes)
		default:
			order = cmp.Compare(a.Identity.PID, b.Identity.PID)
		}
		if desc {
			return -order
		}
		return order
	})
	return out
}

func parseProcessAction(action string) (services.ProcessIdentity, syscall.Signal, bool) {
	identity, ok := parseProcessIdentityAction(action, "process:term")
	return identity, syscall.SIGTERM, ok
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
	switch {
	case strings.HasPrefix(n.Action, "monitor:page:"):
		page := strings.TrimPrefix(n.Action, "monitor:page:")
		if page != monitorPageProcesses && page != monitorPageMetrics {
			return false
		}
		h.monitorPage = page
		r.rebuildPanel(h)
		return true
	case strings.HasPrefix(n.Action, "monitor:filter:"):
		filter := strings.TrimPrefix(n.Action, "monitor:filter:")
		if filter != "all" && filter != "user" && filter != "system" {
			return false
		}
		h.processFilter = filter
		r.rebuildPanel(h)
		return true
	case strings.HasPrefix(n.Action, "monitor:sort:"):
		key := strings.TrimPrefix(n.Action, "monitor:sort:")
		if key != "name" && key != "cpu" && key != "memory" && key != "pid" {
			return false
		}
		if h.processSort == key {
			h.processDesc = !h.processDesc
		} else {
			h.processSort = key
			h.processDesc = key == "cpu" || key == "memory"
		}
		r.rebuildPanel(h)
		return true
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
			current.processStatus = fmt.Sprintf("Sent TERM to PID %d", identity.PID)
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
