package shell

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
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

func TestNotifyCardShowsSummaryBodyAndNamesTheApp(t *testing.T) {
	card := NotificationCard(baseNotification(), nil, true, nil)
	joined := strings.Join(texts(card), "\n")
	for _, want := range []string{"Two new messages", "one"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("card text %q lacks %q", joined, want)
		}
	}
	if strings.Contains(joined, "two") {
		t.Fatalf("collapsed card showed the second body line: %q", joined)
	}
	if lead := card.Children[0].Children[0]; lead.Name != "Mail" {
		t.Fatalf("lead name = %q, want the app name", lead.Name)
	}
}

func TestNotifyCardPreservesBodyStyles(t *testing.T) {
	n := baseNotification()
	n.Body = "one <b>two</b>"
	card := NotificationCard(n, nil, true, nil)
	styled := textNode(card, "two")
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
	card := NotificationCard(n, nil, true, nil)
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

func TestNotifyCardStampsDismissWhenNoDefault(t *testing.T) {
	card := NotificationCard(baseNotification(), nil, true, nil)
	id, rest, ok := parseCardAction(card.Action)
	if !ok || id != 7 || len(rest) == 0 || rest[0] != "dismiss" {
		t.Fatalf("root action = %q, want notify:7:dismiss so a body click can close it", card.Action)
	}
}

func TestNotifyCardMarksTheDefaultActionOnTheBody(t *testing.T) {
	n := baseNotification()
	n.Actions = []protocol.Action{{Key: "default", Label: "Open"}}
	card := NotificationCard(n, nil, true, nil)
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
	without := NotificationCard(n, nil, true, nil)
	var fields []*ui.Node
	collectByKind(without, ui.KindTextField, &fields)
	if len(fields) != 0 {
		t.Fatalf("reply field on a card that did not advertise it: %+v", fields[0])
	}

	n.InlineReply = true
	with := NotificationCard(n, nil, true, nil)
	collectByKind(with, ui.KindTextField, &fields)
	if len(fields) != 1 || !fields[0].Focusable {
		t.Fatalf("inline reply = %+v, want one focusable field", fields)
	}
}

func TestNotifyCardRendersCriticalUrgencyAsErrorTone(t *testing.T) {
	n := baseNotification()
	n.Urgency = protocol.UrgencyCritical
	card := NotificationCard(n, nil, true, nil)
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

func TestNotifyCardAppliesUrgencyToneToTheSummary(t *testing.T) {
	for _, tc := range []struct {
		name    string
		urgency protocol.Urgency
		want    ui.Tone
	}{
		{name: "low", urgency: protocol.UrgencyLow, want: ui.ToneSubtle},
		{name: "normal", urgency: protocol.UrgencyNormal, want: ui.ToneNormal},
		{name: "critical", urgency: protocol.UrgencyCritical, want: ui.ToneError},
	} {
		t.Run(tc.name, func(t *testing.T) {
			n := baseNotification()
			n.Urgency = tc.urgency
			card := NotificationCard(n, nil, true, nil)
			if node := textNode(card, "Two new messages"); node == nil || node.Tone != tc.want {
				t.Fatalf("summary = %+v, want tone %v", node, tc.want)
			}
			if node := textNode(card, "one"); node == nil || node.Tone != ui.ToneSubtle {
				t.Fatalf("body = %+v, want subtle", node)
			}
		})
	}
}

func TestHistoryAndGroupCardsReuseUrgencyTone(t *testing.T) {
	now := time.Unix(1_756_000_000, 0)
	entry := protocol.HistoryEntry{ID: 3, AppName: "Mail", Summary: "Old", Body: "seen", Timestamp: now, Urgency: protocol.UrgencyLow}
	if node := textNode(HistoryCard(entry, now, nil, false), "Old"); node == nil || node.Tone != ui.ToneSubtle {
		t.Fatalf("history summary = %+v, want subtle", node)
	}
	g := activeGroup{key: "mail", members: []protocol.Notification{{ID: 1, AppName: "Mail", Summary: "New", Body: "body", Timestamp: now, Urgency: protocol.UrgencyLow}}}
	if node := textNode(ActiveGroupCard(g, now, false, nil, false), "New"); node == nil || node.Tone != ui.ToneSubtle {
		t.Fatalf("group summary = %+v, want subtle", node)
	}
}

func TestNotifyToastHasNoTimeoutMeter(t *testing.T) {
	var meters []*ui.Node
	collectByKind(NotificationCard(baseNotification(), nil, true, nil), ui.KindMeter, &meters)
	if len(meters) != 0 {
		t.Fatalf("toast carries meters %+v, want none without a value", meters)
	}
}

func TestNotifyCardValueBarIsIndependentOfCardState(t *testing.T) {
	n := baseNotification()
	v := int32(40)
	n.Value = &v
	card := NotificationCard(n, nil, true, nil)
	var meters []*ui.Node
	collectByKind(card, ui.KindMeter, &meters)
	if len(meters) != 1 {
		t.Fatalf("meters = %d, want exactly one value bar", len(meters))
	}
	if meters[0].Value != 0.40 {
		t.Fatalf("value bar = %v, want 0.40", meters[0].Value)
	}

	v = 140
	card = NotificationCard(n, nil, true, nil)
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
	card := HistoryCard(entry, time.Unix(1_756_000_000, 0), nil, true)
	for _, b := range buttons(card) {
		if b.Action != "notify:3:remove" {
			t.Fatalf("history card has a non-remove action: %+v", b)
		}
	}
	if findAction(card, "notify:3:remove") == nil {
		t.Fatal("history card lacks the remove control")
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
	allowed := NotificationCard(n, nil, true, nil)
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

	disallowed := NotificationCard(n, nil, false, nil)
	linkAction = false
	walk(disallowed)
	if linkAction {
		t.Fatal("link action exists without the opener capability")
	}
	if !strings.Contains(strings.Join(texts(disallowed), "\n"), "the page") {
		t.Fatal("disallowed link lost its anchor text")
	}
}

func TestNotifyCardOmitsCriticalBang(t *testing.T) {
	n := baseNotification()
	n.Urgency = protocol.UrgencyCritical
	card := NotificationCard(n, nil, true, nil)
	for _, s := range texts(card) {
		if s == "!" {
			t.Fatal("critical toast still paints !")
		}
	}
	if s := strokeOf(card); s != 0 {
		t.Fatalf("toast tree stroke = %d, want 0: the host paints the rim", s)
	}
	now := time.Unix(1_756_000_000, 0)
	history := HistoryCard(protocol.HistoryEntry{ID: 3, Summary: "Low", Timestamp: now, Urgency: protocol.UrgencyCritical}, now, nil, false)
	if s := strokeOf(history); s != 1 {
		t.Fatalf("critical history stroke = %d, want 1", s)
	}
}

func TestNotifyCardKeepsTheTimeBesideALongSummary(t *testing.T) {
	n := baseNotification()
	n.Summary = strings.Repeat("A very long summary that cannot fit ", 4)
	now := n.Timestamp
	card := notificationCard(n, nil, true, nil, nil, now)
	measure := func(s string, _ ui.TextAttrs) (int, int) { return len([]rune(s)) * 8, 16 }
	if err := ui.LayoutColumn(card, ui.Rect{W: toastCardWidth, H: 400}, measure); err != nil {
		t.Fatal(err)
	}
	stamp := textNode(card, formatNotifyTime(now, now))
	if stamp == nil {
		t.Fatal("no time node")
	}
	if want, _ := measure(stamp.Text, ui.TextAttrs{}); stamp.Bounds.W < want {
		t.Fatalf("time clipped to %d px, want %d", stamp.Bounds.W, want)
	}
}

func TestNotifyCardFallsBackToTheAppNameWithoutASummary(t *testing.T) {
	n := baseNotification()
	n.Summary = ""
	if textNode(NotificationCard(n, nil, true, nil), "Mail") == nil {
		t.Fatal("card without a summary has no headline")
	}
}

func TestNotifyCardPacksActionsIntoRowsThatFit(t *testing.T) {
	n := baseNotification()
	n.Actions = []protocol.Action{
		{Key: "a1", Label: "Open in browser"}, {Key: "a2", Label: "Mark as read"},
		{Key: "a3", Label: "Archive"}, {Key: "a4", Label: "Reply"},
		{Key: "a5", Label: "Snooze for an hour"}, {Key: "a6", Label: "Mute conversation forever and ever"},
	}
	measure := func(s string, _ ui.TextAttrs) (int, int) { return len([]rune(s)) * 9, 18 }
	card := NotificationCard(n, nil, true, measure)
	h, err := ui.ContentHeight(card, toastCardWidth, measure)
	if err != nil {
		t.Fatal(err)
	}
	if problems := ui.CheckFit(card, ui.Rect{W: toastCardWidth, H: h}, measure); len(problems) != 0 {
		t.Fatalf("actions do not fit: %+v", problems)
	}
	if got := len(buttons(card)); got != 6 {
		t.Fatalf("buttons = %d, want 6", got)
	}
	var rows []*ui.Node
	collectByKind(card, ui.KindRow, &rows)
	packed := false
	for _, r := range rows {
		if len(r.Children) > 1 && r.Children[0].Kind == ui.KindButton {
			packed = true
		}
	}
	if !packed {
		t.Fatal("no two short actions shared a row")
	}
}

func strokeOf(n *ui.Node) int {
	if n == nil {
		return 0
	}
	if n.Stroke != 0 {
		return n.Stroke
	}
	for _, c := range n.Children {
		if s := strokeOf(c); s != 0 {
			return s
		}
	}
	return 0
}

func textNode(root *ui.Node, want string) *ui.Node {
	if root == nil {
		return nil
	}
	if root.Kind == ui.KindText && root.Text == want {
		return root
	}
	for _, child := range root.Children {
		if found := textNode(child, want); found != nil {
			return found
		}
	}
	return nil
}

func TestActiveGroupCardShowsCountDismissAndExpand(t *testing.T) {
	now := time.Unix(1_756_000_000, 0)
	g := activeGroup{key: "mail", members: []protocol.Notification{
		{ID: 2, AppName: "Mail", Summary: "new", Body: "b", Timestamp: now.Add(time.Hour), Urgency: protocol.UrgencyNormal},
		{ID: 1, AppName: "Mail", Summary: "old", Timestamp: now, Urgency: protocol.UrgencyNormal},
	}}
	card := ActiveGroupCard(g, now.Add(time.Hour), false, nil, false)
	joined := strings.Join(texts(card), "\n")
	for _, want := range []string{"new", "b", "2"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("group text %q lacks %q", joined, want)
		}
	}
	if buttonByName(card, "Dismiss") == nil {
		t.Fatal("missing Dismiss")
	}
	if buttonByName(card, "Expand") == nil {
		t.Fatal("missing Expand")
	}
	if containsText(card, "old") {
		t.Fatal("collapsed group listed a member summary")
	}
	open := ActiveGroupCard(g, now.Add(time.Hour), true, nil, false)
	if !containsText(open, "old") {
		t.Fatal("expanded group hid members")
	}
}

func TestHistoryRemoveSupportedOnCurrentPin(t *testing.T) {
	if !historyRemoveSupported() {
		t.Fatal("pin carries history.remove but the shell reports it unsupported")
	}
}

func TestHistoryCardCarriesARemoveControl(t *testing.T) {
	now := time.Now()
	card := HistoryCard(protocol.HistoryEntry{ID: 9, AppName: "mail", Summary: "s", Timestamp: now}, now, nil, false)

	n := findAction(card, "notify:9:remove")
	if n == nil {
		t.Fatal("history card has no remove control")
	}
	if n.Shape != ui.ShapeCircle {
		t.Fatalf("remove shape = %v, want circle", n.Shape)
	}
	if n.Name == "" || n.Role != "button" {
		t.Fatalf("remove is not addressable: name=%q role=%q", n.Name, n.Role)
	}
}

func TestActiveCardRemoveDismisses(t *testing.T) {
	now := time.Now()
	g := activeGroup{key: "mail", members: []protocol.Notification{
		{ID: 4, AppName: "mail", Summary: "s", Timestamp: now},
	}}
	card := ActiveGroupCard(g, now, false, nil, false)

	if findAction(card, "notify:4:dismiss") == nil {
		t.Fatal("live card has no remove control")
	}
}

// The wire format is PNG, not a raw raster: a hinted pixmap must decode to a
// BGRA ui.Image, and a PNG claiming more than the wire limits must be dropped
// rather than allocated (GitHub #25).
func TestProtocolImageDecodesPNGData(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	src := image.NewRGBA(image.Rect(0, 0, 1, 1))
	src.SetRGBA(0, 0, color.RGBA{R: 0x11, G: 0x22, B: 0x33, A: 0xFF})
	if err := png.Encode(&buf, src); err != nil {
		t.Fatal(err)
	}
	img := protocolImage(&protocol.Image{MediaType: "image/png", Data: buf.Bytes()})
	if img == nil {
		t.Fatal("protocolImage rejected a valid PNG")
	}
	want := []byte{0x33, 0x22, 0x11, 0xFF}
	if !bytes.Equal(img.Pix, want) {
		t.Fatalf("decoded pixels = %v, want BGRA %v", img.Pix, want)
	}

	if got := protocolImage(&protocol.Image{MediaType: "image/png", Data: []byte("not a png")}); got != nil {
		t.Fatalf("non-PNG data decoded to %+v", got)
	}

	var bomb bytes.Buffer
	if err := png.Encode(&bomb, image.NewRGBA(image.Rect(0, 0, protocol.MaxWireImageLongEdge+1, protocol.MaxWireImageLongEdge+1))); err != nil {
		t.Fatal(err)
	}
	if got := protocolImage(&protocol.Image{MediaType: "image/png", Data: bomb.Bytes()}); got != nil {
		t.Fatal("oversized PNG was not dropped")
	}
}

func TestNotifyCardKeepsALonePillAtItsOwnWidth(t *testing.T) {
	n := baseNotification()
	n.Actions = []protocol.Action{{Key: "reply", Label: "Reply"}}
	measure := func(s string, _ ui.TextAttrs) (int, int) { return len([]rune(s)) * 9, 18 }
	card := NotificationCard(n, nil, true, measure)
	h, err := ui.ContentHeight(card, toastCardWidth, measure)
	if err != nil {
		t.Fatal(err)
	}
	if err := ui.LayoutColumn(card, ui.Rect{W: toastCardWidth, H: h}, measure); err != nil {
		t.Fatal(err)
	}
	pill := buttonByName(card, "Reply")
	if pill == nil {
		t.Fatal("no Reply pill")
	}
	if full := toastCardWidth - 2*cardPadding; pill.Bounds.W >= full {
		t.Fatalf("lone pill is %d px wide, the full row; want its own width", pill.Bounds.W)
	}
}
