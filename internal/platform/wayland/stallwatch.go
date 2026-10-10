package wayland

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime/pprof"
	"sync/atomic"
	"time"
)

const (
	stallAfter    = 3 * time.Second
	stallPoll     = time.Second
	stallMaxDumps = 3
)

// stallWatch notices when the owner goroutine has been working, without
// returning to wait for events, for longer than stallAfter. The owner serves
// input, rendering and shutdown, so such a stretch is what a frozen shell is;
// the dump of every goroutine's stack says where it is stuck.
type stallWatch struct {
	busySince atomic.Int64 // unix nanos of the first unbroken work; 0 while waiting
	dump      func(since time.Time)
}

// working marks the owner as busy. Repeated calls keep the original time, so
// a loop that never waits still ages.
func (w *stallWatch) working() { w.busySince.CompareAndSwap(0, time.Now().UnixNano()) }

// waiting marks the owner as about to block for events.
func (w *stallWatch) waiting() { w.busySince.Store(0) }

// run reports each stall once, up to stallMaxDumps per process.
// ponytail: the cap keeps a shell that is slow forever from filling the disk.
func (w *stallWatch) run(ctx context.Context) {
	t := time.NewTicker(stallPoll)
	defer t.Stop()
	var reported int64
	dumps := 0
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		since := w.busySince.Load()
		if since == 0 || since == reported || time.Since(time.Unix(0, since)) < stallAfter || dumps >= stallMaxDumps {
			continue
		}
		reported = since
		dumps++
		w.dump(time.Unix(0, since))
	}
}

// writeStallDump saves all goroutine stacks under $XDG_STATE_HOME/sysc-shell.
func writeStallDump(since time.Time) {
	base := os.Getenv("XDG_STATE_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			log.Printf("shell: stall since %s; no state dir for the dump: %v", since.Format(time.TimeOnly), err)
			return
		}
		base = filepath.Join(home, ".local", "state")
	}
	dir := filepath.Join(base, "sysc-shell")
	path := filepath.Join(dir, fmt.Sprintf("stall-%s.txt", since.Format("20060102T150405")))
	if err := os.MkdirAll(dir, 0o700); err != nil {
		log.Printf("shell: stall dump: %v", err)
		return
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		log.Printf("shell: stall dump: %v", err)
		return
	}
	defer f.Close()
	if err := pprof.Lookup("goroutine").WriteTo(f, 2); err != nil {
		log.Printf("shell: stall dump: %v", err)
		return
	}
	log.Printf("shell: owner loop busy since %s; goroutine stacks in %s", since.Format(time.TimeOnly), path)
}
