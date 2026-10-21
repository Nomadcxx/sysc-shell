package shell

import (
	"sync"
	"sync/atomic"
	"testing"
)

// The files panel opens a path on the Registry's goroutine, so the open seam
// is read there while the rest of the suite keeps running. A test that swaps
// the seam must not race that read, and the read must not see a half-swapped
// value.
//
// Without the guard the swap is a plain write to a package-level variable and
// the race detector reports it against the reads below.
func TestFilesOpenPathSwapIsSafeAgainstAConcurrentOpen(t *testing.T) {
	var calls atomic.Int64
	restore := filesOpenPath.Swap(func(string, func(error)) { calls.Add(1) })
	defer restore()

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for range 500 {
			// The shape production uses: the panel launches the handler off
			// the Registry lock.
			go filesOpenPath.Get()("/tmp/example", func(error) {})
		}
	}()
	go func() {
		defer wg.Done()
		for range 500 {
			r := filesOpenPath.Swap(func(string, func(error)) { calls.Add(1) })
			r()
		}
	}()
	wg.Wait()

	if calls.Load() == 0 {
		t.Fatal("the swapped seam was never called")
	}
}
