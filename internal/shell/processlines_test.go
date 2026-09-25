package shell

import (
	"reflect"
	"testing"

	"github.com/Nomadcxx/sysc-shell/internal/platform/niri"
	"github.com/Nomadcxx/sysc-shell/internal/services"
)

func proc(pid, ppid int, name string, uid uint32, rss uint64, cpu float64) services.Process {
	return services.Process{
		Identity: services.ProcessIdentity{PID: pid, StartTimeTicks: uint64(pid) * 10},
		Name:     name, ParentPID: ppid, UID: uid, UIDValid: true,
		ResidentBytes: rss, ResidentValid: true,
		CPU: services.ProcessCPU{Fraction: cpu, Valid: true},
	}
}

func lineInput(procs []services.Process, apps []runningAppSlot) processLineInput {
	return processLineInput{
		Processes: procs, Apps: apps, CurrentUID: 1000, Owner: "all", Sort: "mem", Desc: true,
		Expanded: map[string]bool{}, Collapsed: map[string]bool{},
		ShowApps: true, ShowProcesses: true,
		Username: func(uid uint32) string { return map[uint32]string{0: "root", 1000: "nomadx"}[uid] },
		AppIcon:  func(string) string { return "" },
	}
}

func lineKeys(lines []processLine) []string {
	out := make([]string, len(lines))
	for i, l := range lines {
		out[i] = l.Key
	}
	return out
}

func findLine(t *testing.T, lines []processLine, key string) processLine {
	t.Helper()
	for _, l := range lines {
		if l.Key == key {
			return l
		}
	}
	t.Fatalf("no line %q in %v", key, lineKeys(lines))
	return processLine{}
}

// A desktop: foot (pid 10) runs zsh (11) which launched firefox (20);
// firefox has two children. systemd (1) is root's.
func desktopFixture() ([]services.Process, []runningAppSlot) {
	procs := []services.Process{
		proc(1, 0, "systemd", 0, 10<<20, 0),
		proc(10, 1, "foot", 1000, 40<<20, .01),
		proc(11, 10, "zsh", 1000, 5<<20, 0),
		proc(20, 11, "firefox", 1000, 900<<20, .05),
		proc(21, 20, "firefox", 1000, 600<<20, .02),
		proc(22, 20, "firefox", 1000, 500<<20, .01),
	}
	apps := []runningAppSlot{
		{Key: "foot", Name: "Foot", Icon: "foot", Members: []niri.Window{{ID: 1, AppID: "foot", Pid: 10}}},
		{Key: "org.mozilla.firefox", Name: "Firefox", Icon: "firefox", Members: []niri.Window{{ID: 2, AppID: "org.mozilla.firefox", Pid: 20}}},
	}
	return procs, apps
}

func TestApplicationsSumTheirWindowTreeAndStopAtAnotherApp(t *testing.T) {
	procs, apps := desktopFixture()
	lines := projectProcessLines(lineInput(procs, apps))
	ff := findLine(t, lines, "app:org.mozilla.firefox")
	if ff.Totals.Resident != 2000<<20 {
		t.Errorf("firefox rss = %d MiB, want 2000", ff.Totals.Resident>>20)
	}
	foot := findLine(t, lines, "app:foot")
	if foot.Totals.Resident != 45<<20 {
		t.Errorf("foot rss = %d MiB, want 45 (foot + zsh, not firefox)", foot.Totals.Resident>>20)
	}
	if !ff.Expandable || ff.Expanded || ff.PIDText != "" || ff.Name != "Firefox" || ff.Icon != "firefox" {
		t.Errorf("firefox app line = %+v", ff)
	}
}

func TestSectionOrderAndGroupingByName(t *testing.T) {
	procs, apps := desktopFixture()
	got := lineKeys(projectProcessLines(lineInput(procs, apps)))
	want := []string{
		"section:apps", "app:org.mozilla.firefox", "app:foot",
		"section:procs", "exe:name:firefox", "pid:10:100", "pid:1:10", "pid:11:110",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("keys =\n%v\nwant\n%v", got, want)
	}
}

func TestSingleMemberGroupIsAPlainRowWithItsPID(t *testing.T) {
	procs, apps := desktopFixture()
	lines := projectProcessLines(lineInput(procs, apps))
	foot := findLine(t, lines, "pid:10:100")
	if foot.Kind != lineProcess || foot.PIDText != "10" || foot.Expandable || foot.User != "nomadx" {
		t.Fatalf("foot row = %+v", foot)
	}
}

func TestExpandingAGroupListsItsMembersSorted(t *testing.T) {
	procs, apps := desktopFixture()
	in := lineInput(procs, apps)
	in.Expanded["exe:name:firefox"] = true
	got := lineKeys(projectProcessLines(in))
	i := indexOf(got, "exe:name:firefox")
	if !reflect.DeepEqual(got[i+1:i+4], []string{"pid:20:200", "pid:21:210", "pid:22:220"}) {
		t.Fatalf("members after the group = %v", got[i+1:])
	}
	if l := findLine(t, projectProcessLines(in), "pid:21:210"); l.Depth != 1 {
		t.Fatalf("member depth = %d", l.Depth)
	}
}

func TestExpansionSurvivesRecycledPIDs(t *testing.T) {
	procs, apps := desktopFixture()
	in := lineInput(procs, apps)
	in.Expanded["exe:name:firefox"] = true
	for i := range in.Processes {
		if in.Processes[i].Name == "firefox" {
			in.Processes[i].Identity.PID += 1000
			in.Processes[i].Identity.StartTimeTicks += 1
		}
	}
	if !findLine(t, projectProcessLines(in), "exe:name:firefox").Expanded {
		t.Fatal("group collapsed when its PIDs changed")
	}
}

func TestOwnerFilter(t *testing.T) {
	procs, apps := desktopFixture()
	in := lineInput(procs, apps)
	in.Owner = "system"
	got := lineKeys(projectProcessLines(in))
	want := []string{"section:procs", "pid:1:10"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("system owner = %v, want %v", got, want)
	}
}

func TestSearchKeepsAGroupWithOnlyTheMatchingMembersCounted(t *testing.T) {
	procs, apps := desktopFixture()
	procs[5].Args = []string{"firefox", "-contentproc", "tab"}
	in := lineInput(procs, apps)
	in.Query = "CONTENTPROC"
	in.ShowApps = false
	lines := projectProcessLines(in)
	if got := lineKeys(lines); !reflect.DeepEqual(got, []string{"section:procs", "pid:22:220"}) {
		t.Fatalf("keys = %v", got)
	}
}

func TestSortKeysAndInvalidLast(t *testing.T) {
	procs := []services.Process{
		proc(3, 1, "c", 1000, 300, .3),
		proc(1, 1, "a", 1000, 100, .1),
		proc(2, 1, "b", 1000, 200, .2),
		{Identity: services.ProcessIdentity{PID: 4, StartTimeTicks: 40}, Name: "d"},
	}
	tests := []struct {
		sort string
		desc bool
		want []string
	}{
		{"mem", true, []string{"pid:3:30", "pid:2:20", "pid:1:10", "pid:4:40"}},
		{"mem", false, []string{"pid:1:10", "pid:2:20", "pid:3:30", "pid:4:40"}},
		{"cpu", true, []string{"pid:3:30", "pid:2:20", "pid:1:10", "pid:4:40"}},
		{"name", false, []string{"pid:1:10", "pid:2:20", "pid:3:30", "pid:4:40"}},
		{"pid", true, []string{"pid:4:40", "pid:3:30", "pid:2:20", "pid:1:10"}},
	}
	for _, tt := range tests {
		in := lineInput(procs, nil)
		in.Sort, in.Desc = tt.sort, tt.desc
		got := lineKeys(projectProcessLines(in))[1:] // drop section:procs
		if !reflect.DeepEqual(got, tt.want) {
			t.Errorf("%s desc=%v = %v, want %v", tt.sort, tt.desc, got, tt.want)
		}
	}
}

func TestSectionsCollapseAndHide(t *testing.T) {
	procs, apps := desktopFixture()
	in := lineInput(procs, apps)
	in.Collapsed["section:apps"] = true
	in.ShowProcesses = false
	got := lineKeys(projectProcessLines(in))
	if !reflect.DeepEqual(got, []string{"section:apps"}) {
		t.Fatalf("keys = %v", got)
	}
	if findLine(t, projectProcessLines(in), "section:apps").Expanded {
		t.Fatal("collapsed section reports expanded")
	}
}

func TestNoWindowsHidesTheApplicationsSection(t *testing.T) {
	procs, _ := desktopFixture()
	for _, k := range lineKeys(projectProcessLines(lineInput(procs, nil))) {
		if k == "section:apps" {
			t.Fatal("applications section shown with no windows")
		}
	}
}

func indexOf(xs []string, x string) int {
	for i, v := range xs {
		if v == x {
			return i
		}
	}
	return -1
}
