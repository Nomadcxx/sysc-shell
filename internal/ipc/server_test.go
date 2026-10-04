package ipc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestServerRoundTrip(t *testing.T) {
	t.Parallel()
	sock := filepath.Join(t.TempDir(), "ipc.v1.sock")
	var got string
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	srv := NewServer(sock, Handlers{
		Panel: func(action, panel, _ string) error {
			got = action + ":" + panel
			return nil
		},
		Status: func() map[string]any { return map[string]any{"version": "test"} },
	})
	errc := make(chan error, 1)
	go func() { errc <- srv.Serve(ctx) }()
	t.Cleanup(func() { cancel(); _ = srv.Close() })
	waitSock(t, sock)
	out, err := Call(ctx, sock, "panel.toggle", map[string]string{"panel": "session"})
	if err != nil || !strings.Contains(out, `"ok"`) {
		t.Fatalf("call: %v %s", err, out)
	}
	if got != "toggle:session" {
		t.Fatalf("handler got %q", got)
	}
}

func TestThemePreviewCallAllowsGeneratorDuration(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	defer func() {
		select {
		case <-release:
		default:
			close(release)
		}
	}()
	sock, cancel := startServer(t, Handlers{
		Theme: func(string, json.RawMessage) (map[string]any, error) {
			close(started)
			<-release
			return map[string]any{"previewing": true}, nil
		},
	})
	defer cancel()

	type result struct {
		out string
		err error
	}
	called := make(chan result, 1)
	go func() {
		out, err := Call(context.Background(), sock, "theme.preview.show", nil)
		called <- result{out: out, err: err}
	}()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("preview handler did not start")
	}
	// Matugen has a ten-second bound; hold past the ordinary two-second IPC
	// deadline to prove the preview method receives enough time to finish.
	time.Sleep(2100 * time.Millisecond)
	close(release)
	got := <-called
	if got.err != nil {
		t.Fatalf("preview call: %v", got.err)
	}
	if !strings.Contains(got.out, `"previewing":true`) {
		t.Fatalf("preview reply = %s", got.out)
	}
}

func TestIpcPowerAliasTogglesSession(t *testing.T) {
	t.Parallel()
	sock := filepath.Join(t.TempDir(), "ipc.v1.sock")
	var got string
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	srv := NewServer(sock, Handlers{
		Panel: func(action, panel, _ string) error {
			got = action + ":" + panel
			return nil
		},
		Status: func() map[string]any { return map[string]any{"version": "test"} },
	})
	errc := make(chan error, 1)
	go func() { errc <- srv.Serve(ctx) }()
	t.Cleanup(func() { cancel(); _ = srv.Close() })
	waitSock(t, sock)
	out, err := Call(ctx, sock, "panel.toggle", map[string]string{"panel": "power"})
	if err != nil || !strings.Contains(out, `"ok"`) {
		t.Fatalf("call: %v %s", err, out)
	}
	if got != "toggle:power" {
		t.Fatalf("handler got %q", got)
	}
}

func TestUnknownMethodErrors(t *testing.T) {
	t.Parallel()
	sock, cancel := startServer(t, Handlers{})
	defer cancel()
	out, err := Call(context.Background(), sock, "nope", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, `"error"`) {
		t.Fatalf("want error envelope, got %s", out)
	}
}

func TestStaleSocketReplaced(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	sock := filepath.Join(dir, "ipc.v1.sock")
	if err := os.WriteFile(sock, []byte("dead"), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	srv := NewServer(sock, Handlers{Status: func() map[string]any { return map[string]any{} }})
	go func() { _ = srv.Serve(ctx) }()
	t.Cleanup(func() { cancel(); _ = srv.Close() })
	waitSock(t, sock)
	out, err := Call(ctx, sock, "status", nil)
	if err != nil || !strings.Contains(out, `"ok"`) {
		t.Fatalf("stale socket: %v %s", err, out)
	}
}

func TestLiveSocketFailsAsSingleInstance(t *testing.T) {
	t.Parallel()
	sock, cancel := startServer(t, Handlers{Status: func() map[string]any { return map[string]any{} }})
	defer cancel()
	second := NewServer(sock, Handlers{})
	ctx, stop := context.WithTimeout(context.Background(), time.Second)
	defer stop()
	if err := second.Serve(ctx); err != ErrSingleInstance {
		t.Fatalf("second Serve = %v, want ErrSingleInstance", err)
	}
}

func TestOsdStepDispatches(t *testing.T) {
	t.Parallel()
	var kind, action string
	sock, cancel := startServer(t, Handlers{
		OSDStep: func(k, a string) error { kind, action = k, a; return nil },
	})
	defer cancel()
	out, err := Call(context.Background(), sock, "osd.step", map[string]string{"kind": "audio", "action": "up"})
	if err != nil || !strings.Contains(out, `"ok"`) {
		t.Fatalf("call: %v %s", err, out)
	}
	if kind != "audio" || action != "up" {
		t.Fatalf("got %s %s", kind, action)
	}
}

func TestSwitcherShowDispatches(t *testing.T) {
	t.Parallel()
	called := false
	sock, cancel := startServer(t, Handlers{
		Switcher: func() error { called = true; return nil },
	})
	defer cancel()
	out, err := Call(context.Background(), sock, "switcher.show", map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	var response struct {
		OK    bool   `json:"ok"`
		Error string `json:"error"`
	}
	if err := json.Unmarshal([]byte(out), &response); err != nil {
		t.Fatal(err)
	}
	if !response.OK || response.Error != "" || !called {
		t.Fatalf("response = %s, handler called = %v", out, called)
	}
}

func TestSwitcherShowRequiresHandler(t *testing.T) {
	t.Parallel()
	sock, cancel := startServer(t, Handlers{})
	defer cancel()
	out, err := Call(context.Background(), sock, "switcher.show", map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, `"error":"switcher handler unset"`) {
		t.Fatalf("missing handler response = %s", out)
	}
}

func TestPanelParamValidation(t *testing.T) {
	t.Parallel()
	called := false
	sock, cancel := startServer(t, Handlers{
		Panel: func(string, string, string) error { called = true; return nil },
	})
	defer cancel()
	out, err := Call(context.Background(), sock, "panel.open", map[string]string{"panel": "bogus"})
	if err != nil {
		t.Fatal(err)
	}
	var env struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal([]byte(out), &env); err != nil {
		t.Fatal(err)
	}
	if env.Error == "" {
		t.Fatalf("bogus panel got %s", out)
	}
	if called {
		t.Fatal("handler called for bogus panel")
	}
}

func TestPanelToggleLauncherDispatches(t *testing.T) {
	t.Parallel()
	var action, panel string
	sock, cancel := startServer(t, Handlers{
		Panel: func(a, p, _ string) error { action, panel = a, p; return nil },
	})
	defer cancel()
	out, err := Call(context.Background(), sock, "panel.toggle", map[string]string{"panel": "launcher"})
	if err != nil {
		t.Fatal(err)
	}
	var env struct {
		OK    bool   `json:"ok"`
		Error string `json:"error"`
	}
	if err := json.Unmarshal([]byte(out), &env); err != nil {
		t.Fatal(err)
	}
	if env.Error != "" || !env.OK {
		t.Fatalf("launcher toggle got %s", out)
	}
	if action != "toggle" || panel != "launcher" {
		t.Fatalf("handler got %q %q", action, panel)
	}
}

func TestPanelToggleNotificationsDispatches(t *testing.T) {
	t.Parallel()
	var action, panel string
	sock, cancel := startServer(t, Handlers{
		Panel: func(a, p, _ string) error { action, panel = a, p; return nil },
	})
	defer cancel()
	out, err := Call(context.Background(), sock, "panel.toggle", map[string]string{"panel": "notifications"})
	if err != nil {
		t.Fatal(err)
	}
	var env struct {
		OK    bool   `json:"ok"`
		Error string `json:"error"`
	}
	if err := json.Unmarshal([]byte(out), &env); err != nil {
		t.Fatal(err)
	}
	if env.Error != "" || !env.OK {
		t.Fatalf("notifications toggle got %s", out)
	}
	if action != "toggle" || panel != "notifications" {
		t.Fatalf("handler got %q %q", action, panel)
	}
}

func TestPanelOpenNetworkDispatches(t *testing.T) {
	t.Parallel()
	var action, panel string
	sock, cancel := startServer(t, Handlers{
		Panel: func(a, p, _ string) error { action, panel = a, p; return nil },
	})
	defer cancel()
	out, err := Call(context.Background(), sock, "panel.open", map[string]string{"panel": "network"})
	if err != nil {
		t.Fatal(err)
	}
	var env struct {
		OK    bool   `json:"ok"`
		Error string `json:"error"`
	}
	if err := json.Unmarshal([]byte(out), &env); err != nil {
		t.Fatal(err)
	}
	if env.Error != "" || !env.OK {
		t.Fatalf("network open got %s", out)
	}
	if action != "open" || panel != "network" {
		t.Fatalf("handler got %q %q", action, panel)
	}
}

func TestPanelOpenBluetoothDispatches(t *testing.T) {
	t.Parallel()
	var action, panel string
	sock, cancel := startServer(t, Handlers{
		Panel: func(a, p, _ string) error { action, panel = a, p; return nil },
	})
	defer cancel()
	out, err := Call(context.Background(), sock, "panel.open", map[string]string{"panel": "bluetooth"})
	if err != nil {
		t.Fatal(err)
	}
	var env struct {
		OK    bool   `json:"ok"`
		Error string `json:"error"`
	}
	if err := json.Unmarshal([]byte(out), &env); err != nil {
		t.Fatal(err)
	}
	if env.Error != "" || !env.OK {
		t.Fatalf("bluetooth open got %s", out)
	}
	if action != "open" || panel != "bluetooth" {
		t.Fatalf("handler got %q %q", action, panel)
	}
}

func TestPluginsMethodsRouteToTheHandler(t *testing.T) {
	var gotMethod string
	var gotParams string
	s := NewServer("", Handlers{Plugins: func(method string, params json.RawMessage) (map[string]any, error) {
		gotMethod, gotParams = method, string(params)
		return map[string]any{"queued": method}, nil
	}})
	out := string(s.handleLine(`{"id":1,"method":"plugins.install","params":{"source":"sysc","id":"org.sysc.timer"}}`))
	if gotMethod != "plugins.install" || !strings.Contains(gotParams, "org.sysc.timer") || !strings.Contains(out, `"ok"`) {
		t.Fatalf("method %q params %q reply %s", gotMethod, gotParams, out)
	}
	out = string(NewServer("", Handlers{}).handleLine(`{"id":2,"method":"plugins.store"}`))
	if !strings.Contains(out, "plugin store handler unset") {
		t.Fatalf("reply without a handler: %s", out)
	}
}

func startServer(t *testing.T, h Handlers) (string, context.CancelFunc) {
	t.Helper()
	sock := filepath.Join(t.TempDir(), "ipc.v1.sock")
	ctx, cancel := context.WithCancel(context.Background())
	srv := NewServer(sock, h)
	go func() { _ = srv.Serve(ctx) }()
	t.Cleanup(func() { cancel(); _ = srv.Close() })
	waitSock(t, sock)
	return sock, cancel
}

func waitSock(t *testing.T, sock string) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		c, err := net.DialTimeout("unix", sock, 50*time.Millisecond)
		if err == nil {
			_ = c.Close()
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("socket %s never accepted", sock)
}

func TestDefaultSocketStaysInsideARuntimeDirectory(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", "/run/user/1000")
	if got := DefaultSocket(); got != "/run/user/1000/sysc-shell/ipc.v1.sock" {
		t.Fatalf("DefaultSocket() = %q", got)
	}
	t.Setenv("XDG_RUNTIME_DIR", "")
	t.Setenv("TMPDIR", "/tmp")
	got := DefaultSocket()
	if !filepath.IsAbs(got) || strings.HasPrefix(got, "/tmp/") {
		t.Fatalf("fallback socket sits in a shared directory: %q", got)
	}
}

func TestThemeVerbsRouteToTheHandler(t *testing.T) {
	cases := []struct {
		name    string
		line    string
		want    string
		wantErr bool
	}{
		{"mode get", `{"id":1,"method":"theme.mode.get"}`, `"mode":"dark"`, false},
		{"mode set", `{"id":1,"method":"theme.mode.set","params":{"mode":"light"}}`, `"value":"light"`, false},
		{"mode toggle", `{"id":1,"method":"theme.mode.toggle"}`, `"value":"light"`, false},
		{"palette get", `{"id":1,"method":"theme.palette.get"}`, `"source"`, false},
		{"templates apply", `{"id":1,"method":"theme.templates.apply","params":{"name":"foot"}}`, `"applied":"foot"`, false},
		{"unknown verb", `{"id":1,"method":"theme.mode.flip"}`, "unknown method theme.mode.flip", true},
		{"bad params", `{"id":1,"method":"theme.mode.set","params":7}`, "malformed params", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := NewServer("", Handlers{Theme: func(method string, params json.RawMessage) (map[string]any, error) {
				if method == "theme.mode.flip" {
					return nil, fmt.Errorf("unknown method %s", method)
				}
				var p struct {
					Mode string `json:"mode"`
					Name string `json:"name"`
				}
				if len(params) > 0 {
					if err := json.Unmarshal(params, &p); err != nil {
						return nil, errors.New("malformed params")
					}
				}
				switch method {
				case "theme.mode.get":
					return map[string]any{"mode": "dark"}, nil
				case "theme.mode.set":
					return map[string]any{"path": "appearance.mode", "value": p.Mode}, nil
				case "theme.mode.toggle":
					return map[string]any{"path": "appearance.mode", "value": "light"}, nil
				case "theme.palette.get":
					return map[string]any{"source": "wallpaper", "seed": "", "scheme": "", "mode": "dark"}, nil
				case "theme.templates.apply":
					return map[string]any{"applied": p.Name}, nil
				}
				return nil, fmt.Errorf("unknown method %s", method)
			}})
			out := string(s.handleLine(tc.line))
			if !strings.Contains(out, tc.want) {
				t.Fatalf("reply %s lacks %q", out, tc.want)
			}
			if got := strings.Contains(out, `"error"`); got != tc.wantErr {
				t.Fatalf("reply %s wantErr %v", out, tc.wantErr)
			}
		})
	}
	out := string(NewServer("", Handlers{}).handleLine(`{"id":2,"method":"theme.mode.get"}`))
	if !strings.Contains(out, "theme handler unset") {
		t.Fatalf("reply without a handler: %s", out)
	}
	if out := string(NewServer("", Handlers{}).handleLine(`{"id":3,"method":"theme"}`)); !strings.Contains(out, "unknown method") {
		t.Fatalf("non-namespace method must stay unknown: %s", out)
	}
}

// gh #74: cancelling Serve must end open connections and wait for the request
// each is running, so the caller can tear down what handlers use.
func TestServeCancelClosesIdleConnections(t *testing.T) {
	sock := filepath.Join(t.TempDir(), "ipc.v1.sock")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	srv := NewServer(sock, Handlers{})
	served := make(chan error, 1)
	go func() { served <- srv.Serve(ctx) }()
	waitSock(t, sock)
	conn, err := net.Dial("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	cancel()
	select {
	case <-served:
	case <-time.After(time.Second):
		t.Fatal("Serve did not return after cancel with a connection open")
	}
	_ = conn.SetReadDeadline(time.Now().Add(time.Second))
	if _, err := conn.Read(make([]byte, 1)); err == nil || errors.Is(err, os.ErrDeadlineExceeded) {
		t.Fatalf("connection stayed open after Serve returned: %v", err)
	}
}

func TestServeWaitsForInFlightRequestBeforeReturning(t *testing.T) {
	sock := filepath.Join(t.TempDir(), "ipc.v1.sock")
	started := make(chan struct{})
	release := make(chan struct{})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	srv := NewServer(sock, Handlers{
		Switcher: func() error {
			close(started)
			<-release
			return nil
		},
	})
	served := make(chan error, 1)
	go func() { served <- srv.Serve(ctx) }()
	waitSock(t, sock)
	go func() { _, _ = Call(context.Background(), sock, "switcher.show", nil) }()
	<-started

	cancel()
	select {
	case <-served:
		t.Fatal("Serve returned while a handler was still running")
	case <-time.After(100 * time.Millisecond):
	}
	close(release)
	select {
	case <-served:
	case <-time.After(time.Second):
		t.Fatal("Serve did not return once the handler finished")
	}
}

func TestCallTimeouts(t *testing.T) {
	cases := map[string]time.Duration{
		"status": 2 * time.Second, "theme.preview.show": 12 * time.Second, "theme.palettes.save": 25 * time.Second,
	}
	for method, want := range cases {
		if got := callTimeout(method); got != want {
			t.Errorf("callTimeout(%q) = %v, want %v", method, got, want)
		}
	}
}

func TestSessionLockStateRoutesToHandler(t *testing.T) {
	s := NewServer("", Handlers{LockState: func() map[string]any {
		return map[string]any{"running": true, "acquired": true, "exit_code": 0}
	}})
	out := string(s.handleLine(`{"id":1,"method":"session.lock-state"}`))
	if !strings.Contains(out, `"ok"`) || !strings.Contains(out, `"acquired":true`) {
		t.Fatalf("reply: %s", out)
	}
	out = string(NewServer("", Handlers{}).handleLine(`{"id":2,"method":"session.lock-state"}`))
	if !strings.Contains(out, "lock state handler unset") {
		t.Fatalf("reply without a handler: %s", out)
	}
}
