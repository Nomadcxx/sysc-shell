package shell

import (
	"testing"
	"time"

	metrics "github.com/Nomadcxx/sysc-metrics"

	"github.com/Nomadcxx/sysc-shell/internal/config"
	"github.com/Nomadcxx/sysc-shell/internal/services"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

func TestFormatUptimeLongAlwaysNamesThreeUnits(t *testing.T) {
	cases := map[time.Duration]string{
		9*time.Hour + 25*time.Minute: "0 days 9 hours 25 minutes",
		26*time.Hour + time.Minute:   "1 day 2 hours 1 minute",
		0:                            "0 days 0 hours 0 minutes",
	}
	for d, want := range cases {
		if got := formatUptimeLong(d); got != want {
			t.Errorf("formatUptimeLong(%v) = %q, want %q", d, got, want)
		}
	}
}

func TestParseOSReleaseField(t *testing.T) {
	text := "NAME=\"Arch Linux\"\nPRETTY_NAME=\"Arch Linux\"\nLOGO=archlinux-logo\n"
	if got := parseOSReleaseField(text, "LOGO"); got != "archlinux-logo" {
		t.Fatalf("LOGO = %q", got)
	}
	if got := parseOSReleaseField(text, "NAME"); got != "Arch Linux" {
		t.Fatalf("NAME = %q", got)
	}
}

func testMonitorView() monitorView {
	return monitorView{
		Facts: machineFacts{Distro: "Arch Linux", Kernel: "7.2.6-arch2-1", CPU: "AMD Ryzen 5 5600X",
			Board: "Gigabyte Technology Co., Ltd. B550M DS3H AC", UptimeLong: "0 days 9 hours 25 minutes",
			Logo: "archlinux-logo", LogoLetter: "A"},
		Config:   config.Default().Monitor,
		Username: func(uint32) string { return "nomadx" },
		AppIcon:  func(string) string { return "" },
		Snap: services.Snapshot{
			CPU: &metrics.CPUSnapshot{Usage: metrics.CPUUsage{Fraction: .04, Valid: true}},
		},
	}
}

func TestInfoCardShowsFiveFactsAndTwoGauges(t *testing.T) {
	card := monitorInfoCard(testMonitorView(), monitorFactsColumn(testMonitorView().Facts))
	for _, text := range []string{"Arch Linux", "7.2.6-arch2-1", "AMD Ryzen 5 5600X",
		"Gigabyte Technology Co., Ltd. B550M DS3H AC", "0 days 9 hours 25 minutes"} {
		if !treeHasNameOrText(card, text) {
			t.Errorf("info card missing %q", text)
		}
	}
	gauges := 0
	walkNodes(card, func(n *ui.Node) {
		if n.Kind == ui.KindRadialGauge {
			gauges++
		}
	})
	if gauges != 2 {
		t.Fatalf("gauges = %d, want 2", gauges)
	}
	if !treeHasNameOrText(card, "A") {
		t.Fatal("no logo letter tile without an icon image")
	}
	cpu := findKind(findByName(card, "CPU gauge"), ui.KindRadialGauge)
	if cpu == nil || cpu.ValueText != "4%" || cpu.Absent {
		t.Fatalf("CPU gauge = %+v, want 4%%", cpu)
	}
	if mem := findKind(findByName(card, "Memory gauge"), ui.KindRadialGauge); mem == nil || !mem.Absent {
		t.Fatalf("memory gauge without a sample = %+v, want absent", mem)
	}
}

func TestHeaderCarriesPagePillsAndProcessControlsOnlyOnProcesses(t *testing.T) {
	h := &PanelHost{search: ui.NewField(""), monitorPage: monitorPageProcesses}
	head := monitorHeader(h, monitorPageProcesses)
	for _, name := range []string{"Processes", "System", "Search", "Clear search", "View options", "Monitor settings", "Close"} {
		if findByName(head, name) == nil && !treeHasNameOrText(head, name) {
			t.Errorf("processes header missing %q", name)
		}
	}
	sys := monitorHeader(h, monitorPageMetrics)
	for _, name := range []string{"Search", "Clear search", "View options"} {
		if findByName(sys, name) != nil {
			t.Errorf("system header has %q", name)
		}
	}
}

func TestOptionsColumnCarriesTheSectionTogglesAndOwnerFilter(t *testing.T) {
	h := &PanelHost{processFilter: "user"}
	v := testMonitorView()
	v.Config.ShowApps = false
	col := monitorOptionsColumn(h, v)
	apps := findAction(col, "monitor:show:apps")
	if apps == nil || apps.Value != 0 {
		t.Fatalf("show apps toggle = %+v", apps)
	}
	if procs := findAction(col, "monitor:show:procs"); procs == nil || procs.Value == 0 {
		t.Fatalf("show processes toggle = %+v", procs)
	}
	if user := findAction(col, "monitor:owner:user"); user == nil || user.State&ui.StateSelected == 0 {
		t.Fatal("owner User is not selected")
	}
}
