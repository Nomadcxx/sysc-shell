package shell

import (
	"strconv"
	"time"

	"github.com/Nomadcxx/sysc-shell/internal/config"
	"github.com/Nomadcxx/sysc-shell/internal/services"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

// barView is the immutable input every widget formats from. The Registry
// assembles it from one process-wide clock snapshot and this output's Niri
// projection, so two bars share one clock update while keeping their own
// workspace and title.
type barView struct {
	// Now is zero until the first clock tick.
	Now       time.Time
	Workspace string
	Title     string
	// Pills are this output's workspaces in index order.
	Pills []workspacePill
	// Metrics is the newest sampling pass. Its nil fields mean unleased or
	// failed; either renders the placeholder.
	Metrics services.Snapshot
	// History carries each leased selector's samples for a graph to plot.
	History map[services.Selector][]float64
	// Weather is the newest reading. Its zero value renders the placeholder.
	Weather services.Reading
	// Plugins is this output's prepared plugin frames, keyed by instance.
	Plugins map[string]pluginFrame
	// Unread is unseen history. DND is do-not-disturb at this view's clock.
	Unread int
	DND    bool
}

// textWidget is one configured widget instance: a retained node plus the pure
// function that produces its text.
//
// Every Tranche 3A widget is a function of the view alone, with no mutable
// state and no lifecycle, so there is nothing for an interface to abstract.
// Change detection lives in Bar.apply rather than in each widget, because the
// node already holds the last rendered text.
type textWidget struct {
	// node is the capsule the bar lays out and hits. inner is the content it
	// wraps, and is what format writes to.
	node   *ui.Node
	inner  *ui.Node
	format func(barView) string
	// refresh rebuilds a widget whose content is a subtree rather than a
	// string. It reports whether the tree changed. Widgets with a format do
	// not set it.
	refresh func(barView) bool
	tooltip string
	// tip holds a structured plugin tooltip tree. It is a pointer so refresh
	// can replace the tree without copying the widget.
	tip **ui.Node
	// members are a group's contents. They are not laid out or rendered
	// separately, but each carries its own tooltip, so a hit test descends
	// into them.
	members []textWidget
}

// groupGap separates members inside a group capsule. noCapsule tells
// buildWidgets to leave a member unwrapped.
const (
	groupGap  = 10
	noCapsule = -1
)

// workspacePillGap separates adjacent workspace pills, and matches the
// measured gap in the reference bar.
const workspacePillGap = 8

// refreshWorkspacePills rebuilds the pill row when the workspace set, its
// occupancy or its focus changes, and reports whether it did. The signature is
// compared field by field rather than stuffed into a string, so paint stays a
// function of the tree.
func refreshWorkspacePills(row *ui.Node, v barView) bool {
	// With no projection yet, the widget still shows the stable fallback
	// rather than collapsing to nothing, which is what tells an owner that
	// Niri has not reported this output.
	if len(v.Pills) == 0 {
		label := v.Workspace
		if label == "" {
			label = noWorkspace
		}
		if len(row.Children) == 1 && row.Children[0] != nil &&
			len(row.Children[0].Children) == 1 &&
			row.Children[0].Children[0].Text == label {
			return false
		}
		row.Children = append(row.Children[:0], &ui.Node{
			Kind: ui.KindCapsule, Fill: ui.FillContainer,
			Children: []*ui.Node{{Kind: ui.KindText, Text: label}},
		})
		return true
	}
	if workspacePillsMatch(row, v.Pills) {
		return false
	}
	row.Children = row.Children[:0]
	for _, p := range v.Pills {
		fill := ui.FillContainer
		if p.Focused {
			fill = ui.FillAccent
		}
		row.Children = append(row.Children, &ui.Node{
			Kind: ui.KindCapsule,
			Fill: fill,
			Children: []*ui.Node{{
				Kind:    ui.KindText,
				Text:    strconv.Itoa(p.Index),
				Tabular: true,
			}},
		})
	}
	return true
}

func workspacePillsMatch(row *ui.Node, pills []workspacePill) bool {
	if len(row.Children) != len(pills) {
		return false
	}
	for i, p := range pills {
		c := row.Children[i]
		if c == nil || len(c.Children) != 1 || c.Children[0] == nil {
			return false
		}
		want := ui.FillContainer
		if p.Focused {
			want = ui.FillAccent
		}
		if c.Fill != want || c.Children[0].Text != strconv.Itoa(p.Index) {
			return false
		}
	}
	return true
}

// capsuled wraps one built widget in its pill. Wrapping happens in a single
// place so no builder has to know about bar chrome.
// A negative padding means do not wrap. Group members render flat inside their
// group's capsule rather than each gaining one of their own.
func capsuled(w textWidget, pad int) textWidget {
	if pad < 0 || w.node == nil || w.node.Kind == ui.KindCapsule {
		return w
	}
	w.inner = w.node
	w.node = &ui.Node{Kind: ui.KindCapsule, Padding: pad, Action: w.inner.Action, Children: []*ui.Node{w.inner}}
	return w
}

// buildWidgets turns validated items into widget instances. Ids and options
// are validated at load, so an unknown id cannot reach here.
func buildWidgets(items []config.Item, pad int) []textWidget {
	out := make([]textWidget, 0, len(items))
	for _, item := range items {
		switch item.ID {
		case "clock":
			layout := item.Format
			out = append(out, textWidget{
				node: &ui.Node{Kind: ui.KindText, Tabular: true},
				format: func(v barView) string {
					if v.Now.IsZero() {
						return ""
					}
					return v.Now.Format(layout)
				},
			})
		case "workspace":
			row := &ui.Node{Kind: ui.KindRow, Gap: workspacePillGap}
			w := textWidget{node: row}
			w.refresh = func(v barView) bool { return refreshWorkspacePills(row, v) }
			out = append(out, w)
		case "window-title":
			out = append(out, textWidget{
				node:    &ui.Node{Kind: ui.KindText, MaxWidth: item.MaxWidth},
				tooltip: "Focused window",
				format:  func(v barView) string { return v.Title },
			})
		case "cpu", "memory", "filesystem", "block", "network":
			out = append(out, buildMetricWidget(item))
		case "weather":
			node := &ui.Node{Kind: ui.KindText, MaxWidth: item.MaxWidth}
			out = append(out, textWidget{
				node:    node,
				tooltip: "Weather",
				format: func(v barView) string {
					text, tone := formatWeather(item, v.Weather)
					node.Tone = tone
					return text
				},
			})
		case "group":
			// One capsule holding its members as a flat row. Members are not
			// individually capsuled: two nested surfaces read as one blob at
			// the palette contrast a bar uses.
			row := &ui.Node{Kind: ui.KindRow, Gap: groupGap}
			members := buildWidgets(item.Items, noCapsule)
			g := textWidget{node: row, members: members}
			for _, m := range members {
				row.Children = append(row.Children, m.node)
			}
			if len(members) > 0 && members[0].node != nil {
				row.Action = members[0].node.Action
				for _, m := range members[1:] {
					if m.node == nil || m.node.Action != row.Action {
						row.Action = ""
						break
					}
				}
			}
			g.refresh = func(v barView) bool {
				changed := false
				for _, m := range members {
					if m.refresh != nil {
						changed = m.refresh(v) || changed
						continue
					}
					if text := m.format(v); text != m.node.Text {
						m.node.Text = text
						changed = true
					}
				}
				return changed
			}
			out = append(out, g)
		case "battery":
			node := &ui.Node{Kind: ui.KindText, Action: panelSessionAction}
			out = append(out, textWidget{
				node:    node,
				tooltip: "Battery",
				format: func(v barView) string {
					text, tone := formatBattery(item, v.Metrics)
					node.Tone = tone
					return text
				},
			})
		case "notifications":
			out = append(out, buildNotifyWidget())
		case "plugin":
			out = append(out, buildPluginWidget(item))
		}
	}
	for i := range out {
		out[i] = capsuled(out[i], pad)
	}
	return out
}

// clockBoundaries reports the distinct tick boundaries a section set needs.
// The Registry acquires one lease per entry.
func clockBoundaries(sections ...[]config.Item) []time.Duration {
	var out []time.Duration
	for _, section := range sections {
		for _, item := range section {
			// A clock inside a group still needs its tick lease.
			if item.ID == "group" {
				out = append(out, clockBoundaries(item.Items)...)
				continue
			}
			if item.ID == "clock" && item.Boundary > 0 {
				out = append(out, item.Boundary)
			}
		}
	}
	return out
}
