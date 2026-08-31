package plugin

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Nomadcxx/sysc-shell/internal/ui"
	v1 "github.com/Nomadcxx/sysc-shell/plugin/v1"
)

// measureFixed gives every glyph a width of 8 and every line a height of 16.
func measureFixed(s string, _ bool) (int, int) { return len(s) * 8, 16 }

func barJob(viewID string, revision uint64, root *v1.Node) Job {
	return Job{
		ViewID:   viewID,
		Plugin:   "org.sysc.timer",
		View:     v1.ViewBar,
		Revision: revision,
		Root:     root,
		Bounds:   ui.Rect{W: 400, H: 32},
	}
}

// await takes the next result or fails the test.
func await(t *testing.T, p *Preparer) Result {
	t.Helper()
	select {
	case r := <-p.Results():
		return r
	case <-time.After(5 * time.Second):
		t.Fatal("no prepared view arrived")
		return Result{}
	}
}

func TestPreparerLaysOutABarView(t *testing.T) {
	t.Parallel()

	p := NewPreparer(2, measureFixed)
	defer p.Close()

	p.Submit(barJob("v1", 7, timerBar()))
	got := await(t, p)
	if got.Err != nil {
		t.Fatalf("prepare: %v", got.Err)
	}
	if got.ViewID != "v1" || got.Revision != 7 {
		t.Fatalf("result = %+v", got)
	}
	if got.Root.Bounds != (ui.Rect{W: 400, H: 32}) {
		t.Errorf("root bounds = %+v, want the submitted bounds", got.Root.Bounds)
	}
	// Layout is what the preparer adds: the tree it publishes is arranged.
	if got.Root.Children[0].Bounds.W == 0 {
		t.Error("the published tree was never laid out")
	}
}

func TestPreparerLaysOutAPanelAsAColumn(t *testing.T) {
	t.Parallel()

	p := NewPreparer(2, measureFixed)
	defer p.Close()

	job := Job{
		ViewID: "p1", Plugin: "org.sysc.timer", View: v1.ViewPanel, Revision: 1,
		Root: &v1.Node{Kind: v1.KindColumn, Padding: 8, Gap: 4, Children: []*v1.Node{
			{Kind: v1.KindText, Text: "05:00", Tabular: true},
			{Kind: v1.KindProgress, Value: 0.5},
		}},
		Bounds: ui.Rect{W: 320, H: 280},
	}
	p.Submit(job)
	got := await(t, p)
	if got.Err != nil {
		t.Fatalf("prepare: %v", got.Err)
	}
	first, second := got.Root.Children[0].Bounds, got.Root.Children[1].Bounds
	if second.Y <= first.Y {
		t.Errorf("children were not stacked: %+v then %+v", first, second)
	}
}

func TestPreparerReportsAnInvalidTreeWithoutStopping(t *testing.T) {
	t.Parallel()

	p := NewPreparer(2, measureFixed)
	defer p.Close()

	// A malformed view removes only that view. The preparer keeps serving.
	p.Submit(barJob("bad", 1, &v1.Node{Kind: v1.KindRow, Children: []*v1.Node{
		{Kind: v1.KindButton, ID: "b", Text: "x", Role: "button", Events: []v1.EventKind{v1.EventActivate}},
	}}))
	if got := await(t, p); got.Err == nil {
		t.Fatal("an invalid tree was published")
	}

	p.Submit(barJob("good", 1, timerBar()))
	if got := await(t, p); got.Err != nil {
		t.Fatalf("the preparer stopped after one bad view: %v", got.Err)
	}
}

func TestSubmitDoesNotBlockWhileEveryWorkerIsBusy(t *testing.T) {
	t.Parallel()

	// Submit is called from the goroutine that also drives Wayland. If it
	// could block behind a slow layout, one plugin would stall the whole
	// shell's dispatch.
	release := make(chan struct{})
	var once sync.Once
	slow := func(s string, tabular bool) (int, int) {
		<-release
		return measureFixed(s, tabular)
	}
	defer once.Do(func() { close(release) })

	p := NewPreparer(1, slow)
	defer p.Close()

	p.Submit(barJob("busy", 1, timerBar()))

	done := make(chan struct{})
	go func() {
		for i := 2; i < 12; i++ {
			p.Submit(barJob("queued", uint64(i), timerBar()))
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Submit blocked while the only worker was busy")
	}
	once.Do(func() { close(release) })
}

func TestOnePendingJobPerViewReplacesOlderWork(t *testing.T) {
	t.Parallel()

	// A plugin that updates faster than the shell can lay out must not build a
	// backlog: the user only ever sees the newest tree, so preparing the
	// intermediate ones is work with no consumer.
	release := make(chan struct{})
	var prepared atomic.Int64
	slow := func(s string, tabular bool) (int, int) {
		<-release
		prepared.Add(1)
		return measureFixed(s, tabular)
	}

	p := NewPreparer(1, slow)
	defer p.Close()

	p.Submit(barJob("blocker", 1, timerBar()))
	// Wait until the worker is inside the blocked measure, so the rest of the
	// submissions have to queue behind it.
	time.Sleep(50 * time.Millisecond)

	for rev := uint64(2); rev <= 20; rev++ {
		p.Submit(barJob("v1", rev, timerBar()))
	}
	close(release)

	first := await(t, p)
	if first.ViewID != "blocker" {
		t.Fatalf("first result was %q, want the blocked job", first.ViewID)
	}
	second := await(t, p)
	if second.ViewID != "v1" {
		t.Fatalf("second result was %q, want the coalesced view", second.ViewID)
	}
	if second.Revision != 20 {
		t.Fatalf("revision = %d, want only the newest of the nineteen queued", second.Revision)
	}
	select {
	case extra := <-p.Results():
		t.Fatalf("an intermediate revision was prepared as well: %+v", extra)
	case <-time.After(200 * time.Millisecond):
	}
}

func TestPreparerServesEveryViewNotOnlyTheBusiestOne(t *testing.T) {
	t.Parallel()

	p := NewPreparer(2, measureFixed)
	defer p.Close()

	for _, id := range []string{"a", "b", "c"} {
		p.Submit(barJob(id, 1, timerBar()))
	}
	seen := map[string]bool{}
	for i := 0; i < 3; i++ {
		seen[await(t, p).ViewID] = true
	}
	for _, id := range []string{"a", "b", "c"} {
		if !seen[id] {
			t.Errorf("view %q was never prepared", id)
		}
	}
}

func TestClosedPreparerAcceptsAndDropsWork(t *testing.T) {
	t.Parallel()

	p := NewPreparer(2, measureFixed)
	p.Close()
	// Closing races with a plugin still producing views. Submitting after the
	// close must be a no-op rather than a panic on a closed channel.
	p.Submit(barJob("late", 1, timerBar()))
	p.Close()
}

func TestPreparedTreesAreNotSharedBetweenRevisions(t *testing.T) {
	t.Parallel()

	p := NewPreparer(1, measureFixed)
	defer p.Close()

	root := timerBar()
	p.Submit(barJob("v1", 1, root))
	first := await(t, p)
	p.Submit(barJob("v1", 2, root))
	second := await(t, p)

	if first.Root == second.Root {
		t.Fatal("two revisions published the same tree")
	}
	if first.Root.Children[0] == second.Root.Children[0] {
		t.Fatal("two revisions share a child node")
	}
	// Publication is immutable: laying out the second must not have moved the
	// first, which the shell may still be painting.
	if first.Root.Bounds != second.Root.Bounds {
		t.Errorf("bounds differ between identical layouts: %+v and %+v", first.Root.Bounds, second.Root.Bounds)
	}
}
