package shell

import (
	"strings"
	"testing"
	"time"

	"github.com/Nomadcxx/sysc-notify/protocol"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

func baseNotification() protocol.Notification {
	return protocol.Notification{
		ID:              7,
		AppName:         "Mail",
		Summary:         "Two new messages",
		Body:            "one\n<b>two</b>",
		Urgency:         protocol.UrgencyNormal,
		Timestamp:       time.Unix(1_756_000_000, 0),
		ExpireTimeoutMS: 5000,
	}
}

func collectByKind(n *ui.Node, kind ui.Kind, out *[]*ui.Node) {
	if n.Kind == kind {
		*out = append(*out, n)
	}
	for _, c := range n.Children {
		collectByKind(c, kind, out)
	}
}

func texts(n *ui.Node) []string {
	var nodes []*ui.Node
	collectByKind(n, ui.KindText, &nodes)
	out := make([]string, 0, len(nodes))
	for _, c := range nodes {
		out = append(out, c.Text)
	}
	return out
}

func buttons(n *ui.Node) []*ui.Node {
	var nodes []*ui.Node
	collectByKind(n, ui.KindButton, &nodes)
	return nodes
}

func TestNotifyCardShowsSummaryBodyAndApp(t *testing.T) {
	card := NotificationCard(baseNotification(), nil, true)
	joined := strings.Join(texts(card), "\n")
	for _, want := range []string{"Mail", "Two new messages", "one", "two"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("card text %q lacks %q", joined, want)
		}
	}
}

func TestNotifyCardPreservesBodyStyles(t *testing.T) {
	card := NotificationCard(baseNotification(), nil, true)
	var styled *ui.Node
	var walk func(n *ui.Node)
	walk = func(n *ui.Node) {
		if n.Kind == ui.KindText && n.Text == "two" {
			styled = n
		}
		for _, c := range n.Children {
			walk(c)
		}
	}
	walk(card)
	if styled == nil || !styled.Bold {
		t.Fatalf("bold run lost its style: %+v", styled)
	}
}

func TestNotifyCardBuildsSixActionPairs(t *testing.T) {
	n := baseNotification()
	n.Actions = []protocol.Action{
		{Key: "default", Label: "Open"},
		{Key: "a1", Label: "One"},
		{Key: "a2", Label: "Two"},
		{Key: "a3", Label: "Three"},
		{Key: "a4", Label: "Four"},
		{Key: "a5", Label: "Five"},
		{Key: "a6", Label: "Six"},
	}
	card := NotificationCard(n, nil, true)
	got := buttons(card)
	var keys []string
	for _, b := range got {
		keys = append(keys, b.Action)
		if !b.Focusable || b.Role != "button" {
			t.Fatalf("action button %q is not an accessible control", b.Action)
		}
	}
	if len(keys) != 6 {
		t.Fatalf("buttons = %v, want 6 (the pairs, not the default body click)", keys)
	}
	for _, want := range []string{"a1", "a2", "a3", "a4", "a5", "a6"} {
		found := false
		for _, k := range keys {
			if strings.Contains(k, want) {
				found = true
			}
		}
		if !found {
			t.Fatalf("buttons %v lack %q", keys, want)
		}
	}
}

func TestNotifyCardMarksTheDefaultActionOnTheBody(t *testing.T) {
	n := baseNotification()
	n.Actions = []protocol.Action{{Key: "default", Label: "Open"}}
	card := NotificationCard(n, nil, true)
	found := false
	var walk func(n *ui.Node)
	walk = func(n *ui.Node) {
		if strings.Contains(n.Action, "default") {
			found = true
		}
		for _, c := range n.Children {
			walk(c)
		}
	}
	walk(card)
	if !found {
		t.Fatal("no node carries the default action")
	}
}

func TestNotifyCardAddsInlineReplyOnlyWhenAdvertised(t *testing.T) {
	n := baseNotification()
	without := NotificationCard(n, nil, true)
	var fields []*ui.Node
	collectByKind(without, ui.KindTextField, &fields)
	if len(fields) != 0 {
		t.Fatalf("reply field on a card that did not advertise it: %+v", fields[0])
	}

	n.InlineReply = true
	with := NotificationCard(n, nil, true)
	collectByKind(with, ui.KindTextField, &fields)
	if len(fields) != 1 || !fields[0].Focusable {
		t.Fatalf("inline reply = %+v, want one focusable field", fields)
	}
}

func TestNotifyCardRendersCriticalUrgencyAsErrorTone(t *testing.T) {
	n := baseNotification()
	n.Urgency = protocol.UrgencyCritical
	card := NotificationCard(n, nil, true)
	var found bool
	var walk func(n *ui.Node)
	walk = func(n *ui.Node) {
		if n.Kind == ui.KindText && n.Tone == ui.ToneError {
			found = true
		}
		for _, c := range n.Children {
			walk(c)
		}
	}
	walk(card)
	if !found {
		t.Fatal("critical card carries no error-tone text")
	}
}

func TestNotifyCardCountdownUsesTheAuthoritativeLifetime(t *testing.T) {
	n := baseNotification()
	lt := &protocol.Lifetime{ID: 7, DurationMS: 5000, RemainingMS: 3000, Running: true}
	card := NotificationCard(n, lt, true)
	joined := strings.Join(texts(card), "\n")
	if !strings.Contains(joined, "3") {
		t.Fatalf("countdown does not show the remaining seconds: %q", joined)
	}

	// A paused lifetime keeps its remaining value and reads as paused, so a
	// hovered card does not tick down on the shell's own clock.
	lt.Running = false
	paused := strings.Join(texts(NotificationCard(n, lt, true)), "\n")
	if !strings.Contains(paused, "3") {
		t.Fatalf("paused countdown lost the remaining value: %q", paused)
	}

	// A persistent notification (zero duration) shows no countdown at all.
	n.ExpireTimeoutMS = 0
	persist := strings.Join(texts(NotificationCard(n, &protocol.Lifetime{ID: 7}, true)), "\n")
	if strings.Contains(persist, " 0s") {
		t.Fatalf("persistent card shows a countdown: %q", persist)
	}
}

func TestNotifyCardValueBarIsIndependentOfCardState(t *testing.T) {
	n := baseNotification()
	v := int32(40)
	n.Value = &v
	card := NotificationCard(n, nil, true)
	var meters []*ui.Node
	collectByKind(card, ui.KindMeter, &meters)
	if len(meters) != 1 {
		t.Fatalf("meters = %d, want exactly one value bar", len(meters))
	}
	if meters[0].Value != 0.40 {
		t.Fatalf("value bar = %v, want 0.40", meters[0].Value)
	}

	v = 140
	card = NotificationCard(n, nil, true)
	meters = nil
	collectByKind(card, ui.KindMeter, &meters)
	if meters[0].Value != 1 {
		t.Fatalf("out-of-range value bar = %v, want clamped 1", meters[0].Value)
	}
}

func TestNotifyHistoryCardOmitsActionsAndReply(t *testing.T) {
	entry := protocol.HistoryEntry{
		ID: 3, AppName: "Mail", Summary: "Old", Body: "seen",
		Timestamp: time.Unix(1_756_000_000, 0), Urgency: protocol.UrgencyLow,
	}
	card := HistoryCard(entry, true)
	if got := buttons(card); len(got) != 0 {
		t.Fatalf("history card has action buttons: %v", got)
	}
	var fields []*ui.Node
	collectByKind(card, ui.KindTextField, &fields)
	if len(fields) != 0 {
		t.Fatal("history card carries an inline reply field")
	}
	if !strings.Contains(strings.Join(texts(card), "\n"), "Old") {
		t.Fatal("history card lost its summary")
	}
}

func TestNotifyCardGatesLinksOnTheOpenerCapability(t *testing.T) {
	n := baseNotification()
	n.Body = `see <a href="https://example.test">the page</a>`
	allowed := NotificationCard(n, nil, true)
	found := false
	for _, s := range texts(allowed) {
		if strings.Contains(s, "the page") {
			found = true
		}
	}
	if !found {
		t.Fatal("allowed link text vanished")
	}
	var linkAction bool
	var walk func(n *ui.Node)
	walk = func(n *ui.Node) {
		if strings.Contains(n.Action, "https://example.test") {
			linkAction = true
		}
		for _, c := range n.Children {
			walk(c)
		}
	}
	walk(allowed)
	if !linkAction {
		t.Fatal("no node carries the link href when links are allowed")
	}

	disallowed := NotificationCard(n, nil, false)
	linkAction = false
	walk(disallowed)
	if linkAction {
		t.Fatal("link action exists without the opener capability")
	}
	if !strings.Contains(strings.Join(texts(disallowed), "\n"), "the page") {
		t.Fatal("disallowed link lost its anchor text")
	}
}
