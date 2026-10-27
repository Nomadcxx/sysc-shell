package main

import (
	"context"
	"errors"
	"sync"

	"github.com/Nomadcxx/sysc-shell/internal/config"
)

const genericReloadOutcome = "configuration reload was rejected"

type reloadRelay struct {
	mu      sync.Mutex
	queue   chan error
	done    chan struct{}
	cancel  context.CancelFunc
	stopped bool
}

func newReloadRelay(parent context.Context, sink func(error)) *reloadRelay {
	if sink == nil {
		sink = func(error) {}
	}
	ctx, cancel := context.WithCancel(parent)
	r := &reloadRelay{
		queue:  make(chan error, 1),
		done:   make(chan struct{}),
		cancel: cancel,
	}
	go func() {
		defer close(r.done)
		for {
			select {
			case <-ctx.Done():
				return
			case outcome := <-r.queue:
				sink(outcome)
			}
		}
	}()
	return r
}

func queueReload(reloads chan<- struct{}) {
	select {
	case reloads <- struct{}{}:
	default:
	}
}

// submit snapshots the owner's error before placing the latest result in the
// one-slot queue. The Wayland callback and the startup path are the producers.
func (r *reloadRelay) submit(err error) {
	if r == nil {
		return
	}
	outcome := safeReloadOutcome(err)
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.stopped {
		return
	}
	select {
	case r.queue <- outcome:
		return
	default:
	}
	select {
	case <-r.queue:
	default:
	}
	select {
	case r.queue <- outcome:
	default:
	}
}

func (r *reloadRelay) stop() {
	if r == nil {
		return
	}
	r.mu.Lock()
	if !r.stopped {
		r.stopped = true
		r.cancel()
	}
	done := r.done
	r.mu.Unlock()
	<-done
}

func safeReloadOutcome(err error) error {
	if err == nil {
		return nil
	}
	var diagnostic *config.Diagnostic
	if errors.As(err, &diagnostic) && diagnostic != nil {
		return &config.Diagnostic{
			File:    diagnostic.File,
			Field:   diagnostic.Field,
			Message: diagnostic.Message,
			Excerpt: diagnostic.Excerpt,
			Line:    diagnostic.Line,
			Column:  diagnostic.Column,
		}
	}
	return errors.New(genericReloadOutcome)
}
