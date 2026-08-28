package wayland

import (
	"context"
	"fmt"

	"golang.org/x/sys/unix"
)

// wakePipe lets other goroutines wake the owner without touching a Wayland
// proxy. The bridge goroutine only ever writes one byte to this pipe.
type wakePipe struct {
	read  int
	write int
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
func (w *wakePipe) bridge(ctx context.Context, invalidations <-chan struct{}) {
	go func() {
		for {
			select {
			case <-ctx.Done():
				w.signal()
				return
			case <-invalidations:
				w.signal()
			}
		}
	}()
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
