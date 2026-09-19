package config

import (
	"os"
	"path/filepath"
	"testing"
)

// The gate is open: a document may state any of the four edges. The flip is
// proven through the round trip, not by inspecting the map.
func TestSideEdgesRoundTripThroughWriteAndLoad(t *testing.T) {
	t.Parallel()
	for _, edge := range []string{"left", "right"} {
		t.Run(edge, func(t *testing.T) {
			p := filepath.Join(t.TempDir(), "config.json")
			c := Default()
			c.Bar.Edge = edge
			if err := Write(p, c); err != nil {
				t.Fatal(err)
			}
			data, err := os.ReadFile(p)
			if err != nil {
				t.Fatal(err)
			}
			got, err := Parse(data)
			if err != nil {
				t.Fatalf("%s edge: %v", edge, err)
			}
			if got.Bar.Edge != edge {
				t.Fatalf("edge = %q, want %q to survive the round trip", got.Bar.Edge, edge)
			}
		})
	}
}

// Opening the gate changes no document anyone already has.
func TestTheDefaultDocumentKeepsItsTopEdge(t *testing.T) {
	t.Parallel()
	if got := Default().Bar.Edge; got != "top" {
		t.Fatalf("default edge = %q, want top", got)
	}
}
