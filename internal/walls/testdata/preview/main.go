package main

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"
)

func main() {
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGTERM)
	if os.Getenv("SYSC_WALLS_IGNORE_TERM") == "1" {
		signal.Ignore(syscall.SIGTERM)
	}
	started := os.Getenv("SYSC_WALLS_STARTED_FILE")
	if started != "" {
		if err := os.WriteFile(started, []byte(fmt.Sprintf("%d\n", os.Getpid())), 0o600); err != nil {
			os.Exit(2)
		}
	}
	if os.Getenv("SYSC_WALLS_NO_READY") == "1" {
		<-signals
		return
	}
	ready := os.Getenv("SYSC_WALLS_READY_FILE")
	tmp := ready + ".tmp"
	if err := os.WriteFile(tmp, []byte(fmt.Sprintf("%d\n%s", os.Getpid(), os.Getenv("PATH"))), 0o600); err != nil {
		os.Exit(2)
	}
	if err := os.Rename(tmp, ready); err != nil {
		os.Exit(2)
	}
	count, err := os.OpenFile(ready+".count", os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		os.Exit(2)
	}
	_, writeErr := fmt.Fprintln(count, os.Getpid())
	closeErr := count.Close()
	if writeErr != nil || closeErr != nil {
		os.Exit(2)
	}
	fmt.Println("✓ Screensaver launched")
	<-signals
}
