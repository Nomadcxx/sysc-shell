package shell

import (
	"reflect"
	"testing"
)

func TestPanelsTopicFollowsOpenClose(t *testing.T) {
	reg := newPanelRegistry(t)

	// No subscriber: opening a panel publishes nothing.
	if err := reg.HandlePanelByName("open", "clock", ""); err != nil {
		t.Fatal(err)
	}
	drainAuxQueue(reg)
	if reg.panelsPublish != nil {
		t.Fatal("a publish func exists with no subscriber")
	}

	var got [][]string
	topic := reg.PanelsTopic()
	if err := topic.Start(func(v any) { got = append(got, v.([]string)) }); err != nil {
		t.Fatal(err)
	}
	if err := reg.HandlePanelByName("open", "session", ""); err != nil {
		t.Fatal(err)
	}
	drainAuxQueue(reg)
	reg.ClosePanel(PanelClock)
	drainAuxQueue(reg)
	reg.ClosePanel(PanelSession)
	drainAuxQueue(reg)
	want := [][]string{{"clock"}, {"clock", "session"}, {"session"}, {}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("published %q, want %q", got, want)
	}

	topic.Stop()
	if reg.panelsPublish != nil {
		t.Fatal("Stop left the publish func set")
	}
	if err := reg.HandlePanelByName("open", "clock", ""); err != nil {
		t.Fatal(err)
	}
	drainAuxQueue(reg)
	if len(got) != len(want) {
		t.Fatalf("published after Stop: %q", got[len(want):])
	}
}
