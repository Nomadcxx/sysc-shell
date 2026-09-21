package v1

import (
	"bytes"
	"encoding/json"
	"math"
	"strings"
	"testing"
)

// timerPanel is the richest tree Milestone 6A has to carry: the Timer panel,
// with a themed column, a live countdown, a duration field, and three buttons.
func timerPanel() *Node {
	return &Node{
		Kind:    KindColumn,
		Padding: 12,
		Gap:     8,
		Children: []*Node{
			{Kind: KindText, Text: "05:00", Tabular: true},
			{Kind: KindProgress, Value: 0.4},
			{
				Kind: KindTextInput, ID: "duration", Key: "duration",
				Text: "5m", Name: "Duration", Role: "textbox",
				Events: []EventKind{EventChange, EventSubmit},
			},
			{
				Kind: KindRow, Gap: 8,
				Children: []*Node{
					{Kind: KindButton, ID: "start", Text: "Start", Name: "Start", Role: "button", Events: []EventKind{EventActivate}},
					{Kind: KindButton, ID: "pause", Text: "Pause", Name: "Pause", Role: "button", Events: []EventKind{EventActivate}},
					{Kind: KindButton, ID: "reset", Text: "Reset", Name: "Reset", Role: "button", Events: []EventKind{EventActivate}},
				},
			},
		},
	}
}

// chain returns a single-child tree exactly depth levels deep, root included.
func chain(depth int) *Node {
	root := &Node{Kind: KindColumn}
	n := root
	for i := 1; i < depth; i++ {
		child := &Node{Kind: KindColumn}
		n.Children = []*Node{child}
		n = child
	}
	n.Kind = KindText
	n.Text = "leaf"
	return root
}

// wide returns a column holding count text children, so the whole tree is
// count+1 nodes.
func wide(count int) *Node {
	root := &Node{Kind: KindColumn, Children: make([]*Node, count)}
	for i := range root.Children {
		root.Children[i] = &Node{Kind: KindText, Text: "x"}
	}
	return root
}

func TestValidateAcceptsTheTimerPanel(t *testing.T) {
	t.Parallel()

	if err := Validate(timerPanel(), ViewPanel); err != nil {
		t.Fatalf("Validate: %v", err)
	}
}

func TestValidateRejectsANilRoot(t *testing.T) {
	t.Parallel()

	if err := Validate(nil, ViewPanel); err == nil {
		t.Fatal("Validate accepted a nil root")
	}
}

func TestValidateRejectsAnUnknownKind(t *testing.T) {
	t.Parallel()

	err := Validate(&Node{Kind: "canvas"}, ViewPanel)
	if err == nil {
		t.Fatal("Validate accepted an unknown kind")
	}
	if !strings.Contains(err.Error(), "canvas") {
		t.Fatalf("err = %v, want it to name the kind", err)
	}
}

func TestValidateRejectsDuplicateNodeIDs(t *testing.T) {
	t.Parallel()

	root := &Node{Kind: KindRow, Children: []*Node{
		{Kind: KindButton, ID: "go", Text: "A", Name: "A", Role: "button", Events: []EventKind{EventActivate}},
		{Kind: KindButton, ID: "go", Text: "B", Name: "B", Role: "button", Events: []EventKind{EventActivate}},
	}}
	err := Validate(root, ViewPanel)
	if err == nil {
		t.Fatal("Validate accepted a duplicate node ID")
	}
	if !strings.Contains(err.Error(), "go") {
		t.Fatalf("err = %v, want it to name the duplicated ID", err)
	}
}

func TestValidateRejectsDuplicateKeys(t *testing.T) {
	t.Parallel()

	// Keys carry retained editor identity across revisions; two nodes claiming
	// one key would make the retained buffer ambiguous.
	root := &Node{Kind: KindColumn, Children: []*Node{
		{Kind: KindText, Key: "row", Text: "A"},
		{Kind: KindText, Key: "row", Text: "B"},
	}}
	if err := Validate(root, ViewPanel); err == nil {
		t.Fatal("Validate accepted a duplicate key")
	}
}

func TestValidateBoundsTreeDepth(t *testing.T) {
	t.Parallel()

	if err := Validate(chain(MaxDepth), ViewPanel); err != nil {
		t.Fatalf("depth %d rejected: %v", MaxDepth, err)
	}
	if err := Validate(chain(MaxDepth+1), ViewPanel); err == nil {
		t.Fatalf("depth %d accepted, want rejection", MaxDepth+1)
	}
}

// sized returns a tree of exactly count nodes that stays inside the per-node
// child limit, so a node-count test cannot be satisfied by the child check.
func sized(count int) *Node {
	root := &Node{Kind: KindColumn}
	remaining := count - 1
	for remaining > 0 {
		group := &Node{Kind: KindColumn}
		root.Children = append(root.Children, group)
		remaining--
		for n := 0; n < MaxChildren && remaining > 0; n++ {
			group.Children = append(group.Children, &Node{Kind: KindText, Text: "x"})
			remaining--
		}
	}
	return root
}

func TestValidateBoundsNodeCount(t *testing.T) {
	t.Parallel()

	if err := Validate(sized(MaxNodes), ViewPanel); err != nil {
		t.Fatalf("%d nodes rejected: %v", MaxNodes, err)
	}
	if err := Validate(sized(MaxNodes+1), ViewPanel); err == nil {
		t.Fatalf("%d nodes accepted, want rejection", MaxNodes+1)
	}
}

func TestValidateBoundsChildrenPerNode(t *testing.T) {
	t.Parallel()

	if err := Validate(wide(MaxChildren), ViewPanel); err != nil {
		t.Fatalf("%d children rejected: %v", MaxChildren, err)
	}
	if err := Validate(wide(MaxChildren+1), ViewPanel); err == nil {
		t.Fatalf("%d children accepted, want rejection", MaxChildren+1)
	}
}

func TestValidateBoundsTextPerNode(t *testing.T) {
	t.Parallel()

	at := &Node{Kind: KindText, Text: strings.Repeat("a", MaxTextBytes)}
	if err := Validate(at, ViewPanel); err != nil {
		t.Fatalf("text at the limit rejected: %v", err)
	}
	over := &Node{Kind: KindText, Text: strings.Repeat("a", MaxTextBytes+1)}
	if err := Validate(over, ViewPanel); err == nil {
		t.Fatal("text over the limit accepted")
	}
}

func TestValidateRequiresAccessibleIdentityOnInteractiveNodes(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		node *Node
	}{
		{"no id", &Node{Kind: KindButton, Text: "Go", Name: "Go", Role: "button", Events: []EventKind{EventActivate}}},
		{"no name", &Node{Kind: KindButton, ID: "go", Text: "Go", Role: "button", Events: []EventKind{EventActivate}}},
		{"no role", &Node{Kind: KindButton, ID: "go", Text: "Go", Name: "Go", Events: []EventKind{EventActivate}}},
		{"no events", &Node{Kind: KindButton, ID: "go", Text: "Go", Name: "Go", Role: "button"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			if err := Validate(c.node, ViewPanel); err == nil {
				t.Fatalf("Validate accepted an interactive node with %s", c.name)
			}
		})
	}
}

func TestValidateRejectsEventsOnNodesThatCannotEmitThem(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		node *Node
	}{
		{"text emits activate", &Node{Kind: KindText, Text: "x", Events: []EventKind{EventActivate}}},
		{"button emits change", &Node{Kind: KindButton, ID: "b", Text: "x", Name: "x", Role: "button", Events: []EventKind{EventChange}}},
		{"input emits pointer", &Node{Kind: KindTextInput, ID: "i", Name: "i", Role: "textbox", Events: []EventKind{EventPointer}}},
		{"unknown event", &Node{Kind: KindButton, ID: "b", Text: "x", Name: "x", Role: "button", Events: []EventKind{"levitate"}}},
		{"duplicate event", &Node{Kind: KindButton, ID: "b", Text: "x", Name: "x", Role: "button", Events: []EventKind{EventActivate, EventActivate}}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			if err := Validate(c.node, ViewPanel); err == nil {
				t.Fatalf("Validate accepted %s", c.name)
			}
		})
	}
}

func TestValidateRejectsChildrenOnLeafKinds(t *testing.T) {
	t.Parallel()

	// Button takes children since minor five; the rest stay leaves.
	for _, kind := range []NodeKind{KindText, KindIcon, KindProgress, KindTextInput} {
		t.Run(string(kind), func(t *testing.T) {
			t.Parallel()
			n := &Node{Kind: kind, ID: "x", Name: "x", Role: "r", Icon: "clear-day", Text: "x",
				Events: []EventKind{EventActivate}, Children: []*Node{{Kind: KindText, Text: "child"}}}
			if kind == KindTextInput {
				n.Events = []EventKind{EventChange}
			}
			if err := Validate(n, ViewPanel); err == nil {
				t.Fatalf("Validate accepted children on %s", kind)
			}
		})
	}
}

func TestValidateBoundsProgressValue(t *testing.T) {
	t.Parallel()

	if err := Validate(&Node{Kind: KindProgress, Value: 1}, ViewPanel); err != nil {
		t.Fatalf("progress 1 rejected: %v", err)
	}
	for _, v := range []float64{-0.1, 1.1} {
		if err := Validate(&Node{Kind: KindProgress, Value: v}, ViewPanel); err == nil {
			t.Fatalf("progress %v accepted", v)
		}
	}
}

func TestValidateRejectsNonFiniteProgress(t *testing.T) {
	t.Parallel()

	// JSON has no NaN literal, but a malformed float can still reach layout
	// through arithmetic on the host side; reject it at the boundary.
	nan := &Node{Kind: KindProgress, Value: math.NaN()}
	if err := Validate(nan, ViewPanel); err == nil {
		t.Fatal("Validate accepted a non-finite progress value")
	}
}

func TestValidateBoundsGaugeValue(t *testing.T) {
	t.Parallel()

	if err := Validate(&Node{Kind: KindGauge, Value: 1, ValueText: "12:34"}, ViewPanel); err != nil {
		t.Fatalf("gauge 1 rejected: %v", err)
	}
	for _, v := range []float64{-0.1, 1.1} {
		if err := Validate(&Node{Kind: KindGauge, Value: v}, ViewPanel); err == nil {
			t.Fatalf("gauge %v accepted", v)
		}
	}
	nan := &Node{Kind: KindGauge, Value: math.NaN()}
	if err := Validate(nan, ViewPanel); err == nil {
		t.Fatal("Validate accepted a non-finite gauge value")
	}
}

func TestValidateRequiresAnIconName(t *testing.T) {
	t.Parallel()

	if err := Validate(&Node{Kind: KindIcon}, ViewPanel); err == nil {
		t.Fatal("Validate accepted an icon with no name")
	}
	if err := Validate(&Node{Kind: KindIcon, Icon: "../../etc/passwd"}, ViewPanel); err == nil {
		t.Fatal("Validate accepted an icon name that is not an identifier")
	}
	if err := Validate(&Node{Kind: KindIcon, Icon: "clear-day"}, ViewPanel); err != nil {
		t.Fatalf("Validate rejected a plain icon name: %v", err)
	}
}

func TestValidateButtonIcon(t *testing.T) {
	t.Parallel()

	ok := &Node{Kind: KindButton, ID: "camera", Icon: "camera",
		Name: "Open screen recorder", Role: "button", Events: []EventKind{EventActivate}}
	if err := Validate(ok, ViewBar); err != nil {
		t.Fatalf("Validate rejected a button with a catalogue icon: %v", err)
	}
	if err := Validate(&Node{Kind: KindButton, ID: "b", Icon: "../../etc/passwd",
		Name: "x", Role: "button", Events: []EventKind{EventActivate}}, ViewPanel); err == nil {
		t.Fatal("Validate accepted an icon name that is not an identifier")
	}
}

func TestValidateRejectsNegativeGeometry(t *testing.T) {
	t.Parallel()

	for _, n := range []*Node{
		{Kind: KindText, Text: "x", Width: -1},
		{Kind: KindText, Text: "x", MaxWidth: -1},
		{Kind: KindRow, Padding: -1},
		{Kind: KindRow, Gap: -1},
	} {
		if err := Validate(n, ViewPanel); err == nil {
			t.Fatalf("Validate accepted negative geometry on %+v", n)
		}
	}
}

func TestValidateBoundsGeometry(t *testing.T) {
	t.Parallel()

	if err := Validate(&Node{Kind: KindText, Text: "x", Width: MaxExtent + 1}, ViewPanel); err == nil {
		t.Fatal("Validate accepted a width past the extent ceiling")
	}
}

func TestBarViewsRejectKeyboardFields(t *testing.T) {
	t.Parallel()

	// A bar view has no keyboard focus of its own, so a field placed there
	// could never be typed into.
	root := &Node{Kind: KindRow, Children: []*Node{
		{Kind: KindTextInput, ID: "i", Name: "i", Role: "textbox", Events: []EventKind{EventChange}},
	}}
	if err := Validate(root, ViewBar); err == nil {
		t.Fatal("bar view accepted a text input")
	}
	if err := Validate(root, ViewPanel); err != nil {
		t.Fatalf("panel view rejected a text input: %v", err)
	}
}

func TestBarViewsAcceptPointerControls(t *testing.T) {
	t.Parallel()

	root := &Node{Kind: KindRow, Children: []*Node{
		{Kind: KindText, Text: "05:00", Tabular: true},
		{Kind: KindButton, ID: "open", Text: "Timer", Name: "Open timer", Role: "button",
			Events: []EventKind{EventActivate, EventPointer}},
	}}
	if err := Validate(root, ViewBar); err != nil {
		t.Fatalf("bar view rejected a pointer control: %v", err)
	}
}

func TestTooltipViewsAreReadOnly(t *testing.T) {
	t.Parallel()

	for _, n := range []*Node{
		{Kind: KindButton, ID: "b", Text: "x", Name: "x", Role: "button", Events: []EventKind{EventActivate}},
		{Kind: KindTextInput, ID: "i", Name: "i", Role: "textbox", Events: []EventKind{EventChange}},
	} {
		root := &Node{Kind: KindColumn, Children: []*Node{n}}
		if err := Validate(root, ViewTooltip); err == nil {
			t.Fatalf("tooltip accepted an interactive %s", n.Kind)
		}
	}

	ok := &Node{Kind: KindColumn, Children: []*Node{
		{Kind: KindText, Text: "Timer"},
		{Kind: KindText, Text: "04:12 remaining", Tabular: true},
		{Kind: KindRow, Children: []*Node{
			{Kind: KindText, Text: "Humidity"},
			{Kind: KindText, Text: "40%", Tabular: true},
		}},
	}}
	if err := Validate(ok, ViewTooltip); err != nil {
		t.Fatalf("tooltip rejected read-only content: %v", err)
	}

	for _, n := range []*Node{
		{Kind: KindList, Height: 40},
		{Kind: KindDragSource, ID: "d", Name: "d", Role: "button", Events: []EventKind{EventPointer}},
		{Kind: KindDropZone, ID: "z", Accept: []string{"zone"}, Events: []EventKind{EventDrop}},
	} {
		root := &Node{Kind: KindColumn, Children: []*Node{n}}
		if err := Validate(root, ViewTooltip); err == nil {
			t.Fatalf("tooltip accepted a %s", n.Kind)
		}
	}
}

func TestValidateRejectsAnUnknownViewKind(t *testing.T) {
	t.Parallel()

	if err := Validate(&Node{Kind: KindText, Text: "x"}, ViewKind("wallpaper")); err == nil {
		t.Fatal("Validate accepted an unknown view kind")
	}
}

func TestValidateRejectsAnUnknownTone(t *testing.T) {
	t.Parallel()

	if err := Validate(&Node{Kind: KindText, Text: "x", Tone: "chartreuse"}, ViewPanel); err == nil {
		t.Fatal("Validate accepted an unknown tone")
	}
}

func TestValidateAcceptsTheSubtleAndAccentTones(t *testing.T) {
	t.Parallel()

	for _, tone := range []Tone{ToneSubtle, ToneAccent} {
		if err := Validate(&Node{Kind: KindText, Text: "x", Tone: tone}, ViewPanel); err != nil {
			t.Fatalf("Validate rejected tone %q: %v", tone, err)
		}
	}
}

func TestValidateRejectsAnOversizedNodeID(t *testing.T) {
	t.Parallel()

	long := strings.Repeat("a", MaxIdentBytes+1)
	n := &Node{Kind: KindButton, ID: long, Text: "x", Name: "x", Role: "button", Events: []EventKind{EventActivate}}
	if err := Validate(n, ViewPanel); err == nil {
		t.Fatal("Validate accepted an oversized node ID")
	}
}

func TestValidateRejectsListAndDragOutsideAPanel(t *testing.T) {
	t.Parallel()
	list := &Node{Kind: KindRow, Children: []*Node{{Kind: KindList, Height: 80, Children: []*Node{
		{Kind: KindText, Text: "x"},
	}}}}
	if err := Validate(list, ViewBar); err == nil {
		t.Fatal("a bar accepted a list")
	}
	drag := &Node{Kind: KindColumn, Children: []*Node{{
		Kind: KindDragSource, ID: "h", Text: "=", Name: "Reorder", Role: "button",
		Events: []EventKind{EventPointer},
	}}}
	if err := Validate(drag, ViewBar); err == nil {
		t.Fatal("a bar accepted a drag handle")
	}
	unnamed := &Node{Kind: KindColumn, Children: []*Node{{
		Kind: KindDragSource, ID: "h", Text: "=", Role: "button",
		Events: []EventKind{EventPointer},
	}}}
	if err := Validate(unnamed, ViewPanel); err == nil {
		t.Fatal("a nameless drag handle was accepted")
	}
}

func TestValidateKeepsEditorFlagsOnTextInput(t *testing.T) {
	t.Parallel()
	ok := &Node{Kind: KindColumn, Children: []*Node{{
		Kind: KindTextInput, ID: "n", Key: "note", Name: "Note", Role: "textbox",
		Multiline: true, SubmitOnEnter: true, Reseed: 2,
		Events: []EventKind{EventChange},
	}}}
	if err := Validate(ok, ViewPanel); err != nil {
		t.Fatalf("text input rejected editor flags: %v", err)
	}
	bad := &Node{Kind: KindColumn, Children: []*Node{{
		Kind: KindButton, ID: "b", Name: "Go", Role: "button",
		Multiline: true, Events: []EventKind{EventActivate},
	}}}
	if err := Validate(bad, ViewPanel); err == nil {
		t.Fatal("a button accepted multiline")
	}
}

func TestInputEventJSONOmitsIME(t *testing.T) {
	t.Parallel()
	raw, err := json.Marshal(&InputEvent{Node: "n", Event: EventChange, Text: "typed"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "preedit") {
		t.Fatalf("IME leaked onto the wire: %s", raw)
	}
}

func TestValidateAcceptsMinorTwoFields(t *testing.T) {
	t.Parallel()

	root := &Node{Kind: KindColumn, Fill: "card", Radius: 12, Children: []*Node{
		{Kind: KindText, Text: "title", Size: "title", Bold: true, CenterX: true},
		{Kind: KindButton, ID: "go", Text: "Go", Name: "Go", Role: "button",
			Fill: "accent", Disabled: true, Events: []EventKind{EventActivate}},
	}}
	if err := Validate(root, ViewPanel); err != nil {
		t.Fatalf("minor-2 fields rejected: %v", err)
	}
}

func TestValidateRejectsBadMinorTwoValues(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		node *Node
	}{
		{"unknown fill", &Node{Kind: KindColumn, Fill: "neon"}},
		{"fill on text", &Node{Kind: KindText, Text: "x", Fill: "card"}},
		{"unknown size", &Node{Kind: KindText, Text: "x", Size: "giant"}},
		{"size on button", &Node{Kind: KindButton, ID: "b", Text: "x", Name: "b", Role: "button", Size: "title", Events: []EventKind{EventActivate}}},
		{"radius negative", &Node{Kind: KindColumn, Radius: -1}},
		{"radius over limit", &Node{Kind: KindColumn, Radius: 257}},
		{"disabled on text", &Node{Kind: KindText, Text: "x", Disabled: true}},
	}
	for _, tc := range cases {
		if err := Validate(tc.node, ViewPanel); err == nil {
			t.Errorf("%s: Validate accepted", tc.name)
		}
	}
}

func TestMinorTwoFieldsRoundTripOnTheWire(t *testing.T) {
	t.Parallel()

	n := &Node{Kind: KindColumn, Fill: "card", Radius: 12, Children: []*Node{
		{Kind: KindText, Text: "t", Size: "title", Bold: true, CenterX: true},
		{Kind: KindButton, ID: "go", Text: "Go", Name: "Go", Role: "button",
			Fill: "accent", Disabled: true, Events: []EventKind{EventActivate}},
	}}
	raw, err := json.Marshal(n)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{`"fill":"card"`, `"radius":12`, `"bold":true`, `"size":"title"`, `"center_x":true`, `"disabled":true`} {
		if !bytes.Contains(raw, []byte(name)) {
			t.Errorf("wire missing %s: %s", name, raw)
		}
	}
	// Zero values must be omitted: a bare node carries none of the new names.
	bare, err := json.Marshal(&Node{Kind: KindText, Text: "x"})
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{`"fill"`, `"radius"`, `"bold"`, `"size"`, `"disabled"`, `"center_x"`, `"pin_end"`} {
		if bytes.Contains(bare, []byte(name)) {
			t.Errorf("zero value leaked %s: %s", name, bare)
		}
	}
	var back Node
	if err := json.Unmarshal(raw, &back); err != nil {
		t.Fatal(err)
	}
	if back.Fill != "card" || back.Radius != 12 {
		t.Fatalf("round trip = %+v", back)
	}
	// Bold, Size, and CenterX ride on the text child; Fill and Disabled on
	// the button child. The root carries only what it declared.
	if !back.Children[0].Bold || back.Children[0].Size != "title" || !back.Children[0].CenterX ||
		back.Children[1].Fill != "accent" || !back.Children[1].Disabled {
		t.Fatalf("round trip children = %+v %+v", back.Children[0], back.Children[1])
	}
}

func TestValidateAcceptsEveryFillAndSizeName(t *testing.T) {
	t.Parallel()

	for fill := range knownFills {
		if err := Validate(&Node{Kind: KindColumn, Fill: fill}, ViewPanel); err != nil {
			t.Errorf("fill %q rejected: %v", fill, err)
		}
	}
	for size := range knownSizes {
		if err := Validate(&Node{Kind: KindText, Text: "x", Size: size}, ViewPanel); err != nil {
			t.Errorf("size %q rejected: %v", size, err)
		}
	}
	// The boundary radius is legal; one past it is not.
	if err := Validate(&Node{Kind: KindColumn, Radius: MaxRadius}, ViewPanel); err != nil {
		t.Errorf("MaxRadius rejected: %v", err)
	}
	if err := Validate(&Node{Kind: KindColumn, Radius: MaxRadius + 1}, ViewPanel); err == nil {
		t.Error("MaxRadius + 1 accepted")
	}
	// DragSource is interactive and may be disabled like buttons and inputs.
	// It still needs the accessible identity every interactive node carries.
	if err := Validate(&Node{Kind: KindDragSource, ID: "d", Name: "d", Role: "button", DragType: "zone", Disabled: true, Events: []EventKind{EventPointer}}, ViewPanel); err != nil {
		t.Errorf("disabled drag source rejected: %v", err)
	}
}

func TestValidateAcceptsMinorFour(t *testing.T) {
	t.Parallel()

	root := &Node{Kind: KindColumn, Children: []*Node{
		{Kind: KindText, Text: "history", Tooltip: "30 snapshots"},
		{Kind: KindGraph, Values: []float64{0.1, 0.4, 0.9}, Height: 40},
		{Kind: KindGauge, Value: 0.5, ValueText: "50", Absent: true},
		{Kind: KindSeparator},
		{Kind: KindRow, Shape: "card", Children: []*Node{
			{Kind: KindColumn, Shape: "circle", Fill: "accent", Children: []*Node{
				{Kind: KindText, Text: "C"},
			}},
		}},
	}}
	if err := Validate(root, ViewPanel); err != nil {
		t.Fatalf("minor-4 tree rejected: %v", err)
	}
	// A graph is legal where a meter is: the bar strip carries sparklines.
	if err := Validate(&Node{Kind: KindGraph, Values: []float64{0.2, 0.8}}, ViewBar); err != nil {
		t.Fatalf("graph in a bar view rejected: %v", err)
	}
	// Minor seven: a meter may be absent — the elapsed strip with unknown
	// window bounds reserves its slot and paints nothing.
	if err := Validate(&Node{Kind: KindProgress, Value: 0, Absent: true, Height: 3}, ViewBar); err != nil {
		t.Fatalf("absent meter rejected: %v", err)
	}
}

func TestValidateRejectsBadMinorFour(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		node *Node
	}{
		{"unknown shape", &Node{Kind: KindRow, Shape: "neon"}},
		{"shape on text", &Node{Kind: KindText, Text: "x", Shape: "card"}},
		{"tooltip over cap", &Node{Kind: KindText, Text: "x", Tooltip: strings.Repeat("a", 257)}},
		{"graph one sample", &Node{Kind: KindGraph, Values: []float64{0.5}}},
		{"graph over cap", &Node{Kind: KindGraph, Values: make([]float64, 65)}},
		{"graph nan", &Node{Kind: KindGraph, Values: []float64{math.NaN()}}},
		{"graph out of range", &Node{Kind: KindGraph, Values: []float64{1.5}}},
		{"values on text", &Node{Kind: KindText, Text: "x", Values: []float64{0.5}}},
		{"separator with children", &Node{Kind: KindSeparator, Children: []*Node{{Kind: KindText, Text: "x"}}}},
		{"separator in a bar view", &Node{Kind: KindSeparator}},
		{"separator declares events", &Node{Kind: KindSeparator, Events: []EventKind{EventActivate}}},
		{"absent on text", &Node{Kind: KindText, Text: "x", Absent: true}},
	}
	views := map[string]ViewKind{"separator in a bar view": ViewBar, "separator declares events": ViewPanel}
	for _, tc := range cases {
		view := ViewPanel
		if v, ok := views[tc.name]; ok {
			view = v
		}
		if err := Validate(tc.node, view); err == nil {
			t.Errorf("%s: Validate accepted", tc.name)
		}
	}
}

func TestValidateAcceptsMinorFiveFields(t *testing.T) {
	t.Parallel()

	root := &Node{Kind: KindColumn, Stroke: 1, StrokeFill: "outline", Children: []*Node{
		{Kind: KindImage, Path: "/home/x/Pictures/a.png", ImageSize: 96},
		{Kind: KindImage, Path: "/home/x/Pictures/b.png", ImageW: 320, ImageH: 180,
			Background: true, Shape: "card", Radius: 12},
		{Kind: KindButton, ID: "send", Name: "Send", Role: "button", Stroke: 1,
			Events: []EventKind{EventActivate}, Children: []*Node{
				{Kind: KindText, Text: "Send"},
				{Kind: KindIcon, Icon: "send"},
			}},
	}}
	if err := Validate(root, ViewPanel); err != nil {
		t.Fatalf("minor-5 fields rejected: %v", err)
	}
}

func TestValidateRejectsBadMinorFiveValues(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		node *Node
		view ViewKind
	}{
		{"image in a bar", &Node{Kind: KindImage, Path: "/a.png", ImageSize: 96}, ViewBar},
		{"image in a tooltip", &Node{Kind: KindImage, Path: "/a.png", ImageSize: 96}, ViewTooltip},
		{"image without path", &Node{Kind: KindImage, ImageSize: 96}, ViewPanel},
		{"image relative path", &Node{Kind: KindImage, Path: "Pictures/a.png", ImageSize: 96}, ViewPanel},
		{"image path over cap", &Node{Kind: KindImage, Path: "/" + strings.Repeat("a", MaxPathBytes), ImageSize: 96}, ViewPanel},
		{"image without a box", &Node{Kind: KindImage, Path: "/a.png"}, ViewPanel},
		{"image both box forms", &Node{Kind: KindImage, Path: "/a.png", ImageSize: 96, ImageW: 320, ImageH: 180}, ViewPanel},
		{"image half a box", &Node{Kind: KindImage, Path: "/a.png", ImageW: 320}, ViewPanel},
		{"image size over cap", &Node{Kind: KindImage, Path: "/a.png", ImageSize: MaxExtent + 1}, ViewPanel},
		{"image box over cap", &Node{Kind: KindImage, Path: "/a.png", ImageW: MaxExtent + 1, ImageH: 180}, ViewPanel},
		{"image background without a box", &Node{Kind: KindImage, Path: "/a.png", ImageSize: 96, Background: true}, ViewPanel},
		{"image with children", &Node{Kind: KindImage, Path: "/a.png", ImageSize: 96, Children: []*Node{{Kind: KindText, Text: "x"}}}, ViewPanel},
		{"image fields on text", &Node{Kind: KindText, Text: "x", Path: "/a.png"}, ViewPanel},
		{"image size on text", &Node{Kind: KindText, Text: "x", ImageSize: 96}, ViewPanel},
		{"background on text", &Node{Kind: KindText, Text: "x", Background: true}, ViewPanel},
		{"stroke on text", &Node{Kind: KindText, Text: "x", Stroke: 1}, ViewPanel},
		{"stroke on image", &Node{Kind: KindImage, Path: "/a.png", ImageSize: 96, Stroke: 1}, ViewPanel},
		{"stroke negative", &Node{Kind: KindColumn, Stroke: -1}, ViewPanel},
		{"stroke over cap", &Node{Kind: KindColumn, Stroke: MaxStroke + 1}, ViewPanel},
		{"unknown stroke fill", &Node{Kind: KindColumn, Stroke: 1, StrokeFill: "neon"}, ViewPanel},
		{"button with interactive child", &Node{Kind: KindButton, ID: "b", Name: "b", Role: "button",
			Events: []EventKind{EventActivate}, Children: []*Node{{
				Kind: KindButton, ID: "inner", Name: "inner", Role: "button", Events: []EventKind{EventActivate},
			}}}, ViewPanel},
	}
	for _, tc := range cases {
		if err := Validate(tc.node, tc.view); err == nil {
			t.Errorf("%s: Validate accepted", tc.name)
		}
	}
}

func TestMinorFiveFieldsRoundTripOnTheWire(t *testing.T) {
	t.Parallel()

	n := &Node{Kind: KindImage, Path: "/home/x/Pictures/a.png", ImageW: 320, ImageH: 180,
		Background: true, Shape: "card"}
	raw, err := json.Marshal(n)
	if err != nil {
		t.Fatal(err)
	}
	var back Node
	if err := json.Unmarshal(raw, &back); err != nil {
		t.Fatal(err)
	}
	if back.Path != n.Path || back.ImageW != n.ImageW || back.ImageH != n.ImageH ||
		back.Background != n.Background || back.Shape != n.Shape {
		t.Fatalf("image fields did not round-trip: %s", raw)
	}
	if !strings.Contains(string(raw), `"image_w":320`) || !strings.Contains(string(raw), `"background":true`) {
		t.Fatalf("wire names drifted: %s", raw)
	}
}

func TestValidateAcceptsMinorSixFields(t *testing.T) {
	t.Parallel()

	root := &Node{Kind: KindColumn, Children: []*Node{
		{Kind: KindProgress, Key: "battery", Value: 0.4, Animate: true},
		{Kind: KindGauge, Key: "cpu", Value: 0.7, Animate: true, ValueText: "70%"},
		{Kind: KindProgress, Key: "static", Value: 0.9},
	}}
	if err := Validate(root, ViewPanel); err != nil {
		t.Fatalf("minor-6 fields rejected: %v", err)
	}
}

func TestValidateRejectsBadMinorSixValues(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		node *Node
	}{
		{"animate on a row", &Node{Kind: KindRow, Key: "r", Animate: true}},
		{"animate on a button", &Node{Kind: KindButton, ID: "b", Key: "b", Animate: true,
			Name: "B", Role: "button", Events: []EventKind{EventActivate}}},
		{"animate on a gauge without a key", &Node{Kind: KindGauge, Value: 0.5, Animate: true}},
		{"animate on a progress without a key", &Node{Kind: KindProgress, Value: 0.5, Animate: true}},
	}
	for _, tc := range cases {
		root := &Node{Kind: KindColumn, Children: []*Node{tc.node}}
		if err := Validate(root, ViewPanel); err == nil {
			t.Errorf("%s: accepted, want rejection", tc.name)
		}
	}
}

func TestMinorSixFieldsRoundTripOnTheWire(t *testing.T) {
	t.Parallel()

	sent := &Node{Kind: KindProgress, Key: "battery", Value: 0.4, Animate: true}
	b, err := json.Marshal(sent)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got Node
	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	if err := d.Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Animate != true || got.Key != "battery" || got.Value != 0.4 {
		t.Fatalf("round trip lost the animate fields: %+v", got)
	}
}
