package shell

import (
	"fmt"

	metrics "github.com/Nomadcxx/sysc-metrics"

	"github.com/Nomadcxx/sysc-shell/internal/config"
	"github.com/Nomadcxx/sysc-shell/internal/services"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

// metricSource maps a widget id to the telemetry source it leases.
func metricSource(item config.Item) (services.Source, bool) {
	switch item.ID {
	case "cpu":
		return services.SourceCPU, true
	case "memory":
		return services.SourceMemory, true
	case "filesystem":
		return services.SourceFilesystem, true
	case "block":
		return services.SourceBlock, true
	case "network":
		return services.SourceNetwork, true
	}
	return 0, false
}

// metricFraction reports a fraction between zero and one for the sources that
// have one. Rate sources have no full scale and report absent; the loader
// already rejects a meter on them, so nothing asks twice.
func metricFraction(item config.Item, snap services.Snapshot) (float64, bool) {
	switch item.ID {
	case "cpu":
		if snap.CPU == nil || !snap.CPU.Usage.Valid {
			return 0, false
		}
		return snap.CPU.Usage.Fraction, true
	case "memory":
		if snap.Memory == nil {
			return 0, false
		}
		return capacityFraction(snap.Memory.Memory)
	case "filesystem":
		if snap.Filesystem == nil {
			return 0, false
		}
		for _, fs := range snap.Filesystem.Filesystems {
			if fs.MountPoint == item.Path {
				return capacityFraction(fs.Capacity)
			}
		}
	}
	return 0, false
}

// capacityFraction is used over total. A zero total means the capacity was not
// read, which is absent rather than zero per cent.
func capacityFraction(c metrics.Capacity) (float64, bool) {
	if c.TotalBytes == 0 {
		return 0, false
	}
	return float64(c.UsedBytes) / float64(c.TotalBytes), true
}

// metricRate reports bytes per second for the rate sources.
func metricRate(item config.Item, snap services.Snapshot) (float64, bool) {
	switch item.ID {
	case "block":
		if snap.Block == nil {
			return 0, false
		}
		for _, d := range snap.Block.Devices {
			if d.Name != item.Device {
				continue
			}
			if !d.Rates.Valid {
				return 0, false
			}
			if item.Direction == "write" {
				return d.Rates.WriteBytesPerSecond, true
			}
			return d.Rates.ReadBytesPerSecond, true
		}
	case "network":
		if snap.Network == nil {
			return 0, false
		}
		for _, i := range snap.Network.Interfaces {
			if i.Name != item.Interface {
				continue
			}
			if !i.Rates.Valid {
				return 0, false
			}
			if item.Direction == "tx" {
				return i.Rates.TransmitBytesPerSecond, true
			}
			return i.Rates.ReceiveBytesPerSecond, true
		}
	}
	return 0, false
}

// formatMetric renders one metric. An absent source, an absent subject and an
// invalid value all render the same placeholder, so a reader never has to tell
// them apart and no widget invents a zero.
//
// It takes no interval. The shell therefore has no elapsed time to divide by
// even accidentally: rates can only come from the library's comparison of two
// monotonic samples.
func formatMetric(item config.Item, snap services.Snapshot) string {
	if fraction, ok := metricFraction(item, snap); ok {
		return fmt.Sprintf("%.0f%%", fraction*100)
	}
	if rate, ok := metricRate(item, snap); ok {
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
				if fraction, ok := metricFraction(item, v.Metrics); ok {
					node.Value = fraction
				} else {
					node.Value = 0
				}
				return ""
			},
		}
	case "graph":
		node := &ui.Node{Kind: ui.KindGraph, Width: metricGraphWidth}
		src, _ := metricSource(item)
		return textWidget{
			node: node,
			format: func(v barView) string {
				node.Values = normalise(v.History[src])
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
