package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"runtime/pprof"
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
	stacks, err := os.OpenFile(stem+".txt", os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
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
	profile, err := os.OpenFile(stem+".cpu", os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer profile.Close()
	if err := pprof.StartCPUProfile(profile); err != nil {
		return err
	}
	log.Printf("shell: goroutine stacks in %s.txt; capturing CPU for 10s in %s.cpu", stem, stem)
	timer := time.NewTimer(10 * time.Second)
	defer timer.Stop()
	select {
	case <-ctx.Done():
	case <-timer.C:
	}
	pprof.StopCPUProfile()
	if err := profile.Sync(); err != nil {
		return fmt.Errorf("write CPU profile: %w", err)
	}
	log.Printf("shell: CPU profile complete: %s.cpu", stem)
	return nil
}
