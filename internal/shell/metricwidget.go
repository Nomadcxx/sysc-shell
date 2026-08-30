package shell

import (
	"fmt"

	"github.com/Nomadcxx/sysc-shell/internal/config"
	"github.com/Nomadcxx/sysc-shell/internal/services"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

// metricSelector maps a widget to the subject it leases and plots: the source,
// and for the sources with many subjects, which one and in which direction.
func metricSelector(item config.Item) (services.Selector, bool) {
	sel := services.Selector{Direction: item.Direction}
	switch item.ID {
	case "cpu":
		sel.Source = services.SourceCPU
	case "memory":
		sel.Source = services.SourceMemory
	case "filesystem":
		sel.Source, sel.Subject = services.SourceFilesystem, item.Path
	case "block":
		sel.Source, sel.Subject = services.SourceBlock, item.Device
	case "network":
		sel.Source, sel.Subject = services.SourceNetwork, item.Interface
	case "battery":
		sel.Source = services.SourceBattery
	default:
		return services.Selector{}, false
	}
	return sel, true
}

// metricFraction reports a fraction between zero and one for the sources that
// have one. Rate sources have no full scale and report absent; the loader
// already rejects a meter on them, so nothing asks twice.
func metricFraction(item config.Item, snap services.Snapshot) (float64, bool) {
	sel, ok := metricSelector(item)
	if !ok {
		return 0, false
	}
	return snap.Fraction(sel)
}

// metricValue reports whichever kind of number this widget's source yields, for
// a caller that only needs to know whether there is a reading at all.
func metricValue(item config.Item, snap services.Snapshot) (float64, bool) {
	sel, ok := metricSelector(item)
	if !ok {
		return 0, false
	}
	return snap.Value(sel)
}

// formatMetric renders one metric. An absent source, an absent subject and an
// invalid value all render the same placeholder, so a reader never has to tell
// them apart and no widget invents a zero.
//
// It takes no interval. The shell therefore has no elapsed time to divide by
// even accidentally: rates can only come from the library's comparison of two
// monotonic samples.
func formatMetric(item config.Item, snap services.Snapshot) string {
	sel, ok := metricSelector(item)
	if !ok {
		return noWorkspace
	}
	if fraction, ok := snap.Fraction(sel); ok {
		return fmt.Sprintf("%.0f%%", fraction*100)
	}
	if rate, ok := snap.Rate(sel); ok {
		return formatRate(rate)
	}
	return noWorkspace
}

// rateUnits are decimal, following the reference shell: a network rate is
// conventionally quoted in megabytes, not mebibytes.
var rateUnits = []struct {
	suffix string
	scale  float64
}{
	{"GB/s", 1e9},
	{"MB/s", 1e6},
	{"kB/s", 1e3},
}

func formatRate(bytesPerSecond float64) string {
	for _, u := range rateUnits {
		if bytesPerSecond >= u.scale {
			return fmt.Sprintf("%.1f %s", bytesPerSecond/u.scale, u.suffix)
		}
	}
	return fmt.Sprintf("%.0f B/s", bytesPerSecond)
}

const (
	// metricMeterWidth and metricGraphWidth are the reserved widths for the
	// two non-text display modes, in logical pixels.
	metricMeterWidth = 48
	metricGraphWidth = 48
	// metricWidthSample is the widest percentage a fraction widget can render.
	// The node floors at this string's measured width, so a bar does not
	// reflow as a value crosses from 9% to 100%. It is a sample string rather
	// than a pixel count because the width depends on the resolved face.
	metricWidthSample = "100%"
)

// buildMetricWidget makes one metric instance. Display mode is fixed at build
// time, so the format function never branches on configuration at paint time.
func buildMetricWidget(item config.Item) textWidget {
	switch item.Display {
	case "meter":
		node := &ui.Node{Kind: ui.KindMeter, Width: metricMeterWidth}
		return textWidget{
			node: node,
			format: func(v barView) string {
				// A meter carries its value on the node, not as text. The
				// fraction is written here because apply is the one pass that
				// sees each view.
				//
				// With no reading the node is marked absent rather than set to
				// zero. An empty track is indistinguishable from a genuine 0%,
				// so a failed collector would otherwise render as an idle
				// machine.
				fraction, ok := metricFraction(item, v.Metrics)
				if !ok {
					fraction = 0
				}
				node.Value, node.Absent = fraction, !ok
				return ""
			},
		}
	case "graph":
		node := &ui.Node{Kind: ui.KindGraph, Width: metricGraphWidth}
		sel, _ := metricSelector(item)
		return textWidget{
			node: node,
			format: func(v barView) string {
				// The window is plotted only while there is a current reading.
				// The ring keeps its last good samples across a failure, and
				// plotting those would draw a live line for a source that
				// stopped reporting minutes ago.
				if _, ok := metricValue(item, v.Metrics); !ok {
					node.Values, node.Absent = nil, true
					return ""
				}
				node.Values, node.Absent = normalise(v.History[sel]), false
				return ""
			},
		}
	default:
		return textWidget{
			node:   &ui.Node{Kind: ui.KindText, Tabular: true, MinWidthText: metricWidthSample},
			format: func(v barView) string { return formatMetric(item, v.Metrics) },
		}
	}
}

// normalise scales samples against the window maximum, which is what lets a
// rate with no natural full scale drive a graph. An all-zero window plots flat
// rather than dividing by zero.
func normalise(values []float64) []float64 {
	if len(values) == 0 {
		return nil
	}
	maximum := 0.0
	for _, v := range values {
		if v > maximum {
			maximum = v
		}
	}
	out := make([]float64, len(values))
	if maximum <= 0 {
		return out
	}
	for i, v := range values {
		out[i] = v / maximum
	}
	return out
}
