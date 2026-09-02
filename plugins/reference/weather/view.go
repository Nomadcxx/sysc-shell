package weather

import (
	"fmt"
	"strings"
	"time"

	"github.com/Nomadcxx/sysc-shell/internal/render"
	v1 "github.com/Nomadcxx/sysc-shell/plugin/v1"
	owm "github.com/Nomadcxx/sysc-shell/weather"
)

// Options is the presentation the settings declared.
type Options struct {
	ShowTemperature bool
	ShowUnit        bool
	ShowIcon        bool
	ShowCondition   bool
	TooltipMode     string
	Accent          string
}

func ParseOptions(values map[string]any) Options {
	opt := Options{ShowTemperature: true, ShowUnit: true, ShowIcon: true, TooltipMode: "current"}
	if values == nil {
		return opt
	}
	if v, ok := values["bar_temperature"].(bool); ok {
		opt.ShowTemperature = v
	}
	if v, ok := values["bar_unit"].(bool); ok {
		opt.ShowUnit = v
	}
	if v, ok := values["bar_icon"].(bool); ok {
		opt.ShowIcon = v
	}
	if v, ok := values["bar_condition"].(bool); ok {
		opt.ShowCondition = v
	}
	if v, ok := values["tooltip_mode"].(string); ok && v != "" {
		opt.TooltipMode = v
	}
	if v, ok := values["accent"].(string); ok {
		opt.Accent = v
	}
	return opt
}

func Condition(code int) string {
	switch render.IconName(code) {
	case "clear-day":
		return "Clear"
	case "partly-cloudy":
		return "Partly cloudy"
	case "cloud":
		return "Cloudy"
	case "fog":
		return "Fog"
	case "rain":
		return "Rain"
	case "snow":
		return "Snow"
	case "heavy-snow":
		return "Heavy snow"
	case "thunderstorm":
		return "Thunderstorm"
	}
	return "Cloudy"
}

func BarTree(snap Snapshot, opt Options) *v1.Node {
	children := statusNodes(snap, opt)
	children = append(children, &v1.Node{
		Kind: v1.KindButton, ID: "open", Text: "Weather", Name: "Open weather", Role: "button",
		Events: []v1.EventKind{v1.EventActivate},
	})
	return &v1.Node{Kind: v1.KindRow, Gap: 8, Children: children}
}

func TooltipTree(snap Snapshot, opt Options) *v1.Node {
	if snap.Disabled {
		return &v1.Node{Kind: v1.KindColumn, Children: []*v1.Node{{Kind: v1.KindText, Text: "Weather off"}}}
	}
	if !snap.Observed {
		text := "Weather"
		if !snap.FailedSince.IsZero() {
			text = "weather unavailable"
		}
		return &v1.Node{Kind: v1.KindColumn, Children: []*v1.Node{{Kind: v1.KindText, Text: text}}}
	}
	if opt.TooltipMode == "forecast" {
		rows := make([]*v1.Node, 0, len(snap.Forecast.Daily))
		for i, d := range snap.Forecast.Daily {
			rows = append(rows, &v1.Node{Kind: v1.KindText, Key: fmt.Sprintf("day:%d", i), Text: dayLine(d, snap.Unit)})
		}
		return &v1.Node{Kind: v1.KindColumn, Gap: 4, Children: rows}
	}
	return &v1.Node{Kind: v1.KindColumn, Gap: 4, Children: currentCard(snap)}
}

func PanelTree(snap Snapshot, opt Options) *v1.Node {
	_ = opt
	children := []*v1.Node{{Kind: v1.KindText, Text: "Weather"}}
	children = append(children, currentCard(snap)...)
	for i, d := range snap.Forecast.Daily {
		children = append(children, &v1.Node{
			Kind: v1.KindRow, Gap: 8, Key: fmt.Sprintf("day:%d", i),
			Children: []*v1.Node{
				iconNode(d.Code),
				{Kind: v1.KindText, Text: d.Date},
				{Kind: v1.KindText, Text: dayLine(d, snap.Unit)},
			},
		})
	}
	return &v1.Node{Kind: v1.KindColumn, Gap: 8, Children: children}
}

func CurrentPatch(snap Snapshot, opt Options) []v1.Replacement {
	var out []v1.Replacement
	if opt.ShowTemperature && snap.Observed {
		out = append(out, v1.Replacement{Key: "temp", Node: tempNode(snap, opt)})
	}
	if snap.Stale() {
		out = append(out, v1.Replacement{Key: "age", Node: ageNode(snap)})
	}
	return out
}

func statusNodes(snap Snapshot, opt Options) []*v1.Node {
	if snap.Disabled {
		return []*v1.Node{{Kind: v1.KindText, Text: "Weather off"}}
	}
	if !snap.Observed {
		if snap.FailedSince.IsZero() {
			return []*v1.Node{{Kind: v1.KindText, Text: "Weather"}}
		}
		return []*v1.Node{{Kind: v1.KindText, Text: "weather unavailable", Tone: v1.ToneError}}
	}
	var nodes []*v1.Node
	if opt.ShowIcon {
		nodes = append(nodes, iconNode(snap.Forecast.Current.Code))
	}
	if opt.ShowTemperature {
		nodes = append(nodes, tempNode(snap, opt))
	}
	if opt.ShowCondition {
		nodes = append(nodes, &v1.Node{Kind: v1.KindText, Text: Condition(snap.Forecast.Current.Code)})
	}
	if snap.Stale() {
		nodes = append(nodes, ageNode(snap))
	}
	return nodes
}

func currentCard(snap Snapshot) []*v1.Node {
	if !snap.Observed {
		text := "Weather"
		tone := v1.ToneNormal
		if snap.Disabled {
			text = "Weather off"
		} else if !snap.FailedSince.IsZero() {
			text = "weather unavailable"
			tone = v1.ToneError
		}
		return []*v1.Node{{Kind: v1.KindText, Text: text, Tone: tone}}
	}
	nodes := []*v1.Node{
		iconNode(snap.Forecast.Current.Code),
		{Kind: v1.KindText, Text: Condition(snap.Forecast.Current.Code)},
		tempNode(snap, Options{ShowTemperature: true, ShowUnit: true}),
	}
	if len(snap.Forecast.Daily) > 0 {
		d := snap.Forecast.Daily[0]
		nodes = append(nodes,
			&v1.Node{Kind: v1.KindText, Text: dayLine(d, snap.Unit)},
			&v1.Node{Kind: v1.KindText, Text: clockPart(d.Sunrise) + " " + clockPart(d.Sunset)},
		)
	}
	if snap.Stale() {
		nodes = append(nodes, ageNode(snap))
	}
	return nodes
}

func iconNode(code int) *v1.Node {
	return &v1.Node{Kind: v1.KindIcon, Icon: render.IconName(code), Name: Condition(code)}
}

func tempNode(snap Snapshot, opt Options) *v1.Node {
	return &v1.Node{Kind: v1.KindText, Key: "temp", Tabular: true, Text: formatTemp(snap.Forecast.Current.Temperature, snap.Unit, opt.ShowUnit)}
}

func ageNode(snap Snapshot) *v1.Node {
	return &v1.Node{Kind: v1.KindText, Key: "age", Text: "(" + humaniseAge(time.Since(snap.FetchedAt)) + ")"}
}

func formatTemp(temp float64, unit owm.Unit, showUnit bool) string {
	text := fmt.Sprintf("%.0f", temp)
	if showUnit {
		text += unitSuffix(unit)
	}
	return text
}

func unitSuffix(u owm.Unit) string {
	if u == owm.UnitFahrenheit {
		return "°F"
	}
	return "°C"
}

func dayLine(d owm.Day, unit owm.Unit) string {
	return fmt.Sprintf("%.0f/%.0f%s", d.High, d.Low, unitSuffix(unit))
}

func clockPart(value string) string {
	if i := strings.LastIndex(value, "T"); i >= 0 && i+1 < len(value) {
		return value[i+1:]
	}
	return value
}

func humaniseAge(age time.Duration) string {
	switch {
	case age >= 24*time.Hour:
		return fmt.Sprintf("%dd", int(age.Hours())/24)
	case age >= time.Hour:
		return fmt.Sprintf("%dh", int(age.Hours()))
	case age >= time.Minute:
		return fmt.Sprintf("%dm", int(age.Minutes()))
	}
	return "now"
}
