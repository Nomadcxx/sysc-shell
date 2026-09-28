package shell

import (
	"bytes"
	"fmt"
	"image/png"
	"time"

	"github.com/Nomadcxx/sysc-notify/protocol"
	"github.com/Nomadcxx/sysc-shell/internal/theme"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

const (
	// cardIconSize is the lead a card opens with: the notification's image
	// or a glyph tile.
	cardIconSize = 32
	// notifyIconRaster is the size icons resolve at, twice the lead, so the
	// lead stays sharp on a scaled output.
	notifyIconRaster = 64
	cardGap          = 6
	cardPadding      = 12
	cardLeadGap      = 10
	cardSectionGap   = 8
	cardGlyphSize    = 18
	centreIconSize   = 20
	centreIconPad    = 6
)

// protocolImage decodes the notification's wire image. The sysc-notify
// protocol carries PNG bytes (protocol.Image.Validate rejects any other
// media type), so the data is decoded rather than treated as a raw raster.
func protocolImage(img *protocol.Image) *ui.Image {
	if img == nil || len(img.Data) == 0 {
		return nil
	}
	source, err := png.Decode(bytes.NewReader(img.Data))
	if err != nil {
		return nil
	}
	bounds := source.Bounds()
	width, height := bounds.Dx(), bounds.Dy()
	if width <= 0 || height <= 0 ||
		width > protocol.MaxWireImageLongEdge || height > protocol.MaxWireImageLongEdge {
		return nil
	}
	out := &ui.Image{Width: width, Height: height, Stride: width * 4, Pix: make([]byte, width*height*4)}
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			r, g, b, a := source.At(bounds.Min.X+x, bounds.Min.Y+y).RGBA()
			offset := y*out.Stride + x*4
			out.Pix[offset+0] = uint8(b >> 8)
			out.Pix[offset+1] = uint8(g >> 8)
			out.Pix[offset+2] = uint8(r >> 8)
			out.Pix[offset+3] = uint8(a >> 8)
		}
	}
	return out
}

// leadSlot is the card's lead: the notification's own image, or a bell in a
// tinted tile. Layout C has no app line, so the node's Name keeps the app
// name for an accessibility bridge; nothing reads it yet.
func leadSlot(app string, raster *ui.Image, urgency protocol.Urgency) *ui.Node {
	if raster != nil {
		return &ui.Node{Kind: ui.KindImage, Image: raster, ImageSize: cardIconSize, Name: app}
	}
	fill, tone := ui.FillContainerHighest, ui.ToneNormal
	if urgency == protocol.UrgencyCritical {
		fill, tone = ui.FillErrorContainer, ui.ToneError
	}
	return &ui.Node{
		Kind: ui.KindCapsule, Fill: fill, Width: cardIconSize, Height: cardIconSize, Shape: ui.ShapeMedium,
		CenterX: true, CenterY: true, Name: app,
		Children: []*ui.Node{{Kind: ui.KindIcon, Icon: "notifications", IconSize: cardGlyphSize, Tone: tone}},
	}
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

// wrapNotifyCard is the centre's card chrome. A critical entry strokes the
// error colour, the same edge a critical toast paints.
func wrapNotifyCard(inner *ui.Node, critical bool, fill ui.Fill) *ui.Node {
	cap := &ui.Node{
		Kind: ui.KindCapsule, Fill: fill, Padding: cardPadding, Shape: ui.ShapeCard,
		Action: inner.Action, Children: []*ui.Node{inner},
	}
	if critical {
		cap.Stroke, cap.StrokeFill = 1, ui.FillError
	}
	return cap
}

// centreRemoveButton is the per-entry remove control. HistoryCard omits it
// when the pinned service has no command behind it: a painted control the
// service rejects is worse than an absent one.
func centreRemoveButton(action, name string) *ui.Node {
	return &ui.Node{
		Kind: ui.KindButton, Action: action, Name: name, Role: "button",
		Focusable: true, Shape: ui.ShapeCircle, Fill: ui.FillContainerHighest,
		Padding:  centreIconPad,
		Children: []*ui.Node{{Kind: ui.KindIcon, Icon: "delete", IconSize: centreIconSize}},
	}
}

// bodyNodes is the body under the headline. Collapsed, it is the runs up to
// the first break on one row, so a styled word stays in its sentence and the
// row clips at the card edge. Expanded, each run wraps over its own lines.
func bodyNodes(id uint32, body string, allowLinks bool, wrap func(string) []string) []*ui.Node {
	run := func(r Run, text string) *ui.Node {
		node := &ui.Node{Kind: ui.KindText, Text: text, Bold: r.Bold, Italic: r.Italic, Underline: r.Underline, Tone: ui.ToneSubtle}
		if r.Link {
			node.Action = fmt.Sprintf("notify:%d:link:%s", id, r.Href)
		}
		return node
	}
	runs := ParseBody(body, allowLinks)
	if wrap == nil {
		line := &ui.Node{Kind: ui.KindRow}
		for _, r := range runs {
			if r.Break {
				if len(line.Children) > 0 {
					break
				}
				continue
			}
			if r.Text != "" {
				line.Children = append(line.Children, run(r, r.Text))
			}
		}
		switch len(line.Children) {
		case 0:
			return nil
		case 1:
			return line.Children
		}
		return []*ui.Node{line}
	}
	var out []*ui.Node
	for _, r := range runs {
		if r.Break || r.Text == "" {
			continue
		}
		for _, text := range wrap(r.Text) {
			if text != "" {
				out = append(out, run(r, text))
			}
		}
	}
	return out
}

// notificationTree is the text block every notification card shares: the
// lead beside a headline row, with the time pinned right so a long summary
// clips before the time does, then the body and any value bar.
func notificationTree(id uint32, app, summary, body string, urgency protocol.Urgency, raster *ui.Image, value *int32, allowLinks bool, wrap func(string) []string, now, ts time.Time) *ui.Node {
	headline := summary
	if headline == "" {
		headline = app
	}
	if headline == "" {
		// A raw D-Bus sender can omit both; a card is never just a time.
		headline = "Notification"
	}
	head := &ui.Node{Kind: ui.KindRow, Gap: cardGap, Children: []*ui.Node{
		{Kind: ui.KindText, Text: headline, TextRole: theme.RoleFigure, Tone: toneFor(urgency)},
	}}
	if !ts.IsZero() && !now.IsZero() {
		head.PinEnd = true
		head.Children = append(head.Children, &ui.Node{
			Kind: ui.KindText, Text: formatNotifyTime(ts, now), TextRole: theme.RoleCaption, Tone: ui.ToneSubtle,
		})
	}
	text := &ui.Node{Kind: ui.KindColumn, Gap: theme.MarginXXS, Children: []*ui.Node{head}}
	text.Children = append(text.Children, bodyNodes(id, body, allowLinks, wrap)...)
	if m := valueMeter(value); m != nil {
		text.Children = append(text.Children, m)
	}
	return &ui.Node{Kind: ui.KindRow, Gap: cardLeadGap, Children: []*ui.Node{
		leadSlot(app, raster, urgency),
		text,
	}}
}

func toneFor(urgency protocol.Urgency) ui.Tone {
	switch urgency {
	case protocol.UrgencyLow:
		return ui.ToneSubtle
	case protocol.UrgencyCritical:
		return ui.ToneError
	default:
		return ui.ToneNormal
	}
}

// NotificationCard builds one active toast. raster is the already decoded
// icon, or nil for the glyph tile. measure packs action pills into rows; nil
// stacks them one per row.
func NotificationCard(n protocol.Notification, raster *ui.Image, allowLinks bool, measure ui.MeasureText) *ui.Node {
	return notificationCard(n, raster, allowLinks, measure, nil, time.Now(), toastCardWidth)
}

// ExpandedNotificationCard is a toast whose body is wrapped over several
// lines after a vertical drag.
func ExpandedNotificationCard(n protocol.Notification, raster *ui.Image, allowLinks bool, measure ui.MeasureText, wrap func(string) []string) *ui.Node {
	return notificationCard(n, raster, allowLinks, measure, wrap, time.Now(), toastCardWidth)
}

// notificationCard has no chrome of its own: the toast host paints the card
// ground, rim and blur around it. width is the card's laid-out width, which
// the action rows pack against.
func notificationCard(n protocol.Notification, raster *ui.Image, allowLinks bool, measure ui.MeasureText, wrap func(string) []string, now time.Time, width int) *ui.Node {
	if raster == nil {
		raster = protocolImage(n.Image)
	}
	root := &ui.Node{Kind: ui.KindColumn, Gap: cardSectionGap, Padding: cardPadding, Children: []*ui.Node{
		notificationTree(n.ID, n.AppName, n.Summary, n.Body, n.Urgency, raster, n.Value, allowLinks, wrap, now, n.Timestamp),
	}}

	hasDefault := false
	var pills []protocol.Action
	for _, a := range n.Actions {
		if a.Key == "default" {
			hasDefault = true
			markDefault(root, n.ID)
			continue
		}
		pills = append(pills, a)
	}
	rowWidth := width - 2*cardPadding
	for _, row := range actionRows(pills, rowWidth, measure) {
		// Only a pill too wide for any row takes the column, where it spans
		// the card and its label clips; one that fits keeps its own width.
		if len(row) == 1 && pillWidth(row[0], rowWidth, measure) > rowWidth {
			root.Children = append(root.Children, actionPill(n.ID, row[0]))
			continue
		}
		line := &ui.Node{Kind: ui.KindRow, Gap: cardGap}
		for _, a := range row {
			line.Children = append(line.Children, actionPill(n.ID, a))
		}
		root.Children = append(root.Children, line)
	}
	if !hasDefault {
		root.Action = fmt.Sprintf("notify:%d:dismiss", n.ID)
	}
	if n.InlineReply {
		root.Children = append(root.Children, &ui.Node{
			Kind: ui.KindTextField,
			Name: "Reply", Placeholder: "Reply", Role: "text", Focusable: true,
			Action: fmt.Sprintf("notify:%d:reply", n.ID),
		})
	}
	return root
}

func actionPill(id uint32, a protocol.Action) *ui.Node {
	return &ui.Node{
		Kind: ui.KindButton, Text: a.Label, TextRole: theme.RoleLabel, Fill: ui.FillContainerHighest,
		Shape: ui.ShapeMedium, Padding: theme.MarginXS,
		Action: fmt.Sprintf("notify:%d:action:%s", id, a.Key),
		Name:   a.Label, Role: "button", Focusable: true,
	}
}

// pillWidth is the width a pill lays out at, as ui measures a button: its
// label plus padding on each side. Without a measure it cannot be sized, and
// reports wider than the row so the pill takes a row of its own.
func pillWidth(a protocol.Action, width int, measure ui.MeasureText) int {
	if measure == nil {
		return width + 1
	}
	tw, _ := measure(a.Label, ui.TextAttrs{Role: theme.RoleLabel})
	return tw + 2*theme.MarginXS
}

// actionRows packs action pills into rows no wider than width, keeping their
// order. A pill that fits nowhere beside another takes a row alone, which the
// card lays out as a full-width button, so no label can push layout past the
// card. A nil measure cannot size a pill, so every action takes its own row.
func actionRows(actions []protocol.Action, width int, measure ui.MeasureText) [][]protocol.Action {
	var rows [][]protocol.Action
	used := 0
	for _, a := range actions {
		w := pillWidth(a, width, measure)
		if len(rows) == 0 || used+cardGap+w > width {
			rows = append(rows, []protocol.Action{a})
			used = w
			continue
		}
		rows[len(rows)-1] = append(rows[len(rows)-1], a)
		used += cardGap + w
	}
	return rows
}

func markDefault(root *ui.Node, id uint32) {
	if len(root.Children) > 0 {
		root.Children[0].Action = fmt.Sprintf("notify:%d:default", id)
	}
}

// HistoryCard builds one closed history row. A right-pinned remove control
// is present when the pinned service accepts history.remove.
func HistoryCard(e protocol.HistoryEntry, now time.Time, raster *ui.Image, allowLinks bool) *ui.Node {
	if raster == nil {
		raster = protocolImage(e.Image)
	}
	inner := notificationTree(e.ID, e.AppName, e.Summary, e.Body, e.Urgency, raster, nil, allowLinks, nil, now, e.Timestamp)
	if historyRemoveSupported() {
		inner = &ui.Node{Kind: ui.KindRow, Gap: cardGap, PinEnd: true, Children: []*ui.Node{
			inner,
			centreRemoveButton(fmt.Sprintf("notify:%d:remove", e.ID), "Remove"),
		}}
	}
	return cardColumn(wrapNotifyCard(inner, e.Urgency == protocol.UrgencyCritical, ui.FillContainerHigh))
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
	head := notificationTree(latest.ID, latest.AppName, latest.Summary, latest.Body, latest.Urgency, raster, latest.Value, allowLinks, nil, now, latest.Timestamp)
	head = &ui.Node{Kind: ui.KindRow, Gap: cardGap, PinEnd: true, Children: []*ui.Node{
		head,
		centreRemoveButton(fmt.Sprintf("notify:%d:dismiss", latest.ID), "Dismiss"),
	}}
	root := &ui.Node{Kind: ui.KindColumn, Gap: cardGap, Children: []*ui.Node{head}}
	if n := len(g.members); n > 1 {
		root.Children = append(root.Children, &ui.Node{
			Kind: ui.KindCapsule, Key: badgeKeyPrefix + g.key, Fill: ui.FillAccent, Padding: theme.MarginXS, Shape: ui.ShapeMedium,
			Children: []*ui.Node{{Kind: ui.KindText, Text: fmt.Sprintf("%d", n)}},
		})
	}
	actions := &ui.Node{Kind: ui.KindRow, Gap: cardGap, Children: []*ui.Node{
		{Kind: ui.KindButton, Text: "Dismiss", Padding: theme.MarginXS, Name: "Dismiss", Role: "button", Focusable: true,
			Action: "notify:center:dismiss-group:" + g.key},
	}}
	if len(g.members) > 1 {
		label := "Expand"
		if expanded {
			label = "Collapse"
		}
		actions.Children = append(actions.Children, &ui.Node{
			Kind: ui.KindButton, Text: label, Padding: theme.MarginXS, Name: label, Role: "button", Focusable: true,
			Action: "notify:center:expand:" + g.key,
		})
	}
	for _, a := range latest.Actions {
		if a.Key == "default" {
			markDefault(root, latest.ID)
			continue
		}
		actions.Children = append(actions.Children, &ui.Node{
			Kind: ui.KindButton, Text: a.Label, Padding: theme.MarginXS,
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
			root.Children = append(root.Children, &ui.Node{Kind: ui.KindText, Text: m.Summary, Tone: toneFor(m.Urgency)})
		}
	}
	return cardColumn(wrapNotifyCard(root, critical, ui.FillContainerHigh))
}

func cardColumn(n *ui.Node) *ui.Node {
	if n == nil || n.Kind == ui.KindColumn {
		return n
	}
	return &ui.Node{Kind: ui.KindColumn, Action: n.Action, Children: []*ui.Node{n}}
}

// historyRemoveSupported reports whether the pinned sysc-notify accepts
// history.remove. True from v0.1.0-rc.3. The flag stays so a rollback to an
// older pin disables the control rather than painting one the service rejects.
func historyRemoveSupported() bool { return true }
