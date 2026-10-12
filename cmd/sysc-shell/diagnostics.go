package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"runtime/pprof"
	"runtime/trace"
	"syscall"
	"time"
)

// SIGUSR2 captures a stuck input handler without terminating the shell. One
// worker serialises requests because the runtime permits only one CPU profile.
func watchDiagnostics(ctx context.Context) func() {
	ctx, cancel := context.WithCancel(ctx)
	requests := make(chan os.Signal, 1)
	signal.Notify(requests, syscall.SIGUSR2)
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			select {
			case <-ctx.Done():
				return
			case <-requests:
				if err := captureDiagnostics(ctx); err != nil {
					log.Printf("shell: diagnostics: %v", err)
				}
			}
		}
	}()
	return func() {
		signal.Stop(requests)
		cancel()
		<-done
	}
}

func captureDiagnostics(ctx context.Context) error {
	base := os.Getenv("XDG_STATE_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return err
		}
		base = filepath.Join(home, ".local", "state")
	}
	dir := filepath.Join(base, "sysc-shell")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	stem := filepath.Join(dir, "diagnostic-"+time.Now().Format("20060102T150405.000000000"))
	stacks, err := diagnosticFile(stem, ".txt")
	if err != nil {
		return err
	}
	writeErr := pprof.Lookup("goroutine").WriteTo(stacks, 2)
	closeErr := stacks.Close()
	if writeErr != nil {
		return writeErr
	}
	if closeErr != nil {
		return closeErr
	}
	if err := writeMemorySummary(stem); err != nil {
		return err
	}
	if err := writeHeapProfile(stem); err != nil {
		return err
	}
	profile, err := diagnosticFile(stem, ".cpu")
	if err != nil {
		return err
	}
	defer profile.Close()
	traceFile, err := diagnosticFile(stem, ".trace")
	if err != nil {
		return err
	}
	defer traceFile.Close()
	if err := trace.Start(traceFile); err != nil {
		return err
	}
	traceStarted := true
	cpuStarted := false
	defer func() {
		if cpuStarted {
			pprof.StopCPUProfile()
		}
		if traceStarted {
			trace.Stop()
		}
	}()
	if err := pprof.StartCPUProfile(profile); err != nil {
		return err
	}
	cpuStarted = true
	log.Printf("shell: goroutine stacks in %s.txt; capturing CPU and interaction trace for 10s in %s.cpu and %s.trace", stem, stem, stem)
	timer := time.NewTimer(10 * time.Second)
	defer timer.Stop()
	select {
	case <-ctx.Done():
	case <-timer.C:
	}
	pprof.StopCPUProfile()
	cpuStarted = false
	trace.Stop()
	traceStarted = false
	if err := profile.Sync(); err != nil {
		return fmt.Errorf("write CPU profile: %w", err)
	}
	if err := traceFile.Sync(); err != nil {
		return fmt.Errorf("write execution trace: %w", err)
	}
	log.Printf("shell: CPU profile and interaction trace complete: %s.cpu, %s.trace", stem, stem)
	return nil
}

func diagnosticFile(stem, suffix string) (*os.File, error) {
	return os.OpenFile(stem+suffix, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
}

func writeMemorySummary(stem string) error {
	var stats runtime.MemStats
	runtime.ReadMemStats(&stats)
	f, err := diagnosticFile(stem, ".mem")
	if err != nil {
		return err
	}
	_, writeErr := fmt.Fprintf(f,
		"HeapAlloc=%d\nHeapInuse=%d\nHeapSys=%d\nHeapReleased=%d\nTotalAlloc=%d\nSys=%d\nHeapObjects=%d\nNumGC=%d\nNumForcedGC=%d\n",
		stats.HeapAlloc, stats.HeapInuse, stats.HeapSys, stats.HeapReleased,
		stats.TotalAlloc, stats.Sys, stats.HeapObjects, stats.NumGC, stats.NumForcedGC)
	closeErr := f.Close()
	if writeErr != nil {
		return writeErr
	}
	return closeErr
}

func writeHeapProfile(stem string) error {
	f, err := diagnosticFile(stem, ".heap")
	if err != nil {
		return err
	}
	// The standard heap profile includes in-use and cumulative allocation
	// samples and does not force a collection to produce them.
	writeErr := pprof.Lookup("heap").WriteTo(f, 0)
	closeErr := f.Close()
	if writeErr != nil {
		return writeErr
	}
	return closeErr
}
