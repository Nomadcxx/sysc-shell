package ipc

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
)

// TopicPanels is the topic carrying the sorted ids of the open panels.
const TopicPanels = "panels"

// TopicNames is the topic vocabulary the shell publishes. The documentation
// gate checks examples against it; a server lists a topic in capabilities only
// when it has a provider for it.
func TopicNames() []string {
	return []string{TopicPanels}
}

// SubscribeUsage is the argument form of `sysc-shell ipc subscribe`.
const SubscribeUsage = "usage: sysc-shell ipc subscribe TOPIC... [--seconds N]"

// ParseSubscribeArgs reads `TOPIC... [--seconds N]`. N defaults to 30 and must
// be a whole number from 1 to 300.
func ParseSubscribeArgs(args []string) (topics []string, seconds int, err error) {
	seconds = 30
	for i := 0; i < len(args); i++ {
		switch {
		case args[i] == "--seconds":
			if i+1 >= len(args) {
				return nil, 0, errors.New(SubscribeUsage)
			}
			n, convErr := strconv.Atoi(args[i+1])
			if convErr != nil || n < 1 || n > 300 {
				return nil, 0, fmt.Errorf("--seconds takes a whole number from 1 to 300, not %q", args[i+1])
			}
			seconds = n
			i++
		case strings.HasPrefix(args[i], "-"):
			return nil, 0, errors.New(SubscribeUsage)
		default:
			topics = append(topics, args[i])
		}
	}
	if len(topics) == 0 {
		return nil, 0, errors.New(SubscribeUsage)
	}
	return topics, seconds, nil
}

// Stream subscribes to topics on a connection of its own and hands every event
// to fn until ctx ends, fn returns an error, or the connection is lost. A
// refused subscribe returns an *Error carrying the server's code. When ctx
// ends, Stream closes the connection, which unsubscribes, and returns
// ctx.Err().
func Stream(ctx context.Context, sock string, topics []string, fn func(Event) error) error {
	var d net.Dialer
	conn, err := d.DialContext(ctx, "unix", sock)
	if err != nil {
		return err
	}
	stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer func() {
		stop()
		_ = conn.Close()
	}()

	req, err := json.Marshal(map[string]any{
		"id": 1, "method": "ipc.subscribe", "params": topicsParams{Topics: topics},
	})
	if err != nil {
		return err
	}
	if _, err := conn.Write(append(req, '\n')); err != nil {
		return streamErr(ctx, err)
	}
	sc := bufio.NewScanner(conn)
	// An event line is its state, bounded by pending_bytes, plus its envelope.
	sc.Buffer(make([]byte, 4096), pendingBytes+4096)
	if !sc.Scan() {
		return streamErr(ctx, lost(sc.Err()))
	}
	var reply struct {
		OK    bool   `json:"ok"`
		Error string `json:"error"`
		Code  string `json:"code"`
	}
	if err := json.Unmarshal(sc.Bytes(), &reply); err != nil {
		return fmt.Errorf("subscribe reply: %v", err)
	}
	if !reply.OK {
		return &Error{Code: reply.Code, Message: reply.Error}
	}
	for sc.Scan() {
		var ev Event
		if err := json.Unmarshal(sc.Bytes(), &ev); err != nil {
			return fmt.Errorf("event line: %v", err)
		}
		if ev.Event == "" {
			// A reply on this connection; Stream sends no further requests.
			continue
		}
		if err := fn(ev); err != nil {
			return err
		}
	}
	return streamErr(ctx, lost(sc.Err()))
}

func lost(err error) error {
	if err == nil {
		return errors.New("event connection closed")
	}
	return err
}

// streamErr reports ctx's error when the failure was Stream's own cancel.
func streamErr(ctx context.Context, err error) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return err
}
