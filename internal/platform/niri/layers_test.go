package niri

import "testing"

const layersFixture = `{"Ok":{"Layers":[` +
	`{"namespace":"quickshell","output":"DP-1","layer":"Background"},` +
	`{"namespace":"dms:bar","output":"DP-1","layer":"Top"},` +
	`{"namespace":"slapper","output":"DP-3","layer":"Background"},` +
	`{"namespace":"quickshell","output":"DP-3","layer":"Background"}]}}`

func TestParseLayers(t *testing.T) {
	layers, err := parseLayers([]byte(layersFixture))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(layers) != 4 {
		t.Fatalf("got %d layers, want 4", len(layers))
	}
	if layers[0].Namespace != "quickshell" || layers[0].Output != "DP-1" || layers[0].Layer != "Background" {
		t.Fatalf("first = %+v", layers[0])
	}
}

func TestParseLayersRejects(t *testing.T) {
	for name, line := range map[string]string{
		"error":   `{"Err":"no such request"}`,
		"empty":   `{}`,
		"garbage": `not json`,
	} {
		if _, err := parseLayers([]byte(line)); err == nil {
			t.Errorf("%s must fail", name)
		}
	}
}

func TestBackgroundOwnersIgnoresOurOwn(t *testing.T) {
	layers, err := parseLayers([]byte(layersFixture))
	if err != nil {
		t.Fatal(err)
	}
	ours := func(ns string) bool { return ns == "slapper" }
	owners := BackgroundOwners(layers, ours)

	if owners["DP-1"] != "quickshell" {
		t.Errorf("DP-1 owner = %q, want quickshell", owners["DP-1"])
	}
	// DP-3 carries our own slapper surface and a foreign one; the foreign one
	// is what matters, because it is what covers us.
	if owners["DP-3"] != "quickshell" {
		t.Errorf("DP-3 owner = %q, want the foreign surface", owners["DP-3"])
	}
	if got := BackgroundOwners(layers, func(string) bool { return true }); len(got) != 0 {
		t.Errorf("everything ours means no foreign owner, got %v", got)
	}
}
