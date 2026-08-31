package plugin

import (
	"sync"

	"github.com/Nomadcxx/sysc-shell/internal/ui"
	v1 "github.com/Nomadcxx/sysc-shell/plugin/v1"
)

// Job is one request to turn a wire tree into a painted-ready shell tree.
type Job struct {
	ViewID   string
	Plugin   string
	View     v1.ViewKind
	Revision uint64
	Root     *v1.Node
	// Bounds is the space the host has reserved. It comes from the shell, not
	// from the plugin: a wire tree can never supply its own geometry.
	Bounds ui.Rect
}

// Result is one prepared view, or the reason it could not be prepared.
//
// A failure names one view and only that view. A tree the shell cannot render
// removes the plugin's widget from its slot; it does not disturb the plugin's
// other views, other plugins, or the shell's own.
type Result struct {
	ViewID   string
	Plugin   string
	Revision uint64
	Root     *ui.Node
	Err      error
}

// Preparer converts and lays out plugin views on a fixed pool of workers.
//
// It exists to keep two things apart. Validation, conversion, and layout are
// unbounded in the sense that matters: their cost depends on what a plugin
// sent. The goroutine that dispatches Wayland events is not allowed to wait on
// any of that, so Submit only ever records work, and the shell receives
// finished, immutable trees through Results.
//
// Each view keeps exactly one pending job. A plugin that updates faster than
// the shell can lay out must not build a backlog: the user only ever sees the
// newest tree, so preparing the intermediate ones would be work with no
// consumer, done while the newest one waits.
type Preparer struct {
	measure ui.MeasureText
	results chan Result

	mu      sync.Mutex
	cond    *sync.Cond
	pending map[string]Job
	// order holds the view ids waiting, oldest first, so one busy view cannot
	// starve the others.
	order  []string
	closed bool

	wg sync.WaitGroup
}

// NewPreparer starts a pool of workers.
func NewPreparer(workers int, measure ui.MeasureText) *Preparer {
	if workers < 1 {
		workers = 1
	}
	p := &Preparer{
		measure: measure,
		results: make(chan Result, workers*4),
		pending: make(map[string]Job),
	}
	p.cond = sync.NewCond(&p.mu)
	p.wg.Add(workers)
	for i := 0; i < workers; i++ {
		go p.work()
	}
	return p
}

// Results carries finished views.
func (p *Preparer) Results() <-chan Result { return p.results }

// Submit queues one view for preparation, replacing any work still pending for
// the same view. It never blocks and never fails: a caller on the shell's
// dispatch path has nothing useful to do with either.
func (p *Preparer) Submit(j Job) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		// Closing races with a plugin still producing views. Dropping the work
		// is the only correct answer once the pool has gone.
		return
	}
	if _, waiting := p.pending[j.ViewID]; !waiting {
		p.order = append(p.order, j.ViewID)
	}
	p.pending[j.ViewID] = j
	p.cond.Signal()
}

// Close stops the workers. It is safe to call more than once.
func (p *Preparer) Close() {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return
	}
	p.closed = true
	p.pending = nil
	p.order = nil
	p.mu.Unlock()

	p.cond.Broadcast()
	p.wg.Wait()
	close(p.results)
}

// work takes the oldest waiting view and prepares whatever its newest job is.
func (p *Preparer) work() {
	defer p.wg.Done()
	for {
		j, ok := p.next()
		if !ok {
			return
		}
		p.results <- p.prepare(j)
	}
}

func (p *Preparer) next() (Job, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for {
		if p.closed {
			return Job{}, false
		}
		if len(p.order) > 0 {
			id := p.order[0]
			p.order = p.order[1:]
			j := p.pending[id]
			delete(p.pending, id)
			return j, true
		}
		p.cond.Wait()
	}
}

// prepare converts and lays out one tree. Every node it publishes is freshly
// allocated, so a result the shell is painting cannot be disturbed by the next
// revision of the same view.
func (p *Preparer) prepare(j Job) Result {
	out := Result{ViewID: j.ViewID, Plugin: j.Plugin, Revision: j.Revision}

	root, err := Convert(j.Root, j.View)
	if err != nil {
		out.Err = err
		return out
	}
	if j.View == v1.ViewBar {
		err = ui.Layout(root, j.Bounds, p.measure)
	} else {
		err = ui.LayoutColumn(root, j.Bounds, p.measure)
	}
	if err != nil {
		out.Err = err
		return out
	}
	out.Root = root
	return out
}
