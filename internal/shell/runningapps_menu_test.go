package shell

import (
	"fmt"
	"testing"

	"github.com/Nomadcxx/sysc-shell/internal/config"
	"github.com/Nomadcxx/sysc-shell/internal/platform/niri"
	"github.com/Nomadcxx/sysc-shell/internal/platform/wayland"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

func TestRunningAppsMenuSizeUsesMeasuredVisibleRows(t *testing.T) {
	_, host := runningAppsLongMenu(t, 12)
	if host.text == nil || host.style.Size <= 0 || host.style.Scale120 != ui.Scale120(150) {
		t.Fatalf("text renderer/style not initialized before sizing: text=%t size=%d scale=%d", host.text != nil, host.style.Size, host.style.Scale120)
	}
	width, height := host.size()
	if err := host.configure(width, height, 150); err != nil {
		t.Fatal(err)
	}
	if got := len(capsules(host.root)); got != menuVisibleRows {
		t.Fatalf("visible capsule rows = %d, want %d", got, menuVisibleRows)
	}
	want, err := ui.ContentHeight(host.root, width, host.measureText())
	if err != nil {
		t.Fatal(err)
	}
	if got := height; got != min(want, trayMenuMaxHeight) {
		t.Fatalf("surface height = %d, want measured tree height %d capped at %d", got, want, trayMenuMaxHeight)
	}
	if height > trayMenuMaxHeight {
		t.Fatalf("surface height = %d, exceeds tray menu cap %d", height, trayMenuMaxHeight)
	}
}

func TestRunningAppsMenuWheelDelta(t *testing.T) {
	for _, tc := range []struct {
		name  string
		event wayland.Event
		want  float64
	}{
		{"discrete down", wayland.Event{AxisDiscrete: 1}, wheelStepPixels},
		{"discrete magnitude", wayland.Event{AxisDiscrete: 3}, wheelStepPixels * 3},
		{"value120 up", wayland.Event{AxisValue120: -240}, -wheelStepPixels * 2},
		{"smooth down", wayland.Event{AxisValue: 0.5}, 0.5},
		{"horizontal", wayland.Event{Axis: 1, AxisDiscrete: 1}, 0},
		{"empty", wayland.Event{}, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := runningAppMenuWheelDelta(tc.event); got != tc.want {
				t.Fatalf("wheel delta = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestRunningAppsMenuWheelKeepsMagnitudeAndAccumulatesSmoothInput(t *testing.T) {
	for _, tc := range []struct {
		name   string
		events []wayland.Event
		want   int
	}{
		{"discrete magnitude", []wayland.Event{{AxisDiscrete: 3}}, 3},
		{"value120 magnitude", []wayland.Event{{AxisValue120: 240}}, 2},
		{"smooth accumulation", []wayland.Event{{AxisValue: 20}, {AxisValue: 20}}, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, host := runningAppsLongMenu(t, 9)
			width, height := host.size()
			if err := host.configure(width, height, 120); err != nil {
				t.Fatal(err)
			}
			for _, event := range tc.events {
				event.Kind = wayland.EventPointerAxis
				if !host.handle(event) {
					t.Fatal("wheel input was not consumed")
				}
				drainInvalidations(host.r)
			}
			if got := host.highlight(); got != tc.want {
				t.Fatalf("wheel cursor = %d, want %d", got, tc.want)
			}
		})
	}
}

func TestRunningAppsMenuKeyboardAndWheelReachOffscreenRows(t *testing.T) {
	_, host := runningAppsLongMenu(t, 9)
	width, height := host.size()
	if err := host.configure(width, height, 120); err != nil {
		t.Fatal(err)
	}
	for range len(host.rows) - 1 {
		if !host.handle(wayland.Event{Kind: wayland.EventKeyPress, Key: keyDown}) {
			t.Fatal("keyboard navigation was ignored")
		}
		drainInvalidations(host.r)
	}
	if got := host.highlight(); got != len(host.rows)-1 {
		t.Fatalf("keyboard cursor = %d, want last row %d", got, len(host.rows)-1)
	}
	if got := len(capsules(host.root)); got != menuVisibleRows {
		t.Fatalf("keyboard viewport has %d rows, want %d", got, menuVisibleRows)
	}
	last := capsules(host.root)[menuVisibleRows-1]
	if got, ok := host.hitRow(last.Bounds.X+1, last.Bounds.Y+1); !ok || got != len(host.rows)-1 {
		t.Fatalf("last visible hit = (%d, %t), want original row %d", got, ok, len(host.rows)-1)
	}

	_, host = runningAppsLongMenu(t, 9)
	width, height = host.size()
	if err := host.configure(width, height, 150); err != nil {
		t.Fatal(err)
	}
	for range len(host.rows) - 1 {
		if !host.handle(wayland.Event{Kind: wayland.EventPointerAxis, Axis: 0, AxisDiscrete: 1}) {
			t.Fatal("vertical wheel navigation was ignored")
		}
		drainInvalidations(host.r)
	}
	if got := host.highlight(); got != len(host.rows)-1 {
		t.Fatalf("wheel cursor = %d, want last row %d", got, len(host.rows)-1)
	}
	if got := len(capsules(host.root)); got != menuVisibleRows {
		t.Fatalf("wheel viewport has %d rows, want %d", got, menuVisibleRows)
	}
	measured, err := ui.ContentHeight(host.root, width, host.measureText())
	if err != nil {
		t.Fatal(err)
	}
	if measured > height {
		t.Fatalf("bottom viewport height = %d, exceeds configured surface height %d", measured, height)
	}
	wheelRow := capsules(host.root)[4]
	if got, ok := host.hitRow(wheelRow.Bounds.X+1, wheelRow.Bounds.Y+1); !ok || got != len(host.rows)-2 {
		t.Fatalf("scrolled hit = (%d, %t), want original row %d", got, ok, len(host.rows)-2)
	}
}

func runningAppsLongMenu(t *testing.T, actionCount int) (*Registry, *runningAppMenuHost) {
	t.Helper()
	cfg := config.Default()
	cfg.Bar.Left, cfg.Bar.Center, cfg.Bar.Right = nil, nil, nil
	reg := NewRegistry(cfg)
	t.Cleanup(reg.Close)
	newHosts(t, reg, map[uint32]string{1: "DP-1"})
	if err := reg.bars[1].Configure(cfg.Bar.SurfaceExtent(), 900, 150); err != nil {
		t.Fatal(err)
	}

	actions := make([]runningAppAction, actionCount)
	for i := range actions {
		actions[i] = runningAppAction{ID: fmt.Sprintf("action-%d", i), Name: fmt.Sprintf("Action %d", i), Exec: fmt.Sprintf("app --action %d", i)}
	}
	member := niri.Window{ID: 80, AppID: "test.app", WorkspaceID: 10, HasWorkspace: true}
	reg.UpdateNiri(niri.Snapshot{Windows: []niri.Window{member}})
	slot := runningAppSlot{Key: "test.app", Actions: actions, Members: []niri.Window{member}, MRU: member}
	reg.runningMenu = newRunningAppMenuHost(reg)
	reg.runningMenu.request = func(wayland.AuxRequest) {}
	reg.mu.Lock()
	reg.runningMenu.openLocked(1, slot, ui.Rect{})
	reg.mu.Unlock()
	return reg, reg.runningMenu
}
