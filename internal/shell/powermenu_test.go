package shell

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"github.com/Nomadcxx/sysc-shell/internal/platform/niri"
	"github.com/Nomadcxx/sysc-shell/internal/platform/wayland"
	"github.com/Nomadcxx/sysc-shell/internal/sockpath"
	"github.com/Nomadcxx/sysc-shell/internal/theme"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
	"net"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestSessionPowerUsesSupportedNativeCommands(t *testing.T) {
	for action, verb := range map[string]string{"session-suspend": "suspend", "session-reboot": "reboot", "session-poweroff": "poweroff"} {
		if got, want := sessionArgv(action, ""), []string{"systemctl", "--no-block", verb}; !reflect.DeepEqual(got, want) {
			t.Errorf("%s: %v, want %v", action, got, want)
		}
	}
}
func TestSuspendTrackedUsesNativeSleepDispatch(t *testing.T) {
	r := newPanelRegistry(t)
	var got []string
	r.runArgv = func(argv []string) error { got = argv; return nil }
	if err := r.SuspendTracked(); err != nil {
		t.Fatal(err)
	}
	if want := []string{"systemctl", "--no-block", "suspend"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("%v, want %v", got, want)
	}
}

func TestSessionPowerObservedFailure(t *testing.T) {
	for _, tc := range []struct{ name, script, want string }{
		{"refused", "printf 'Access denied\\nPolicy refuses\\033' >&2; exit 1", "Access denied Policy refuses"},
		{"bounded", "head -c 100000 /dev/zero | tr '\\000' x >&2; exit 1", "xxx"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := runArgvOutputContext(context.Background(), []string{"sh", "-c", tc.script})
			if err == nil || !strings.Contains(err.Error(), tc.want) || len(err.Error()) > 500 || strings.ContainsAny(err.Error(), "\n\x1b") {
				t.Fatalf("error = %v", err)
			}
		})
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	if _, err := runArgvOutputContext(ctx, []string{"sh", "-c", "exec sleep 10"}); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("timeout: %v", err)
	}
	if _, err := runArgvOutputContext(context.Background(), []string{"/no/such/sysc-power-command"}); err == nil {
		t.Fatal("missing executable reported success")
	}
	for _, action := range []string{"session-suspend", "session-reboot", "session-poweroff"} {
		if err := runArgvDefault(sessionArgv(action, "")); err == nil || !strings.Contains(err.Error(), "test binary") {
			t.Fatalf("unguarded %s: %v", action, err)
		}
	}
}

func TestBatterySurfaceHasNoSessionActions(t *testing.T) {
	_, h := newSessionHost(t, "swaylock")
	for _, n := range ui.Focusables(h.root) {
		if strings.HasPrefix(n.Action, "session-") {
			t.Errorf("battery contains %s", n.Action)
		}
	}
}
func TestControlCentrePowerOpensCompactMenu(t *testing.T) {
	r := newPanelRegistry(t)
	if err := r.OpenPanel(PanelControlCenter, 7, Trigger{}); err != nil {
		t.Fatal(err)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	h := r.panelHosts[PanelControlCenter]
	if !h.activateControlCentre(r, &ui.Node{Action: "cc:power"}) {
		t.Fatal("power entry ignored")
	}
	id, err := parsePanelName("power")
	if err != nil || id == PanelSession || r.panelHosts[id] == nil {
		t.Fatal("power entry still uses battery surface")
	}
}

func TestPowerMenuConfirmsDestructiveActions(t *testing.T) {
	for _, action := range []string{"session-logout", "session-reboot", "session-poweroff"} {
		t.Run(action, func(t *testing.T) {
			r, h := newPowerHost(t)
			calls := make(chan string, 2)
			r.runArgv = func([]string) error { calls <- action; return errors.New("Policy refused") }
			r.niriSend = func(any) error { calls <- action; return errors.New("Policy refused") }
			r.mu.Lock()
			h.activatePower(r, &ui.Node{Action: action})
			if h.powerConfirm != action || h.focused().Action != "power:cancel" {
				t.Fatal("missing safe confirmation focus")
			}
			select {
			case <-calls:
				t.Fatal("ran before confirmation")
			default:
			}
			h.activatePower(r, &ui.Node{Action: "power:cancel"})
			if h.powerConfirm != "" {
				t.Fatal("cancel retained intent")
			}
			h.activatePower(r, &ui.Node{Action: action})
			h.activatePower(r, &ui.Node{Action: "power:confirm"})
			h.activatePower(r, &ui.Node{Action: "power:confirm"})
			r.mu.Unlock()
			select {
			case <-calls:
			case <-time.After(time.Second):
				t.Fatal("confirmed action missing")
			}
			waitFor(t, func() bool {
				r.mu.Lock()
				defer r.mu.Unlock()
				return !h.sessionPending && strings.Contains(h.errLabel, "Policy refused")
			})
			select {
			case <-calls:
				t.Fatal("duplicate action")
			default:
			}
		})
	}
}
func newPowerHost(t *testing.T) (*Registry, *PanelHost) {
	t.Helper()
	r := newPanelRegistry(t)
	r.cfg.Session.Locker = "swaylock"
	if err := r.OpenPanel(PanelPower, 7, Trigger{OutW: 1920, OutH: 1080}); err != nil {
		t.Fatal(err)
	}
	drainAuxQueue(r)
	return r, r.panelHosts[PanelPower]
}
func TestPowerMenuPresentationRetainsIntent(t *testing.T) {
	r, h := newPowerHost(t)
	r.mu.Lock()
	defer r.mu.Unlock()
	h.activatePower(r, &ui.Node{Action: "session-poweroff"})
	h.errLabel = "Policy refused"
	for _, full := range []bool{true, false} {
		r.setPowerPresentationLocked(h, full)
		if r.panelHosts[PanelPower] != h || h.powerConfirm != "session-poweroff" || h.focused().Action != "power:cancel" || h.errLabel != "Policy refused" {
			t.Fatal("mode switch lost host, intent, focus or error")
		}
		if full && h.rect != (ui.Rect{W: 1920, H: 1080}) {
			t.Fatalf("full screen: %+v", h.rect)
		}
	}
}

func TestPowerMenuLayoutFitsMeasuredContent(t *testing.T) {
	for _, density := range theme.Densities() {
		for _, font := range []int{100, 200} {
			for _, scale := range []int{120, 150} {
				for _, size := range []ui.Rect{{W: 1920, H: 1080}, {W: 800, H: 600}, {W: 420, H: 500}, {W: 320, H: 240}} {
					t.Run(fmt.Sprintf("%s-font%d-%d-%dx%d", density, font, scale, size.W, size.H), func(t *testing.T) {
						r := newPanelRegistry(t)
						r.cfg.Theme.Density = density
						r.cfg.Theme.FontScale = font
						if err := r.OpenPanel(PanelPower, 7, Trigger{OutW: size.W, OutH: size.H}); err != nil {
							t.Fatal(err)
						}
						r.mu.Lock()
						defer r.mu.Unlock()
						h := r.panelHosts[PanelPower]
						for _, full := range []bool{false, true} {
							r.setPowerPresentationLocked(h, full)
							for _, state := range []string{"chooser", "confirm", "error", "pending"} {
								h.powerConfirm, h.powerFailed = "", ""
								h.errLabel = ""
								h.sessionPending = false
								switch state {
								case "confirm":
									h.powerConfirm = "session-poweroff"
								case "error":
									h.powerFailed = "session-poweroff"
									h.errLabel = strings.Repeat("Policy denied. ", 10)
								case "pending":
									h.sessionPending = true
								}
								r.rebuildPanel(h)
								if err := h.configure(h.place.Panel.W, h.place.Panel.H, scale); err != nil {
									t.Fatalf("full=%v state=%s: %v", full, state, err)
								}
								var visit func(*ui.Node)
								visit = func(n *ui.Node) {
									if n == nil {
										return
									}
									if n.Kind == ui.KindButton && (n.Bounds.W < 1 || n.Bounds.X < 0 || n.Bounds.X+n.Bounds.W > h.logicalW) {
										t.Errorf("%s outside width: %+v / %d", n.Action, n.Bounds, h.logicalW)
									}
									for _, c := range n.Children {
										visit(c)
									}
								}
								visit(h.root)
							}
						}
					})
				}
			}
		}
	}
}

func TestPowerMenuColumns(t *testing.T) {
	for _, tc := range []struct{ width, want int }{{1200, 6}, {800, 3}, {400, 2}, {200, 1}} {
		if got := powerColumns(tc.width, 160, 8, true); got != tc.want {
			t.Errorf("width %d: %d, want %d", tc.width, got, tc.want)
		}
	}
}

func TestPowerMenuConfigureReflows(t *testing.T) {
	r, h := newPowerHost(t)
	r.mu.Lock()
	defer r.mu.Unlock()
	r.setPowerPresentationLocked(h, true)
	if err := h.configure(320, 240, 150); err != nil {
		t.Fatal(err)
	}
	body := keyedScroll(h.root, "power-body")
	if body == nil || body.Bounds.Y+body.Bounds.H > 240 {
		t.Fatalf("viewport didn't follow configure: %+v", body)
	}
	for _, n := range ui.Focusables(h.root) {
		if n.Bounds.X+n.Bounds.W > 320 {
			t.Fatalf("%s didn't reflow: %+v", n.Name, n.Bounds)
		}
	}
}

func TestPowerMenuClosedHostCannotDispatch(t *testing.T) {
	r, h := newPowerHost(t)
	calls := make(chan struct{}, 1)
	r.runArgv = func([]string) error { calls <- struct{}{}; return nil }
	r.mu.Lock()
	h.activatePower(r, &ui.Node{Action: "session-reboot"})
	r.mu.Unlock()
	r.ClosePanel(PanelPower)
	if err := r.OpenPanel(PanelPower, 7, Trigger{}); err != nil {
		t.Fatal(err)
	}
	r.mu.Lock()
	handled := h.activatePower(r, &ui.Node{Action: "power:confirm"})
	r.mu.Unlock()
	if handled || h.sessionPending {
		t.Fatal("stale confirmation accepted")
	}
	select {
	case <-calls:
		t.Fatal("stale native request")
	default:
	}
}

func TestPowerMenuFullscreenDimsDesktop(t *testing.T) {
	r, h := newPowerHost(t)
	r.mu.Lock()
	defer r.mu.Unlock()
	h.backdrop = &ui.Image{Width: 1, Height: 1, Pix: []byte{0, 0, 0, 255}}
	r.setPowerPresentationLocked(h, true)
	s := h.rootStyle(h.theme, true)
	if fill := s.RootFill(); fill.A == 0 || fill.A == 255 || s.Radius != 0 || s.SurfaceRadius != 0 || s.Rim.A != 0 {
		t.Fatalf("full screen must be a translucent scrim without frame: fill %+v radius %d surface %d rim %+v", fill, s.Radius, s.SurfaceRadius, s.Rim)
	}
	if h.backdropFor(h.theme) != nil {
		t.Fatal("resized surface retained old capture")
	}
	h.maybeRecapture(r, h.theme, time.Now())
	if h.lensPending {
		t.Fatal("resized surface requested old capture region")
	}
	if err := h.configure(h.place.Panel.W, h.place.Panel.H, 120); err != nil {
		t.Fatal(err)
	}
	sheet := findNodeKey(h.root, "power-sheet")
	if sheet == nil || sheet.Fill == ui.FillNone || sheet.Bounds.W >= h.logicalW || sheet.Bounds.H >= h.logicalH/2 {
		t.Fatalf("full-screen deck needs a contained sheet: %+v", sheet)
	}
	b := sheet.Bounds
	if abs(2*b.X+b.W-h.logicalW) > 2 || abs(2*b.Y+b.H-h.logicalH) > 2 {
		t.Fatalf("sheet not centred in %dx%d: %+v", h.logicalW, h.logicalH, b)
	}
	r.setPowerPresentationLocked(h, false)
	if h.backdropFor(h.theme) == nil {
		t.Fatal("compact view at its captured size lost its backdrop")
	}
}

func TestPowerMenuCardsAreNeutral(t *testing.T) {
	r, h := newPowerHost(t)
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, a := range sessionActionOptions() {
		n := findNode(h.root, func(n *ui.Node) bool { return n.Action == a.id })
		if n == nil || n.Tone == ui.ToneError || n.Fill == ui.FillOutline || n.Fill == ui.FillError {
			t.Errorf("%s: card must stay neutral at rest: %+v", a.id, n)
		}
	}
}

func TestPowerMenuConfirmIsAStandardDialog(t *testing.T) {
	r, h := newPowerHost(t)
	r.mu.Lock()
	defer r.mu.Unlock()
	h.activatePower(r, &ui.Node{Action: "session-poweroff"})
	if err := h.configure(h.place.Panel.W, h.place.Panel.H, 120); err != nil {
		t.Fatal(err)
	}
	if findNode(h.root, func(n *ui.Node) bool { return n.Kind == ui.KindIcon && n.Icon == "warning" }) != nil {
		t.Error("confirmation repeats the alarm the confirm button already carries")
	}
	cancel := findNode(h.root, func(n *ui.Node) bool { return n.Action == "power:cancel" })
	confirm := findNode(h.root, func(n *ui.Node) bool { return n.Action == "power:confirm" })
	if cancel == nil || confirm == nil || confirm.Fill != ui.FillError {
		t.Fatalf("dialog actions: cancel %+v confirm %+v", cancel, confirm)
	}
	if cancel.Bounds.Y != confirm.Bounds.Y || cancel.Bounds.X+cancel.Bounds.W > confirm.Bounds.X {
		t.Fatalf("actions must share one row, Cancel first: %+v %+v", cancel.Bounds, confirm.Bounds)
	}
	if !textIn(h.root, "Power off?") {
		t.Error("dialog headline missing")
	}
}

func TestPowerMenuFailureNamesActionAndOffersRetry(t *testing.T) {
	r, h := newPowerHost(t)
	calls := make(chan []string, 2)
	r.runArgv = func(argv []string) error {
		calls <- argv
		return &commandError{name: "systemctl", detail: "Access denied", err: errors.New("exit status 1")}
	}
	r.mu.Lock()
	h.activatePower(r, &ui.Node{Action: "session-reboot"})
	h.activatePower(r, &ui.Node{Action: "power:confirm"})
	r.mu.Unlock()
	<-calls
	waitFor(t, func() bool {
		r.mu.Lock()
		defer r.mu.Unlock()
		return h.powerFailed == "session-reboot"
	})
	r.mu.Lock()
	if !textIn(h.root, "Restart failed") || !textIn(h.root, "Access denied") || textIn(h.root, "systemctl") || textIn(h.root, "exit status") {
		t.Errorf("failure must name the action in plain words: %q", texts(h.root))
	}
	if h.focused() == nil || h.focused().Action != "power:back" {
		t.Error("failure must focus Back")
	}
	h.activatePower(r, &ui.Node{Action: "power:retry"})
	r.mu.Unlock()
	select {
	case <-calls:
	case <-time.After(time.Second):
		t.Fatal("retry did not dispatch")
	}
}

func TestSessionFailureDetail(t *testing.T) {
	h := &PanelHost{}
	for _, tc := range []struct {
		err  error
		want string
	}{
		{&commandError{name: "systemctl", detail: "Access denied", err: errors.New("exit status 1")}, "Access denied"},
		{fmt.Errorf("systemctl: %w", context.DeadlineExceeded), "The request timed out."},
		{&exec.Error{Name: "systemctl", Err: exec.ErrNotFound}, "systemctl is not installed."},
		{errors.New("Policy refused"), "Policy refused"},
	} {
		if got := sessionFailureDetail(h, tc.err); got != tc.want {
			t.Errorf("%v: %q, want %q", tc.err, got, tc.want)
		}
	}
}

func TestPowerMenuIsLocalized(t *testing.T) {
	r, h := newPowerHost(t)
	r.mu.Lock()
	defer r.mu.Unlock()
	h.draft.Session.Language = "es"
	english := []string{"Power", "Suspend", "Screen off", "Log out", "Restart", "Power off", "Full screen", "Compact view", "Close", "Cancel", "Power off?", "Back", "Try again"}
	check := func(state string) {
		t.Helper()
		r.rebuildPanel(h)
		var visit func(*ui.Node)
		visit = func(n *ui.Node) {
			for _, e := range english {
				if n.Text == e || n.Name == e {
					t.Errorf("%s: untranslated %q", state, e)
				}
			}
			for _, c := range n.Children {
				visit(c)
			}
		}
		visit(h.root)
	}
	check("chooser")
	r.setPowerPresentationLocked(h, true)
	check("full screen")
	h.powerConfirm = "session-poweroff"
	check("confirm")
	h.powerConfirm, h.powerFailed, h.errLabel = "", "session-poweroff", "x"
	check("failed")
}

func textIn(n *ui.Node, want string) bool {
	return findNode(n, func(n *ui.Node) bool { return strings.Contains(n.Text, want) }) != nil
}

func TestPowerMenuLauncherRequiresConfirmation(t *testing.T) {
	r := newPanelRegistry(t)
	if err := r.OpenPanel(PanelLauncher, 7, Trigger{}); err != nil {
		t.Fatal(err)
	}
	r.mu.Lock()
	h := r.panelHosts[PanelLauncher]
	h.launcherAttempt = 2
	h.launcherPendingAttempt = 2
	r.mu.Unlock()
	r.runArgv = func([]string) error {
		t.Error("destructive launcher result dispatched without confirmation")
		return nil
	}
	p := r.sessionProvider()
	if err := p.Activate("reboot", "session-reboot", ""); err != nil {
		t.Fatal(err)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	power := r.panelHosts[PanelPower]
	if power == nil || power.powerConfirm != "session-reboot" || power.focused().Action != "power:cancel" {
		t.Fatal("launcher skipped power confirmation")
	}
}

func TestPowerMenuCompositorDispatch(t *testing.T) {
	for _, compositor := range []string{"niri", "sysc-compositor"} {
		for _, action := range []string{"session-logout", "session-display-off"} {
			for _, reply := range []string{`{"Ok":"Handled"}`, `{"Err":"unsupported action"}`} {
				t.Run(compositor+"/"+action+"/"+reply, func(t *testing.T) {
					socket := filepath.Join(sockpath.Dir(t, "power"), compositor+".sock")
					ln, err := net.Listen("unix", socket)
					if err != nil {
						t.Fatal(err)
					}
					defer ln.Close()
					got := make(chan string, 1)
					go func() {
						conn, err := ln.Accept()
						if err != nil {
							got <- ""
							return
						}
						defer conn.Close()
						sc := bufio.NewScanner(conn)
						sc.Scan()
						got <- sc.Text()
						fmt.Fprintln(conn, reply)
					}()
					r := newPanelRegistry(t)
					r.niriSend = func(body any) error {
						ctx, cancel := context.WithTimeout(context.Background(), time.Second)
						defer cancel()
						return niri.Action(ctx, socket, body)
					}
					r.mu.Lock()
					run := r.sessionActionRunnerLocked(action)
					r.mu.Unlock()
					err = run()
					if (err != nil) != strings.Contains(reply, "Err") {
						t.Fatalf("dispatch error: %v", err)
					}
					want := `{"Action":{"Quit":{"skip_confirmation":true}}}`
					if action == "session-display-off" {
						want = `{"Action":{"PowerOffMonitors":{}}}`
					}
					if wire := <-got; wire != want {
						t.Fatalf("%s, want %s", wire, want)
					}
				})
			}
		}
	}
	r := newPanelRegistry(t)
	r.niriSend = func(any) error { return errors.New("no compositor connection") }
	if err := r.sessionActionRunnerLocked("session-logout")(); err == nil {
		t.Fatal("missing connection reported success")
	}
	if err := r.sessionActionRunnerLocked("unknown")(); err == nil {
		t.Fatal("unknown action accepted")
	}
}

func TestPowerMenuPendingDoesNotChangeReplacement(t *testing.T) {
	r, h := newPowerHost(t)
	started, release, finished := make(chan struct{}), make(chan struct{}), make(chan struct{})
	r.runArgv = func([]string) error { close(started); <-release; close(finished); return errors.New("old refusal") }
	r.mu.Lock()
	h.activatePower(r, &ui.Node{Action: "session-reboot"})
	h.activatePower(r, &ui.Node{Action: "power:confirm"})
	h.activatePower(r, &ui.Node{Action: "power:confirm"})
	r.mu.Unlock()
	<-started
	r.ClosePanel(PanelPower)
	if err := r.OpenPanel(PanelPower, 7, Trigger{}); err != nil {
		t.Fatal(err)
	}
	close(release)
	<-finished
	r.mu.Lock()
	defer r.mu.Unlock()
	current := r.panelHosts[PanelPower]
	if current == h || current.errLabel != "" || current.sessionPending {
		t.Fatal("old request altered replacement")
	}
}

func TestPowerMenuKeyboardUsesResolvedKeysAndReturnsFocus(t *testing.T) {
	r := newPanelRegistry(t)
	if err := r.OpenPanel(PanelControlCenter, 7, Trigger{}); err != nil {
		t.Fatal(err)
	}
	drainAuxQueue(r)
	r.mu.Lock()
	defer r.mu.Unlock()
	cc := r.panelHosts[PanelControlCenter]
	cc.activateControlCentre(r, &ui.Node{Action: "cc:power"})
	drainAuxQueue(r)
	h := r.panelHosts[PanelPower]
	if !h.keyInput(r, ui.KeyInput{Code: 1, Sym: ui.SymTab}) || !h.focusVisible || r.panelHosts[PanelPower] != h {
		t.Fatal("resolved Tab dismissed menu")
	}
	h.activatePower(r, &ui.Node{Action: "session-logout"})
	h.keyInput(r, ui.KeyInput{Code: 15, Sym: ui.SymEscape})
	if h.powerConfirm != "" || r.panelHosts[PanelPower] != h {
		t.Fatal("Escape didn't cancel confirmation")
	}
	h.keyInput(r, ui.KeyInput{Sym: ui.SymEscape})
	cc = r.panelHosts[PanelControlCenter]
	if cc == nil || cc.section != "home" || cc.focused() == nil || cc.focused().Action != "cc:power" {
		t.Fatal("power dismissal lost Control Centre focus")
	}
}

func TestPowerMenuOutputLossClosesSurface(t *testing.T) {
	r := newPanelRegistry(t)
	cb, err := r.NewHost(7, "test-output")
	if err != nil {
		t.Fatal(err)
	}
	if err := cb.Configure(800, 40, 120); err != nil {
		t.Fatal(err)
	}
	if err := r.OpenPanel(PanelPower, 7, Trigger{OutW: 800, OutH: 600}); err != nil {
		t.Fatal(err)
	}
	r.DropHost(7)
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.panelHosts[PanelPower] != nil {
		t.Fatal("power survived output loss")
	}
}

func TestPowerMenuCardsUseMeasuredCardLayout(t *testing.T) {
	r, h := newPowerHost(t)
	r.mu.Lock()
	defer r.mu.Unlock()
	small := findNode(h.root, func(n *ui.Node) bool { return n.Action == "session-lock" })
	if small == nil || small.Shape != ui.ShapeCard {
		t.Fatal("power action lacks card shape")
	}
	icon := findNode(small, func(n *ui.Node) bool { return n.Kind == ui.KindIcon })
	compactSize := icon.IconSize
	r.setPowerPresentationLocked(h, true)
	large := findNode(h.root, func(n *ui.Node) bool { return n.Action == "session-lock" })
	icon = findNode(large, func(n *ui.Node) bool { return n.Kind == ui.KindIcon })
	if icon.IconSize <= compactSize {
		t.Fatal("full-screen card didn't increase icon hierarchy")
	}
	h.powerLockLabel = "Lock running (unverified)"
	h.root = powerTree(h)
	if err := h.configure(320, 240, 150); err != nil {
		t.Fatal(err)
	}
	var check func(*ui.Node)
	check = func(n *ui.Node) {
		if n.Kind == ui.KindText {
			w, _ := h.measureText()(n.Text, ui.TextAttrsOf(n))
			if w > n.Bounds.W {
				t.Errorf("text clipped: %q: %d > %d", n.Text, w, n.Bounds.W)
			}
		}
		for _, c := range n.Children {
			check(c)
		}
	}
	check(h.root)
}

func TestPowerMenuRefitsAtConfiguredScale(t *testing.T) {
	for _, density := range theme.Densities() {
		t.Run(string(density), func(t *testing.T) {
			r := newPanelRegistry(t)
			r.cfg.Theme.Density = density
			r.cfg.Session.Locker = "swaylock"
			if err := r.OpenPanel(PanelPower, 7, Trigger{OutW: 1536, OutH: 864}); err != nil {
				t.Fatal(err)
			}
			var open *wayland.AuxSpec
			for _, req := range drainAux(t, r, 2) {
				if req.Open != nil && req.Open.ID == panelSurfaceID(PanelPower) {
					open = req.Open
				}
			}
			if open == nil {
				t.Fatal("power surface not opened")
			}
			if err := open.Callbacks.Configure(int(open.Width), int(open.Height), 150); err != nil {
				t.Fatal(err)
			}
			r.mu.Lock()
			defer r.mu.Unlock()
			h := r.panelHosts[PanelPower]
			if err := h.configure(h.place.Panel.W, h.place.Panel.H, 150); err != nil {
				t.Fatal(err)
			}
			body := keyedScroll(h.root, "power-body")
			deck := body.Children[0].Children[0]
			if deck.Bounds.H+2*powerRingGutter > body.Bounds.H {
				t.Fatalf("compact deck scrolls at 150: deck %d body %d", deck.Bounds.H, body.Bounds.H)
			}
		})
	}
}
