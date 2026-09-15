package integration

import (
	"bytes"
	"encoding/json"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strconv"
	"syscall"
	"testing"

	v1 "github.com/Nomadcxx/sysc-shell/plugin/v1"
)

// runGateFakeRecorder backs the gpu-screen-recorder symlink that the recorder
// host gate installs on PATH: the test binary re-executes itself as the fake
// backend when SYSC_FAKE_RECORDER is set.
func runGateFakeRecorder() int {
	signal.Reset(syscall.SIGINT)
	if p := os.Getenv("SYSC_FAKE_PID"); p != "" {
		_ = os.WriteFile(p, []byte(strconv.Itoa(os.Getpid())), 0o644)
	}
	if p := os.Getenv("SYSC_FAKE_ARGV"); p != "" {
		raw, _ := json.Marshal(os.Args[1:])
		_ = os.WriteFile(p, raw, 0o644)
	}
	switch os.Getenv("SYSC_FAKE_BEHAVIOR") {
	case "crash":
		_, _ = os.Stdout.WriteString("ready\n")
		return 1
	case "flood":
		chunk := bytes.Repeat([]byte("x"), 4096)
		for i := 0; i < 40; i++ {
			_, _ = os.Stderr.Write(chunk)
		}
	case "ignore-int":
		signal.Ignore(syscall.SIGINT)
		_, _ = os.Stdout.WriteString("ready\n")
		select {}
	}
	if p := argValue("-o"); p != "" {
		body := []byte("mp4")
		if os.Getenv("SYSC_FAKE_BEHAVIOR") == "zero" {
			body = nil
		}
		_ = os.WriteFile(p, body, 0o644)
	}
	ch := make(chan os.Signal, 2)
	signal.Notify(ch, syscall.SIGINT, syscall.SIGUSR1)
	_, _ = os.Stdout.WriteString("ready\n")
	for sig := range ch {
		if sig == syscall.SIGUSR1 {
			if dir := argValue("-ro"); dir != "" {
				_ = os.MkdirAll(dir, 0o755)
				_ = os.WriteFile(filepath.Join(dir, "gsr.mp4"), []byte("mp4"), 0o644)
			}
			continue
		}
		return 0
	}
	return 0
}

func argValue(flag string) string {
	args := os.Args[1:]
	for i, a := range args {
		if a == flag && i+1 < len(args) {
			return args[i+1]
		}
	}
	return ""
}

func repoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("no caller")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "../.."))
}

func dumpTree(n *v1.Node) string {
	raw, _ := json.Marshal(n)
	return string(raw)
}
