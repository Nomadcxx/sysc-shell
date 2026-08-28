package niri

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
)

// maxLine bounds one event line.
const maxLine = 1 << 20

// request is the event-stream request, written as a JSON string and a newline.
const request = `"EventStream"`

// wireReply is the handshake reply. It must be read and checked before any
// event: because unknown events are ignored, an unread Err reply would be
// discarded and the client would wait forever.
type wireReply struct {
	Ok  *string `json:"Ok"`
	Err *string `json:"Err"`
}

// Stream connects to the compositor and publishes workspace snapshots until the
// context is cancelled. The snapshot channel carries only the newest state when
// the consumer is slow.
func Stream(ctx context.Context, socketPath string) (<-chan Snapshot, <-chan error) {
	snapshots := make(chan Snapshot, 1)
	errs := make(chan error, 1)

	conn, reader, err := handshake(ctx, socketPath)
	if err != nil {
		errs <- err
		close(snapshots)
		close(errs)
		return snapshots, errs
	}

	go read(ctx, conn, reader, snapshots, errs)
	return snapshots, errs
}

// handshake dials the socket, sends the request, and validates the reply.
func handshake(ctx context.Context, socketPath string) (net.Conn, *bufio.Scanner, error) {
	var dialer net.Dialer
	conn, err := dialer.DialContext(ctx, "unix", socketPath)
	if err != nil {
		return nil, nil, fmt.Errorf("niri: connect to %s: %w", socketPath, err)
	}

	if _, err := fmt.Fprintln(conn, request); err != nil {
		conn.Close()
		return nil, nil, fmt.Errorf("niri: send request: %w", err)
	}

	scanner := bufio.NewScanner(conn)
	scanner.Buffer(make([]byte, 0, 64*1024), maxLine)
	if !scanner.Scan() {
		conn.Close()
		return nil, nil, fmt.Errorf("niri: no reply to %s: %w", request, scanner.Err())
	}

	var reply wireReply
	if err := json.Unmarshal(scanner.Bytes(), &reply); err != nil {
		conn.Close()
		return nil, nil, fmt.Errorf("niri: decode reply: %w", err)
	}
	switch {
	case reply.Err != nil:
		conn.Close()
		return nil, nil, fmt.Errorf("niri: the compositor refused the event stream: %s", *reply.Err)
	case reply.Ok == nil:
		conn.Close()
		return nil, nil, fmt.Errorf("niri: expected a reply to %s, got %s", request, scanner.Text())
	}
	return conn, scanner, nil
}

// read consumes the event stream on its own goroutine.
func read(ctx context.Context, conn net.Conn, scanner *bufio.Scanner, snapshots chan Snapshot, errs chan<- error) {
	defer close(snapshots)
	defer close(errs)
	defer conn.Close()

	// Closing the connection unblocks a pending read on cancellation.
	stopped := make(chan struct{})
	defer close(stopped)
	go func() {
		select {
		case <-ctx.Done():
			conn.Close()
		case <-stopped:
		}
	}()

	var s state
	for scanner.Scan() {
		publish, err := s.apply(scanner.Bytes())
		if err != nil {
			errs <- err
			return
		}
		if publish {
			send(snapshots, s.snapshot())
		}
	}

	if err := ctx.Err(); err != nil {
		return
	}
	if err := scanner.Err(); err != nil {
		if errors.Is(err, bufio.ErrTooLong) {
			err = fmt.Errorf("niri: event line exceeds %d bytes: %w", maxLine, err)
		}
		errs <- err
		return
	}
	errs <- errors.New("niri: the compositor closed the event stream")
}

// send publishes the newest snapshot, replacing one the consumer has not read.
// This goroutine is the only sender, so the retry always finds room.
func send(snapshots chan Snapshot, snap Snapshot) {
	select {
	case snapshots <- snap:
		return
	default:
	}
	select {
	case <-snapshots:
	default:
	}
	select {
	case snapshots <- snap:
	default:
	}
}
