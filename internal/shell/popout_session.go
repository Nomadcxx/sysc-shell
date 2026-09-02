package shell

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"

	metrics "github.com/Nomadcxx/sysc-metrics"

	"github.com/Nomadcxx/sysc-shell/internal/render"
	"github.com/Nomadcxx/sysc-shell/internal/services"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

func sessionTree(h *PanelHost, snap services.Snapshot, locker string) *ui.Node {
	children := make([]*ui.Node, 0, 4)
	if h.errLabel != "" {
		children = append(children, &ui.Node{Kind: ui.KindText, Text: h.errLabel, Tone: ui.ToneError})
	}
	if card := sessionBatteryCard(snap.Battery); card != nil {
		children = append(children, card)
	}
	if h.profilesOK && len(h.profiles) > 0 {
		children = append(children, sessionProfileCard(h))
	}
	children = append(children, sessionActionsCard(locker))
	return &ui.Node{Kind: ui.KindColumn, Gap: monitorCardGap, Padding: monitorPanelPadding, Children: children}
}

func sessionBatteryCard(b *metrics.BatterySnapshot) *ui.Node {
	if b == nil || !b.Present || !b.ChargeValid {
		return nil
	}
	charging := b.State == metrics.BatteryCharging || b.State == metrics.BatteryFull
	glyph := string(render.BatteryIconRune(b.Charge, charging, false))
	rows := []*ui.Node{
		monitorCardTitle("Battery", 0),
		{Kind: ui.KindRow, Gap: 8, Children: []*ui.Node{
			{Kind: ui.KindText, Text: glyph},
			{Kind: ui.KindText, Text: fmt.Sprintf("%.0f%%", b.Charge*100), Tabular: true},
		}},
		{Kind: ui.KindMeter, Value: b.Charge, Max: 1},
		{Kind: ui.KindText, Text: batteryStateLabel(b.State)},
	}
	if b.TimeValid {
		if d := batteryDuration(b.TimeRemaining); d != "" {
			rows = append(rows, &ui.Node{Kind: ui.KindText, Text: d + " remaining"})
		}
	}
	if b.RateValid {
		rows = append(rows, &ui.Node{Kind: ui.KindText, Text: fmt.Sprintf("%.1f W", b.RateWatts), Tabular: true})
	}
	return monitorCard(rows)
}

func batteryStateLabel(s metrics.BatteryState) string {
	switch s {
	case metrics.BatteryCharging:
		return "Charging"
	case metrics.BatteryDischarging:
		return "Discharging"
	case metrics.BatteryFull:
		return "Full"
	default:
		return "Unknown"
	}
}

func sessionProfileCard(h *PanelHost) *ui.Node {
	tabs := make([]*ui.Node, 0, len(h.profiles))
	for _, name := range h.profiles {
		tabs = append(tabs, &ui.Node{
			Kind: ui.KindButton, Text: powerProfileLabel(name), Action: "profile:" + name,
			Name: powerProfileLabel(name), Role: "tab", Focusable: true, Bold: name == h.profileActive,
		})
	}
	return monitorCard([]*ui.Node{
		monitorCardTitle("Power profile", 0),
		{Kind: ui.KindRow, Gap: 8, Children: tabs},
	})
}

func sessionActionsCard(locker string) *ui.Node {
	type action struct{ name, id string }
	acts := []action{
		{"Lock", "session-lock"},
		{"Log out", "session-logout"},
		{"Suspend", "session-suspend"},
		{"Reboot", "session-reboot"},
		{"Power off", "session-poweroff"},
	}
	if locker == "" {
		acts = acts[1:]
	}
	rows := []*ui.Node{monitorCardTitle("Session", 0)}
	for _, a := range acts {
		rows = append(rows, &ui.Node{
			Kind: ui.KindButton, Text: a.name, Action: a.id,
			Name: a.name, Role: "button", Focusable: true,
		})
	}
	return monitorCard(rows)
}

func sessionArgv(action, locker string) []string {
	switch action {
	case "session-lock":
		return strings.Fields(locker)
	case "session-logout":
		return []string{"loginctl", "terminate-session", "self"}
	case "session-suspend":
		return []string{"loginctl", "suspend"}
	case "session-reboot":
		return []string{"loginctl", "reboot"}
	case "session-poweroff":
		return []string{"loginctl", "poweroff"}
	}
	return nil
}

func (r *Registry) loadProfiles(h *PanelHost) {
	if r.lookPath == nil || !powerProfilesAvailable(r.lookPath) {
		h.profilesOK = false
		h.profiles = nil
		h.profileActive = ""
		return
	}
	if r.runArgvOutput == nil {
		h.profilesOK = false
		return
	}
	out, err := r.runArgvOutput([]string{"powerprofilesctl", "list"})
	if err != nil {
		h.profilesOK = false
		return
	}
	h.profiles, h.profileActive = parsePowerProfiles(out)
	h.profilesOK = len(h.profiles) > 0
}

func (r *Registry) setSessionProfile(h *PanelHost, name string) {
	if !profileSupports(h.profiles, name) {
		return
	}
	if err := r.runArgv(powerProfileSetArgv(name)); err != nil {
		h.errLabel = err.Error()
		r.rebuildPanel(h)
		return
	}
	h.errLabel = ""
	r.loadProfiles(h)
	r.rebuildPanel(h)
}

// runArgvDefault starts or runs one argv slice. A Registry holds this in a
// field rather than a package variable: parallel tests each replace their own
// Registry's hook, where one shared variable raced between them.
func runArgvDefault(argv []string) error {
	if len(argv) == 0 {
		return errors.New("empty command")
	}
	path, err := exec.LookPath(argv[0])
	if err != nil {
		return err
	}
	if argv[0] == "loginctl" || argv[0] == "powerprofilesctl" {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return exec.CommandContext(ctx, path, argv[1:]...).Run()
	}
	return exec.Command(path, argv[1:]...).Start()
}

func runArgvOutputDefault(argv []string) (string, error) {
	if len(argv) == 0 {
		return "", errors.New("empty command")
	}
	path, err := exec.LookPath(argv[0])
	if err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, path, argv[1:]...).Output()
	return string(out), err
}
