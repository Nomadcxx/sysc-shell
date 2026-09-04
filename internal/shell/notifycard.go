package shell

import (
	"fmt"
	"strings"
	"time"

	"github.com/Nomadcxx/sysc-notify/protocol"
	"github.com/Nomadcxx/sysc-shell/internal/theme"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

const (
	cardIconSize = 56
	cardGap      = 6
	cardPadding  = 12
	cardChipW    = 8
	toastMeterH  = 3
)

func protocolImage(img *protocol.Image) *ui.Image {
	if img == nil || img.Width == 0 || img.Height == 0 {
		return nil
	}
	stride := int(img.Width) * 4
	if len(img.Data) != stride*int(img.Height) {
		return nil
	}
	return &ui.Image{Width: int(img.Width), Height: int(img.Height), Stride: stride, Pix: img.Data}
}

func appLetter(app string) string {
	app = strings.TrimSpace(app)
	for _, r := range app {
		return strings.ToUpper(string(r))
	}
	return "?"
}

func iconSlot(app string, raster *ui.Image) *ui.Node {
	if raster != nil {
		return &ui.Node{Kind: ui.KindImage, Image: raster, ImageSize: cardIconSize}
	}
	return &ui.Node{
		Kind: ui.KindCapsule, Fill: ui.FillContainer, Width: cardIconSize, Shape: ui.ShapeMedium,
		Children: []*ui.Node{{Kind: ui.KindText, Text: appLetter(app)}},
	}
}

func timeoutMeter(lt *protocol.Lifetime) *ui.Node {
	if lt == nil || lt.DurationMS == 0 {
		return nil
	}
	v := float64(lt.RemainingMS) / float64(lt.DurationMS)
	if v < 0 {
		v = 0
	}
	if v > 1 {
		v = 1
	}
	return &ui.Node{Kind: ui.KindMeter, Height: toastMeterH, Value: v}
}

func valueMeter(value *int32) *ui.Node {
	if value == nil {
		return nil
	}
	v := float64(*value) / 100
	if v < 0 {
		v = 0
	}
	if v > 1 {
		v = 1
	}
	return &ui.Node{Kind: ui.KindMeter, Value: v}
}

func wrapNotifyCard(inner *ui.Node, critical bool) *ui.Node {
	body := inner
	if critical {
		body = &ui.Node{Kind: ui.KindRow, Gap: 0, Children: []*ui.Node{
			{Kind: ui.KindCapsule, Fill: ui.FillAccent, Width: cardChipW},
			inner,
		}}
	}
	cap := &ui.Node{
		Kind: ui.KindCapsule, Fill: ui.FillNone, Padding: cardPadding, Shape: ui.ShapeCard,
		Action: inner.Action, Children: []*ui.Node{body},
	}
	if critical {
		cap.Stroke = 2
		cap.StrokeFill = ui.FillAccent
	}
	return cap
}

func notificationTree(id uint32, app, summary, body string, urgency protocol.Urgency, raster *ui.Image, value *int32, allowLinks bool, now, ts time.Time) *ui.Node {
	text := &ui.Node{Kind: ui.KindColumn, Gap: 2, Children: []*ui.Node{}}
	identity := &ui.Node{Kind: ui.KindRow, Gap: cardGap, Children: []*ui.Node{}}
	if app != "" {
		identity.Children = append(identity.Children, &ui.Node{Kind: ui.KindText, Text: app})
	}
	if !ts.IsZero() && !now.IsZero() {
		identity.Children = append(identity.Children, &ui.Node{Kind: ui.KindText, Text: formatNotifyTime(ts, now)})
	}
	if len(identity.Children) > 0 {
		text.Children = append(text.Children, identity)
	}
	if summary != "" {
		text.Children = append(text.Children, &ui.Node{
			Kind: ui.KindText, Text: summary, TextRole: theme.RoleTitle, Tone: toneFor(urgency),
		})
	}
	for _, run := range ParseBody(body, allowLinks) {
		if run.Break || run.Text == "" {
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
	if m := valueMeter(value); m != nil {
		text.Children = append(text.Children, m)
	}
	return &ui.Node{Kind: ui.KindRow, Gap: cardGap, Children: []*ui.Node{
		iconSlot(app, raster),
		text,
	}}
}

func toneFor(urgency protocol.Urgency) ui.Tone {
	if urgency == protocol.UrgencyCritical {
		return ui.ToneError
	}
	return ui.ToneNormal
}

// NotificationCard builds the retained tree for one active toast or ungrouped
// record. lt is the service's authoritative lifetime; raster is the already
// decoded icon, or nil for the letter fallback.
func NotificationCard(n protocol.Notification, lt *protocol.Lifetime, raster *ui.Image, allowLinks bool) *ui.Node {
	if raster == nil {
		raster = protocolImage(n.Image)
	}
	now := time.Now()
	root := &ui.Node{Kind: ui.KindColumn, Gap: cardGap, Children: []*ui.Node{
		notificationTree(n.ID, n.AppName, n.Summary, n.Body, n.Urgency, raster, n.Value, allowLinks, now, n.Timestamp),
	}}

	hasDefault := false
	for _, a := range n.Actions {
		if a.Key == "default" {
			hasDefault = true
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
	if m := timeoutMeter(lt); m != nil {
		root.Children = append(root.Children, m)
	}
	return cardColumn(wrapNotifyCard(root, n.Urgency == protocol.UrgencyCritical))
}

func markDefault(root *ui.Node, id uint32) {
	if len(root.Children) > 0 {
		root.Children[0].Action = fmt.Sprintf("notify:%d:default", id)
	}
}

// HistoryCard builds one closed history row. No actions, no close until
// history.remove exists on the pin.
func HistoryCard(e protocol.HistoryEntry, now time.Time, raster *ui.Image, allowLinks bool) *ui.Node {
	if raster == nil {
		raster = protocolImage(e.Image)
	}
	inner := notificationTree(e.ID, e.AppName, e.Summary, e.Body, e.Urgency, raster, nil, allowLinks, now, e.Timestamp)
	return cardColumn(wrapNotifyCard(inner, e.Urgency == protocol.UrgencyCritical))
}

// ActiveGroupCard is one Current-tab group. Actions come from the newest
// member. Expand lists up to 10 members.
func ActiveGroupCard(g activeGroup, now time.Time, expanded bool, raster *ui.Image, allowLinks bool) *ui.Node {
	if len(g.members) == 0 {
		return &ui.Node{Kind: ui.KindColumn}
	}
	latest := g.members[0]
	if raster == nil {
		raster = protocolImage(latest.Image)
	}
	critical := groupCritical(g.members)
	head := notificationTree(latest.ID, latest.AppName, latest.Summary, latest.Body, latest.Urgency, raster, latest.Value, allowLinks, now, latest.Timestamp)
	root := &ui.Node{Kind: ui.KindColumn, Gap: cardGap, Children: []*ui.Node{head}}
	if n := len(g.members); n > 1 {
		root.Children = append(root.Children, &ui.Node{
			Kind: ui.KindCapsule, Fill: ui.FillAccent, Padding: 4, Shape: ui.ShapeMedium,
			Children: []*ui.Node{{Kind: ui.KindText, Text: fmt.Sprintf("%d", n)}},
		})
	}
	actions := &ui.Node{Kind: ui.KindRow, Gap: cardGap, Children: []*ui.Node{
		{Kind: ui.KindButton, Text: "Dismiss", Padding: 4, Name: "Dismiss", Role: "button", Focusable: true,
			Action: "notify:center:dismiss-group:" + g.key},
	}}
	if len(g.members) > 1 {
		label := "Expand"
		if expanded {
			label = "Collapse"
		}
		actions.Children = append(actions.Children, &ui.Node{
			Kind: ui.KindButton, Text: label, Padding: 4, Name: label, Role: "button", Focusable: true,
			Action: "notify:center:expand:" + g.key,
		})
	}
	for _, a := range latest.Actions {
		if a.Key == "default" {
			markDefault(root, latest.ID)
			continue
		}
		actions.Children = append(actions.Children, &ui.Node{
			Kind: ui.KindButton, Text: a.Label, Padding: 4,
			Action: fmt.Sprintf("notify:%d:action:%s", latest.ID, a.Key),
			Name:   a.Label, Role: "button", Focusable: true,
		})
	}
	root.Children = append(root.Children, actions)
	if expanded {
		limit := len(g.members)
		if limit > 10 {
			limit = 10
		}
		for _, m := range g.members[:limit] {
			root.Children = append(root.Children, &ui.Node{Kind: ui.KindText, Text: m.Summary})
		}
	}
	return cardColumn(wrapNotifyCard(root, critical))
}

func cardColumn(n *ui.Node) *ui.Node {
	if n == nil || n.Kind == ui.KindColumn {
		return n
	}
	return &ui.Node{Kind: ui.KindColumn, Action: n.Action, Children: []*ui.Node{n}}
}

// historyRemoveSupported is false on sysc-notify v0.1.0-rc.2: that pin has
// no history.remove. A later tag can flip this and paint the close control.
func historyRemoveSupported() bool { return false }
