package plugin

import (
	"testing"

	v1 "github.com/Nomadcxx/sysc-shell/plugin/v1"
)

func TestTextOutCoalescesChangeAndOrdersSubmit(t *testing.T) {
	t.Parallel()
	var o TextOut
	if got := o.Push(v1.InputEvent{Node: "n", Event: v1.EventChange, Text: "a"}); got != nil {
		t.Fatalf("first change sent immediately: %+v", got)
	}
	if got := o.Push(v1.InputEvent{Node: "n", Event: v1.EventChange, Text: "ab"}); got != nil {
		t.Fatalf("second change sent immediately: %+v", got)
	}
	got := o.Push(v1.InputEvent{Node: "n", Event: v1.EventSubmit, Text: "ab"})
	if len(got) != 2 {
		t.Fatalf("submit drained %d events, want change then submit", len(got))
	}
	if got[0].Event != v1.EventChange || got[0].Text != "ab" {
		t.Fatalf("coalesced change = %+v", got[0])
	}
	if got[1].Event != v1.EventSubmit {
		t.Fatalf("second event = %+v", got[1])
	}
	if extra := o.Flush(); len(extra) != 0 {
		t.Fatalf("flush after submit still held %+v", extra)
	}
}

// A view that closes takes its unsent text with it unless the close drains
// it first; other views keep theirs.
func TestTextOutTakeDrainsOneView(t *testing.T) {
	var o TextOut
	o.Push(v1.InputEvent{ViewID: "a", Node: "body", Event: v1.EventChange, Text: "last words"})
	o.Push(v1.InputEvent{ViewID: "b", Node: "body", Event: v1.EventChange, Text: "other"})
	got := o.Take("a")
	if len(got) != 1 || got[0].Text != "last words" {
		t.Fatalf("Take(a) = %+v, want the pending change", got)
	}
	if again := o.Take("a"); len(again) != 0 {
		t.Fatalf("second Take(a) = %+v, want nothing", again)
	}
	if rest := o.Flush(); len(rest) != 1 || rest[0].ViewID != "b" {
		t.Fatalf("Flush = %+v, want only view b", rest)
	}
}
