package main

import (
	"bytes"
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Nomadcxx/sysc-shell/internal/sockpath"
)

func TestDoctorExitStatusFollowsTheWorstFinding(t *testing.T) {
	check := func(level doctorLevel) doctorCheck {
		return doctorCheck{Subsystem: "probe", Level: level}
	}
	for _, test := range []struct {
		name   string
		checks []doctorCheck
		strict bool
		want   int
	}{
		{"all ok", []doctorCheck{check(doctorOK)}, false, 0},
		{"warning is not a failure", []doctorCheck{check(doctorOK), check(doctorWarn)}, false, 0},
		{"strict warning is a failure", []doctorCheck{check(doctorWarn)}, true, 1},
		{"failure", []doctorCheck{check(doctorOK), check(doctorFail)}, false, 1},
		{"strict failure", []doctorCheck{check(doctorFail)}, true, 1},
		{"empty report", nil, false, 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := doctorExit(test.checks, test.strict); got != test.want {
				t.Fatalf("doctorExit = %d, want %d", got, test.want)
			}
		})
	}
}

func TestDoctorReportNamesEachFindingAndItsFix(t *testing.T) {
	checks := []doctorCheck{
		{Subsystem: "compositor", Dependency: "NIRI_SOCKET", Present: true, Version: "n/a", Detail: "connected", Level: doctorOK},
		{Subsystem: "notify", Dependency: "$XDG_RUNTIME_DIR/sysc-notify/presenter.v1.sock", Present: false, Version: "protocol 1", Detail: "socket not found", Fix: "start it with: systemctl --user start sysc-notify.service", Level: doctorWarn},
	}
	var out bytes.Buffer
	if err := printDoctor(&out, checks); err != nil {
		t.Fatalf("printDoctor: %v", err)
	}
	text := out.String()
	for _, want := range []string{
		"STATUS", "SUBSYSTEM", "PRESENT", "DEPENDENCY", "VERSION", "DETAIL",
		"compositor", "NIRI_SOCKET", "connected",
		"notify", "protocol 1", "socket not found", "systemctl --user start sysc-notify.service",
		"1 ok, 1 warn, 0 fail", "fixes:", "notify: start it",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("report does not contain %q:\n%s", want, text)
		}
	}
}

func TestDoctorReportSurfacesAWriteFailure(t *testing.T) {
	err := printDoctor(failingWriter{}, []doctorCheck{{Subsystem: "compositor", Level: doctorOK}})
	if err == nil {
		t.Fatal("printDoctor accepted a writer that always fails")
	}
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errors.New("broken pipe") }

func TestDoctorProbesNameTheirSubsystemWithoutAnEnvironment(t *testing.T) {
	t.Setenv("NIRI_SOCKET", "")
	t.Setenv("WAYLAND_DISPLAY", "")
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())

	checks := []doctorCheck{
		compositorCheck(context.Background()),
		waylandCheck(context.Background()),
	}
	checks = append(checks, daemonChecks()...)
	checks = append(checks, pluginCheck(), fontCheck(), localeCheck(), tzdataCheck())
	for _, check := range checks {
		if check.Subsystem == "" {
			t.Fatalf("probe returned no subsystem: %+v", check)
		}
		if check.Level != doctorOK && check.Level != doctorWarn && check.Level != doctorFail {
			t.Fatalf("%s returned level %q", check.Subsystem, check.Level)
		}
		if check.Detail == "" {
			t.Fatalf("%s returned no detail", check.Subsystem)
		}
	}
	if checks[0].Level != doctorFail || checks[0].Present {
		t.Fatalf("compositor without NIRI_SOCKET = %+v, want a failure", checks[0])
	}
	if checks[1].Level != doctorFail || checks[1].Present {
		t.Fatalf("wayland without WAYLAND_DISPLAY = %+v, want a failure", checks[1])
	}
}

func TestDoctorRedactorWithoutAPrivatePath(t *testing.T) {
	redact := doctorRedactor()
	if got := redact("plain text"); got != "plain text" {
		t.Fatalf("redactor changed text without a private prefix: %q", got)
	}
}

func TestDoctorRedactorHidesHomeAndRuntimePaths(t *testing.T) {
	redact := doctorRedactorFor("/home/alice", "/run/user/1000")
	input := "font cache /home/alice/.cache/sysc-shell/fontscan; socket /run/user/1000/sysc-shell/ipc.v1.sock"
	got := redact(input)
	if strings.Contains(got, "/home/alice") || strings.Contains(got, "/run/user/1000") {
		t.Fatalf("redacted text still holds a private path: %q", got)
	}
	if !strings.Contains(got, "~/.cache/sysc-shell/fontscan") {
		t.Fatalf("home prefix was not replaced with ~: %q", got)
	}
	if !strings.Contains(got, "$XDG_RUNTIME_DIR/sysc-shell/ipc.v1.sock") {
		t.Fatalf("runtime prefix was not replaced: %q", got)
	}
}

func TestDoctorRedactorPrefersTheMoreSpecificPrefix(t *testing.T) {
	redact := doctorRedactorFor("/home/alice", "/home/alice/.run")
	got := redact("/home/alice/.run/sysc-shell/ipc.v1.sock")
	if !strings.HasPrefix(got, "$XDG_RUNTIME_DIR/") {
		t.Fatalf("runtime directory inside home = %q, want the runtime placeholder", got)
	}
}

func TestDoctorUsage(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := runDoctor([]string{"--help"}, &stdout, &stderr); code != 0 {
		t.Fatalf("--help exit = %d, want 0", code)
	}
	if !strings.Contains(stdout.String(), "usage: sysc-shell doctor") {
		t.Fatalf("--help output = %q", stdout.String())
	}
	stdout.Reset()
	stderr.Reset()
	if code := runDoctor([]string{"--nonsense"}, &stdout, &stderr); code != 2 {
		t.Fatalf("unknown flag exit = %d, want 2", code)
	}
	if !strings.Contains(stderr.String(), "unknown argument") {
		t.Fatalf("unknown flag stderr = %q", stderr.String())
	}
}

func TestInspectSocketAcceptsOnlyTheLayoutClientsTrust(t *testing.T) {
	dir := sockpath.Dir(t, "doctor-")
	path := filepath.Join(dir, "presenter.sock")
	listener, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	if err := os.Chmod(path, 0o600); err != nil {
		t.Fatal(err)
	}
	uid := uint32(os.Geteuid())
	if err := inspectSocket(path, uid); err != nil {
		t.Fatalf("private socket rejected: %v", err)
	}
	if err := inspectSocket(path, uid+1); err == nil {
		t.Fatal("a socket owned by another uid was accepted")
	}
	if err := os.Chmod(path, 0o666); err != nil {
		t.Fatal(err)
	}
	if err := inspectSocket(path, uid); err == nil {
		t.Fatal("a world-writable socket was accepted")
	}
	if err := os.Chmod(path, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := inspectSocket(path, uid); err == nil {
		t.Fatal("a socket the clients reject for its mode was accepted")
	}
	if err := os.Chmod(path, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := inspectCompositorSocket(path, uid); err != nil {
		t.Fatalf("the mode Niri creates was rejected: %v", err)
	}
	if err := inspectCompositorSocket(path, uid+1); err == nil {
		t.Fatal("a compositor socket owned by another uid was accepted")
	}

	regular := filepath.Join(dir, "not-a-socket")
	if err := os.WriteFile(regular, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := inspectSocket(regular, uid); err == nil {
		t.Fatal("a regular file was accepted as a socket")
	}

	link := filepath.Join(dir, "link.sock")
	if err := os.Symlink(path, link); err != nil {
		t.Fatal(err)
	}
	if err := inspectSocket(link, uid); err == nil {
		t.Fatal("a symlink was accepted as a socket")
	}
}

func TestDaemonCheckSeparatesRequiredFromOptionalSockets(t *testing.T) {
	missing := filepath.Join(sockpath.Dir(t, "doctor-"), "missing.sock")
	uid := uint32(os.Geteuid())
	required := daemonCheck("ipc", missing, "sysc-shell", true, "/run/user/1000", uid)
	if required.Level != doctorFail || required.Present {
		t.Fatalf("missing shell socket = %+v, want a failure with present false", required)
	}
	if !strings.Contains(required.Fix, "start it") && !strings.Contains(required.Fix, "start sysc-shell") {
		t.Fatalf("missing shell socket fix = %q", required.Fix)
	}
	optional := daemonCheck("notify", missing, "sysc-notify.service", false, "/run/user/1000", uid)
	if optional.Level != doctorWarn || optional.Present {
		t.Fatalf("missing notify socket = %+v, want a warning with present false", optional)
	}
	if !strings.Contains(optional.Fix, "systemctl --user start sysc-notify.service") {
		t.Fatalf("missing notify fix = %q", optional.Fix)
	}
	noRuntime := daemonCheck("notify", "", "sysc-notify.service", false, "", uid)
	if noRuntime.Level != doctorWarn || noRuntime.Dependency != "$XDG_RUNTIME_DIR" {
		t.Fatalf("missing runtime directory = %+v", noRuntime)
	}
}

func TestLocaleCheckReportsUTF8AndFallback(t *testing.T) {
	t.Setenv("LC_ALL", "")
	t.Setenv("LC_MESSAGES", "")
	t.Setenv("LANG", "en_AU.UTF-8")
	if got := localeCheck(); got.Level != doctorOK || !got.Present || got.Version != "en_AU.UTF-8" {
		t.Fatalf("UTF-8 locale = %+v", got)
	}

	t.Setenv("LANG", "C")
	got := localeCheck()
	if got.Level != doctorWarn || !got.Present {
		t.Fatalf("C locale = %+v, want a warning that it is present", got)
	}

	t.Setenv("LANG", "")
	got = localeCheck()
	if got.Level != doctorWarn || got.Present {
		t.Fatalf("unset locale = %+v, want a warning that it is absent", got)
	}
}
