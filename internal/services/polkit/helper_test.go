package polkit

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestHelperSetuidProtocol(t *testing.T) {
	dir := t.TempDir()
	helper, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	script := "#!/bin/sh\n" +
		"export POLKIT_HELPER_TEST_CHILD=1\n" +
		"export POLKIT_HELPER_TEST_USERNAME=\"$1\"\n" +
		"exec " + shellQuote(helper) + " -test.run='^TestHelperProcess$'\n"
	binary := filepath.Join(dir, "helper")
	if err := os.WriteFile(binary, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}

	var prompts []Prompt
	s := HelperSession{SocketPath: filepath.Join(dir, "missing.sock"), BinaryPath: binary, Username: "alice", Cookie: "cookie"}
	if outcome, err := s.Run(context.Background(), func(p Prompt) (string, error) {
		prompts = append(prompts, p)
		return "secret", nil
	}); err != nil || outcome != tagSuccess {
		t.Fatalf("Run() = %q, %v; want SUCCESS", outcome, err)
	}
	if len(prompts) != 1 || prompts[0] != (Prompt{Text: "Password:", Secret: true}) {
		t.Fatalf("prompts = %#v", prompts)
	}
}

func TestHelperEchoOnPromptIsNotMasked(t *testing.T) {
	dir := t.TempDir()
	helper, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	script := "#!/bin/sh\n" +
		"export POLKIT_HELPER_TEST_CHILD=1\n" +
		"export POLKIT_HELPER_TEST_USERNAME=alice\n" +
		"export POLKIT_HELPER_TEST_ECHO=on\n" +
		"exec " + shellQuote(helper) + " -test.run='^TestHelperProcess$'\n"
	binary := filepath.Join(dir, "helper")
	if err := os.WriteFile(binary, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}

	var got Prompt
	s := HelperSession{SocketPath: filepath.Join(dir, "missing.sock"), BinaryPath: binary, Username: "alice", Cookie: "cookie"}
	if outcome, err := s.Run(context.Background(), func(prompt Prompt) (string, error) {
		got = prompt
		return "secret", nil
	}); err != nil || outcome != tagSuccess {
		t.Fatalf("Run() = %q, %v; want SUCCESS", outcome, err)
	}
	if got != (Prompt{Text: "Password:", Secret: true, Echo: true}) {
		t.Fatalf("echo-on prompt = %+v, want visible response", got)
	}
}

func TestHelperRelaysErrorAndInfoLines(t *testing.T) {
	listener, err := net.Listen("unix", filepath.Join(t.TempDir(), "helper.sock"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	serverErr := make(chan error, 1)
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			serverErr <- err
			return
		}
		defer conn.Close()
		reader := bufio.NewReader(conn)
		for range 2 {
			if _, err := reader.ReadString('\n'); err != nil {
				serverErr <- err
				return
			}
		}
		_, _ = fmt.Fprintln(conn, `PAM_ERROR_MSG Sorry\ntry again`)
		_, _ = fmt.Fprintln(conn, tagTextInfo+" Touch the sensor")
		_, _ = fmt.Fprintln(conn, tagEchoOff+" Password:")
		answer, err := reader.ReadString('\n')
		if err != nil {
			serverErr <- err
			return
		}
		if answer != "typed\n" {
			serverErr <- fmt.Errorf("response = %q, want typed", answer)
			return
		}
		_, _ = fmt.Fprintln(conn, tagSuccess)
		serverErr <- nil
	}()

	var prompts []Prompt
	session := HelperSession{SocketPath: listener.Addr().String(), Username: "alice", Cookie: "cookie"}
	if outcome, err := session.Run(context.Background(), func(prompt Prompt) (string, error) {
		prompts = append(prompts, prompt)
		if prompt.Secret {
			return "typed", nil
		}
		return "", nil
	}); err != nil || outcome != tagSuccess {
		t.Fatalf("Run() = %q, %v; want SUCCESS", outcome, err)
	}
	want := []Prompt{
		{Text: "Sorry\ntry again"},
		{Text: "Touch the sensor"},
		{Text: "Password:", Secret: true},
	}
	if fmt.Sprint(prompts) != fmt.Sprint(want) {
		t.Fatalf("prompts = %+v, want %+v", prompts, want)
	}
	if err := <-serverErr; err != nil {
		t.Fatalf("helper server: %v", err)
	}
}

func TestHelperResponseBytesAreZeroedAfterWrite(t *testing.T) {
	var writer capturedWrites
	if err := writeResponse(&writer, "sensitive response"); err != nil {
		t.Fatal(err)
	}
	if len(writer.writes) != 2 || string(writer.writes[1]) != "\n" {
		t.Fatalf("writes = %q", writer.writes)
	}
	for _, b := range writer.writes[0] {
		if b != 0 {
			t.Fatalf("response buffer still contains data: %q", writer.writes[0])
		}
	}
}

type capturedWrites struct{ writes [][]byte }

func (w *capturedWrites) Write(p []byte) (int, error) {
	w.writes = append(w.writes, p)
	return len(p), nil
}

func TestHelperSessionStopsOnContextCancellation(t *testing.T) {
	listener, err := net.Listen("unix", filepath.Join(t.TempDir(), "helper.sock"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	serverDone := make(chan struct{})
	go func() {
		defer close(serverDone)
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		reader := bufio.NewReader(conn)
		for range 2 {
			if _, err := reader.ReadString('\n'); err != nil {
				return
			}
		}
		_, _ = fmt.Fprintln(conn, tagEchoOff+" Password:")
		_, _ = reader.ReadString('\n')
	}()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	prompted := make(chan Prompt, 1)
	result := make(chan error, 1)
	session := HelperSession{SocketPath: listener.Addr().String(), Username: "alice", Cookie: "cookie"}
	go func() {
		_, err := session.Run(ctx, func(prompt Prompt) (string, error) {
			prompted <- prompt
			<-ctx.Done()
			return "", ErrHelperCancelled
		})
		result <- err
	}()
	select {
	case got := <-prompted:
		if !got.Secret {
			t.Fatalf("prompt = %+v, want secret", got)
		}
	case <-time.After(time.Second):
		t.Fatal("helper did not prompt before cancellation")
	}
	cancel()
	select {
	case err := <-result:
		if err != ErrHelperCancelled {
			t.Fatalf("Run cancellation = %v, want %v", err, ErrHelperCancelled)
		}
	case <-time.After(time.Second):
		t.Fatal("helper session did not stop after cancellation")
	}
	select {
	case <-serverDone:
	case <-time.After(time.Second):
		t.Fatal("helper connection remained open after cancellation")
	}
}

func TestHelperSessionCancellationDuringDialReturnsCancelled(t *testing.T) {
	listener, err := net.Listen("unix", filepath.Join(t.TempDir(), "helper.sock"))
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	session := HelperSession{SocketPath: listener.Addr().String(), Username: "alice", Cookie: "cookie"}
	if _, err := session.Run(ctx, nil); err != ErrHelperCancelled {
		t.Fatalf("Run with cancelled dial = %v, want %v", err, ErrHelperCancelled)
	}
}

// TestHelperProcess is re-executed by the fake setuid helper above.
func TestHelperProcess(t *testing.T) {
	if os.Getenv("POLKIT_HELPER_TEST_CHILD") != "1" {
		return
	}
	if os.Getenv("POLKIT_HELPER_TEST_USERNAME") != "alice" {
		os.Exit(10)
	}
	in := bufio.NewReader(os.Stdin)
	cookie, err := in.ReadString('\n')
	if err != nil || cookie != "cookie\n" {
		os.Exit(11)
	}
	tag := tagEchoOff
	if os.Getenv("POLKIT_HELPER_TEST_ECHO") == "on" {
		tag = tagEchoOn
	}
	if _, err := fmt.Fprintln(os.Stdout, tag+" Password:"); err != nil {
		os.Exit(12)
	}
	answer, err := in.ReadString('\n')
	if err != nil || strings.TrimSuffix(answer, "\n") != "secret" {
		os.Exit(13)
	}
	_, _ = fmt.Fprintln(os.Stdout, tagSuccess)
	os.Exit(0)
}

func shellQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'" }
