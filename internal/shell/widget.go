package shell

import (
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
	// Metrics is the newest sampling pass. Its nil fields mean unleased or
	// failed; either renders the placeholder.
	Metrics services.Snapshot
	// History carries each leased selector's samples for a graph to plot.
	History map[services.Selector][]float64
	// Weather is the newest reading. Its zero value renders the placeholder.
	Weather services.Reading
}

// textWidget is one configured widget instance: a retained node plus the pure
// function that produces its text.
//
// Every Tranche 3A widget is a function of the view alone, with no mutable
// state and no lifecycle, so there is nothing for an interface to abstract.
// Change detection lives in Bar.apply rather than in each widget, because the
// node already holds the last rendered text.
type textWidget struct {
	node   *ui.Node
	format func(barView) string
}

// buildWidgets turns validated items into widget instances. Ids and options
// are validated at load, so an unknown id cannot reach here.
func buildWidgets(items []config.Item) []textWidget {
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
			out = append(out, textWidget{
				node:   &ui.Node{Kind: ui.KindText},
				format: func(v barView) string { return v.Workspace },
			})
		case "window-title":
			out = append(out, textWidget{
				node:   &ui.Node{Kind: ui.KindText, MaxWidth: item.MaxWidth},
				format: func(v barView) string { return v.Title },
			})
		case "cpu", "memory", "filesystem", "block", "network":
			out = append(out, buildMetricWidget(item))
		case "weather":
			node := &ui.Node{Kind: ui.KindText, MaxWidth: item.MaxWidth}
			out = append(out, textWidget{
				node: node,
				format: func(v barView) string {
					text, tone := formatWeather(item, v.Weather)
					node.Tone = tone
					return text
				},
			})
		}
	}
	return out
}

// clockBoundaries reports the distinct tick boundaries a section set needs.
// The Registry acquires one lease per entry.
func clockBoundaries(sections ...[]config.Item) []time.Duration {
	var out []time.Duration
	for _, section := range sections {
		for _, item := range section {
			if item.ID == "clock" && item.Boundary > 0 {
				out = append(out, item.Boundary)
			}
		}
	}
	return out
}
