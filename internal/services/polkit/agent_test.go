package polkit

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/godbus/dbus/v5"
)

type testSubject struct {
	Kind   string
	Values map[string]dbus.Variant
}

type testAuthority struct {
	registered   chan struct{}
	unregistered chan struct{}
}

func (s *testAuthority) RegisterAuthenticationAgentWithOptions(_ testSubject, _ string, _ dbus.ObjectPath, _ map[string]dbus.Variant) *dbus.Error {
	select {
	case s.registered <- struct{}{}:
	default:
	}
	return nil
}

func (s *testAuthority) UnregisterAuthenticationAgent(_ testSubject, _ dbus.ObjectPath) *dbus.Error {
	select {
	case s.unregistered <- struct{}{}:
	default:
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
