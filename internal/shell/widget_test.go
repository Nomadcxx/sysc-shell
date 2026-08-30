package shell

import (
	"testing"
	"time"

	"github.com/Nomadcxx/sysc-shell/internal/config"
)

// reference is a fixed instant, so format assertions do not depend on when the
// test runs.
var reference = time.Date(2026, 8, 30, 15, 4, 5, 0, time.UTC)

func TestAClockWidgetFormatsTheSharedSnapshot(t *testing.T) {
	t.Parallel()
	widgets := buildWidgets([]config.Item{
		{ID: "clock", Format: "15:04", Boundary: time.Minute},
		{ID: "clock", Format: "Mon 2 Jan", Boundary: time.Minute},
	})

	view := barView{Now: reference}
	if got := widgets[0].format(view); got != "15:04" {
		t.Fatalf("time clock = %q, want 15:04", got)
	}
	if got := widgets[1].format(view); got != "Sun 30 Aug" {
		t.Fatalf("date clock = %q, want Sun 30 Aug", got)
	}
}

// Before the first tick there is no time to show, and a bar must still render.
func TestAClockWidgetIsEmptyBeforeTheFirstTick(t *testing.T) {
	t.Parallel()
	widgets := buildWidgets([]config.Item{{ID: "clock", Format: "15:04"}})

	if got := widgets[0].format(barView{}); got != "" {
		t.Fatalf("clock before the first tick = %q, want empty", got)
	}
}

func TestNiriWidgetsReadTheirOutputsProjection(t *testing.T) {
	t.Parallel()
	widgets := buildWidgets([]config.Item{
		{ID: "workspace"},
		{ID: "window-title", MaxWidth: 120},
	})
	view := barView{Workspace: "code", Title: "Fixture One"}

	if got := widgets[0].format(view); got != "code" {
		t.Fatalf("workspace = %q, want code", got)
	}
	if got := widgets[1].format(view); got != "Fixture One" {
		t.Fatalf("title = %q, want Fixture One", got)
	}
	if got := widgets[1].node.MaxWidth; got != 120 {
		t.Fatalf("title node max width = %d, want 120", got)
	}
}

func TestApplyWritesOnlyChangedText(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	bar, err := NewWithTheme(ThemeFrom(cfg, cfg.Bar), cfg.Bar, "DP-9")
	if err != nil {
		t.Fatalf("NewWithTheme: %v", err)
	}

	if changed := bar.apply(barView{Now: reference, Workspace: "code", Title: "Fixture One"}); !changed {
		t.Fatal("the first view reported no change")
	}
	// Re-applying the same view must report nothing: no change, no redraw.
	if changed := bar.apply(barView{Now: reference, Workspace: "code", Title: "Fixture One"}); changed {
		t.Fatal("an identical view reported a change")
	}
	// A different instant inside the same minute renders identical text.
	sameMinute := reference.Add(20 * time.Second)
	if changed := bar.apply(barView{Now: sameMinute, Workspace: "code", Title: "Fixture One"}); changed {
		t.Fatal("a tick inside the same minute reported a change")
	}
	// Crossing the minute must change.
	nextMinute := reference.Add(time.Minute)
	if changed := bar.apply(barView{Now: nextMinute, Workspace: "code", Title: "Fixture One"}); !changed {
		t.Fatal("crossing a minute boundary reported no change")
	}
}

func TestABarRemembersItsConnector(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	bar, err := NewWithTheme(ThemeFrom(cfg, cfg.Bar), cfg.Bar, "HDMI-A-9")
	if err != nil {
		t.Fatalf("NewWithTheme: %v", err)
	}
	if got := bar.connector(); got != "HDMI-A-9" {
		t.Fatalf("connector = %q, want HDMI-A-9", got)
	}
}

// Only the clock asks for tabular figures; nothing else should.
func TestOnlyClockWidgetsRequestTabularFigures(t *testing.T) {
	t.Parallel()
	widgets := buildWidgets([]config.Item{
		{ID: "clock", Format: "15:04"},
		{ID: "workspace"},
		{ID: "window-title", MaxWidth: 120},
	})

	if !widgets[0].node.Tabular {
		t.Fatal("the clock node does not request tabular figures")
	}
	if widgets[1].node.Tabular || widgets[2].node.Tabular {
		t.Fatal("a non-clock widget requested tabular figures")
	}
}
