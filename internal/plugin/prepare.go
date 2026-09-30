package plugin

import (
	"sync"
	"time"

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
	// Events remains paired with this prepared revision while a newer wire
	// tree is waiting for layout.
	Events map[string][]v1.EventKind
	Err    error
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
	measure  ui.MeasureText
	results  chan Result
	now      func() time.Time
	budget   time.Duration
	schedule *Schedule

	mu       sync.Mutex
	cond     *sync.Cond
	pending  map[string]Job
	waiting  map[string][]string
	plugins  []string
	overruns map[string][]time.Time
	degraded map[string]bool
	closed   bool

	done chan struct{}
	wg   sync.WaitGroup
}

const (
	layoutBudget  = 8 * time.Millisecond
	overrunWindow = 10 * time.Second
	overrunLimit  = 3
)

// NewPreparer starts a pool of workers.
func NewPreparer(workers int, measure ui.MeasureText) *Preparer {
	if workers < 1 {
		workers = 1
	}
	p := &Preparer{
		measure:  measure,
		results:  make(chan Result, workers*4),
		now:      time.Now,
		budget:   layoutBudget,
		pending:  make(map[string]Job),
		waiting:  make(map[string][]string),
		overruns: make(map[string][]time.Time),
		degraded: make(map[string]bool),
		done:     make(chan struct{}),
	}
	// The schedule reads the clock through the field, so a test that swaps
	// now for a fake clock moves the publish gate with it.
	p.schedule = NewSchedule(func() time.Time { return p.now() })
	p.cond = sync.NewCond(&p.mu)
	p.wg.Add(workers + 1)
	for i := 0; i < workers; i++ {
		go p.work()
	}
	go p.tick()
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
		return
	}
	if p.degraded[j.Plugin] {
		return
	}
	if _, waiting := p.pending[j.ViewID]; !waiting {
		if len(p.waiting[j.Plugin]) == 0 {
			p.plugins = append(p.plugins, j.Plugin)
		}
		p.waiting[j.Plugin] = append(p.waiting[j.Plugin], j.ViewID)
	}
	p.pending[j.ViewID] = j
	p.cond.Signal()
}

// Degraded reports whether plugin's layout work is suppressed.
func (p *Preparer) Degraded(plugin string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.degraded[plugin]
}

// Recover clears layout-budget degradation, as a clean snapshot or restart does.
func (p *Preparer) Recover(plugin string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	delete(p.degraded, plugin)
	delete(p.overruns, plugin)
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
	p.waiting = nil
	p.plugins = nil
	p.mu.Unlock()

	close(p.done)
	p.cond.Broadcast()
	p.wg.Wait()
	close(p.results)
}

// tick wakes the workers on the publish interval. A view held back by the
// rate gate is prepared as soon as it is due, even when no further update
// arrives to signal the condition.
func (p *Preparer) tick() {
	defer p.wg.Done()
	t := time.NewTicker(publishInterval)
	defer t.Stop()
	for {
		select {
		case <-p.done:
			return
		case <-t.C:
			p.cond.Broadcast()
		}
	}
}

// work takes the oldest waiting view and prepares whatever its newest job is.
func (p *Preparer) work() {
	defer p.wg.Done()
	for {
		j, ok := p.next()
		if !ok {
			return
		}
		select {
		case p.results <- p.prepare(j):
		case <-p.done:
			return
		}
	}
}

func (p *Preparer) next() (Job, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for {
		if p.closed {
			return Job{}, false
		}
		for i, plugin := range p.plugins {
			ids := p.waiting[plugin]
			if len(ids) == 0 {
				continue
			}
			id := ids[0]
			// One view commits at most once per publish interval. A view
			// that is not due stays pending, so the newest job is the one
			// that eventually goes out; the ticker wakes the pool when it
			// comes due.
			if !p.schedule.Due(id) {
				continue
			}
			p.plugins = append(p.plugins[:i], p.plugins[i+1:]...)
			p.waiting[plugin] = ids[1:]
			if len(p.waiting[plugin]) == 0 {
				delete(p.waiting, plugin)
			} else {
				p.plugins = append(p.plugins, plugin)
			}
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
	start := p.now()

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
	p.noteDuration(j.Plugin, start, p.now())
	if err != nil {
		out.Err = err
		return out
	}
	out.Root = root
	out.Events = preparedEvents(j.Root)
	return out
}

func preparedEvents(root *v1.Node) map[string][]v1.EventKind {
	events := make(map[string][]v1.EventKind)
	var walk func(*v1.Node)
	walk = func(node *v1.Node) {
		if node == nil {
			return
		}
		if node.ID != "" && len(node.Events) != 0 {
			events[node.ID] = append([]v1.EventKind(nil), node.Events...)
		}
		for _, child := range node.Children {
			walk(child)
		}
	}
	walk(root)
	return events
}

func (p *Preparer) noteDuration(plugin string, start, end time.Time) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if plugin == "" {
		return
	}
	if end.Sub(start) <= p.budget {
		delete(p.degraded, plugin)
		delete(p.overruns, plugin)
		return
	}
	times := append(p.overruns[plugin], end)
	cutoff := end.Add(-overrunWindow)
	n := 0
	for _, t := range times {
		if !t.Before(cutoff) {
			times[n] = t
			n++
		}
	}
	p.overruns[plugin] = times[:n]
	if n >= overrunLimit {
		p.degraded[plugin] = true
	}
}
