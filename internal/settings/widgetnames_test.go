package settings

import (
	"testing"

	"github.com/Nomadcxx/sysc-shell/internal/config"
)

// The point of this test is the assertion, not the map: a widget added to the
// vocabulary later without a label would render in the editor as its raw
// configuration token, which nobody would notice until it shipped.
func TestEveryWidgetIdHasADisplayName(t *testing.T) {
	t.Parallel()
	for _, id := range config.WidgetIDs() {
		if widgetNames[id] == "" {
			t.Errorf("widget %q has no display name", id)
		}
	}
}

func TestNoDisplayNameNamesAWidgetThatDoesNotExist(t *testing.T) {
	t.Parallel()
	known := map[string]bool{}
	for _, id := range config.WidgetIDs() {
		known[id] = true
	}
	for id := range widgetNames {
		if !known[id] {
			t.Errorf("display name for %q, which is not a widget", id)
		}
	}
}

func TestWidgetNameHandlesGroupsAndPlacements(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		it   config.Item
		want string
	}{
		{config.Item{ID: "group"}, "Group"},
		{config.Item{ID: "plugin", Entry: "timer"}, "timer"},
		{config.Item{ID: "plugin"}, "Plugin"},
		{config.Item{ID: "window-title"}, "Window title"},
		{config.Item{ID: "not-a-widget"}, "not-a-widget"},
	} {
		if got := WidgetName(tc.it); got != tc.want {
			t.Errorf("WidgetName(%+v) = %q, want %q", tc.it, got, tc.want)
		}
	}
}
