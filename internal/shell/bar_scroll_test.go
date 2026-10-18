package shell

import (
	"strings"
	"testing"
	"time"

	"github.com/Nomadcxx/sysc-shell/internal/config"
	"github.com/Nomadcxx/sysc-shell/internal/platform/wayland"
)

func TestBarScrollActionTargetsOnlyTheWidgetUnderPointer(t *testing.T) {
	cfg, err := config.Parse([]byte(`{"bar":{"items":{
		"left":[
			{"id":"group","items":[{"id":"clock","format":"15:04","scroll":"brightness"}]},
			{"id":"launcher","scroll":"volume"},
			{"id":"volume","scroll":"none"}
		],"center":[],"right":[]}}}`))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	bar, err := NewWithTheme(ThemeFrom(cfg, cfg.Bar), cfg.Bar, "DP-9")
	if err != nil {
		t.Fatalf("NewWithTheme: %v", err)
	}
	t.Cleanup(bar.stopAnimation)
	bar.apply(barView{Now: time.Date(2026, 10, 18, 13, 45, 0, 0, time.UTC)})
	if err := bar.Configure(1200, BarHeight, 120); err != nil {
		t.Fatalf("Configure: %v", err)
	}
	type call struct {
		action string
		delta  int
	}
	var calls []call
	bar.setAxisHandler(func(action string, delta int) bool {
		if !strings.HasPrefix(action, "bar-scroll:") {
			return false
		}
		calls = append(calls, call{action: action, delta: delta})
		return true
	})

	for _, tc := range []struct {
		index  int
		delta  int
		want   string
		wantBy bool
	}{
		{index: 0, delta: -1, want: "bar-scroll:brightness", wantBy: true},
		{index: 1, delta: 1, want: "bar-scroll:volume", wantBy: true},
		{index: 2, delta: 1, want: "panel:audio", wantBy: false},
	} {
		bar.mu.Lock()
		bounds := bar.left[tc.index].node.Bounds
		bar.mu.Unlock()
		if bounds.W <= 0 || bounds.H <= 0 {
			t.Fatalf("widget %d bounds = %+v", tc.index, bounds)
		}
		x, y := bounds.X+bounds.W/2, bounds.Y+bounds.H/2
		bar.Handle(wayland.Event{Kind: wayland.EventPointerEnter, X: float64(x), Y: float64(y)})
		got := bar.Handle(wayland.Event{Kind: wayland.EventPointerAxis, AxisDiscrete: int32(tc.delta)})
		if got != tc.wantBy {
			t.Fatalf("widget %d scroll consumed = %t, want %t", tc.index, got, tc.wantBy)
		}
		if tc.index == 2 {
			if len(calls) != 2 {
				t.Fatalf("disabled widget dispatched an action: %+v", calls)
			}
			continue
		}
		if len(calls) != tc.index+1 || calls[tc.index] != (call{action: tc.want, delta: tc.delta}) {
			t.Fatalf("widget %d dispatches = %+v, want %q delta %d", tc.index, calls, tc.want, tc.delta)
		}
	}

	bar.mu.Lock()
	bounds := bar.left[1].node.Bounds
	click, ok := bar.hitLocked(bounds.X+bounds.W/2, bounds.Y+bounds.H/2)
	bar.mu.Unlock()
	if !ok || click != panelLauncherAction {
		t.Fatalf("launcher click action = %q, %t; scroll configuration changed click routing", click, ok)
	}
}

func TestBarScrollFractionalDetentsStayWithOneWidget(t *testing.T) {
	cfg, err := config.Parse([]byte(`{"bar":{"items":{
		"left":[
			{"id":"clock","format":"15:04","scroll":"volume"},
			{"id":"clock","format":"15:04","scroll":"volume"}
		],"center":[],"right":[]}}}`))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	bar, err := NewWithTheme(ThemeFrom(cfg, cfg.Bar), cfg.Bar, "DP-9")
	if err != nil {
		t.Fatalf("NewWithTheme: %v", err)
	}
	t.Cleanup(bar.stopAnimation)
	bar.apply(barView{Now: time.Date(2026, 10, 18, 13, 45, 0, 0, time.UTC)})
	if err := bar.Configure(1200, BarHeight, 120); err != nil {
		t.Fatalf("Configure: %v", err)
	}
	var got []int
	bar.setAxisHandler(func(action string, delta int) bool {
		if action != "bar-scroll:volume" {
			t.Fatalf("axis action = %q, want bar-scroll:volume", action)
		}
		got = append(got, delta)
		return true
	})
	for _, index := range []int{0, 1, 1} {
		bar.mu.Lock()
		bounds := bar.left[index].node.Bounds
		bar.mu.Unlock()
		x, y := bounds.X+bounds.W/2, bounds.Y+bounds.H/2
		bar.Handle(wayland.Event{Kind: wayland.EventPointerEnter, X: float64(x), Y: float64(y)})
		if !bar.Handle(wayland.Event{Kind: wayland.EventPointerAxis, AxisValue120: 60}) {
			t.Fatalf("scroll over widget %d was not consumed", index)
		}
	}
	if len(got) != 3 || got[0] != 0 || got[1] != 0 || got[2] != 1 {
		t.Fatalf("fractional detents dispatched %v, want [0 0 1]", got)
	}
}
