package shell

import (
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Nomadcxx/sysc-shell/internal/config"
	"github.com/Nomadcxx/sysc-shell/internal/platform/niri"
)

func TestRunningAppScopeAppIDsMapsFoldedSteamGames(t *testing.T) {
	t.Parallel()
	slot := runningAppSlot{
		Key: "steam",
		Members: []niri.Window{
			{ID: 80, AppID: "steam"},
			{ID: 81, AppID: "steam_app_123"},
			{ID: 82, AppID: "steam_app_456"},
		},
		SteamGameIDs: map[uint64]struct{}{81: {}, 82: {}},
	}
	if got := runningAppScopeAppIDs(slot); len(got) != 1 || got[0] != "steam" {
		t.Fatalf("scope app IDs = %v, want [steam]", got)
	}
}

func TestAppScopeUnitMatchesExactEscapedIDAndPID(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		unit string
		want bool
	}{
		{"app-niri-steam-545355.scope", true},
		{"app-steam-545355.scope", true},
		{"app-niri-steam-1.scope", true},
		{"app-niri-steam-0.scope", false},
		{"app-niri-steam-x.scope", false},
		{"app-niri-steam-545355.service", false},
		{"app-niri-steamer-545355.scope", false},
		{"app-niri-steam-545355-extra.scope", false},
	} {
		t.Run(tc.unit, func(t *testing.T) {
			if got := appScopeUnitMatchesAppID("steam", tc.unit); got != tc.want {
				t.Fatalf("match(%q) = %t, want %t", tc.unit, got, tc.want)
			}
		})
	}
	if !appScopeUnitMatchesAppID(`brave\x2dorigin\x2dnightly`, `app-niri-brave\x2dorigin\x2dnightly-4009582.scope`) {
		t.Fatal("escaped desktop app ID did not match its Niri scope")
	}
	if !appScopeUnitMatchesAppID("discord", "app-discord-3740209.scope") {
		t.Fatal("plain app scope did not match its exact app ID")
	}
}

func TestAppScopeUnitMatchesOnlyExactWindowPID(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		unit string
		want bool
	}{
		{"app-niri-sh-3740209.scope", true},
		{"app-discord-3740209.scope", false},
		{"app-niri-unrelated-3740210.scope", false},
		{"app-niri-sh-03740209.scope", false},
		{"app-niri-sh-0.scope", false},
		{"app-niri-sh-3740209.service", false},
		{"user-3740209.scope", false},
	} {
		t.Run(tc.unit, func(t *testing.T) {
			if got := appScopeUnitMatchesWindowPID(3740209, tc.unit); got != tc.want {
				t.Fatalf("match(%q) = %t, want %t", tc.unit, got, tc.want)
			}
		})
	}
}

func TestRunningAppsMenuForceQuitStopsScopesByIDAndWindowPID(t *testing.T) {
	t.Parallel()
	reg, bar := steamMenuReg(t)
	reg.UpdateNiri(niri.Snapshot{Windows: []niri.Window{{
		ID: 80, AppID: "discord", Pid: 3740209, Title: "Discord",
	}}})
	if !bar.onAction("running-app:discord", buttonRight) {
		t.Fatal("Discord menu did not open")
	}
	host := reg.runningMenu
	w, height := host.size()
	if err := host.configure(w, height, 120); err != nil {
		t.Fatal(err)
	}
	stopped := make(chan string, 2)
	reg.runArgvOutput = func(argv []string) (string, error) {
		switch {
		case argv[0] == "systemd-escape":
			return "discord\n", nil
		case argv[0] == "systemctl" && argv[2] == "list-units":
			return strings.Join([]string{
				"app-discord-3740209.scope loaded active running app-discord-3740209.scope",
				"app-niri-sh-3740209.scope loaded active running app-niri-sh-3740209.scope",
				"app-niri-unrelated-3740210.scope loaded active running app-niri-unrelated-3740210.scope",
			}, "\n"), nil
		case argv[0] == "systemctl" && argv[2] == "show":
			unit := argv[len(argv)-1]
			uid := os.Getuid()
			return fmt.Sprintf("Id=%s\nLoadState=loaded\nActiveState=active\nKillMode=control-group\nControlGroup=/user.slice/user-%d.slice/user@%d.service/app.slice/%s\n",
				unit, uid, uid, unit), nil
		default:
			return "", fmt.Errorf("unexpected command: %v", argv)
		}
	}
	reg.forceQuitRunningAppScope = func(unit string) error {
		stopped <- unit
		return nil
	}

	forceQuit := findAction(host.root, "running-app-force-quit")
	if forceQuit == nil {
		t.Fatal("app-level force-quit row missing")
	}
	host.choose(host.hitRows[forceQuit.Action])
	confirm := findAction(host.root, "running-app-force-quit-confirm")
	if confirm == nil {
		t.Fatal("force-quit confirmation action missing")
	}
	host.choose(host.hitRows[confirm.Action])

	got := map[string]bool{}
	for range 2 {
		select {
		case unit := <-stopped:
			got[unit] = true
		case <-time.After(time.Second):
			t.Fatalf("killed scopes = %v, want app and launch scopes", got)
		}
	}
	for _, unit := range []string{"app-discord-3740209.scope", "app-niri-sh-3740209.scope"} {
		if !got[unit] {
			t.Errorf("scope %q was not stopped; got %v", unit, got)
		}
	}
}

func TestNiriAppScopeValidationRequiresActiveControlGroupInUserAppSlice(t *testing.T) {
	t.Parallel()
	uid := os.Getuid()
	unit := "app-niri-steam-545355.scope"
	valid := niriAppScopeProperties{
		ID: unit, LoadState: "loaded", ActiveState: "active", KillMode: "control-group",
		ControlGroup: fmt.Sprintf("/user.slice/user-%d.slice/user@%d.service/app.slice/%s", uid, uid, unit),
	}
	if err := validateNiriAppScopeProperties(uid, unit, valid); err != nil {
		t.Fatalf("valid scope rejected: %v", err)
	}
	for _, mutate := range []struct {
		name string
		edit func(*niriAppScopeProperties)
	}{
		{"wrong ID", func(p *niriAppScopeProperties) { p.ID = "app-niri-steamer-545355.scope" }},
		{"not loaded", func(p *niriAppScopeProperties) { p.LoadState = "not-found" }},
		{"inactive", func(p *niriAppScopeProperties) { p.ActiveState = "inactive" }},
		{"mixed kill mode", func(p *niriAppScopeProperties) { p.KillMode = "mixed" }},
		{"outside app slice", func(p *niriAppScopeProperties) {
			p.ControlGroup = "/user.slice/user-1000.slice/user@1000.service/session.slice/other.scope"
		}},
	} {
		t.Run(mutate.name, func(t *testing.T) {
			props := valid
			mutate.edit(&props)
			if err := validateNiriAppScopeProperties(uid, unit, props); err == nil {
				t.Fatal("unsafe scope properties were accepted")
			}
		})
	}
}

func TestForceQuitRunningAppScopesValidatesAllExactTargetsBeforeKilling(t *testing.T) {
	t.Parallel()
	reg := NewRegistry(config.Default())
	t.Cleanup(reg.Close)
	reg.runArgvOutput = func(argv []string) (string, error) {
		switch {
		case argv[0] == "systemd-escape":
			return "steam\n", nil
		case argv[0] == "systemctl" && argv[2] == "list-units":
			return strings.Join([]string{
				"app-niri-steam-545355.scope loaded active running app-niri-steam-545355.scope",
				"app-niri-steam-545356.scope loaded active running app-niri-steam-545356.scope",
				"app-niri-steamer-545357.scope loaded active running app-niri-steamer-545357.scope",
				"app-niri-steam-545358.service loaded active running app-niri-steam-545358.service",
			}, "\n"), nil
		case argv[0] == "systemctl" && argv[2] == "show":
			unit := argv[len(argv)-1]
			killMode := "control-group"
			if strings.Contains(unit, "545356") {
				killMode = "mixed"
			}
			uid := os.Getuid()
			return fmt.Sprintf("Id=%s\nLoadState=loaded\nActiveState=active\nKillMode=%s\nControlGroup=/user.slice/user-%d.slice/user@%d.service/app.slice/%s\n",
				unit, killMode, uid, uid, unit), nil
		default:
			return "", fmt.Errorf("unexpected command: %v", argv)
		}
	}
	var stopped []string
	reg.forceQuitRunningAppScope = func(unit string) error {
		stopped = append(stopped, unit)
		return nil
	}
	if err := reg.forceQuitRunningAppScopes([]string{"steam"}, nil); err == nil {
		t.Fatal("mixed KillMode scope did not reject the whole operation")
	}
	if len(stopped) != 0 {
		t.Fatalf("killed scopes = %v, want none when any target is unsafe", stopped)
	}
}

func TestForceQuitRunningAppScopesTargetsOnlyValidatedExactScope(t *testing.T) {
	t.Parallel()
	reg := NewRegistry(config.Default())
	t.Cleanup(reg.Close)
	reg.runArgvOutput = func(argv []string) (string, error) {
		switch {
		case argv[0] == "systemd-escape":
			return "steam\n", nil
		case argv[0] == "systemctl" && argv[2] == "list-units":
			return "app-niri-steam-545355.scope loaded active running app-niri-steam-545355.scope\napp-niri-steamer-545356.scope loaded active running app-niri-steamer-545356.scope\n", nil
		case argv[0] == "systemctl" && argv[2] == "show":
			unit := argv[len(argv)-1]
			uid := os.Getuid()
			return fmt.Sprintf("Id=%s\nLoadState=loaded\nActiveState=active\nKillMode=control-group\nControlGroup=/user.slice/user-%d.slice/user@%d.service/app.slice/%s\n",
				unit, uid, uid, unit), nil
		default:
			return "", fmt.Errorf("unexpected command: %v", argv)
		}
	}
	var stopped []string
	reg.forceQuitRunningAppScope = func(unit string) error {
		stopped = append(stopped, unit)
		return nil
	}
	if err := reg.forceQuitRunningAppScopes([]string{"steam"}, nil); err != nil {
		t.Fatal(err)
	}
	if len(stopped) != 1 || stopped[0] != "app-niri-steam-545355.scope" {
		t.Fatalf("killed scopes = %v, want only exact Steam app scope", stopped)
	}
}

func installRunningAppScopeTestCommands(r *Registry, stop func(string) error) {
	r.runArgvOutput = func(argv []string) (string, error) {
		switch {
		case argv[0] == "systemd-escape":
			return argv[len(argv)-1] + "\n", nil
		case argv[0] == "systemctl" && argv[2] == "list-units":
			unit := "app-niri-steam-545355.scope"
			return unit + " loaded active running " + unit + "\n", nil
		case argv[0] == "systemctl" && argv[2] == "show":
			unit := argv[len(argv)-1]
			uid := os.Getuid()
			return fmt.Sprintf("Id=%s\nLoadState=loaded\nActiveState=active\nKillMode=control-group\nControlGroup=/user.slice/user-%d.slice/user@%d.service/app.slice/%s\n",
				unit, uid, uid, unit), nil
		default:
			return "", fmt.Errorf("unexpected command: %v", argv)
		}
	}
	r.forceQuitRunningAppScope = stop
}

func TestAppScopeUnitFromProcCgroup(t *testing.T) {
	t.Parallel()
	uid := os.Getuid()
	appSlice := fmt.Sprintf("/user.slice/user-%d.slice/user@%d.service/app.slice/", uid, uid)
	for _, tc := range []struct {
		name string
		line string
		want string
	}{
		{"Steam launcher scope", "0::" + appSlice + "app-niri-steam-3743000.scope", "app-niri-steam-3743000.scope"},
		{"legacy app scope", "0::" + appSlice + "app-discord-3431535.scope", "app-discord-3431535.scope"},
		{"wrong slice", fmt.Sprintf("0::/user.slice/user-%d.slice/user@%d.service/session.slice/app-niri-steam-3743000.scope", uid+1, uid+1), ""},
		{"nested cgroup", "0::" + appSlice + "app-niri-steam-3743000.scope/child.scope", ""},
		{"non-app scope", "0::" + appSlice + "session-1.scope", ""},
		{"malformed PID suffix", "0::" + appSlice + "app-niri-steam-x.scope", ""},
		{"zero PID suffix", "0::" + appSlice + "app-niri-steam-0.scope", ""},
		{"service unit", "0::" + appSlice + "app-niri-steam-3743000.service", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := appScopeUnitFromProcCgroup(uid, tc.line+"\n")
			if tc.want == "" {
				if ok {
					t.Fatalf("unit = %q, want no scope", got)
				}
				return
			}
			if !ok || got != tc.want {
				t.Fatalf("unit = %q, ok = %t, want %q", got, ok, tc.want)
			}
		})
	}
}

func TestForceQuitRunningAppScopesFindsContainingProcessCgroup(t *testing.T) {
	t.Parallel()
	reg := NewRegistry(config.Default())
	t.Cleanup(reg.Close)
	uid := os.Getuid()
	reg.readProcessCgroup = func(pid int) (string, error) {
		if pid != 3746511 {
			t.Fatalf("read cgroup for PID %d, want Steam client PID 3746511", pid)
		}
		return fmt.Sprintf("0::/user.slice/user-%d.slice/user@%d.service/app.slice/app-niri-steam-3743000.scope\n", uid, uid), nil
	}
	reg.runArgvOutput = func(argv []string) (string, error) {
		switch {
		case argv[0] == "systemd-escape":
			return "xwayland-mismatched-app-id\n", nil
		case argv[0] == "systemctl" && argv[2] == "list-units":
			return "app-niri-steam-3743000.scope loaded active running app-niri-steam-3743000.scope\n", nil
		case argv[0] == "systemctl" && argv[2] == "show":
			unit := argv[len(argv)-1]
			return fmt.Sprintf("Id=%s\nLoadState=loaded\nActiveState=active\nKillMode=control-group\nControlGroup=/user.slice/user-%d.slice/user@%d.service/app.slice/%s\n",
				unit, uid, uid, unit), nil
		default:
			return "", fmt.Errorf("unexpected command: %v", argv)
		}
	}
	var stopped []string
	reg.forceQuitRunningAppScope = func(unit string) error {
		stopped = append(stopped, unit)
		return nil
	}

	if err := reg.forceQuitRunningAppScopes([]string{"xwayland-mismatched-app-id"}, []int{3746511}); err != nil {
		t.Fatal(err)
	}
	if len(stopped) != 1 || stopped[0] != "app-niri-steam-3743000.scope" {
		t.Fatalf("killed scopes = %v, want containing Steam scope only", stopped)
	}
}
