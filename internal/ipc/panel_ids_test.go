package ipc

import (
	"reflect"
	"sort"
	"testing"
)

func TestPanelIDsIsTheSortedKnownPanelSet(t *testing.T) {
	got := PanelIDs()
	want := make([]string, 0, len(knownPanels))
	for id := range knownPanels {
		want = append(want, id)
	}
	sort.Strings(want)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("PanelIDs() = %v, want %v", got, want)
	}
	if len(got) == 0 {
		t.Fatal("PanelIDs() is empty")
	}
	got[0] = "mutated"
	if again := PanelIDs(); reflect.DeepEqual(again, want) == false {
		t.Fatalf("PanelIDs() handed out its own slice: a caller mutated the vocabulary")
	}
}
