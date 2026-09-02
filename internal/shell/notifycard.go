package shell

import (
	"fmt"

	"github.com/Nomadcxx/sysc-notify/protocol"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

// Card geometry and identity conventions. Action strings carry the
// notification ID so Task 7 can correlate a click with its record.
const (
	cardIconSize = 32
	cardGap      = 6
	cardPadding  = 12
)

// notificationTree builds the shared card body: icon column, identity and
// text lines, optional countdown and value bar. Both builders share it.
func notificationTree(id uint32, app, summary, body string, urgency protocol.Urgency, image *protocol.Image, value *int32, lt *protocol.Lifetime, allowLinks bool) *ui.Node {
	text := &ui.Node{Kind: ui.KindColumn, Gap: 2, Children: []*ui.Node{}}

	identity := &ui.Node{Kind: ui.KindRow, Gap: cardGap, Children: []*ui.Node{}}
	if app != "" {
		identity.Children = append(identity.Children, &ui.Node{Kind: ui.KindText, Text: app})
	}
	if urgency == protocol.UrgencyCritical {
		mark := &ui.Node{Kind: ui.KindText, Text: "!", Tone: ui.ToneError}
		identity.Children = append(identity.Children, mark)
	}
	if len(identity.Children) > 0 {
		text.Children = append(text.Children, identity)
	}

	if summary != "" {
		text.Children = append(text.Children, &ui.Node{
			Kind: ui.KindText, Text: summary, Bold: true,
			Tone: toneFor(urgency),
		})
	}

	for _, run := range ParseBody(body, allowLinks) {
		if run.Break {
			continue
		}
		if run.Text == "" {
			continue
		}
		node := &ui.Node{
			Kind: ui.KindText, Text: run.Text,
			Bold: run.Bold, Italic: run.Italic, Underline: run.Underline,
		}
		if run.Link {
			node.Action = fmt.Sprintf("notify:%d:link:%s", id, run.Href)
		}
		text.Children = append(text.Children, node)
	}

	if seconds, ok := countdownSeconds(lt); ok {
		text.Children = append(text.Children, &ui.Node{
			Kind: ui.KindText, Text: fmt.Sprintf("%ds", seconds), Tabular: true,
		})
	}

	if value != nil {
		v := float64(*value) / 100
		if v < 0 {
			v = 0
		}
		if v > 1 {
			v = 1
		}
		text.Children = append(text.Children, &ui.Node{Kind: ui.KindMeter, Value: v})
	}

	head := &ui.Node{Kind: ui.KindRow, Gap: cardGap, Children: []*ui.Node{}}
	if image != nil {
		head.Children = append(head.Children, &ui.Node{Kind: ui.KindImage, ImageSize: cardIconSize})
	}
	head.Children = append(head.Children, text)

	return &ui.Node{Kind: ui.KindColumn, Gap: cardGap, Padding: cardPadding, Children: []*ui.Node{head}}
}

// countdownSeconds reads the service-owned remaining lifetime. A persistent
// notification has no countdown. The value is a floor, so "0s" means under a
// second remains rather than expired.
func countdownSeconds(lt *protocol.Lifetime) (int, bool) {
	if lt == nil || lt.DurationMS == 0 {
		return 0, false
	}
	return int(lt.RemainingMS / 1000), true
}

func toneFor(urgency protocol.Urgency) ui.Tone {
	if urgency == protocol.UrgencyCritical {
		return ui.ToneError
	}
	return ui.ToneNormal
}

// NotificationCard builds the retained tree for one active notification.
// lt is the service's authoritative lifetime for the record; allowLinks is
// the shell's qualified opener capability.
func NotificationCard(n protocol.Notification, lt *protocol.Lifetime, allowLinks bool) *ui.Node {
	root := notificationTree(n.ID, n.AppName, n.Summary, n.Body, n.Urgency, n.Image, n.Value, lt, allowLinks)

	hasDefault := false
	for _, a := range n.Actions {
		if a.Key == "default" {
			hasDefault = true
			// The default action rides the body click; it is not a button.
			markDefault(root, n.ID)
			continue
		}
		root.Children = append(root.Children, &ui.Node{
			Kind: ui.KindButton, Text: a.Label, Padding: 4,
			Action: fmt.Sprintf("notify:%d:action:%s", n.ID, a.Key),
			Name:   a.Label, Role: "button", Focusable: true,
		})
	}
	if !hasDefault {
		root.Action = fmt.Sprintf("notify:%d:dismiss", n.ID)
	}

	if n.InlineReply {
		root.Children = append(root.Children, &ui.Node{
			Kind: ui.KindTextField,
			Name: "Reply", Role: "text", Focusable: true,
			Action: fmt.Sprintf("notify:%d:reply", n.ID),
		})
	}
	return root
}

// markDefault labels the body column with the default action so a body click
// resolves to it.
func markDefault(root *ui.Node, id uint32) {
	if len(root.Children) > 0 {
		root.Children[0].Action = fmt.Sprintf("notify:%d:default", id)
	}
}

// HistoryCard builds the retained tree for one closed history entry. History
// carries no actions and no inline reply.
func HistoryCard(e protocol.HistoryEntry, allowLinks bool) *ui.Node {
	return notificationTree(e.ID, e.AppName, e.Summary, e.Body, e.Urgency, e.Image, nil, nil, allowLinks)
}
