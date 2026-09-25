package shell

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/Nomadcxx/sysc-shell/internal/services"
	"github.com/Nomadcxx/sysc-shell/internal/theme"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

// The detail view's value columns: the command line gets the room, the
// byte counts a fixed right column.
const (
	detailWideValueW   = 420
	detailNarrowValueW = 160
)

// processDetailCard replaces the info card while a process is selected, as
// the reference's process view does. It returns nil with no selection.
func processDetailCard(h *PanelHost, in monitorView, snap services.ProcessSnapshot) *ui.Node {
	id := h.processSelected
	if id == (services.ProcessIdentity{}) {
		return nil
	}
	var p services.Process
	found := false
	for _, candidate := range snap.Processes {
		if candidate.Identity == id {
			p, found = candidate, true
			break
		}
	}
	suffix := fmt.Sprintf(":%d:%d", id.PID, id.StartTimeTicks)
	own := found && p.UIDValid && p.UID == in.UID
	action := func(icon, act, name string) *ui.Node {
		n := centreIconButton(icon, act+suffix, name)
		n.Tone = ui.ToneError
		if !own {
			n.AriaDisabled = true
			n.State |= ui.StateDisabled
		}
		return n
	}
	buttons := &ui.Node{Kind: ui.KindColumn, Gap: theme.MarginXS, Children: []*ui.Node{
		action("disabled_by_default", "process:int", "Kill (SIGINT)"),
		action("skull", "process:kill", "Force kill (SIGKILL)"),
		centreIconButton("cancel", "monitor:detail:close", "Close process view"),
	}}
	if !found {
		return detailFrame(buttons, &ui.Node{Kind: ui.KindText, Text: "Process exited", Tone: ui.ToneSubtle}, nil)
	}
	cpu := ccDash
	if p.CPU.Valid {
		cpu = fmt.Sprintf("%.1f%%", p.CPU.Fraction*100)
	}
	mem := ccDash
	if p.ResidentValid {
		mem = formatProcessBytes(p.ResidentBytes)
	}
	swap := ccDash
	if s, ok := processSwap(p); ok {
		swap = formatProcessBytes(s)
	}
	exe := processExecutable(p)
	if exe == "" {
		exe = ccDash
	}
	ioRead, ioWrite := ccDash, ccDash // Task 13 fills these from the per-process I/O rates
	private, shared := ccDash, ccDash // Task 13 fills these from the per-process memory detail
	left := detailPairs([][2]string{
		{"comm:", p.Name}, {"PID:", strconv.Itoa(p.Identity.PID)}, {"PPID:", strconv.Itoa(p.ParentPID)},
		{"exe:", exe}, {"cmdline:", strings.Join(p.Args, " ")}, {"cpu:", cpu},
	}, detailWideValueW)
	right := detailPairs([][2]string{
		{"private:", private}, {"shared:", shared}, {"mem:", mem},
		{"swap:", swap}, {"io read:", ioRead}, {"io write:", ioWrite},
	}, detailNarrowValueW)
	return detailFrame(buttons, left, right)
}

func detailPairs(pairs [][2]string, valueWidth int) *ui.Node {
	col := &ui.Node{Kind: ui.KindColumn, Gap: theme.MarginXXS}
	for _, kv := range pairs {
		value := kv[1]
		if value == "" {
			value = ccDash
		}
		col.Children = append(col.Children, &ui.Node{Kind: ui.KindRow, Gap: theme.MarginS, Children: []*ui.Node{
			{Kind: ui.KindText, Text: kv[0], Tone: ui.ToneSubtle, MinWidthText: "io write:"},
			{Kind: ui.KindText, Text: value, MaxWidth: valueWidth, Tabular: true, Name: kv[0] + " value"},
		}})
	}
	return col
}

func detailFrame(buttons, left, right *ui.Node) *ui.Node {
	children := []*ui.Node{buttons, {Kind: ui.KindSeparator}, left}
	if right != nil {
		children = append(children, &ui.Node{Kind: ui.KindSeparator}, right)
	}
	return &ui.Node{Kind: ui.KindCapsule, Height: monitorInfoH, Padding: theme.MarginL,
		Fill: ui.FillContainerHigh, Shape: ui.ShapeCard, Children: []*ui.Node{
			{Kind: ui.KindRow, Gap: theme.MarginL, Children: children},
		}}
}
