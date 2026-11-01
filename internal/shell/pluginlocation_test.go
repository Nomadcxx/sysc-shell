package shell

import (
	"testing"

	v1 "github.com/Nomadcxx/sysc-shell/plugin/v1"
)

// A reload that leaves the place alone must not push location.changed again,
// and each plugin is tracked on its own.
func TestPluginLocationsPushOnlyNews(t *testing.T) {
	var l pluginLocations
	melbourne := v1.Location{Configured: true, Latitude: -37.81, Longitude: 144.96, Label: "Melbourne"}
	if !l.record("a", melbourne) {
		t.Fatal("first location for a plugin is not news")
	}
	if l.record("a", melbourne) {
		t.Fatal("an unchanged location was pushed again")
	}
	if !l.record("b", melbourne) {
		t.Fatal("a second plugin shared the first one's record")
	}
	london := v1.Location{Configured: true, Latitude: 51.51, Longitude: -0.13, Label: "London"}
	if !l.record("a", london) {
		t.Fatal("a moved location was not news")
	}
}
