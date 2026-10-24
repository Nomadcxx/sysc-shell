package wayland

import (
	"context"
	"errors"
	"io"
	"net"
	"time"

	"golang.org/x/sys/unix"
)

// connectionGoneGrace is how long the shell waits for the stop signal after
// the compositor's connection is gone. A session exit quits the compositor
// first and stops the shell second, so the shell waits for the stop it was
// promised instead of failing the unit.
const connectionGoneGrace = 2 * time.Second

// connectionGone reports whether err is the compositor's connection going
// away: a peer that reset the socket, a broken pipe, or a stream that ended.
// A protocol failure is not a lost connection and stays a failure.
func connectionGone(err error) bool {
	switch {
	case errors.Is(err, unix.ECONNRESET),
		errors.Is(err, unix.EPIPE),
		errors.Is(err, io.EOF),
		errors.Is(err, io.ErrUnexpectedEOF),
		errors.Is(err, net.ErrClosed):
		return true
	}
	return false
}

// onlyConnectionGone reports whether err holds no failure other than the
// compositor's connection going away. A joined error carries its failures
// side by side, so a real failure next to a lost connection keeps both.
func onlyConnectionGone(err error) bool {
	for _, leaf := range errorLeaves(err) {
		if !connectionGone(leaf) {
			return false
		}
	}
	return true
}

// errorLeaves flattens a joined error into the errors it was built from.
func errorLeaves(err error) []error {
	if err == nil {
		return nil
	}
	if joined, ok := err.(interface{ Unwrap() []error }); ok {
		var leaves []error
		for _, inner := range joined.Unwrap() {
			leaves = append(leaves, errorLeaves(inner)...)
		}
		return leaves
	}
	return []error{err}
}

// sessionEnd reports the failure a session carries to its exit. A compositor
// that left while the shell was stopping is the expected end of a session: the
// shutdown roundtrip cannot complete against a socket the peer already closed,
// and the unit must not end failed after a session exit. A failure that is not
// the lost connection stays visible.
func sessionEnd(stopping bool, err error) error {
	if stopping && onlyConnectionGone(err) {
		return nil
	}
	return err
}

// awaitStop waits for the stop signal that ends a session after the
// compositor's connection is gone. A stop that arrives within the grace ends
// the session without a failure; a compositor that died without a stop keeps
// its failure, so the shell stays restartable.
func awaitStop(ctx context.Context, grace time.Duration, err error) error {
	if err == nil {
		return nil
	}
	timer := time.NewTimer(grace)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return nil
	case <-timer.C:
		return err
	}
}
