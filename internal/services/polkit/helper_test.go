package polkit

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
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
