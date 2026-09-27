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
		{config.Item{ID: "plugin", Plugin: "org.example.timer", Entry: "bar"}, "org.example.timer"},
		{config.Item{ID: "plugin"}, "Plugin"},
		{config.Item{ID: "window-title"}, "Window title"},
		{config.Item{ID: "not-a-widget"}, "not-a-widget"},
	} {
		if got := WidgetName(tc.it, nil); got != tc.want {
			t.Errorf("WidgetName(%+v) = %q, want %q", tc.it, got, tc.want)
		}
	}
}

func TestPluginWidgetsAreNamedByTheirPlugin(t *testing.T) {
	t.Parallel()
	it := config.Item{ID: "plugin", Plugin: "org.sysc.mini-docker", Entry: "bar"}
	names := func(id string) string {
		if id == "org.sysc.mini-docker" {
			return "Mini Docker"
		}
		return ""
	}
	if got := WidgetName(it, names); got != "Mini Docker" {
		t.Errorf("named %q, want Mini Docker", got)
	}
	if got := WidgetName(it, func(string) string { return "" }); got != "org.sysc.mini-docker" {
		t.Errorf("unknown plugin named %q, want its id", got)
	}
	if got := WidgetName(config.Item{ID: "clock"}, nil); got != "Clock" {
		t.Errorf("clock named %q", got)
	}
}
