package main

import (
	"compress/gzip"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestDiagnosticsCancellationFlushesPrivateFiles(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_STATE_HOME", dir)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	stop := watchDiagnostics(ctx)
	defer stop()
	if err := syscall.Kill(os.Getpid(), syscall.SIGUSR2); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(time.Second)
	for {
		files, _ := filepath.Glob(filepath.Join(dir, "sysc-shell", "diagnostic-*.cpu"))
		if len(files) == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("SIGUSR2 did not start a capture")
		}
		time.Sleep(time.Millisecond)
	}
	start := time.Now()
	cancel()
	stop()
	if time.Since(start) > time.Second {
		t.Fatal("cancelled capture delayed shutdown")
	}
	files, err := filepath.Glob(filepath.Join(dir, "sysc-shell", "diagnostic-*"))
	if err != nil || len(files) != 2 {
		t.Fatalf("diagnostic files = %v: %v", files, err)
	}
	for _, path := range files {
		info, err := os.Stat(path)
		if err != nil || info.Mode().Perm() != 0o600 || info.Size() == 0 {
			t.Fatalf("private, nonempty capture %s: %v, %v", path, info, err)
		}
		if strings.HasSuffix(path, ".cpu") {
			f, err := os.Open(path)
			if err != nil {
				t.Fatal(err)
			}
			defer f.Close()
			z, err := gzip.NewReader(f)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := io.Copy(io.Discard, z); err != nil {
				t.Fatalf("profile did not flush: %v", err)
			}
			z.Close()
		}
	}
}
