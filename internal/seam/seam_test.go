package seam

import (
	"runtime"
	"sync"
	"testing"
)

// TestSwapRacesWithConcurrentReads is the check that the lock does its job:
// readers and a swapper run at the same time. Without the lock the race
// detector reports the swap against the reads, which is the failure the
// package exists to prevent.
func TestSwapRacesWithConcurrentReads(t *testing.T) {
	g := New(func(int) int { return 1 })

	const readers = 4
	const rounds = 20000

	start := make(chan struct{})
	var wg sync.WaitGroup

	for range readers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			for range rounds {
				switch g.Get()(1) {
				case 1, 2:
				default:
					t.Errorf("Get returned a value that was never stored")
					return
				}
				runtime.Gosched()
			}
		}()
	}

	wg.Add(1)
	go func() {
		defer wg.Done()
		<-start
		for range rounds {
			restore := g.Swap(func(int) int { return 2 })
			restore()
			runtime.Gosched()
		}
	}()

	close(start)
	wg.Wait()

	if got := g.Get()(1); got != 1 {
		t.Fatalf("after the swaps the guard holds %d, want the original 1", got)
	}
}

// TestSwapRestoresThePreviousValue pins the restore order: a second swap
// restores the value the first one replaced, not the value from before it.
func TestSwapRestoresThePreviousValue(t *testing.T) {
	g := New("a")
	restoreFirst := g.Swap("b")
	restoreSecond := g.Swap("c")

	if got := g.Get(); got != "c" {
		t.Fatalf("guard holds %q, want the newest swap %q", got, "c")
	}
	restoreSecond()
	if got := g.Get(); got != "b" {
		t.Fatalf("guard holds %q after the second restore, want %q", got, "b")
	}
	restoreFirst()
	if got := g.Get(); got != "a" {
		t.Fatalf("guard holds %q after the first restore, want %q", got, "a")
	}
}
