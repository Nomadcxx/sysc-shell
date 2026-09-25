package shell

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/Nomadcxx/sysc-shell/internal/render"
	"github.com/Nomadcxx/sysc-shell/internal/services"
	"github.com/Nomadcxx/sysc-shell/internal/theme"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

const (
	// monitorCardGap is a step on the shared spacing scale, not a component
	// dimension, so it does not vary by density. The card inset and the panel
	// inset do, and both come from the metrics row.
	monitorCardGap = 8
)

// selectGPU returns a stable selector for one GPU in a snapshot. A selector
// must identify one device because Snapshot.Fraction otherwise takes the first
// matching entry. Multiple devices without unique PCI identities are therefore
// unavailable rather than an arbitrary list position.
func selectGPU(snap services.Snapshot) (services.Selector, bool) {
	selector := services.Selector{Source: services.SourceGPU}
	if snap.GPU == nil || len(snap.GPU.GPUs) == 0 {
		return selector, false
	}

	indices := make([]int, len(snap.GPU.GPUs))
	for i := range indices {
		indices[i] = i
	}
	if len(indices) == 1 {
		selector.Subject = snap.GPU.GPUs[indices[0]].PCIID
		return selector, true
	}
	seen := make(map[string]struct{}, len(indices))
	for _, gpu := range snap.GPU.GPUs {
		if gpu.PCIID == "" {
			continue
		}
		if _, duplicate := seen[gpu.PCIID]; duplicate {
			return selector, false
		}
		seen[gpu.PCIID] = struct{}{}
	}
	sort.SliceStable(indices, func(i, j int) bool {
		left, right := snap.GPU.GPUs[indices[i]].PCIID, snap.GPU.GPUs[indices[j]].PCIID
		switch {
		case left == "" && right != "":
			return false
		case left != "" && right == "":
			return true
		case left != right:
			return left < right
		default:
			return snap.GPU.GPUs[indices[i]].Name < snap.GPU.GPUs[indices[j]].Name
		}
	})
	first := snap.GPU.GPUs[indices[0]]
	if first.PCIID == "" {
		return selector, false
	}
	selector.Subject = first.PCIID
	return selector, true
}

// machineFacts is the monitor's info card: the five identity rows the
// reference draws beside the distro logo. Logo is an icon-theme name from
// os-release; the letter tile stands in when it is empty or unresolvable.
// Uptime is the short form the Control Centre's Home identity shows;
// UptimeLong is the monitor's always-three-units form.
type machineFacts struct {
	Distro, Kernel, CPU, Board string
	Uptime, UptimeLong         string
	Logo, LogoLetter           string
}

// readMachineFacts is a one-shot identity read. Uptime is the only field
// that moves, and /proc/uptime is cheap enough to reread when the panel
// rebuilds rather than joining the leased sample loop.
func readMachineFacts() machineFacts {
	cpu, _ := os.ReadFile("/proc/cpuinfo")
	osrel, _ := os.ReadFile("/etc/os-release")
	release, _ := os.ReadFile("/proc/sys/kernel/osrelease")
	vendor, _ := os.ReadFile("/sys/class/dmi/id/board_vendor")
	board, _ := os.ReadFile("/sys/class/dmi/id/board_name")
	facts := machineFacts{
		Distro: parseOSRelease(string(osrel)),
		Kernel: strings.TrimSpace(string(release)),
		CPU:    parseCPUModel(string(cpu)),
		Board:  strings.TrimSpace(strings.TrimSpace(string(vendor)) + " " + strings.TrimSpace(string(board))),
		Logo:   parseOSReleaseField(string(osrel), "LOGO"),
	}
	for _, r := range parseOSReleaseField(string(osrel), "NAME") {
		facts.LogoLetter = strings.ToUpper(string(r))
		break
	}
	if d, ok := services.ReadUptime(); ok {
		facts.Uptime, facts.UptimeLong = formatUptime(d), formatUptimeLong(d)
	}
	return facts
}

func parseOSReleaseField(text, key string) string {
	for _, line := range strings.Split(text, "\n") {
		k, v, ok := strings.Cut(line, "=")
		if ok && strings.TrimSpace(k) == key {
			return strings.Trim(strings.TrimSpace(v), `"`)
		}
	}
	return ""
}

// formatUptimeLong always names days, hours and minutes, as the reference's
// info card does: "0 days 9 hours 25 minutes".
func formatUptimeLong(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	days := int64(d / (24 * time.Hour))
	hours := int64(d % (24 * time.Hour) / time.Hour)
	mins := int64(d % time.Hour / time.Minute)
	return countUnit(days, "day", "days") + " " + countUnit(hours, "hour", "hours") + " " + countUnit(mins, "minute", "minutes")
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

// monitorCard wraps content in the same capsule the bar uses, so a panel card
// and a bar widget are visibly the same surface.
// monitorCard is a panel card: a capsule that declares the high container role
// and keeps the theme's card radius rather than clamping to a stadium.
func monitorCard(m theme.Metrics, rows []*ui.Node) *ui.Node {
	return &ui.Node{
		Kind: ui.KindCapsule, Padding: m.CardPadding, Fill: ui.FillContainerHigh,
		Shape:    ui.ShapeCard,
		Children: []*ui.Node{{Kind: ui.KindColumn, Gap: theme.MarginXS, Children: rows}},
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
	return &ui.Node{Kind: ui.KindText, Text: text, TextRole: theme.RoleTitle, Name: label, Role: "heading"}
}

// monitorKeyValue is one labelled figure. The value is pinned to the trailing
// edge at layout, matching the reference's right-aligned facts.
func monitorKeyValue(label, value string) *ui.Node {
	return &ui.Node{Kind: ui.KindRow, Gap: theme.MarginS, Children: []*ui.Node{
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
