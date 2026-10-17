package ipc

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestStreamDeliversAndCancels(t *testing.T) {
	topic := &fakeTopic{initial: "zero"}
	sock := startServerWith(t, Handlers{Topics: map[string]Provider{"fake": topic.provider()}}, nil)
	ctx, cancel := context.WithCancel(context.Background())
	events := make(chan Event, 8)
	done := make(chan error, 1)
	go func() {
		done <- Stream(ctx, sock, []string{"fake"}, func(ev Event) error {
			events <- ev
			return nil
		})
	}()
	next := func() Event {
		select {
		case ev := <-events:
			return ev
		case <-time.After(2 * time.Second):
			t.Fatal("no event")
			return Event{}
		}
	}
	if ev := next(); ev.Event != EventSnapshot || string(ev.Data) != `"zero"` {
		t.Fatalf("first event = %+v", ev)
	}
	topic.pub("one")
	if ev := next(); ev.Event != EventState || string(ev.Data) != `"one"` {
		t.Fatalf("second event = %+v", ev)
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Stream after cancel = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Stream ignored its context")
	}
	waitFor(t, "the provider to stop after cancel", func() bool { return topic.stops.Load() == 1 })

	err := Stream(context.Background(), sock, []string{"nope"}, func(Event) error { return nil })
	if !isCode(err, CodeUnsupported) {
		t.Fatalf("unknown topic = %v, want unsupported", err)
	}
}
