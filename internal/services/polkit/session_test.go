package polkit

import (
	"context"
	"testing"

	"github.com/godbus/dbus/v5"
)

type testLogind struct{}

func (testLogind) GetSessionByPID(pid uint32) (dbus.ObjectPath, *dbus.Error) {
	return dbus.ObjectPath("/org/freedesktop/login1/session/_1"), nil
}

func (testLogind) GetId() (string, *dbus.Error) { return "test-session", nil }

func TestSessionIDUsesEnvironment(t *testing.T) {
	t.Setenv("XDG_SESSION_ID", "42")
	got, err := sessionID(context.Background(), nil)
	if err != nil || got != "42" {
		t.Fatalf("sessionID() = %q, %v", got, err)
	}
}

func TestSessionIDFallsBackToLogind(t *testing.T) {
	startPrivatePolkitBus(t)
	t.Setenv("XDG_SESSION_ID", "")
	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	if reply, err := conn.RequestName(logindName, dbus.NameFlagDoNotQueue); err != nil || reply != dbus.RequestNameReplyPrimaryOwner {
		t.Fatalf("logind name: reply=%v err=%v", reply, err)
	}
	if err := conn.Export(testLogind{}, dbus.ObjectPath(logindPath), logindManager); err != nil {
		t.Fatal(err)
	}
	if err := conn.Export(testLogind{}, dbus.ObjectPath("/org/freedesktop/login1/session/_1"), logindSession); err != nil {
		t.Fatal(err)
	}
	got, err := sessionID(context.Background(), conn)
	if err != nil || got != "test-session" {
		t.Fatalf("sessionID() = %q, %v", got, err)
	}
}

func TestSubjectUsesUnixSessionStruct(t *testing.T) {
	subject := subject("42")
	if got := dbus.SignatureOf(subject).String(); got != "(sa{sv})" {
		t.Fatalf("subject signature = %q", got)
	}
	if subject.Kind != "unix-session" || subject.Values["session-id"].Value() != "42" {
		t.Fatalf("subject = %#v", subject)
	}
}
