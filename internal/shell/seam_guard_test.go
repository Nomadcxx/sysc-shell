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
	const readers = 500

	var calls atomic.Int64
	restore := filesOpenPath.Swap(func(string, func(error)) { calls.Add(1) })
	defer restore()

	// Two groups, because the loops and the reads are different things.
	// "loops" covers the two loops below; "reads" covers every reader each
	// of them starts. Joining the readers before the check is what makes the
	// count meaningful, and it also keeps the deferred restore from putting
	// the real open back while a reader is still running.
	var loops, reads sync.WaitGroup
	loops.Add(2)
	go func() {
		defer loops.Done()
		for range readers {
			reads.Add(1)
			go func() {
				defer reads.Done()
				// The shape production uses: the panel launches the handler off
				// the Registry lock.
				filesOpenPath.Get()("/tmp/example", func(error) {})
			}()
		}
	}()
	go func() {
		defer loops.Done()
		for range readers {
			r := filesOpenPath.Swap(func(string, func(error)) { calls.Add(1) })
			r()
		}
	}()
	loops.Wait()
	reads.Wait()

	// Every reader has to have reached a swapped seam. A reader that saw the
	// real open instead would leave the count short, because the real open
	// does not answer here.
	if got := calls.Load(); got != readers {
		t.Fatalf("the swapped seam was called %d times, want %d: a reader saw the real open", got, readers)
	}
}
