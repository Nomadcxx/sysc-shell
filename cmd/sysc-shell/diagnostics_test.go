package main

import (
	"compress/gzip"
	"context"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestDiagnosticsCancellationFlushesPrivateHeapAndTimingArtifacts(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_STATE_HOME", dir)
	var before runtime.MemStats
	runtime.ReadMemStats(&before)
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
	var after runtime.MemStats
	runtime.ReadMemStats(&after)
	if after.NumForcedGC != before.NumForcedGC {
		t.Fatalf("diagnostic capture forced GC: before=%d after=%d", before.NumForcedGC, after.NumForcedGC)
	}
	if time.Since(start) > time.Second {
		t.Fatal("cancelled capture delayed shutdown")
	}
	files, err := filepath.Glob(filepath.Join(dir, "sysc-shell", "diagnostic-*"))
	if err != nil || len(files) != 5 {
		t.Fatalf("diagnostic files = %v, want stacks, CPU, heap, memory and trace: %v", files, err)
	}
	artifactDir, err := os.Stat(filepath.Join(dir, "sysc-shell"))
	if err != nil {
		t.Fatal(err)
	}
	if artifactDir.Mode().Perm() != 0o700 {
		t.Fatalf("diagnostic directory mode = %v; want 0700", artifactDir.Mode())
	}
	seen := make(map[string]bool, len(files))
	for _, path := range files {
		info, err := os.Stat(path)
		if err != nil || info.Mode().Perm() != 0o600 || info.Size() == 0 {
			t.Fatalf("private, nonempty capture %s: %v, %v", path, info, err)
		}
		seen[filepath.Ext(path)] = true
		if ext := filepath.Ext(path); ext == ".cpu" || ext == ".heap" {
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
	for _, ext := range []string{".txt", ".cpu", ".heap", ".mem", ".trace"} {
		if !seen[ext] {
			t.Errorf("missing diagnostic artifact %s", ext)
		}
	}
	memPath := strings.TrimSuffix(files[0], filepath.Ext(files[0])) + ".mem"
	mem, err := os.ReadFile(memPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"HeapAlloc=", "HeapInuse=", "HeapSys=", "HeapReleased=", "TotalAlloc=", "NumGC=", "NumForcedGC="} {
		if !strings.Contains(string(mem), field) {
			t.Errorf("runtime memory summary is missing %s", field)
		}
	}
}
