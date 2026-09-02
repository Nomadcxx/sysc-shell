package recorder

import (
	"strings"
	"testing"

	v1 "github.com/Nomadcxx/sysc-shell/plugin/v1"
)

func TestBarTreeStates(t *testing.T) {
	t.Parallel()
	cases := []struct {
		mode Mode
		want string
		tone v1.Tone
	}{
		{Idle, "Record", v1.ToneNormal},
		{Recording, "Recording", v1.ToneNormal},
		{ReplayActive, "Replay", v1.ToneNormal},
		{Stopping, "Stopping", v1.ToneNormal},
		{Unavailable, "unavailable", v1.ToneError},
		{Failed, "failed", v1.ToneError},
		{Adopted, "Recording", v1.ToneNormal},
	}
	for _, tc := range cases {
		root := BarTree(Snapshot{Mode: tc.mode}, Config{})
		if err := v1.Validate(root, v1.ViewBar); err != nil {
			t.Fatalf("%s: %v", tc.mode, err)
		}
		body := flatten(root)
		if !strings.Contains(strings.ToLower(body), strings.ToLower(tc.want)) {
			t.Fatalf("%s bar = %q, want %q", tc.mode, body, tc.want)
		}
		if tone(root) != tc.tone {
			t.Fatalf("%s tone = %q, want %q", tc.mode, tone(root), tc.tone)
		}
		if name(root) == "" {
			t.Fatalf("%s missing accessible name", tc.mode)
		}
	}
}

func TestBarTreeHidesWhenIdleAndConfigured(t *testing.T) {
	t.Parallel()
	root := BarTree(Snapshot{Mode: Idle}, Config{HideInactive: true})
	if err := v1.Validate(root, v1.ViewBar); err != nil {
		t.Fatal(err)
	}
	if flatten(root) != "" {
		t.Fatalf("hidden idle bar = %q", flatten(root))
	}
	shown := BarTree(Snapshot{Mode: Recording}, Config{HideInactive: true})
	if !strings.Contains(flatten(shown), "Recording") {
		t.Fatalf("hide_inactive hid an active recorder: %q", flatten(shown))
	}
}

func TestTooltipTreeIncludesFailureLog(t *testing.T) {
	t.Parallel()
	root := TooltipTree(Snapshot{Mode: Failed, Err: "zero-byte artifact", Logs: "gpu: encoder failed"})
	if err := v1.Validate(root, v1.ViewTooltip); err != nil {
		t.Fatal(err)
	}
	body := flatten(root)
	if !strings.Contains(body, "failed") || !strings.Contains(body, "encoder failed") {
		t.Fatalf("tooltip = %q", body)
	}
}

func TestHandleInputButtons(t *testing.T) {
	t.Parallel()
	record, replay, save := HandleInput(&v1.InputEvent{Node: nodeRecord, Event: v1.EventActivate})
	if !record || replay || save {
		t.Fatalf("activate = %v %v %v", record, replay, save)
	}
	record, replay, save = HandleInput(&v1.InputEvent{Node: nodeRecord, Event: v1.EventPointer, Button: v1.ButtonSecondary})
	if record || !replay || save {
		t.Fatalf("secondary = %v %v %v", record, replay, save)
	}
	record, replay, save = HandleInput(&v1.InputEvent{Node: nodeRecord, Event: v1.EventPointer, Button: v1.ButtonMiddle})
	if record || replay || !save {
		t.Fatalf("middle = %v %v %v", record, replay, save)
	}
}

func flatten(n *v1.Node) string {
	if n == nil {
		return ""
	}
	var b strings.Builder
	b.WriteString(n.Text)
	for _, c := range n.Children {
		b.WriteString(flatten(c))
	}
	return b.String()
}

func tone(n *v1.Node) v1.Tone {
	if n == nil {
		return ""
	}
	if n.Tone != "" {
		return n.Tone
	}
	for _, c := range n.Children {
		if t := tone(c); t != "" {
			return t
		}
	}
	return v1.ToneNormal
}

func name(n *v1.Node) string {
	if n == nil {
		return ""
	}
	if n.Name != "" {
		return n.Name
	}
	for _, c := range n.Children {
		if s := name(c); s != "" {
			return s
		}
	}
	return ""
}
