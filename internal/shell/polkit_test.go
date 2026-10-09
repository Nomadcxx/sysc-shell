package shell

import (
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/Nomadcxx/sysc-shell/internal/config"
	"github.com/Nomadcxx/sysc-shell/internal/platform/wayland"
	"github.com/Nomadcxx/sysc-shell/internal/services/polkit"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

func TestPolkitStatusLabel(t *testing.T) {
	for _, tc := range []struct {
		name string
		in   polkit.Status
		want string
	}{
		{"off", polkit.Status{Policy: polkit.PolicyOff, Reason: "disabled"}, "Off"},
		{"registered", polkit.Status{Policy: polkit.PolicyAuto, Registered: true}, "Registered"},
		{"passive", polkit.Status{Policy: polkit.PolicyAuto, Passive: "polkit-gnome-authentication-agent-1"}, "Passive · polkit-gnome-authentication-agent-1"},
		{"helper missing", polkit.Status{Policy: polkit.PolicyAuto, Reason: polkit.ErrNoHelper.Error()}, "Helper missing"},
		{"unavailable", polkit.Status{Policy: polkit.PolicyAuto, Reason: "system bus unavailable"}, "Unavailable"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := polkitStatusLabel(tc.in); got != tc.want {
				t.Fatalf("polkitStatusLabel(%+v) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestPolkitPromptFocusOrder(t *testing.T) {
	for _, tc := range []struct {
		phase              polkitPromptPhase
		hasDetails         bool
		hasIdentityChooser bool
		want               []string
	}{
		{polkitChoosingIdentity, true, true, []string{"identity", "continue", "cancel", "details"}},
		{polkitChoosingIdentity, true, false, []string{"continue", "cancel", "details"}},
		{polkitEnteringResponse, true, false, []string{"password", "submit", "cancel", "details"}},
		{polkitShowingInfo, false, false, []string{"continue", "cancel"}},
		{polkitAwaitingPrompt, false, false, []string{"cancel"}},
	} {
		if got := polkitFocusOrder(tc.phase, tc.hasDetails, tc.hasIdentityChooser); !slices.Equal(got, tc.want) {
			t.Fatalf("polkitFocusOrder(%d, %t, %t) = %v, want %v", tc.phase, tc.hasDetails, tc.hasIdentityChooser, got, tc.want)
		}
	}
}

func TestPolkitEchoOnResponseStaysVisible(t *testing.T) {
	if polkitPromptMasked(polkit.Prompt{Secret: true, Echo: true}) {
		t.Fatal("PAM echo-on response is masked")
	}
	if !polkitPromptMasked(polkit.Prompt{Secret: true}) {
		t.Fatal("PAM echo-off response is visible")
	}
}

func TestPolkitOutputPrefersFocusAndFallsBackToLowestGlobal(t *testing.T) {
	r := &Registry{
		focused: "DP-2",
		bars: map[uint32]*Bar{
			10: {conn: "DP-2"},
			4:  {conn: "DP-1"},
		},
	}
	if global, connector, ok := r.polkitOutputLocked(); !ok || global != 10 || connector != "DP-2" {
		t.Fatalf("focused output = %d, %q, %t; want 10, DP-2, true", global, connector, ok)
	}
	r.focused = "missing"
	if global, connector, ok := r.polkitOutputLocked(); !ok || global != 4 || connector != "DP-1" {
		t.Fatalf("fallback output = %d, %q, %t; want 4, DP-1, true", global, connector, ok)
	}
}

func TestPolkitPromptPreservesActionMessageVerbatim(t *testing.T) {
	const message = "  Verify the disk\n before continuing.  "
	h := &polkitHost{request_: polkit.Request{Message: message}, phase: polkitAwaitingPrompt}
	h.rebuild()
	if findNode(h.root, func(n *ui.Node) bool { return n.Kind == ui.KindText && n.Text == message }) == nil {
		t.Fatalf("prompt tree does not preserve message %q", message)
	}
}

func TestPolkitPromptShowsAppIconOrShieldFallback(t *testing.T) {
	h := &polkitHost{request_: polkit.Request{IconName: "missing-app-icon"}, phase: polkitAwaitingPrompt}
	h.rebuild()
	icon := findNode(h.root, func(n *ui.Node) bool {
		return n.Kind == ui.KindIcon || (n.Kind == ui.KindImage && n.Image != nil)
	})
	if icon == nil || icon.Kind != ui.KindIcon || icon.Icon != "shield" {
		t.Fatalf("unavailable app icon node = %+v, want shield fallback", icon)
	}
}

func TestPolkitPromptOmitsIdentityChooserForSingleIdentity(t *testing.T) {
	identity := polkit.Identity{Kind: "unix-user", Name: "alice", Values: map[string]string{"uid": "1000"}}
	h := &polkitHost{
		request_:   polkit.Request{Identities: []polkit.Identity{identity}},
		identities: []polkit.Identity{identity}, phase: polkitChoosingIdentity,
	}
	h.rebuild()
	if h.nodes["identity"] != nil {
		t.Fatal("single identity was shown as a chooser")
	}
}

func TestPolkitSelectionPrefersCurrentUser(t *testing.T) {
	identities := []polkit.Identity{
		{Kind: "unix-user", Name: "alice", Values: map[string]string{"uid": "1000"}},
		{Kind: "unix-user", Name: "bob", Values: map[string]string{"uid": "1001"}},
	}
	if got := polkitSelectedIdentity(identities, "1001"); got != 1 {
		t.Fatalf("selected identity index = %d, want current user at 1", got)
	}
	if got := polkitSelectedIdentity(identities, "1002"); got != 0 {
		t.Fatalf("selected identity index = %d, want first offered identity", got)
	}
}

func TestPolkitRetryMarksPasswordFieldUntilEdited(t *testing.T) {
	h := &polkitHost{open_: true, phase: polkitAwaitingPrompt}
	h.setPromptLocked(polkit.Prompt{Text: "Authentication failed. Try again."})
	h.setPromptLocked(polkit.Prompt{Text: "Password:", Secret: true})
	if node := h.nodes["password"]; node == nil || node.Tone != ui.ToneError {
		t.Fatalf("retry password field = %+v, want error tone", node)
	}
	h.handleLocked(wayland.Event{Kind: wayland.EventKeyPress, Sym: 'x', Text: "x"})
	if node := h.nodes["password"]; node == nil || node.Tone == ui.ToneError {
		t.Fatalf("edited password field = %+v, want normal tone", node)
	}
}

func TestPolkitDetailsIncludeActionAndRequestingProgram(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	h := &polkitHost{request_: polkit.Request{
		ActionID: "org.example.Test",
		Details:  map[string]string{"polkit.caller-pid": strconv.Itoa(os.Getpid()), "reason": "unit test"},
	}}
	lines := h.detailLines()
	if !slices.Contains(lines, "Action ID: org.example.Test") {
		t.Fatalf("details %v omit action id", lines)
	}
	if !slices.Contains(lines, "Program: "+filepath.Base(executable)) {
		t.Fatalf("details %v omit requesting program %q", lines, filepath.Base(executable))
	}
}

func TestPolkitPasswordPasteShortcutRequestsClipboard(t *testing.T) {
	r := &Registry{selections: make(chan wayland.SelectionRequest, 1)}
	h := &polkitHost{
		r: r, open_: true, phase: polkitEnteringResponse,
		field: ui.NewField(""), focus: "password",
	}
	if !h.handleLocked(wayland.Event{Kind: wayland.EventKeyPress, Sym: 'v', Text: "v", Mods: ui.ModCtrl, Serial: 41}) {
		t.Fatal("Ctrl+V was not handled by the password field")
	}
	select {
	case request := <-r.selections:
		if !request.Paste || request.Serial != 41 {
			t.Fatalf("clipboard request = %+v, want paste with serial 41", request)
		}
	default:
		t.Fatal("Ctrl+V did not request clipboard paste")
	}
}

func TestPolkitPromptWaitsUntilAnOutputIsAvailable(t *testing.T) {
	r := NewRegistry(config.Default())
	defer r.Close()
	agent := polkit.New(polkit.Options{})
	r.mu.Lock()
	r.polkitAgent = agent
	r.mu.Unlock()
	req := polkit.Request{Identities: []polkit.Identity{{Kind: "unix-user", Name: "alice"}}}
	if r.presentPolkitPrompt(agent, req) {
		t.Fatal("prompt presented without an output")
	}
	r.mu.Lock()
	open := r.polkitHost.open_
	r.mu.Unlock()
	if open {
		t.Fatal("prompt opened before an output appeared")
	}
	r.mu.Lock()
	r.bars[4] = &Bar{conn: "DP-1"}
	r.mu.Unlock()
	if !r.presentPolkitPrompt(agent, req) {
		t.Fatal("waiting prompt was not presented after an output appeared")
	}
	r.mu.Lock()
	open = r.polkitHost.open_
	r.mu.Unlock()
	if !open {
		t.Fatal("waiting prompt was not presented after an output appeared")
	}
}

func TestPolkitPromptIsNotPresentedWhileLockerRuns(t *testing.T) {
	r := NewRegistry(config.Default())
	defer r.Close()
	agent := polkit.New(polkit.Options{})
	req := polkit.Request{Cookie: "locked", Identities: []polkit.Identity{{Kind: "unix-user", Name: "alice"}}}
	r.mu.Lock()
	r.polkitAgent = agent
	r.lockerRunning = true
	r.bars[4] = &Bar{conn: "DP-1"}
	r.mu.Unlock()

	if r.presentPolkitPrompt(agent, req) {
		t.Fatal("prompt was presented while the session locker was running")
	}
	r.mu.Lock()
	open := r.polkitHost.open_
	r.lockerRunning = false
	r.polkitHoldLocked(false)
	r.mu.Unlock()
	if open {
		t.Fatal("prompt host opened while the session locker was running")
	}
	select {
	case <-r.polkitOutputEvents:
	default:
		t.Fatal("unlock did not wake a prompt waiting for an output")
	}
	if !r.presentPolkitPrompt(agent, req) {
		t.Fatal("waiting prompt was not presented after unlock")
	}
}

func TestPolkitPromptSuspendsAndResumesAcrossLocker(t *testing.T) {
	r := NewRegistry(config.Default())
	defer r.Close()
	agent := polkit.New(polkit.Options{})
	req := polkit.Request{Cookie: "suspend", Identities: []polkit.Identity{{Kind: "unix-user", Name: "alice"}}}
	r.mu.Lock()
	r.polkitAgent = agent
	r.polkitHost.openLocked(req, 4, "DP-1", 0)
	r.polkitHost.phase = polkitEnteringResponse
	r.polkitHost.field = ui.NewField("secret")
	r.lockerRunning = true
	r.polkitHoldLocked(true)
	suspended := r.polkitHost.open_ && r.polkitHost.closed && r.polkitHost.field.Text == ""
	r.lockerRunning = false
	r.polkitHoldLocked(false)
	resumed := r.polkitHost.open_ && !r.polkitHost.closed
	r.mu.Unlock()
	if !suspended {
		t.Fatal("prompt was left visible or retained its response while the locker ran")
	}
	if !resumed {
		t.Fatal("active prompt did not resume after the locker stopped")
	}
}

func TestPolkitCancellationToastNamesRequestingProgram(t *testing.T) {
	r := NewRegistry(config.Default())
	defer r.Close()
	recorder := &pluginToastRecorder{}
	r.BindNotifications(recorder)
	r.showPolkitCancellation(polkit.CancelledRequest{Details: map[string]string{"polkit.caller-pid": strconv.Itoa(os.Getpid())}})
	commands := recorder.commands()
	if len(commands) != 1 || commands[0].Producer == nil {
		t.Fatalf("cancellation toast commands = %+v", commands)
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	want := "Authentication request from " + filepath.Base(executable) + " was cancelled"
	if commands[0].Producer.Body != want {
		t.Fatalf("cancellation toast body = %q, want %q", commands[0].Producer.Body, want)
	}
}

func TestPolkitCompetingAgentToastNamesHolder(t *testing.T) {
	r := NewRegistry(config.Default())
	defer r.Close()
	recorder := &pluginToastRecorder{}
	r.BindNotifications(recorder)
	r.showPolkitAgentNotice("polkit-gnome-authentication-agent-1")
	commands := recorder.commands()
	if len(commands) != 1 || commands[0].Producer == nil {
		t.Fatalf("competing agent toast commands = %+v", commands)
	}
	if !strings.Contains(commands[0].Producer.Body, "polkit-gnome-authentication-agent-1") {
		t.Fatalf("competing agent toast body = %q", commands[0].Producer.Body)
	}
}
