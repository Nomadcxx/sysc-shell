package shell

import (
	"strings"
	"testing"

	"github.com/Nomadcxx/sysc-notify/protocol"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

func TestWrapLines(t *testing.T) {
	measure := func(s string) int { return len(s) * 10 }
	got := wrapLines("the quick brown fox jumps over the lazy dog", 100, measure, 8)
	want := []string{"the quick", "brown fox", "jumps over", "the lazy", "dog"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("got %q", got)
	}
	long := wrapLines("abcdefghijklmnopqrstuvwxyz", 100, measure, 8)
	if long[0] != "abcdefghij" || long[1] != "klmnopqrst" {
		t.Fatalf("hard break: %q", long)
	}
	capped := wrapLines(strings.Repeat("word ", 40), 100, measure, 3)
	if len(capped) != 3 || !strings.HasSuffix(capped[2], "…") {
		t.Fatalf("cap: %q", capped)
	}
}

func TestExpandedNotificationCardUsesWrappedBody(t *testing.T) {
	root := ExpandedNotificationCard(protocol.Notification{ID: 1, Body: "one two"}, nil, false, nil,
		func(string) []string { return []string{"one", "two"} })
	var got []string
	var walk func(*ui.Node)
	walk = func(n *ui.Node) {
		if n == nil {
			return
		}
		if n.Kind == ui.KindText {
			got = append(got, n.Text)
		}
		for _, child := range n.Children {
			walk(child)
		}
	}
	walk(root)
	if !strings.Contains(strings.Join(got, "|"), "one|two") {
		t.Fatalf("text nodes = %q", got)
	}
}
