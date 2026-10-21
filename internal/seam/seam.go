// Package seam guards the package-level functions that tests replace.
//
// A test swaps a seam while the shell's own goroutines keep running. An
// unguarded package-level variable is then written by the test and read by a
// production goroutine at the same moment, which the race detector reports
// and which can hand a production call the test's fake. Guard keeps the swap
// and the read behind one lock, so a swap cannot race with a concurrent read.
//
// Production code reads a seam through Get. Tests replace one through Swap
// and register the returned restore with t.Cleanup.
package seam

import "sync"

// Guard holds one replaceable value. Build one with New.
type Guard[T any] struct {
	mu sync.RWMutex
	fn T
}

// New returns a Guard holding fn.
func New[T any](fn T) *Guard[T] {
	return &Guard[T]{fn: fn}
}

// Get returns the value the guard holds now. A swap that happens while a
// caller holds the returned value cannot change that value under it.
func (g *Guard[T]) Get() T {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.fn
}

// Swap replaces the value and returns a function that puts the old one back.
// A test registers the returned function with t.Cleanup.
func (g *Guard[T]) Swap(fn T) func() {
	g.mu.Lock()
	old := g.fn
	g.fn = fn
	g.mu.Unlock()
	return func() { g.Swap(old) }
}
