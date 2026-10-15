package main

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Nomadcxx/sysc-shell/internal/config"
)

func TestReloadRelayKeepsLatestOutcomeWithoutBlockingOwner(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	delivered := make(chan string, 3)
	var calls atomic.Int32
	relay := newReloadRelay(context.Background(), func(err error) {
		if calls.Add(1) == 1 {
			close(started)
			<-release
		}
		if err == nil {
			delivered <- "clear"
			return
		}
		delivered <- err.Error()
	})
	t.Cleanup(relay.stop)

	first := &config.Diagnostic{Field: "bar.height", Message: "first"}
	relay.submit(first)
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("relay did not enter the blocked sink")
	}
	first.Message = "mutated after callback"

	queued := make(chan struct{})
	go func() {
		relay.submit(&config.Diagnostic{Field: "bar.height", Message: "second"})
		relay.submit(nil)
		close(queued)
	}()
	select {
	case <-queued:
	case <-time.After(time.Second):
		t.Fatal("owner callback waited on the blocked relay sink")
	}
	close(release)

	got := []string{<-delivered, <-delivered}
	if !strings.Contains(got[0], "first") || got[0] == "" || got[1] != "clear" {
		t.Fatalf("delivered outcomes = %q, want immutable first then latest clear", got)
	}
}

func TestReloadRelayJoinsAndDropsOutcomesAfterStop(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	var calls atomic.Int32
	relay := newReloadRelay(context.Background(), func(error) {
		calls.Add(1)
		close(started)
		<-release
	})
	relay.submit(errors.New("secret-token"))
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("relay did not start its sink")
	}
	stopped := make(chan struct{})
	go func() { relay.stop(); close(stopped) }()
	select {
	case <-stopped:
		t.Fatal("stop returned while the sink was still active")
	case <-time.After(20 * time.Millisecond):
	}
	close(release)
	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("stop did not join the worker")
	}
	relay.submit(errors.New("later"))
	if got := calls.Load(); got != 1 {
		t.Fatalf("sink calls after stop = %d, want 1", got)
	}
}
