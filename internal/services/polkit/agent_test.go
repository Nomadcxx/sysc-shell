package polkit

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/godbus/dbus/v5"
)

func TestAgentDoesNotConnectOrRegisterWithoutHelper(t *testing.T) {
	if got := ErrNoHelper.Error(); got != "polkit authentication helper not found" {
		t.Fatalf("missing-helper message = %q", got)
	}
	dir := t.TempDir()
	busCalled := false
	agent := New(Options{
		Policy: PolicyAuto,
		Bus: func() (*dbus.Conn, error) {
			busCalled = true
			return nil, errors.New("system bus should not be used")
		},
		Helper: HelperSession{SocketPath: dir + "/missing.sock", BinaryPath: dir + "/missing-helper"},
	})
	if err := agent.Run(context.Background()); err != nil {
		t.Fatalf("Run without helper: %v", err)
	}
	if busCalled {
		t.Fatal("agent connected to the bus without an authentication helper")
	}
	if got := agent.Status().Reason; got != ErrNoHelper.Error() {
		t.Fatalf("status reason = %q, want %q", got, ErrNoHelper)
	}
}

func TestAgentRunRetriesAfterHelperFailure(t *testing.T) {
	dir := t.TempDir()
	listener, err := net.Listen("unix", filepath.Join(dir, "helper.sock"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	answers := make(chan []string, 1)
	serverErr := make(chan error, 1)
	go func() {
		got := make([]string, 0, 2)
		for _, result := range []string{tagFailure, tagSuccess} {
			conn, err := listener.Accept()
			if err != nil {
				serverErr <- err
				return
			}
			reader := bufio.NewReader(conn)
			if _, err = reader.ReadString('\n'); err != nil {
				serverErr <- err
				_ = conn.Close()
				return
			}
			if _, err = reader.ReadString('\n'); err != nil {
				serverErr <- err
				_ = conn.Close()
				return
			}
			_, _ = fmt.Fprintln(conn, tagEchoOff+" Password:")
			answer, readErr := reader.ReadString('\n')
			if readErr != nil {
				serverErr <- readErr
				_ = conn.Close()
				return
			}
			got = append(got, strings.TrimSuffix(answer, "\n"))
			_, _ = fmt.Fprintln(conn, result)
			_ = conn.Close()
		}
		answers <- got
	}()

	uid := fmt.Sprint(os.Getuid())
	identity := Identity{Kind: "unix-user", Name: usernameOf(uid), Values: map[string]string{"uid": uid}}
	req := newRequest()
	req.Cookie = "retry-cookie"
	req.Identities = []Identity{identity}
	if !req.Start(identity) {
		t.Fatal("identity was not accepted")
	}
	agent := New(Options{Helper: HelperSession{SocketPath: listener.Addr().String()}})
	result := make(chan *dbus.Error, 1)
	go func() { result <- agent.run(req) }()

	var prompt Prompt
	select {
	case prompt = <-req.Prompts():
	case err := <-serverErr:
		t.Fatalf("helper server before first prompt: %v", err)
	case <-time.After(time.Second):
		t.Fatal("first authentication prompt did not arrive")
	}
	if prompt != (Prompt{Text: "Password:", Secret: true}) {
		t.Fatalf("first prompt = %+v", prompt)
	}
	req.Submit("wrong")
	select {
	case prompt = <-req.Prompts():
	case err := <-result:
		t.Fatalf("authentication ended after wrong response: %v", err)
	case <-time.After(time.Second):
		t.Fatal("failure message did not arrive")
	}
	if prompt != (Prompt{Text: "Authentication failed. Try again."}) {
		t.Fatalf("failure prompt = %+v", prompt)
	}
	req.Submit("")
	if prompt := receivePrompt(t, req); prompt != (Prompt{Text: "Password:", Secret: true}) {
		t.Fatalf("retry prompt = %+v", prompt)
	}
	req.Submit("correct")
	select {
	case err := <-result:
		if err != nil {
			t.Fatalf("agent run: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("authentication session did not finish after retry")
	}
	select {
	case got := <-answers:
		if fmt.Sprint(got) != "[wrong correct]" {
			t.Fatalf("helper answers = %v", got)
		}
	case err := <-serverErr:
		t.Fatalf("helper server: %v", err)
	case <-time.After(time.Second):
		t.Fatal("helper did not receive both responses")
	}
}

func TestAgentRunStopsAfterThreeHelperFailures(t *testing.T) {
	dir := t.TempDir()
	listener, err := net.Listen("unix", filepath.Join(dir, "helper.sock"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	serverErr := make(chan error, 1)
	go func() {
		for range 3 {
			conn, err := listener.Accept()
			if err != nil {
				serverErr <- err
				return
			}
			reader := bufio.NewReader(conn)
			for range 2 {
				if _, err := reader.ReadString('\n'); err != nil {
					serverErr <- err
					_ = conn.Close()
					return
				}
			}
			_, _ = fmt.Fprintln(conn, tagEchoOff+" Password:")
			if _, err := reader.ReadString('\n'); err != nil {
				serverErr <- err
				_ = conn.Close()
				return
			}
			_, _ = fmt.Fprintln(conn, tagFailure)
			_ = conn.Close()
		}
		serverErr <- nil
	}()

	uid := fmt.Sprint(os.Getuid())
	identity := Identity{Kind: "unix-user", Name: usernameOf(uid), Values: map[string]string{"uid": uid}}
	req := newRequest()
	req.Cookie = "bounded-retry-cookie"
	req.Identities = []Identity{identity}
	if !req.Start(identity) {
		t.Fatal("identity was not accepted")
	}
	agent := New(Options{Helper: HelperSession{SocketPath: listener.Addr().String()}})
	result := make(chan *dbus.Error, 1)
	go func() { result <- agent.run(req) }()

	for attempt := range 3 {
		if prompt := receivePrompt(t, req); prompt != (Prompt{Text: "Password:", Secret: true}) {
			t.Fatalf("attempt %d prompt = %+v", attempt+1, prompt)
		}
		if !req.Submit("bad") {
			t.Fatalf("attempt %d response was rejected", attempt+1)
		}
		if attempt < 2 {
			if prompt := receivePrompt(t, req); prompt.Text == "" {
				t.Fatalf("attempt %d got an empty failure prompt", attempt+1)
			}
			if !req.Submit("") {
				t.Fatalf("attempt %d retry was rejected", attempt+1)
			}
		}
	}
	select {
	case err := <-result:
		if err == nil {
			t.Fatal("three helper failures returned success")
		}
	case <-time.After(time.Second):
		t.Fatal("agent continued retrying after three helper failures")
	}
	if err := <-serverErr; err != nil {
		t.Fatalf("helper server: %v", err)
	}
}

func TestAgentDefaultBusDoesNotShareSystemBusConnection(t *testing.T) {
	shared, err := dbus.SystemBus()
	if err != nil {
		t.Skipf("system bus unavailable: %v", err)
	}
	agent := New(Options{})
	owned, err := agent.opts.Bus()
	if err != nil {
		t.Fatal(err)
	}
	if owned == shared {
		t.Fatal("agent default bus is the shared godbus connection")
	}
	if err := owned.Close(); err != nil {
		t.Fatal(err)
	}
	call := shared.Object("org.freedesktop.DBus", "/org/freedesktop/DBus").Call("org.freedesktop.DBus.GetId", 0)
	if call.Err != nil {
		t.Fatalf("closing agent bus closed shared system bus: %v", call.Err)
	}
}

func receivePrompt(t *testing.T, req Request) Prompt {
	t.Helper()
	select {
	case prompt := <-req.Prompts():
		return prompt
	case <-time.After(time.Second):
		t.Fatal("authentication prompt did not arrive")
		return Prompt{}
	}
}

type testSubject struct {
	Kind   string
	Values map[string]dbus.Variant
}

type testAuthority struct {
	registered   chan struct{}
	unregistered chan struct{}
	registerErr  *dbus.Error
	onRegister   func()
	onUnregister func()
}

func (s *testAuthority) RegisterAuthenticationAgentWithOptions(_ testSubject, _ string, _ dbus.ObjectPath, _ map[string]dbus.Variant) *dbus.Error {
	if s.registerErr != nil {
		return s.registerErr
	}
	if s.onRegister != nil {
		s.onRegister()
	}
	select {
	case s.registered <- struct{}{}:
	default:
	}
	return nil
}

func TestAgentReportsExistingAuthenticationAgent(t *testing.T) {
	startPrivatePolkitBus(t)
	owner, err := dbus.ConnectSessionBus()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = owner.Close() })
	if reply, err := owner.RequestName(authorityName, dbus.NameFlagDoNotQueue); err != nil || reply != dbus.RequestNameReplyPrimaryOwner {
		t.Fatalf("authority name: reply=%v err=%v", reply, err)
	}
	authority := &testAuthority{
		registered:   make(chan struct{}, 1),
		unregistered: make(chan struct{}, 1),
		registerErr:  dbus.NewError(errFailed, []any{alreadyExistsText}),
	}
	if err := owner.Export(authority, authorityPath, authorityIface); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	socketPath := filepath.Join(dir, "helper.sock")
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	procRoot := filepath.Join(dir, "proc")
	proc := filepath.Join(procRoot, "12345")
	if err := os.MkdirAll(proc, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(proc, "cmdline"), []byte("/usr/bin/polkit-gnome-authentication-agent-1\x00--replace"), 0o600); err != nil {
		t.Fatal(err)
	}
	connReady := make(chan struct{}, 1)
	agent := New(Options{
		Policy:   PolicyAuto,
		ProcRoot: procRoot,
		Bus: func() (*dbus.Conn, error) {
			conn, err := dbus.ConnectSessionBus()
			connReady <- struct{}{}
			return conn, err
		},
		SessionID: func() (string, error) { return "test-session", nil },
		Helper:    HelperSession{SocketPath: socketPath},
	})
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() { result <- agent.Run(ctx) }()
	select {
	case <-connReady:
	case <-time.After(5 * time.Second):
		t.Fatal("agent did not connect to the private bus")
	}
	deadline := time.Now().Add(5 * time.Second)
	for agent.Status().Passive == "" && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if got := agent.Status(); got.Registered || got.Passive != "polkit-gnome-authentication-agent-1" {
		t.Fatalf("passive status = %+v", got)
	}
	select {
	case notice := <-agent.Notice():
		if notice != "polkit-gnome-authentication-agent-1" {
			t.Fatalf("notice = %q", notice)
		}
	case <-time.After(time.Second):
		t.Fatal("existing agent was not reported")
	}
	cancel()
	select {
	case err := <-result:
		if err != nil {
			t.Fatalf("Agent.Run: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("agent did not stop")
	}
}

func TestAgentPolicyOnKeepsSuccessfulRegistration(t *testing.T) {
	startPrivatePolkitBus(t)
	owner, err := dbus.ConnectSessionBus()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = owner.Close() })
	if reply, err := owner.RequestName(authorityName, dbus.NameFlagDoNotQueue); err != nil || reply != dbus.RequestNameReplyPrimaryOwner {
		t.Fatalf("authority name: reply=%v err=%v", reply, err)
	}
	authority := &testAuthority{registered: make(chan struct{}, 2), unregistered: make(chan struct{}, 1)}
	if err := owner.Export(authority, authorityPath, authorityIface); err != nil {
		t.Fatal(err)
	}
	caller, err := dbus.ConnectSessionBus()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = caller.Close() })

	agent := New(Options{Policy: PolicyOn})
	agent.register(context.Background(), caller, "test-session")
	agent.register(context.Background(), caller, "test-session")
	if got := len(authority.registered); got != 1 {
		t.Fatalf("registration calls = %d, want 1 after success", got)
	}
	if !agent.Status().Registered {
		t.Fatalf("status after successful registration = %+v", agent.Status())
	}
}

func TestAgentStopsWhenRegistrationReplyStalls(t *testing.T) {
	startPrivatePolkitBus(t)
	owner, err := dbus.ConnectSessionBus()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = owner.Close() })
	if reply, err := owner.RequestName(authorityName, dbus.NameFlagDoNotQueue); err != nil || reply != dbus.RequestNameReplyPrimaryOwner {
		t.Fatalf("authority name: reply=%v err=%v", reply, err)
	}
	started := make(chan struct{}, 1)
	release := make(chan struct{})
	released := false
	defer func() {
		if !released {
			close(release)
		}
	}()
	authority := &testAuthority{
		registered: make(chan struct{}, 1), unregistered: make(chan struct{}, 1),
		onRegister: func() {
			started <- struct{}{}
			<-release
		},
	}
	if err := owner.Export(authority, authorityPath, authorityIface); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	listener, err := net.Listen("unix", filepath.Join(dir, "helper.sock"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	agent := New(Options{
		Policy:    PolicyAuto,
		Bus:       func() (*dbus.Conn, error) { return dbus.ConnectSessionBus() },
		SessionID: func() (string, error) { return "test-session", nil },
		Helper:    HelperSession{SocketPath: listener.Addr().String()},
	})
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() { result <- agent.Run(ctx) }()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		close(release)
		released = true
		cancel()
		t.Fatal("registration call did not reach the authority")
	}
	cancel()
	select {
	case err := <-result:
		if err != nil {
			t.Fatalf("Agent.Run: %v", err)
		}
	case <-time.After(time.Second):
		close(release)
		released = true
		select {
		case <-result:
		case <-time.After(5 * time.Second):
			t.Fatal("agent did not stop after releasing the stalled authority")
		}
		t.Fatal("agent shutdown waited for the stalled registration reply")
	}
	close(release)
	released = true
}

func TestAgentClosesQueueWhenContextEndsBeforeConnection(t *testing.T) {
	listener, err := net.Listen("unix", filepath.Join(t.TempDir(), "helper.sock"))
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	ctx, cancel := context.WithCancel(context.Background())
	agent := New(Options{
		Policy: PolicyAuto,
		Bus: func() (*dbus.Conn, error) {
			cancel()
			return nil, errors.New("bus disconnected")
		},
		Helper: HelperSession{SocketPath: listener.Addr().String()},
	})
	active := newTestRequest("active-before-connect-failure")
	if _, err := agent.queue.push(active); err != nil {
		t.Fatalf("queue active request: %v", err)
	}
	if err := agent.Run(ctx); err != nil {
		t.Fatalf("Agent.Run: %v", err)
	}
	select {
	case <-active.Done():
	default:
		t.Fatal("active request survived final shutdown")
	}
	if _, err := agent.queue.push(newTestRequest("late-after-connect-failure")); err != errQueueClosed {
		t.Fatalf("late push after final shutdown = %v, want %v", err, errQueueClosed)
	}
}

func TestAgentClosesQueueWhenContextEndsDuringReconnectBackoff(t *testing.T) {
	listener, err := net.Listen("unix", filepath.Join(t.TempDir(), "helper.sock"))
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	agent := New(Options{
		Policy: PolicyAuto,
		Bus:    func() (*dbus.Conn, error) { return nil, errors.New("bus disconnected") },
		Helper: HelperSession{SocketPath: listener.Addr().String()},
		Logf:   func(string, ...any) { cancel() },
	})
	if err := agent.Run(ctx); err != nil {
		t.Fatalf("Agent.Run: %v", err)
	}
	if _, err := agent.queue.push(newTestRequest("late-during-shutdown")); err != errQueueClosed {
		t.Fatalf("late push after final shutdown = %v, want %v", err, errQueueClosed)
	}
}

func TestAgentRejectsAuthenticationDuringShutdown(t *testing.T) {
	startPrivatePolkitBus(t)
	owner, err := dbus.ConnectSessionBus()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = owner.Close() })
	if reply, err := owner.RequestName(authorityName, dbus.NameFlagDoNotQueue); err != nil || reply != dbus.RequestNameReplyPrimaryOwner {
		t.Fatalf("authority name: reply=%v err=%v", reply, err)
	}
	releaseUnregister := make(chan struct{})
	registeredRelease := false
	defer func() {
		if !registeredRelease {
			close(releaseUnregister)
		}
	}()
	authority := &testAuthority{
		registered: make(chan struct{}, 1), unregistered: make(chan struct{}, 1),
		onUnregister: func() { <-releaseUnregister },
	}
	if err := owner.Export(authority, authorityPath, authorityIface); err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("unix", filepath.Join(t.TempDir(), "helper.sock"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	agent := New(Options{
		Policy:    PolicyAuto,
		Bus:       func() (*dbus.Conn, error) { return dbus.ConnectSessionBus() },
		SessionID: func() (string, error) { return "test-session", nil },
		Helper:    HelperSession{SocketPath: listener.Addr().String()},
	})
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() { result <- agent.Run(ctx) }()
	select {
	case <-authority.registered:
	case <-time.After(5 * time.Second):
		cancel()
		t.Fatal("agent did not register")
	}
	active := newTestRequest("active-before-shutdown")
	if _, err := agent.queue.push(active); err != nil {
		t.Fatalf("queue active request: %v", err)
	}
	if got := <-agent.Requests(); got.Cookie != active.Cookie {
		t.Fatalf("active request = %q, want %q", got.Cookie, active.Cookie)
	}
	cancel()
	select {
	case <-authority.unregistered:
	case <-time.After(5 * time.Second):
		t.Fatal("agent did not begin unregistering")
	}
	select {
	case <-active.Done():
	default:
		t.Fatal("active request was not cancelled before unregistering")
	}
	var sender string
	if err := owner.BusObject().Call("org.freedesktop.DBus.GetNameOwner", 0, authorityName).Store(&sender); err != nil {
		t.Fatal(err)
	}
	begin := make(chan *dbus.Error, 1)
	go func() {
		begin <- agent.BeginAuthentication(dbus.Sender(sender), "org.example.action", "Authenticate", "", nil,
			"late-shutdown-cookie", []polkitIdentity{{Kind: "unix-user", Values: map[string]dbus.Variant{"uid": dbus.MakeVariant(os.Getuid())}}})
	}()
	select {
	case callErr := <-begin:
		if callErr == nil || callErr.Name != errCancelled {
			t.Fatalf("late BeginAuthentication error = %v, want %s", callErr, errCancelled)
		}
	case req := <-agent.Requests():
		req.Cancel()
		t.Fatal("BeginAuthentication admitted a request during shutdown")
	case <-time.After(time.Second):
		t.Fatal("BeginAuthentication did not reject promptly during shutdown")
	}
	close(releaseUnregister)
	registeredRelease = true
	select {
	case err := <-result:
		if err != nil {
			t.Fatalf("Agent.Run: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("agent did not stop")
	}
}

func TestAgentReregistersWhenAuthorityChangesDuringRegistration(t *testing.T) {
	startPrivatePolkitBus(t)
	firstOwner, err := dbus.ConnectSessionBus()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = firstOwner.Close() })
	if reply, err := firstOwner.RequestName(authorityName, dbus.NameFlagDoNotQueue); err != nil || reply != dbus.RequestNameReplyPrimaryOwner {
		t.Fatalf("first authority name: reply=%v err=%v", reply, err)
	}
	secondOwner, err := dbus.ConnectSessionBus()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = secondOwner.Close() })
	secondAuthority := &testAuthority{registered: make(chan struct{}, 1), unregistered: make(chan struct{}, 1)}
	if err := secondOwner.Export(secondAuthority, authorityPath, authorityIface); err != nil {
		t.Fatal(err)
	}
	changeRequested := make(chan struct{}, 1)
	finishRegistration := make(chan struct{}, 1)
	defer func() {
		select {
		case finishRegistration <- struct{}{}:
		default:
		}
	}()
	firstAuthority := &testAuthority{
		registered: make(chan struct{}, 1), unregistered: make(chan struct{}, 1),
		onRegister: func() {
			changeRequested <- struct{}{}
			<-finishRegistration
		},
	}
	if err := firstOwner.Export(firstAuthority, authorityPath, authorityIface); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	listener, err := net.Listen("unix", filepath.Join(dir, "helper.sock"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	connections := make(chan *dbus.Conn, 1)
	agent := New(Options{
		Policy: PolicyAuto,
		Bus: func() (*dbus.Conn, error) {
			conn, err := dbus.ConnectSessionBus()
			if err == nil {
				connections <- conn
			}
			return conn, err
		},
		SessionID: func() (string, error) { return "test-session", nil },
		Helper:    HelperSession{SocketPath: listener.Addr().String()},
	})
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() { result <- agent.Run(ctx) }()
	select {
	case <-connections:
	case <-time.After(5 * time.Second):
		t.Fatal("agent did not connect to the private bus")
	}
	select {
	case <-changeRequested:
	case <-time.After(5 * time.Second):
		t.Fatal("authority registration did not reach the first owner")
	}
	if _, err := firstOwner.ReleaseName(authorityName); err != nil {
		t.Fatalf("release first authority name: %v", err)
	}
	if reply, err := secondOwner.RequestName(authorityName, dbus.NameFlagDoNotQueue); err != nil || reply != dbus.RequestNameReplyPrimaryOwner {
		t.Fatalf("replacement authority name: reply=%v err=%v", reply, err)
	}
	finishRegistration <- struct{}{}
	select {
	case <-firstAuthority.registered:
	case <-time.After(5 * time.Second):
		t.Fatal("first authority did not finish registration")
	}
	select {
	case <-secondAuthority.registered:
	case <-time.After(5 * time.Second):
		cancel()
		t.Fatalf("agent did not register with the replacement authority; status=%+v", agent.Status())
	}
	deadline := time.Now().Add(5 * time.Second)
	for !agent.Status().Registered && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if !agent.Status().Registered {
		t.Fatalf("status after authority replacement = %+v", agent.Status())
	}
	cancel()
	select {
	case err := <-result:
		if err != nil {
			t.Fatalf("Agent.Run: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("agent did not stop")
	}
}

func TestAgentIgnoresForgedAuthorityOwnerChange(t *testing.T) {
	startPrivatePolkitBus(t)
	authorityConn, err := dbus.ConnectSessionBus()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = authorityConn.Close() })
	if reply, err := authorityConn.RequestName(authorityName, dbus.NameFlagDoNotQueue); err != nil || reply != dbus.RequestNameReplyPrimaryOwner {
		t.Fatalf("authority name: reply=%v err=%v", reply, err)
	}
	authority := &testAuthority{registered: make(chan struct{}, 1), unregistered: make(chan struct{}, 1)}
	if err := authorityConn.Export(authority, authorityPath, authorityIface); err != nil {
		t.Fatal(err)
	}

	attacker, err := dbus.ConnectSessionBus()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = attacker.Close() })
	listener, err := net.Listen("unix", filepath.Join(t.TempDir(), "helper.sock"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	agent := New(Options{
		Policy:    PolicyAuto,
		Bus:       func() (*dbus.Conn, error) { return dbus.ConnectSessionBus() },
		SessionID: func() (string, error) { return "test-session", nil },
		Helper:    HelperSession{SocketPath: listener.Addr().String()},
	})
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() { result <- agent.Run(ctx) }()
	t.Cleanup(func() {
		cancel()
		select {
		case err := <-result:
			if err != nil {
				t.Errorf("Agent.Run: %v", err)
			}
		case <-time.After(5 * time.Second):
			t.Error("agent did not stop")
		}
	})
	select {
	case <-authority.registered:
	case <-time.After(5 * time.Second):
		t.Fatal("agent did not register")
	}
	deadline := time.Now().Add(5 * time.Second)
	for !agent.Status().Registered && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if !agent.Status().Registered {
		t.Fatalf("agent status after registration = %+v", agent.Status())
	}
	var owner string
	if err := authorityConn.BusObject().Call("org.freedesktop.DBus.GetNameOwner", 0, authorityName).Store(&owner); err != nil {
		t.Fatal(err)
	}

	request := newTestRequest("forged-owner-change")
	if _, err := agent.queue.push(request); err != nil {
		t.Fatalf("queue request: %v", err)
	}
	select {
	case <-agent.Requests():
	case <-time.After(time.Second):
		t.Fatal("request did not reach the active slot")
	}
	if err := attacker.Emit(dbus.ObjectPath("/org/freedesktop/DBus"), "org.freedesktop.DBus.NameOwnerChanged",
		authorityName, owner, ":1.999"); err != nil {
		t.Fatalf("emit forged owner change: %v", err)
	}

	deadline = time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if !agent.Status().Registered {
			t.Fatalf("forged signal changed registration status: %+v", agent.Status())
		}
		select {
		case <-request.Done():
			t.Fatal("forged signal cancelled the active authentication request")
		default:
		}
		time.Sleep(time.Millisecond)
	}
}

func TestAgentIgnoresOwnerChangeBufferedBeforeRegistration(t *testing.T) {
	agent := New(Options{Policy: PolicyAuto})
	agent.mu.Lock()
	agent.status = Status{Policy: PolicyAuto, Registered: true}
	agent.registeredOwner = ":1.9"
	agent.mu.Unlock()
	req := newTestRequest("active")
	if _, err := agent.queue.push(req); err != nil {
		t.Fatal(err)
	}
	select {
	case <-agent.Requests():
	case <-time.After(time.Second):
		t.Fatal("request did not reach the display channel")
	}

	if agent.handleAuthorityOwnerChange(":1.2", ":1.9") {
		t.Fatal("an owner change ending at the registered authority invalidated the registration")
	}
	if status := agent.Status(); !status.Registered {
		t.Fatalf("status after stale owner change = %+v, want registered", status)
	}
	select {
	case <-req.Done():
		t.Fatal("stale owner change cancelled an active request")
	default:
	}

	if !agent.handleAuthorityOwnerChange(":1.9", ":1.10") {
		t.Fatal("change from the registered authority owner was ignored")
	}
	if status := agent.Status(); status.Registered {
		t.Fatalf("status after registered owner disappeared = %+v", status)
	}
	select {
	case <-req.Done():
	default:
		t.Fatal("real owner change did not cancel the active request")
	}
}

func TestAgentReconnectsWhenSystemBusCloses(t *testing.T) {
	startPrivatePolkitBus(t)
	owner, err := dbus.ConnectSessionBus()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = owner.Close() })
	if reply, err := owner.RequestName(authorityName, dbus.NameFlagDoNotQueue); err != nil || reply != dbus.RequestNameReplyPrimaryOwner {
		t.Fatalf("authority name: reply=%v err=%v", reply, err)
	}
	authority := &testAuthority{registered: make(chan struct{}, 2), unregistered: make(chan struct{}, 1)}
	if err := owner.Export(authority, authorityPath, authorityIface); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	listener, err := net.Listen("unix", filepath.Join(dir, "helper.sock"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	connections := make(chan *dbus.Conn, 2)
	agent := New(Options{
		Policy: PolicyAuto,
		Bus: func() (*dbus.Conn, error) {
			conn, err := dbus.ConnectSessionBus()
			if err == nil {
				connections <- conn
			}
			return conn, err
		},
		SessionID: func() (string, error) { return "test-session", nil },
		Helper:    HelperSession{SocketPath: listener.Addr().String()},
	})
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() { result <- agent.Run(ctx) }()
	var first *dbus.Conn
	select {
	case first = <-connections:
	case <-time.After(5 * time.Second):
		t.Fatal("agent did not connect to the private bus")
	}
	select {
	case <-authority.registered:
	case <-time.After(5 * time.Second):
		t.Fatal("agent did not register initially")
	}
	_ = first.Close()
	var second *dbus.Conn
	select {
	case second = <-connections:
	case err := <-result:
		t.Fatalf("agent stopped after bus disconnect: %v", err)
	case <-time.After(10 * time.Second):
		t.Fatal("agent did not reconnect to the private bus")
	}
	_ = second.Close()
	select {
	case <-authority.registered:
	case <-time.After(5 * time.Second):
		t.Fatal("agent did not register again after reconnecting")
	}
	cancel()
	select {
	case err := <-result:
		if err != nil {
			t.Fatalf("Agent.Run: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("agent did not stop")
	}
}

func (s *testAuthority) UnregisterAuthenticationAgent(_ testSubject, _ dbus.ObjectPath) *dbus.Error {
	select {
	case s.unregistered <- struct{}{}:
	default:
	}
	if s.onUnregister != nil {
		s.onUnregister()
	}
	return nil
}

func TestAgentBeginAuthenticationUsesPolkitIdentityAndHelper(t *testing.T) {
	startPrivatePolkitBus(t)
	owner, err := dbus.ConnectSessionBus()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = owner.Close() })
	if reply, err := owner.RequestName(authorityName, dbus.NameFlagDoNotQueue); err != nil || reply != dbus.RequestNameReplyPrimaryOwner {
		t.Fatalf("authority name: reply=%v err=%v", reply, err)
	}
	authority := &testAuthority{registered: make(chan struct{}, 1), unregistered: make(chan struct{}, 1)}
	if err := owner.Export(authority, authorityPath, authorityIface); err != nil {
		t.Fatal(err)
	}

	dir := t.TempDir()
	socketPath := dir + "/helper.sock"
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	helperResult := make(chan [3]string, 1)
	logs := make(chan string, 32)
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			helperResult <- [3]string{"accept", "", err.Error()}
			return
		}
		defer conn.Close()
		reader := bufio.NewReader(conn)
		username, _ := reader.ReadString('\n')
		cookie, _ := reader.ReadString('\n')
		_, _ = fmt.Fprintln(conn, tagEchoOff+" Password:")
		answer, _ := reader.ReadString('\n')
		_, _ = fmt.Fprintln(conn, tagSuccess)
		helperResult <- [3]string{strings.TrimSuffix(username, "\n"), strings.TrimSuffix(cookie, "\n"), strings.TrimSuffix(answer, "\n")}
	}()

	connReady := make(chan *dbus.Conn, 1)
	agent := New(Options{
		Policy:    PolicyAuto,
		Logf:      func(format string, args ...any) { logs <- fmt.Sprintf(format, args...) },
		Bus:       func() (*dbus.Conn, error) { c, err := dbus.ConnectSessionBus(); connReady <- c; return c, err },
		SessionID: func() (string, error) { return "test-session", nil },
		Helper:    HelperSession{SocketPath: socketPath, BinaryPath: dir + "/missing-helper"},
	})
	ctx, cancel := context.WithCancel(context.Background())
	runResult := make(chan error, 1)
	go func() { runResult <- agent.Run(ctx) }()
	var agentConn *dbus.Conn
	select {
	case agentConn = <-connReady:
	case <-time.After(5 * time.Second):
		t.Fatal("agent did not connect to the private bus")
	}
	defer func() {
		cancel()
		select {
		case err := <-runResult:
			if err != nil {
				t.Errorf("Agent.Run: %v", err)
			}
		case <-time.After(5 * time.Second):
			t.Error("Agent.Run did not stop")
		}
	}()
	select {
	case <-authority.registered:
	case <-time.After(5 * time.Second):
		t.Fatalf("agent did not register: %+v", agent.Status())
	}

	client, err := dbus.ConnectSessionBus()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })
	unauthorized := client.Object(agentConn.Names()[0], agentPath).Call(agentIface+".BeginAuthentication", 0,
		"org.example.Test", "Authentication required", "dialog-password",
		map[string]string{"reason": "test"}, "fake-cookie", []polkitIdentity{{Kind: "unix-user", Values: map[string]dbus.Variant{"uid": dbus.MakeVariant(uint32(os.Getuid()))}}})
	if unauthorized.Err == nil {
		t.Fatal("a non-polkit caller created an authentication prompt")
	}
	if got := agent.Waiting(); got != 0 {
		t.Fatalf("unauthorized request left %d prompts queued", got)
	}
	if err := client.Object(agentConn.Names()[0], agentPath).Call(agentIface+".CancelAuthentication", 0, "fake-cookie").Err; err == nil {
		t.Fatal("a non-polkit caller cancelled an authentication prompt")
	}
	callResult := make(chan *dbus.Call, 1)
	identities := []polkitIdentity{{Kind: "unix-user", Values: map[string]dbus.Variant{"uid": dbus.MakeVariant(uint32(os.Getuid()))}}}
	go func() {
		callResult <- owner.Object(agentConn.Names()[0], agentPath).Call(agentIface+".BeginAuthentication", 0,
			"org.example.Test", "Authentication required", "dialog-password",
			map[string]string{"reason": "test"}, "test-cookie", identities)
	}()

	var req Request
	select {
	case req = <-agent.Requests():
	case <-time.After(5 * time.Second):
		t.Fatal("BeginAuthentication did not reach the request queue")
	}
	if req.ActionID != "org.example.Test" || req.Cookie != "test-cookie" || req.Details["reason"] != "test" {
		t.Fatalf("request = %+v", req)
	}
	identity := req.Identities[0]
	if !req.Start(identity) {
		t.Fatal("identity chooser rejected its live request")
	}
	select {
	case prompt := <-req.Prompts():
		if prompt.Text != "Password:" || !prompt.Secret {
			t.Fatalf("prompt = %+v", prompt)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("helper did not ask for the password")
	}
	if !req.Submit("polkit-test-secret") {
		t.Fatal("request rejected the password response")
	}
	select {
	case got := <-helperResult:
		if got != [3]string{usernameOf(fmt.Sprint(os.Getuid())), "test-cookie", "polkit-test-secret"} {
			t.Fatalf("helper received %q, want username, cookie, response", got)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("helper did not finish")
	}
	select {
	case call := <-callResult:
		if call.Err != nil {
			t.Fatalf("BeginAuthentication: %v", call.Err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("BeginAuthentication did not return after the helper")
	}
	select {
	case <-req.Done():
	case <-time.After(time.Second):
		t.Fatal("completed request was not retired")
	}
	if err := owner.Object(agentConn.Names()[0], agentPath).Call(agentIface+".CancelAuthentication", 0, "unknown-private-cookie").Err; err != nil {
		t.Fatalf("cancel of unknown request: %v", err)
	}
	for len(logs) > 0 {
		line := <-logs
		for _, private := range []string{"test-cookie", "unknown-private-cookie", "polkit-test-secret"} {
			if strings.Contains(line, private) {
				t.Fatalf("log contains private request data %q: %s", private, line)
			}
		}
	}

	agent.Hold(true)
	queuedResult := make(chan *dbus.Call, 1)
	go func() {
		queuedResult <- owner.Object(agentConn.Names()[0], agentPath).Call(agentIface+".BeginAuthentication", 0,
			"org.example.Queued", "Queued authentication", "dialog-password",
			map[string]string{"polkit.caller-pid": fmt.Sprint(os.Getpid())}, "queued-private-cookie", identities)
	}()
	deadline := time.Now().Add(5 * time.Second)
	for agent.Waiting() != 1 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if agent.Waiting() != 1 {
		t.Fatal("locked request did not remain queued")
	}
	if err := owner.Object(agentConn.Names()[0], agentPath).Call(agentIface+".CancelAuthentication", 0, "queued-private-cookie").Err; err != nil {
		t.Fatalf("cancel queued authentication: %v", err)
	}
	select {
	case withdrawn := <-agent.Withdrawn():
		if withdrawn.ActionID != "org.example.Queued" || withdrawn.Details["polkit.caller-pid"] != fmt.Sprint(os.Getpid()) {
			t.Fatalf("withdrawn request = %+v", withdrawn)
		}
	case <-time.After(time.Second):
		t.Fatal("queued cancellation was not reported to the shell")
	}
	select {
	case call := <-queuedResult:
		if call.Err == nil {
			t.Fatal("cancelled queued request returned success")
		}
	case <-time.After(time.Second):
		t.Fatal("queued BeginAuthentication did not return after cancellation")
	}

	cancel()
	select {
	case <-authority.unregistered:
	case <-time.After(5 * time.Second):
		t.Fatal("agent did not unregister")
	}
}

func startPrivatePolkitBus(t *testing.T) {
	t.Helper()
	command, err := exec.LookPath("dbus-daemon")
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(command, "--session", "--nofork", "--nopidfile", "--print-address=1")
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	cmd.Stderr = io.Discard
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill(); _ = cmd.Wait() })
	address := make(chan string, 1)
	go func() { line, _ := bufio.NewReader(stdout).ReadString('\n'); address <- line }()
	select {
	case value := <-address:
		if strings.TrimSpace(value) == "" {
			t.Fatal("dbus-daemon returned an empty address")
		}
		t.Setenv("DBUS_SESSION_BUS_ADDRESS", strings.TrimSpace(value))
	case <-time.After(5 * time.Second):
		t.Fatal("dbus-daemon did not report its address")
	}
}
