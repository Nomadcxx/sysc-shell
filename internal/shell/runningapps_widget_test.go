package shell

import (
	"testing"

	"github.com/Nomadcxx/sysc-shell/internal/config"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

func TestRunningAppsCapsule(t *testing.T) {
	t.Parallel()
	widgets := buildWidgets([]config.Item{{ID: "running-apps"}}, 8)
	if len(widgets) != 1 {
		t.Fatalf("built %d widgets, want 1", len(widgets))
	}
	w := widgets[0]
	view := barView{Running: []runningAppSlot{
		{Key: "firefox", Focused: true},
		{Key: "brave"},
	}}
	if w.refresh == nil {
		t.Fatal("running-apps should refresh a tree")
	}
	if !w.refresh(view) {
		t.Fatal("the first refresh reported no change")
	}
	if w.node.Kind != ui.KindCapsule {
		t.Fatalf("root kind = %d, want KindCapsule", w.node.Kind)
	}
	row := w.inner
	if row == nil || row.Kind != ui.KindRow || len(row.Children) != 2 {
		t.Fatalf("inner = %+v, want a row of two tiles", row)
	}
	if row.Gap != 4 {
		t.Fatalf("gap = %d, want 4", row.Gap)
	}
	firefox, brave := row.Children[0], row.Children[1]
	if firefox.Width != 24 || firefox.Height != 24 || firefox.Fill != ui.FillAccent {
		t.Fatalf("focused tile = %+v, want 24x24 FillAccent", firefox)
	}
	if brave.Width != 24 || brave.Height != 24 || brave.Fill != ui.FillNone {
		t.Fatalf("idle tile = %+v, want 24x24 FillNone", brave)
	}
	if firefox.Action != "running-app:firefox" || brave.Action != "running-app:brave" {
		t.Fatalf("actions = %q %q", firefox.Action, brave.Action)
	}
	if letter := tileLetter(firefox); letter != "F" {
		t.Fatalf("firefox letter = %q, want F", letter)
	}
	if w.refresh(view) {
		t.Fatal("an unchanged view rebuilt the tiles")
	}
	if !w.refresh(barView{}) {
		t.Fatal("clearing slots reported no change")
	}
	if len(w.node.Children) != 0 {
		t.Fatalf("empty capsule children = %d, want none", len(w.node.Children))
	}
}

func tileLetter(n *ui.Node) string {
	if n == nil || len(n.Children) != 1 || n.Children[0] == nil {
		return ""
	}
	return n.Children[0].Text
}
