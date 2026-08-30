package wayland

import (
	"context"
	"fmt"
	"sync"

	"golang.org/x/sys/unix"
)

// wakePipe lets other goroutines wake the owner without touching a Wayland
// proxy. The bridge goroutine only ever writes one byte to this pipe and
// appends to the pending queue; the owner goroutine drains both.
type wakePipe struct {
	read  int
	write int

	// pending carries the invalidations that arrived since the last drain. The
	// pipe itself has no payload, so routing a redraw to one connector needs
	// this queue beside it.
	mu      sync.Mutex
	pending []Invalidation
	// reload is set when a SIGHUP arrived. The owner reads and clears it, so
	// repeated signals during one wait coalesce into a single reload.
	reload bool
	// tooltip is the newest hover request. Repeated hovers during one wait
	// keep only the last, which is the pointer's current widget.
	tooltip    TooltipRequest
	hasTooltip bool
}

func newWakePipe() (*wakePipe, error) {
	var fds [2]int
	if err := unix.Pipe2(fds[:], unix.O_CLOEXEC|unix.O_NONBLOCK); err != nil {
		return nil, fmt.Errorf("wayland: wake pipe: %w", err)
	}
	return &wakePipe{read: fds[0], write: fds[1]}, nil
}

// bridge forwards cancellation and application invalidations to the pipe. It
// never closes the caller-owned invalidation channel and never calls a proxy.
func (w *wakePipe) bridge(ctx context.Context, invalidations <-chan Invalidation, reloads <-chan struct{}, tooltips <-chan TooltipRequest) {
	go func() {
		for {
			select {
			case <-ctx.Done():
				w.signal()
				return
			case inv, ok := <-invalidations:
				if !ok {
					return
				}
				w.push(inv)
				w.signal()
			case _, ok := <-reloads:
				if !ok {
					return
				}
				w.mu.Lock()
				w.reload = true
				w.mu.Unlock()
				w.signal()
			case req, ok := <-tooltips:
				if !ok {
					return
				}
				w.mu.Lock()
				w.tooltip, w.hasTooltip = req, true
				w.mu.Unlock()
				w.signal()
			}
		}
	}()
}

// takeReload reports and clears a pending reload request.
func (w *wakePipe) takeReload() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	pending := w.reload
	w.reload = false
	return pending
}

// takeTooltip reports and clears a pending tooltip request.
func (w *wakePipe) takeTooltip() (TooltipRequest, bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if !w.hasTooltip {
		return TooltipRequest{}, false
	}
	req := w.tooltip
	w.tooltip, w.hasTooltip = TooltipRequest{}, false
	return req, true
}

// push queues one invalidation for the owner goroutine.
func (w *wakePipe) push(inv Invalidation) {
	w.mu.Lock()
	w.pending = append(w.pending, inv)
	w.mu.Unlock()
}

// take removes and returns every queued invalidation. A cancellation wake
// returns an empty slice, which the loop treats as no redraw request.
func (w *wakePipe) take() []Invalidation {
	w.mu.Lock()
	out := w.pending
	w.pending = nil
	w.mu.Unlock()
	return out
}

// signal writes one byte, ignoring a full pipe: a pending wake is enough.
func (w *wakePipe) signal() {
	var one [1]byte
	_, _ = unix.Write(w.write, one[:])
}

// drain empties the pipe so a single wake does not spin the loop.
func (w *wakePipe) drain() {
	var buf [64]byte
	for {
		n, err := unix.Read(w.read, buf[:])
		if n < len(buf) || err != nil {
			return
		}
	}
}

func (w *wakePipe) close() {
	_ = unix.Close(w.read)
	_ = unix.Close(w.write)
}
