package ipc

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func TestMethodsListsEveryExactMethod(t *testing.T) {
	want := []string{
		"ipc.capabilities",
		"ipc.subscribe",
		"ipc.unsubscribe",
		"osd.step",
		"panel.close",
		"panel.open",
		"panel.toggle",
		"screenshot",
		"session.lock-state",
		"status",
		"switcher.show",
	}
	if got := Methods(); !reflect.DeepEqual(got, want) {
		t.Fatalf("Methods() = %v, want %v", got, want)
	}
}

func TestMethodPrefixesAreTheDelegatedFamilies(t *testing.T) {
	want := []string{"nightlight.", "plugins.", "theme."}
	if got := MethodPrefixes(); !reflect.DeepEqual(got, want) {
		t.Fatalf("MethodPrefixes() = %v, want %v", got, want)
	}
}

func TestDispatchOrderIsUnchanged(t *testing.T) {
	// Exact method wins over a prefix family, an unknown method still fails the
	// same way, and a delegated family still reaches its handler.
	s := &Server{}
	if got := string(s.handleLine(`{"method":"status"}`)); !strings.Contains(got, `"ok"`) {
		t.Fatalf("status reply = %s", got)
	}
	if got := string(s.handleLine(`{"method":"nope"}`)); !strings.Contains(got, "unknown method") {
		t.Fatalf("unknown reply = %s", got)
	}
	if got := string(s.handleLine(`{"method":"theme.nope"}`)); !strings.Contains(got, "theme handler unset") {
		t.Fatalf("theme reply = %s", got)
	}
	if got := string(s.handleLine(`not json`)); !strings.Contains(got, "malformed json") {
		t.Fatalf("malformed reply = %s", got)
	}
}

func TestDispatchedFamiliesReachTheirHandler(t *testing.T) {
	s := &Server{h: Handlers{
		Plugins: func(method string, _ json.RawMessage) (map[string]any, error) {
			return map[string]any{"saw": method}, nil
		},
		Theme:      func(string, json.RawMessage) (map[string]any, error) { return map[string]any{}, nil },
		NightLight: func(string, json.RawMessage) (map[string]any, error) { return map[string]any{}, nil },
	}}
	if got := string(s.handleLine(`{"method":"plugins.install"}`)); !strings.Contains(got, "plugins.install") {
		t.Fatalf("plugins reply = %s", got)
	}
	if got := string(s.handleLine(`{"method":"nightlight.set"}`)); !strings.Contains(got, `"ok"`) {
		t.Fatalf("nightlight reply = %s", got)
	}
}

func TestPanelMethodsStillCarryTheirAction(t *testing.T) {
	var gotAction, gotPanel string
	s := &Server{h: Handlers{Panel: func(action, panel, section string) error {
		gotAction, gotPanel = action, panel
		return nil
	}}}
	for method, wantAction := range map[string]string{
		"panel.toggle": "toggle",
		"panel.open":   "open",
		"panel.close":  "close",
	} {
		reply := string(s.handleLine(`{"method":"` + method + `","params":{"panel":"control-center"}}`))
		if !strings.Contains(reply, `"ok"`) {
			t.Fatalf("%s reply = %s", method, reply)
		}
		if gotAction != wantAction || gotPanel != "control-center" {
			t.Fatalf("%s called (%q, %q), want (%q, control-center)", method, gotAction, gotPanel, wantAction)
		}
	}
	if got := string(s.handleLine(`{"method":"panel.toggle","params":{"panel":"nope"}}`)); !strings.Contains(got, "unknown panel") {
		t.Fatalf("unknown panel reply = %s", got)
	}
}
