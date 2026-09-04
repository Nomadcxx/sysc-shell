package shell

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"testing"
	"time"

	metrics "github.com/Nomadcxx/sysc-metrics"

	"github.com/Nomadcxx/sysc-shell/internal/render"
	"github.com/Nomadcxx/sysc-shell/internal/services"
	"github.com/Nomadcxx/sysc-shell/internal/theme"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

func sessionTree(h *PanelHost, snap services.Snapshot, locker string) *ui.Node {
	children := make([]*ui.Node, 0, 4)
	if h.errLabel != "" {
		children = append(children, &ui.Node{Kind: ui.KindText, Text: h.errLabel, Tone: ui.ToneError})
	}
	if card := sessionBatteryCard(h.theme.Metrics, snap.Battery); card != nil {
		children = append(children, card)
	}
	if h.profilesOK && len(h.profiles) > 0 {
		children = append(children, sessionProfileCard(h))
	}
	children = append(children, sessionActionsCard(h.theme, locker))
	return &ui.Node{Kind: ui.KindColumn, Gap: monitorCardGap, Padding: h.theme.Metrics.PanelPadding, Children: children}
}

func sessionBatteryCard(m theme.Metrics, b *metrics.BatterySnapshot) *ui.Node {
	if b == nil || !b.Present || !b.ChargeValid {
		return nil
	}
	charging := b.State == metrics.BatteryCharging || b.State == metrics.BatteryFull
	glyph := string(render.BatteryIconRune(b.Charge, charging, false))
	rows := []*ui.Node{
		monitorCardTitle("Battery", 0),
		{Kind: ui.KindRow, Gap: 8, Children: []*ui.Node{
			{Kind: ui.KindText, Text: glyph},
			// Reserve the width of a full charge so the meter and the rows
			// below it do not shift when the figure reaches three digits.
			{Kind: ui.KindText, Text: fmt.Sprintf("%.0f%%", b.Charge*100),
				Tabular: true, MinWidthText: "100%"},
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
	return monitorCard(m, rows)
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

// Profile segments carry an 18 px glyph and session actions a 20 px one, per
// the asset contract. The sizes differ because a segment shares its row with
// two others and an action owns the full card width.
const (
	// The segmented row's budget is tight and fixed: a 420 px panel less the
	// panel and card padding leaves 364 px for three segments. The widest
	// label measures 88 px, so an 18 px glyph, its gap, and the label come to
	// 110 px, and the gap and padding below are what leave room for that.
	// TestProfileRowIsOneSegmentedControlThatFitsEveryLabel holds the line.
	sessionSegmentGap = 2
	// sessionSegmentContentGap separates a segment's glyph from its label.
	sessionSegmentContentGap = 4
	// sessionSegmentPadding is tighter than a full-width action's padding:
	// three labels share one card and every one of them has to fit.
	sessionSegmentPadding = 4
)

// sessionProfileIcon maps a power profile to its Material glyph. An unknown
// profile gets no icon rather than a wrong one; its label still names it.
func sessionProfileIcon(name string) string {
	switch name {
	case "performance":
		return "speed"
	case "balanced":
		return "balance"
	case "power-saver":
		return "energy_savings_leaf"
	}
	return ""
}

func sessionProfileCard(h *PanelHost) *ui.Node {
	segments := make([]*ui.Node, 0, len(h.profiles))
	for _, name := range h.profiles {
		label := powerProfileLabel(name)
		selected := name == h.profileActive

		// The check replaces the profile's own glyph on the active segment.
		// The label always stays, so a segment is never identified by icon
		// alone and the row reads the same with or without icons.
		icon := sessionProfileIcon(name)
		if selected {
			icon = "check"
		}
		content := make([]*ui.Node, 0, 2)
		if icon != "" {
			content = append(content, &ui.Node{
				Kind: ui.KindIcon, Icon: icon, IconSize: h.theme.Metrics.IconProfile,
			})
		}
		content = append(content, &ui.Node{Kind: ui.KindText, Text: label})

		segment := &ui.Node{
			Kind: ui.KindButton, Action: "profile:" + name,
			Name: label, Role: "tab", Focusable: true,
			Gap: sessionSegmentContentGap, Padding: sessionSegmentPadding,
			Height: h.theme.Metrics.StandardControl, Children: content,
		}
		if selected {
			segment.State |= ui.StateSelected
		}
		segments = append(segments, segment)
	}
	return monitorCard(h.theme.Metrics, []*ui.Node{
		monitorCardTitle("Power profile", 0),
		{
			Kind: ui.KindSegmented, Key: "power-profiles", Gap: sessionSegmentGap,
			Height: h.theme.Metrics.StandardControl, Children: segments,
		},
	})
}

func sessionActionsCard(th Theme, locker string) *ui.Node {
	// destructive marks the two actions that end the session without warning.
	// They take the error-toned outline from the catalogue rather than a solid
	// red block: a permanent red slab reads as an alarm, not as a control.
	type action struct {
		name, id, icon string
		destructive    bool
	}
	acts := []action{
		{name: "Lock", id: "session-lock", icon: "lock"},
		{name: "Log out", id: "session-logout", icon: "logout"},
		{name: "Suspend", id: "session-suspend", icon: "bedtime"},
		{name: "Reboot", id: "session-reboot", icon: "restart_alt", destructive: true},
		{name: "Power off", id: "session-poweroff", icon: "power_settings_new", destructive: true},
	}
	if locker == "" {
		acts = acts[1:]
	}
	rows := []*ui.Node{monitorCardTitle("Session", 0)}
	for _, a := range acts {
		node := &ui.Node{
			Kind: ui.KindButton, Action: a.id,
			Name: a.name, Role: "button", Focusable: true,
			Gap: th.Metrics.ButtonPadding / 2, Padding: th.Metrics.ButtonPadding,
			Height: th.Metrics.StandardControl,
			Children: []*ui.Node{
				{Kind: ui.KindIcon, Icon: a.icon, IconSize: th.Metrics.IconNormal},
				{Kind: ui.KindText, Text: a.name},
			},
		}
		if a.destructive {
			node.Fill = ui.FillOutline
			node.Tone = ui.ToneError
		}
		rows = append(rows, node)
	}
	return monitorCard(th.Metrics, rows)
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

func fetchProfileList(look func(string) (string, error), run func([]string) (string, error)) (names []string, active string, ok bool) {
	if look == nil || !powerProfilesAvailable(look) || run == nil {
		return nil, "", false
	}
	out, err := run([]string{"powerprofilesctl", "list"})
	if err != nil {
		return nil, "", false
	}
	names, active = parsePowerProfiles(out)
	return names, active, len(names) > 0
}

// scheduleLoadProfiles copies the exec hooks and runs list off the owner.
// Caller holds r.mu. Output() runs without it; apply takes the lock again.
func (r *Registry) scheduleLoadProfiles(h *PanelHost) {
	look := r.lookPath
	run := r.runArgvOutput
	if look == nil || !powerProfilesAvailable(look) {
		return
	}
	go func() {
		names, active, ok := fetchProfileList(look, run)
		r.mu.Lock()
		defer r.mu.Unlock()
		if r.panelHosts[PanelSession] != h {
			return
		}
		h.profiles = names
		h.profileActive = active
		h.profilesOK = ok
		r.rebuildPanel(h)
		r.publishSurface(h.output, panelSurfaceID(h.id))
	}()
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
	r.scheduleLoadProfiles(h)
	r.rebuildPanel(h)
}

// runArgvDefault starts or runs one argv slice. A Registry holds this in a
// field rather than a package variable: parallel tests each replace their own
// Registry's hook, where one shared variable raced between them.
func runArgvDefault(argv []string) error {
	if len(argv) == 0 {
		return errors.New("empty command")
	}
	// A session action ends the session it runs in. Under `go test` that is
	// the developer's own login session, so a test that activates a session
	// row logs them out -- and the suite still passes, which leaves nothing
	// pointing at the cause. Tests that mean to observe an action replace
	// Registry.runArgv; reaching the real launcher from a test binary is
	// always a mistake, so refuse rather than execute.
	//
	// The refusal belongs here rather than at the call sites: this is the one
	// place every action is launched from, and the stub tests usually install
	// (Registry.lookPath) is not consulted below.
	if testing.Testing() {
		return fmt.Errorf("refusing to run %q from a test binary: replace Registry.runArgv", argv[0])
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
