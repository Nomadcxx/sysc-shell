package ipc

import (
	"context"
	"encoding/json"
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
		Panel: func(action, panel string) error {
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

func TestPanelParamValidation(t *testing.T) {
	t.Parallel()
	called := false
	sock, cancel := startServer(t, Handlers{
		Panel: func(string, string) error { called = true; return nil },
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
