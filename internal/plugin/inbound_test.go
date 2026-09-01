package plugin

import (
	"testing"
	"time"

	v1 "github.com/Nomadcxx/sysc-shell/plugin/v1"
)

func TestInboundAllowsABurstThenSustains(t *testing.T) {
	t.Parallel()
	now := time.Unix(0, 0)
	in := NewInbound(func() time.Time { return now }, v1.DefaultLimits)

	for i := 0; i < v1.DefaultLimits.UpdateBurst; i++ {
		if !in.Allow() {
			t.Fatalf("burst token %d refused", i)
		}
	}
	if in.Allow() {
		t.Fatal("burst+1 was allowed")
	}

	now = now.Add(time.Second)
	var got int
	for in.Allow() {
		got++
		if got > v1.DefaultLimits.UpdatesPerSecond+1 {
			break
		}
	}
	if got != v1.DefaultLimits.UpdatesPerSecond {
		t.Fatalf("sustained = %d, want %d", got, v1.DefaultLimits.UpdatesPerSecond)
	}
}

func TestInboundDiscardsBeforeDecodeAfterExhaustion(t *testing.T) {
	t.Parallel()
	now := time.Unix(0, 0)
	in := NewInbound(func() time.Time { return now }, v1.DefaultLimits)
	for i := 0; i < v1.DefaultLimits.UpdateBurst; i++ {
		in.Allow()
	}
	raw := []byte(`{"type":"view.snapshot","view_id":"v1","revision":1,"root":{"kind":"row"}}`)
	if _, ok := in.Accept(raw); ok {
		t.Fatal("exhausted inbound decoded a discarded line")
	}
}

func TestInboundDegradesAfterRepeatedViolations(t *testing.T) {
	t.Parallel()
	now := time.Unix(0, 0)
	in := NewInbound(func() time.Time { return now }, v1.DefaultLimits)
	for i := 0; i < v1.DefaultLimits.UpdateBurst; i++ {
		in.Allow()
	}
	for i := 0; i < 3; i++ {
		if in.Allow() {
			t.Fatal("exhausted allow succeeded")
		}
	}
	if !in.Degraded() {
		t.Fatal("repeated violations did not degrade the plugin")
	}
}

func TestQueueKeepsOneOverwriteSlotPerView(t *testing.T) {
	t.Parallel()
	q := NewQueue()
	q.PushView("v1", &v1.ViewSnapshot{ViewID: "v1", Revision: 1})
	q.PushView("v1", &v1.ViewSnapshot{ViewID: "v1", Revision: 9})
	q.PushView("v2", &v1.ViewPatch{ViewID: "v2", Revision: 2})
	got := q.TakeViews()
	if len(got) != 2 {
		t.Fatalf("views = %d, want 2", len(got))
	}
	if got["v1"].(*v1.ViewSnapshot).Revision != 9 {
		t.Fatal("older snapshot was not overwritten")
	}
}

func TestQueueCapsOrderedControlMessages(t *testing.T) {
	t.Parallel()
	q := NewQueue()
	for i := 0; i < 40; i++ {
		q.PushControl(&v1.HostCall{ID: string(rune('a' + i%26)), Call: v1.CallStateGet})
	}
	got := q.TakeControl()
	if len(got) != 32 {
		t.Fatalf("control = %d, want 32", len(got))
	}
	if got[0].(*v1.HostCall).ID != "a" {
		t.Fatalf("first = %q, want the oldest retained call", got[0].(*v1.HostCall).ID)
	}
}

func TestSchedulePublishesAtThirtyHertz(t *testing.T) {
	t.Parallel()
	now := time.Unix(0, 0)
	s := NewSchedule(func() time.Time { return now })
	if !s.Due("v1") {
		t.Fatal("first publish was not due")
	}
	now = now.Add(20 * time.Millisecond)
	if s.Due("v1") {
		t.Fatal("second publish within 33ms was due")
	}
	now = now.Add(20 * time.Millisecond)
	if !s.Due("v1") {
		t.Fatal("publish after 40ms was not due")
	}
}
