package ui

import (
	"strings"
	"testing"
)

func TestAnimatedTracksOnlyInteractiveChrome(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		node *Node
		want bool
	}{
		{"button", &Node{Kind: KindButton, Action: "a"}, true},
		{"segmented", &Node{Kind: KindSegmented, Key: "profiles"}, true},
		{"clickable capsule", &Node{Kind: KindCapsule, Action: "battery"}, true},
		{"display capsule", &Node{Kind: KindCapsule}, false},
		{"text", &Node{Kind: KindText, Text: "42%"}, false},
		{"nil", nil, false},
	} {
		if got := Animated(tc.node); got != tc.want {
			t.Errorf("Animated(%s) = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestValidateKeysAcceptsDistinctKeys(t *testing.T) {
	t.Parallel()
	root := &Node{Kind: KindColumn, Children: []*Node{
		{Kind: KindButton, Text: "Lock", Action: "session:lock"},
		{Kind: KindButton, Text: "Log out", Key: "session-logout"},
		{Kind: KindSegmented, Key: "profiles", Children: []*Node{
			{Kind: KindButton, Text: "Power save", Action: "profile:power-saver"},
			{Kind: KindButton, Text: "Balanced", Action: "profile:balanced"},
		}},
		{Kind: KindCapsule, Text: "CPU 12%"},
	}}
	if err := ValidateKeys(root); err != nil {
		t.Fatalf("ValidateKeys = %v, want nil", err)
	}
}

func TestValidateKeysRejectsDuplicateKey(t *testing.T) {
	t.Parallel()
	root := &Node{Kind: KindColumn, Children: []*Node{
		{Kind: KindButton, Text: "Retry", Action: "plugin-retry"},
		{Kind: KindButton, Text: "Retry", Action: "plugin-retry"},
	}}
	err := ValidateKeys(root)
	if err == nil {
		t.Fatal("ValidateKeys = nil, want a duplicate-key error")
	}
	if !strings.Contains(err.Error(), "plugin-retry") {
		t.Errorf("error %q does not name the duplicate key", err)
	}
}

func TestValidateKeysRejectsUnkeyedAnimatedNode(t *testing.T) {
	t.Parallel()
	root := &Node{Kind: KindColumn, Children: []*Node{
		{Kind: KindButton, Text: "Reboot"},
	}}
	err := ValidateKeys(root)
	if err == nil {
		t.Fatal("ValidateKeys = nil, want an empty-key error")
	}
	// The message has to name the offender: an unkeyed button is a composition
	// bug the caller fixes by hand.
	if !strings.Contains(err.Error(), "Reboot") {
		t.Errorf("error %q does not name the offending button", err)
	}
}

func TestValidateKeysIgnoresUnanimatedDuplicates(t *testing.T) {
	t.Parallel()
	// Two display capsules with no action share the empty key and must not trip
	// validation; only clickable chrome is tracked.
	root := &Node{Kind: KindRow, Children: []*Node{
		{Kind: KindCapsule, Text: "CPU"},
		{Kind: KindCapsule, Text: "Memory"},
	}}
	if err := ValidateKeys(root); err != nil {
		t.Fatalf("ValidateKeys = %v, want nil", err)
	}
}

func TestFocusablesSkipDisabledNodes(t *testing.T) {
	t.Parallel()
	off := &Node{Kind: KindButton, Text: "Lock", Action: "session:lock",
		Focusable: true, State: StateDisabled}
	on := &Node{Kind: KindButton, Text: "Log out", Action: "session:logout", Focusable: true}
	got := Focusables(&Node{Kind: KindColumn, Children: []*Node{off, on}})
	if len(got) != 1 || got[0] != on {
		t.Fatalf("Focusables returned %d nodes, want only the enabled one", len(got))
	}
}
