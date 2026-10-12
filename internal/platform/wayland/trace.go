package wayland

import (
	"context"
	"runtime/trace"
)

// startTraceRegion records a stage only during an explicit diagnostic capture.
// ponytail: use the standard execution trace for opt-in timing; derive summaries
// offline if needed instead of maintaining a production timing buffer.
func startTraceRegion(name string) *trace.Region {
	if !trace.IsEnabled() {
		return nil
	}
	return trace.StartRegion(context.Background(), name)
}
