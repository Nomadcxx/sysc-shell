package shell

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"testing"
	"time"
	"unicode"

	metrics "github.com/Nomadcxx/sysc-metrics"

	"github.com/Nomadcxx/sysc-shell/internal/platform/niri"
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
	if card := sessionBatteryCard(h.theme.Metrics, h.tr, snap.Battery); card != nil {
		children = append(children, card)
	}
	if h.profilesOK && len(h.profiles) > 0 {
		children = append(children, sessionProfileCard(h))
	}
	if len(children) == 0 {
		children = append(children, &ui.Node{Kind: ui.KindText, Text: h.tr("session.no_battery", "No battery or power profiles available.")})
	}
	return &ui.Node{Kind: ui.KindColumn, Gap: monitorCardGap, Padding: h.theme.Metrics.PanelPadding, Children: children}
}

func sessionBatteryCard(m theme.Metrics, tr func(string, string) string, b *metrics.BatterySnapshot) *ui.Node {
	if b == nil || !b.Present || !b.ChargeValid {
		return nil
	}
	charging := b.State == metrics.BatteryCharging || b.State == metrics.BatteryFull
	glyph := string(render.BatteryIconRune(b.Charge, charging, false))
	rows := []*ui.Node{
		monitorCardTitle(tr("session.battery", "Battery"), 0),
		{Kind: ui.KindRow, Gap: theme.MarginM, Children: []*ui.Node{
			{Kind: ui.KindText, Text: glyph},
			// Reserve the width of a full charge so the meter and the rows
			// below it do not shift when the figure reaches three digits.
			{Kind: ui.KindText, Text: fmt.Sprintf("%.0f%%", b.Charge*100),
				Tabular: true, MinWidthText: "100%"},
		}},
		{Kind: ui.KindMeter, Value: b.Charge, Max: 1},
		{Kind: ui.KindText, Text: tr("session.state."+batteryStateLabel(b.State), batteryStateLabel(b.State))},
	}
	if b.TimeValid {
		if d := batteryDuration(b.TimeRemaining); d != "" {
			rows = append(rows, &ui.Node{Kind: ui.KindText,
				Text: fmt.Sprintf(tr("session.battery.remaining", "%s remaining"), d)})
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
		label := h.tr("session.profile."+name, powerProfileLabel(name))
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
		monitorCardTitle(h.tr("session.power_profile", "Power profile"), 0),
		{
			Kind: ui.KindSegmented, Key: "power-profiles", Gap: sessionSegmentGap,
			Height: h.theme.Metrics.StandardControl, Children: segments,
		},
	})
}

// sessionAction is one session action. key names its catalog entry, which
// predates the English label: Reboot's translations now read as Restart.
// destructive actions end the session and ask for confirmation first.
type sessionAction struct {
	name, key, id, icon string
	destructive         bool
}

func sessionActionOptions() []sessionAction {
	return []sessionAction{
		{name: "Lock", key: "Lock", id: "session-lock", icon: "lock"},
		{name: "Suspend", key: "Suspend", id: "session-suspend", icon: "bedtime"},
		{name: "Screen off", key: "Screen off", id: "session-display-off", icon: "visibility_off"},
		{name: "Log out", key: "Log out", id: "session-logout", icon: "logout", destructive: true},
		{name: "Restart", key: "Reboot", id: "session-reboot", icon: "restart_alt", destructive: true},
		{name: "Power off", key: "Power off", id: "session-poweroff", icon: "power_settings_new", destructive: true},
	}
}

func sessionArgv(action, locker string) []string {
	switch action {
	case "session-lock":
		return strings.Fields(locker)
	case "session-suspend":
		return []string{"systemctl", "--no-block", "suspend"}
	case "session-reboot":
		return []string{"systemctl", "--no-block", "reboot"}
	case "session-poweroff":
		return []string{"systemctl", "--no-block", "poweroff"}
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
		if r.panelHosts[h.id] != h {
			r.mu.Unlock()
			return
		}
		h.profiles = names
		h.profileActive = active
		h.profilesOK = ok
		r.rebuildPanel(h)
		output, id := h.output, panelSurfaceID(h.id)
		r.mu.Unlock()
		r.publishSurface(output, id)
	}()
}

func (r *Registry) setSessionProfile(h *PanelHost, name string) {
	if h.profilePending || !profileSupports(h.profiles, name) {
		return
	}
	run := r.runArgv
	look := r.lookPath
	list := r.runArgvOutput
	h.profilePending = true
	clearError := h.errLabel != ""
	h.errLabel = ""
	if clearError {
		r.rebuildPanel(h)
	}
	go func() {
		err := run(powerProfileSetArgv(name))
		var names []string
		active := ""
		ok := false
		if err == nil {
			names, active, ok = fetchProfileList(look, list)
		}

		r.mu.Lock()
		if r.panelHosts[h.id] != h {
			r.mu.Unlock()
			return
		}
		h.profilePending = false
		if err != nil {
			h.errLabel = err.Error()
		} else {
			h.errLabel = ""
			if ok {
				h.profiles, h.profileActive, h.profilesOK = names, active, true
			} else {
				// The set command succeeded; keep the visible selection truthful
				// when a follow-up list cannot be read.
				h.profileActive = name
			}
		}
		r.rebuildPanel(h)
		output, id := h.output, panelSurfaceID(h.id)
		r.mu.Unlock()
		r.publishSurface(output, id)
	}()
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
	if argv[0] == "systemctl" || argv[0] == "loginctl" || argv[0] == "powerprofilesctl" {
		_, err := runArgvOutputDefault(argv)
		return err
	}
	return startArgvDetached(path, argv[1:])
}

// startArgvDetached reaps a launched process after it exits without waiting on
// the caller for the process lifetime.
func startArgvDetached(path string, args []string) error {
	cmd := exec.Command(path, args...)
	if err := cmd.Start(); err != nil {
		return err
	}
	go func() { _ = cmd.Wait() }()
	return nil
}

func startInhibitDefault() (io.Closer, error) {
	if testing.Testing() {
		return nil, errors.New("refusing to start an idle inhibit from a test binary: replace Registry.startInhibit")
	}
	path, err := exec.LookPath("systemd-inhibit")
	if err != nil {
		return nil, err
	}
	cmd := exec.Command(path, caffeineInhibitArgs()...)
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	return &processHold{cmd: cmd}, nil
}

func caffeineInhibitArgs() []string {
	return []string{"--what=idle", "--who=sysc-shell", "--why=Caffeine", "--mode=block", "sleep", "infinity"}
}

type processHold struct{ cmd *exec.Cmd }

func (p *processHold) Close() error {
	if p == nil || p.cmd == nil || p.cmd.Process == nil {
		return nil
	}
	err := p.cmd.Process.Kill()
	_ = p.cmd.Wait()
	return err
}

func runArgvOutputDefault(argv []string) (string, error) {
	if len(argv) == 0 {
		return "", errors.New("empty command")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return runArgvOutputContext(ctx, argv)
}

func runArgvOutputContext(ctx context.Context, argv []string) (string, error) {
	path, err := exec.LookPath(argv[0])
	if err != nil {
		return "", err
	}
	cmd := exec.CommandContext(ctx, path, argv[1:]...)
	cmd.WaitDelay = time.Second
	out, err := cmd.Output()
	if err != nil {
		if ctx.Err() != nil {
			return "", fmt.Errorf("%s: %w", argv[0], ctx.Err())
		}
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			return "", &commandError{name: argv[0], detail: stderrDetail(exit.Stderr), err: err}
		}
		return "", fmt.Errorf("%s: %w", argv[0], err)
	}
	return string(out), nil
}

// commandError is a native command that ran and refused. detail is its own
// explanation, bounded and on one line, which the power menu shows on its own;
// Error keeps the command name and exit status for the journal.
type commandError struct {
	name, detail string
	err          error
}

func (e *commandError) Error() string {
	if e.detail == "" {
		return fmt.Sprintf("%s: %v", e.name, e.err)
	}
	return fmt.Sprintf("%s: %s (%v)", e.name, e.detail, e.err)
}

func (e *commandError) Unwrap() error { return e.err }

// stderrDetail flattens stderr to one bounded line of printable text.
func stderrDetail(stderr []byte) string {
	detail := strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, string(stderr))
	detail = strings.Join(strings.Fields(detail), " ")
	if runes := []rune(detail); len(runes) > 400 {
		detail = string(runes[:400]) + "…"
	}
	return detail
}

// sessionFailureDetail says why a session action failed in the user's words:
// the command's own explanation when it gave one, otherwise the cause.
func sessionFailureDetail(h *PanelHost, err error) string {
	var refused *commandError
	var missing *exec.Error
	switch {
	case errors.As(err, &refused) && refused.detail != "":
		return refused.detail
	case errors.Is(err, context.DeadlineExceeded):
		return h.tr("power.timeout", "The request timed out.")
	case errors.As(err, &missing) && errors.Is(missing.Err, exec.ErrNotFound):
		return fmt.Sprintf(h.tr("power.missing", "%s is not installed."), missing.Name)
	}
	return err.Error()
}

// sessionActionRunnerLocked captures fixed intent and the current compositor socket.
// The caller invokes the returned request off Registry.mu.
func (r *Registry) sessionActionRunnerLocked(action string) func() error {
	switch action {
	case "session-lock":
		return r.LockTracked
	case "session-suspend":
		return r.SuspendTracked
	case "session-logout", "session-display-off":
		if r.niriSend == nil && testing.Testing() {
			return func() error { return errors.New("refusing session IPC from a test binary: replace Registry.niriSend") }
		}
		var body any = niri.PowerOffMonitors{}
		if action == "session-logout" {
			body = niri.Quit{SkipConfirmation: true}
		}
		return r.niriActionLocked(body)
	case "session-reboot", "session-poweroff":
		run, argv := r.runArgv, sessionArgv(action, "")
		return func() error { return run(argv) }
	default:
		return func() error { return errors.New("unknown session action") }
	}
}
