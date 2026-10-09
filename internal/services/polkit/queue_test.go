package polkit

import (
	"runtime"
	"testing"
	"time"
)

func newTestRequest(cookie string) Request {
	req := newRequest()
	req.Cookie = cookie
	return req
}

func pushActive(t *testing.T, q *queue, out chan Request, cookie string) Request {
	t.Helper()
	if _, err := q.push(newTestRequest(cookie)); err != nil {
		t.Fatalf("push %s: %v", cookie, err)
	}
	return displayed(t, out)
}

func enqueueBehind(t *testing.T, q *queue, cookie string) (Request, <-chan error) {
	t.Helper()
	want := q.waiting() + 1
	req := newTestRequest(cookie)
	result := make(chan error, 1)
	go func() {
		_, err := q.push(req)
		result <- err
	}()
	waitForWaiting(t, q, want)
	return req, result
}

func displayed(t *testing.T, out chan Request) Request {
	t.Helper()
	select {
	case got := <-out:
		return got
	case <-time.After(time.Second):
		t.Fatal("no request reached the display channel")
		return Request{}
	}
}

func waitForWaiting(t *testing.T, q *queue, want int) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for q.waiting() != want {
		if time.Now().After(deadline) {
			t.Fatalf("waiting = %d, want %d", q.waiting(), want)
		}
		runtime.Gosched()
	}
}

func expectNoResult(t *testing.T, result <-chan error) {
	t.Helper()
	select {
	case err := <-result:
		t.Fatalf("queued request returned before activation: %v", err)
	default:
	}
}

func TestQueueServesOneAtATimeInOrder(t *testing.T) {
	out := make(chan Request, 1)
	q := &queue{out: out}
	first := pushActive(t, q, out, "a")
	second, secondResult := enqueueBehind(t, q, "b")
	expectNoResult(t, secondResult)

	q.finish(first.Cookie)
	if err := <-secondResult; err != nil {
		t.Fatalf("push second: %v", err)
	}
	shown := displayed(t, out)
	if shown.Cookie != second.Cookie {
		t.Fatalf("displayed %q, want b", shown.Cookie)
	}
	q.finish(shown.Cookie)
	if waiting := q.waiting(); waiting != 0 {
		t.Fatalf("waiting = %d, want 0", waiting)
	}
}

func TestQueueRefusesPastTheLimit(t *testing.T) {
	out := make(chan Request, 1)
	q := &queue{out: out}
	active := pushActive(t, q, out, "active")
	var queued []Request
	for i := 0; i < maxPending; i++ {
		req, _ := enqueueBehind(t, q, string(rune('a'+i)))
		queued = append(queued, req)
	}
	if _, err := q.push(newTestRequest("one too many")); err != errQueueFull {
		t.Fatalf("push past the limit = %v, want %v", err, errQueueFull)
	}
	if waiting := q.waiting(); waiting != maxPending {
		t.Fatalf("waiting = %d, want %d", waiting, maxPending)
	}
	q.cancelAll()
	<-active.Done()
	for _, req := range queued {
		<-req.Done()
	}
}

func TestQueueHoldsWhileLocked(t *testing.T) {
	out := make(chan Request, 1)
	q := &queue{out: out}
	q.hold(true)
	req := newTestRequest("while locked")
	result := make(chan error, 1)
	go func() {
		_, err := q.push(req)
		result <- err
	}()
	waitForWaiting(t, q, 1)
	expectNoResult(t, result)
	q.hold(false)
	if err := <-result; err != nil {
		t.Fatalf("push after unlock: %v", err)
	}
	if got := displayed(t, out); got.Cookie != "while locked" {
		t.Fatalf("displayed %q after release", got.Cookie)
	}
}

func TestQueueWithdrawPendingCancelsItsCaller(t *testing.T) {
	out := make(chan Request, 1)
	q := &queue{out: out}
	active := pushActive(t, q, out, "active")
	queued := newTestRequest("queued")
	queued.ActionID = "org.example.Queued"
	queued.Details = map[string]string{"polkit.caller-pid": "42"}
	result := make(chan error, 1)
	go func() {
		_, err := q.push(queued)
		result <- err
	}()
	waitForWaiting(t, q, 1)
	withdrawn, ok := q.withdraw("queued")
	if !ok {
		t.Fatal("withdraw of a queued request reported nothing withdrawn")
	}
	if !withdrawn.queued || withdrawn.request.ActionID != queued.ActionID || withdrawn.request.Details["polkit.caller-pid"] != "42" {
		t.Fatalf("queued withdrawal = %+v", withdrawn)
	}
	if err := <-result; err != errQueueCancelled {
		t.Fatalf("queued push after withdrawal = %v, want cancellation", err)
	}
	select {
	case <-queued.Done():
	default:
		t.Fatal("withdrawn request was not signalled")
	}
	if q.waiting() != 0 {
		t.Fatalf("waiting = %d after withdrawal", q.waiting())
	}
	q.finish(active.Cookie)
}

func TestQueueWithdrawDisplayedSignalsCaller(t *testing.T) {
	out := make(chan Request, 1)
	q := &queue{out: out}
	active := pushActive(t, q, out, "active")
	withdrawn, ok := q.withdraw(active.Cookie)
	if !ok {
		t.Fatal("withdraw of displayed request reported nothing withdrawn")
	}
	if withdrawn.queued || withdrawn.request.Cookie != active.Cookie {
		t.Fatalf("displayed withdrawal = %+v", withdrawn)
	}
	select {
	case <-active.Done():
	default:
		t.Fatal("displayed request was not signalled")
	}
	if _, ok := q.withdraw("no such cookie"); ok {
		t.Fatal("withdraw of unknown cookie reported a withdrawal")
	}
	q.finish(active.Cookie)
}

func TestWaitStartedPrefersDispatchWhenCancelled(t *testing.T) {
	for range 256 {
		started := make(chan struct{})
		cancelled := make(chan struct{})
		close(started)
		close(cancelled)
		if !waitStarted(started, cancelled) {
			t.Fatal("a dispatched request was reported cancelled")
		}
	}

	started := make(chan struct{})
	cancelled := make(chan struct{})
	close(cancelled)
	if waitStarted(started, cancelled) {
		t.Fatal("a request cancelled before dispatch was reported started")
	}
}

func TestQueueCancelAllUnblocksEveryRequest(t *testing.T) {
	out := make(chan Request, 1)
	q := &queue{out: out}
	active := pushActive(t, q, out, "active")
	queued, result := enqueueBehind(t, q, "queued")
	q.cancelAll()
	<-active.Done()
	<-queued.Done()
	if err := <-result; err != errQueueCancelled {
		t.Fatalf("queued push after cancelAll = %v, want cancellation", err)
	}
	if waiting := q.waiting(); waiting != 0 {
		t.Fatalf("waiting = %d after cancelAll", waiting)
	}
	if _, err := q.push(newTestRequest("after")); err != nil {
		t.Fatalf("push after cancelAll: %v", err)
	}
	if got := displayed(t, out); got.Cookie != "after" {
		t.Fatalf("displayed %q after cancelAll, want after", got.Cookie)
	}
}

func TestQueueCancelAllReturnsRequestsForNotifications(t *testing.T) {
	out := make(chan Request, 1)
	q := &queue{out: out}
	active := newTestRequest("active")
	active.ActionID = "org.example.Active"
	active.Details = map[string]string{"polkit.caller-pid": "41"}
	if _, err := q.push(active); err != nil {
		t.Fatal(err)
	}
	_ = displayed(t, out)
	queued := newTestRequest("queued")
	queued.ActionID = "org.example.Queued"
	result := make(chan error, 1)
	go func() {
		_, err := q.push(queued)
		result <- err
	}()
	waitForWaiting(t, q, 1)
	withdrawn := q.cancelAll()
	if len(withdrawn) != 2 || withdrawn[0].ActionID != active.ActionID || withdrawn[1].ActionID != queued.ActionID {
		t.Fatalf("cancelled requests = %+v", withdrawn)
	}
	if err := <-result; err != errQueueCancelled {
		t.Fatalf("queued request after cancellation = %v", err)
	}
}

func TestQueueDropsLateFinishAndRejectsDuplicateCookies(t *testing.T) {
	out := make(chan Request, 1)
	q := &queue{out: out}
	active := pushActive(t, q, out, "active")
	if _, err := q.push(newTestRequest(active.Cookie)); err != errQueueCookie {
		t.Fatalf("duplicate cookie push = %v, want %v", err, errQueueCookie)
	}
	q.finish(active.Cookie)
	q.finish(active.Cookie)
	if q.active != nil {
		t.Fatal("late finish restored a completed request")
	}
}
