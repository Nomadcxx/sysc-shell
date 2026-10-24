package main

import (
	"bytes"
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Nomadcxx/sysc-shell/internal/ipc"
	"github.com/Nomadcxx/sysc-shell/internal/sockpath"
)

func serveTopics(t *testing.T, topics map[string]ipc.Provider) string {
	t.Helper()
	sock := filepath.Join(sockpath.Dir(t, "ipc"), "ipc.v1.sock")
	ctx, cancel := context.WithCancel(context.Background())
	srv := ipc.NewServer(sock, ipc.Handlers{Topics: topics})
	if err := srv.Listen(); err != nil {
		t.Fatal(err)
	}
	go func() { _ = srv.Serve(ctx) }()
	t.Cleanup(func() { cancel(); _ = srv.Close() })
	return sock
}

func staticTopic(v any) ipc.Provider {
	return ipc.Provider{Start: func(publish func(any)) error { publish(v); return nil }, Stop: func() {}}
}

func TestIPCSubscribeCommand(t *testing.T) {
	sock := serveTopics(t, map[string]ipc.Provider{
		"fake": staticTopic([]string{"clock"}),
		"huge": staticTopic(strings.Repeat("x", 3<<20)),
	})

	var out, errOut bytes.Buffer
	start := time.Now()
	code := runIPCSubscribe(context.Background(), sock, []string{"fake", "--seconds", "1"}, &out, &errOut)
	if code != 0 {
		t.Fatalf("exit %d at the deadline, stderr %q", code, errOut.String())
	}
	if d := time.Since(start); d < 900*time.Millisecond || d > 3*time.Second {
		t.Fatalf("--seconds 1 ran %v", d)
	}
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	var ev ipc.Event
	if len(lines) != 1 || json.Unmarshal([]byte(lines[0]), &ev) != nil ||
		ev.Event != ipc.EventSnapshot || ev.Topic != "fake" || string(ev.Data) != `["clock"]` {
		t.Fatalf("stdout = %q", out.String())
	}

	for _, args := range [][]string{
		{"fake", "--seconds", "0"},
		{"fake", "--seconds", "301"},
		{"fake", "--seconds", "soon"},
		{"fake", "--seconds"},
		{"--seconds", "5"},
		{"fake", "--verbose"},
	} {
		out.Reset()
		errOut.Reset()
		if code := runIPCSubscribe(context.Background(), sock, args, &out, &errOut); code == 0 || errOut.Len() == 0 || out.Len() != 0 {
			t.Errorf("%v: exit %d, stdout %q, stderr %q", args, code, out.String(), errOut.String())
		}
	}

	// A refused topic and an error event both fail.
	for _, topic := range []string{"nope", "huge"} {
		errOut.Reset()
		if code := runIPCSubscribe(context.Background(), sock, []string{topic, "--seconds", "5"}, &out, &errOut); code == 0 {
			t.Errorf("%s: exit 0, want a failure", topic)
		}
	}

	// A lost connection fails.
	if code := runIPCSubscribe(context.Background(), filepath.Join(t.TempDir(), "absent.sock"), []string{"fake"}, &out, &errOut); code == 0 {
		t.Error("absent socket: exit 0")
	}
}
