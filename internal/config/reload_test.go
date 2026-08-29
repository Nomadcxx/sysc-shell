package config

import (
	"strings"
	"testing"
)

func TestResolveReturnsOnePolicyPerConnector(t *testing.T) {
	t.Parallel()
	cfg, err := Parse([]byte(`{"outputs":[{"connector":"DP-1","bar":{"height":44}}]}`))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	bars, err := Resolve(cfg, []string{"DP-1", "DP-3"})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if len(bars) != 2 {
		t.Fatalf("Resolve returned %d policies, want 2", len(bars))
	}
	if bars[0].Height != 44 {
		t.Fatalf("DP-1 height = %d, want the override 44", bars[0].Height)
	}
	if bars[1].Height != 48 {
		t.Fatalf("DP-3 height = %d, want the base 48", bars[1].Height)
	}
}

func TestResolveRejectsTheWholeCandidateWhenOneHostFails(t *testing.T) {
	t.Parallel()
	// Valid as a document, but the override leaves DP-1 with no painted body.
	cfg := Default()
	cfg.Outputs = []OutputOverride{{
		Connector: "DP-1",
		Bar:       Bar{Enabled: true, Edge: "top", Height: 6, Gap: 4, FontSize: 14},
	}}

	if _, err := Resolve(cfg, []string{"DP-1", "DP-3"}); err == nil {
		t.Fatal("Resolve accepted a policy that leaves no body")
	} else if !strings.Contains(err.Error(), "DP-1") {
		t.Fatalf("error %q does not name the failing connector", err)
	}
}

func TestResolveOfNoConnectorsSucceeds(t *testing.T) {
	t.Parallel()
	// A shell with no outputs connected is not an error.
	bars, err := Resolve(Default(), nil)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if len(bars) != 0 {
		t.Fatalf("Resolve returned %d policies for no connectors", len(bars))
	}
}

func TestResolvePreservesConnectorOrder(t *testing.T) {
	t.Parallel()
	cfg, err := Parse([]byte(`{"outputs":[
	  {"connector":"DP-3","bar":{"height":50}},
	  {"connector":"DP-1","bar":{"height":44}}]}`))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	// The result is indexed against the caller's connector slice, not the
	// configuration's order, so the owner can zip it against its host list.
	bars, err := Resolve(cfg, []string{"DP-1", "DP-3"})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if bars[0].Height != 44 || bars[1].Height != 50 {
		t.Fatalf("policies = %d/%d, want 44/50 in the caller's order",
			bars[0].Height, bars[1].Height)
	}
}
