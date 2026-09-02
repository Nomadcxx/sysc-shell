package plugin

import (
	"strings"
	"testing"

	"github.com/Nomadcxx/sysc-shell/internal/render"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
	v1 "github.com/Nomadcxx/sysc-shell/plugin/v1"
)

// timerBar is the Timer widget as it appears on the bar.
func timerBar() *v1.Node {
	return &v1.Node{Kind: v1.KindRow, Gap: 6, Children: []*v1.Node{
		{Kind: v1.KindText, Text: "04:12", Tabular: true, MaxWidth: 80},
		{Kind: v1.KindButton, ID: "open", Text: "Timer", Name: "Open the timer", Role: "button",
			Events: []v1.EventKind{v1.EventActivate}},
	}}
}

func TestConvertBuildsAShellOwnedTree(t *testing.T) {
	t.Parallel()

	got, err := Convert(timerBar(), v1.ViewBar)
	if err != nil {
		t.Fatalf("Convert: %v", err)
	}
	if got.Kind != ui.KindRow || got.Gap != 6 {
		t.Fatalf("root = %+v", got)
	}
	if len(got.Children) != 2 {
		t.Fatalf("children = %d, want 2", len(got.Children))
	}

	text := got.Children[0]
	if text.Kind != ui.KindText || text.Text != "04:12" || !text.Tabular || text.MaxWidth != 80 {
		t.Errorf("text = %+v", text)
	}
	button := got.Children[1]
	if button.Kind != ui.KindButton || button.Text != "Timer" {
		t.Errorf("button = %+v", button)
	}
	// The accessible identity the plugin declared has to survive, because the
	// host has no other source for it.
	if button.Name != "Open the timer" || button.Role != "button" || !button.Focusable {
		t.Errorf("button identity = %+v", button)
	}
	// The node id becomes the action, which is how an event finds its way back
	// to the node the plugin addressed.
	if button.Action != "open" {
		t.Errorf("action = %q, want the node id", button.Action)
	}
	// Nothing on the wire can supply arranged bounds.
	if button.Bounds != (ui.Rect{}) {
		t.Errorf("bounds = %+v, want them unset until layout runs", button.Bounds)
	}
}

func TestConvertMapsEveryVersionOneKind(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		node *v1.Node
		want ui.Kind
	}{
		{"row", &v1.Node{Kind: v1.KindRow}, ui.KindRow},
		{"column", &v1.Node{Kind: v1.KindColumn}, ui.KindColumn},
		{"text", &v1.Node{Kind: v1.KindText, Text: "x"}, ui.KindText},
		{"icon", &v1.Node{Kind: v1.KindIcon, Icon: "rain"}, ui.KindText},
		{"progress", &v1.Node{Kind: v1.KindProgress, Value: 0.5}, ui.KindMeter},
		{"button", &v1.Node{Kind: v1.KindButton, ID: "b", Text: "x", Name: "x", Role: "button",
			Events: []v1.EventKind{v1.EventActivate}}, ui.KindButton},
		{"text input", &v1.Node{Kind: v1.KindTextInput, ID: "i", Name: "i", Role: "textbox",
			Events: []v1.EventKind{v1.EventChange}}, ui.KindTextField},
		{"list", &v1.Node{Kind: v1.KindList, Height: 80}, ui.KindScroll},
		{"drag", &v1.Node{Kind: v1.KindDragSource, ID: "d", Name: "Reorder", Role: "button",
			Events: []v1.EventKind{v1.EventPointer}}, ui.KindDragSource},
		{"drop", &v1.Node{Kind: v1.KindDropZone, Accept: []string{"zone"}}, ui.KindDropZone},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			root := &v1.Node{Kind: v1.KindColumn, Children: []*v1.Node{c.node}}
			got, err := Convert(root, v1.ViewPanel)
			if err != nil {
				t.Fatalf("Convert: %v", err)
			}
			if got.Children[0].Kind != c.want {
				t.Fatalf("kind = %v, want %v", got.Children[0].Kind, c.want)
			}
		})
	}
}

func TestConvertResolvesAnIconToItsGlyph(t *testing.T) {
	t.Parallel()

	root := &v1.Node{Kind: v1.KindColumn, Children: []*v1.Node{{Kind: v1.KindIcon, Icon: "thunderstorm"}}}
	got, err := Convert(root, v1.ViewPanel)
	if err != nil {
		t.Fatalf("Convert: %v", err)
	}
	want, ok := render.IconByName("thunderstorm")
	if !ok {
		t.Fatal("the catalogue lost thunderstorm")
	}
	if got.Children[0].Text != string(want) {
		t.Errorf("icon text = %q, want the catalogue glyph", got.Children[0].Text)
	}
}

func TestConvertMapsAWeatherIconNameToTheWMOGlyph(t *testing.T) {
	t.Parallel()

	// A plugin names the symbol; the host maps it to the same glyph the bar
	// weather widget already paints for that WMO code.
	name := render.IconName(61)
	root := &v1.Node{Kind: v1.KindColumn, Children: []*v1.Node{{Kind: v1.KindIcon, Icon: name}}}
	got, err := Convert(root, v1.ViewPanel)
	if err != nil {
		t.Fatalf("Convert: %v", err)
	}
	if got.Children[0].Text != string(render.IconRune(61)) {
		t.Fatalf("icon = %q, want the rain glyph for WMO 61", got.Children[0].Text)
	}
}

func TestConvertRejectsAnIconTheShellDoesNotHave(t *testing.T) {
	t.Parallel()

	// A name the font has no glyph for must fail loudly, not paint a
	// missing-glyph box the user has to interpret.
	root := &v1.Node{Kind: v1.KindColumn, Children: []*v1.Node{{Kind: v1.KindIcon, Icon: "unicorn"}}}
	_, err := Convert(root, v1.ViewPanel)
	if err == nil {
		t.Fatal("Convert accepted an icon the shell cannot draw")
	}
	if !strings.Contains(err.Error(), "unicorn") {
		t.Fatalf("err = %v, want it to name the icon", err)
	}
}

func TestConvertResolvesAButtonIcon(t *testing.T) {
	t.Parallel()
	btn := &v1.Node{Kind: v1.KindButton, ID: "camera", Icon: "camera",
		Name: "Open screen recorder", Role: "button",
		Events: []v1.EventKind{v1.EventActivate}}
	if err := v1.Validate(btn, v1.ViewBar); err != nil {
		t.Fatal(err)
	}
	got, err := Convert(&v1.Node{Kind: v1.KindRow, Children: []*v1.Node{btn}}, v1.ViewBar)
	if err != nil {
		t.Fatal(err)
	}
	want, ok := render.IconByName("camera")
	if !ok {
		t.Fatal("catalogue missing camera")
	}
	button := got.Children[0]
	if button.Text != string(want) || button.Action != "camera" {
		t.Fatalf("button = %+v", button)
	}
	if button.Fill != ui.FillNone {
		t.Fatalf("camera fill = %v, want no chrome (the pill is the fill)", button.Fill)
	}
}

func TestConvertErrorToneTextButtonFillsAsAnErrorChip(t *testing.T) {
	t.Parallel()
	root := &v1.Node{Kind: v1.KindRow, Children: []*v1.Node{{
		Kind: v1.KindButton, ID: "record", Text: "Record", Name: "Record", Role: "button",
		Tone: v1.ToneError, Events: []v1.EventKind{v1.EventActivate},
	}}}
	got, err := Convert(root, v1.ViewBar)
	if err != nil {
		t.Fatal(err)
	}
	btn := got.Children[0]
	if btn.Fill != ui.FillError || btn.Padding != 4 {
		t.Fatalf("record chip = fill %v padding %d", btn.Fill, btn.Padding)
	}
}

func TestConvertMapsToneOntoTheThemeRole(t *testing.T) {
	t.Parallel()

	root := &v1.Node{Kind: v1.KindColumn, Children: []*v1.Node{
		{Kind: v1.KindText, Text: "ok"},
		{Kind: v1.KindText, Text: "broken", Tone: v1.ToneError},
	}}
	got, err := Convert(root, v1.ViewPanel)
	if err != nil {
		t.Fatalf("Convert: %v", err)
	}
	if got.Children[0].Tone != ui.ToneNormal {
		t.Errorf("default tone = %v", got.Children[0].Tone)
	}
	if got.Children[1].Tone != ui.ToneError {
		t.Errorf("error tone = %v", got.Children[1].Tone)
	}
}

func TestConvertRunsTheWireValidator(t *testing.T) {
	t.Parallel()

	// Conversion is the only path from plugin JSON into a shell tree, so it
	// cannot be reachable with a tree the validator would reject.
	root := &v1.Node{Kind: v1.KindColumn, Children: []*v1.Node{
		{Kind: v1.KindButton, ID: "b", Text: "x", Role: "button", Events: []v1.EventKind{v1.EventActivate}},
	}}
	if _, err := Convert(root, v1.ViewPanel); err == nil {
		t.Fatal("Convert accepted a tree with no accessible name")
	}
}

func TestConvertEnforcesTheRootKindEachViewCanLayOut(t *testing.T) {
	t.Parallel()

	// A bar strip is laid out as a row and a panel as a column. A root of the
	// other kind has no layout to run, so it is refused at conversion rather
	// than failing later with a layout error the plugin cannot act on.
	if _, err := Convert(&v1.Node{Kind: v1.KindColumn}, v1.ViewBar); err == nil {
		t.Error("a bar accepted a column root")
	}
	if _, err := Convert(&v1.Node{Kind: v1.KindRow}, v1.ViewPanel); err == nil {
		t.Error("a panel accepted a row root")
	}
	if _, err := Convert(&v1.Node{Kind: v1.KindRow}, v1.ViewBar); err != nil {
		t.Errorf("a bar rejected a row root: %v", err)
	}
	if _, err := Convert(&v1.Node{Kind: v1.KindColumn}, v1.ViewTooltip); err != nil {
		t.Errorf("a tooltip rejected a column root: %v", err)
	}
}

func TestConvertTooltipBuildsLabelValueRowsWithoutFocus(t *testing.T) {
	t.Parallel()
	root := &v1.Node{Kind: v1.KindColumn, Gap: 4, Padding: 8, MaxWidth: 280, Children: []*v1.Node{
		{Kind: v1.KindRow, Gap: 8, Children: []*v1.Node{
			{Kind: v1.KindText, Text: "Humidity"},
			{Kind: v1.KindText, Key: "humidity", Text: "40%", Tabular: true},
		}},
		{Kind: v1.KindText, Text: "stale", Tone: v1.ToneError},
	}}
	got, err := Convert(root, v1.ViewTooltip)
	if err != nil {
		t.Fatal(err)
	}
	if got.Kind != ui.KindColumn || got.MaxWidth != 280 || got.Padding != 8 {
		t.Fatalf("root = %+v", got)
	}
	if got.Focusable {
		t.Fatal("tooltip root is focusable")
	}
	row := got.Children[0]
	if row.Kind != ui.KindRow || len(row.Children) != 2 || row.Children[0].Text != "Humidity" || row.Children[1].Text != "40%" {
		t.Fatalf("row = %+v", row)
	}
	if row.Children[0].Focusable || row.Children[1].Focusable {
		t.Fatal("label/value row is focusable")
	}
	if got.Children[1].Tone != ui.ToneError {
		t.Fatalf("tone = %v", got.Children[1].Tone)
	}
}

func TestConvertTooltipRejectsButtonsInputsListsAndDrag(t *testing.T) {
	t.Parallel()
	for _, n := range []*v1.Node{
		{Kind: v1.KindButton, ID: "b", Text: "x", Name: "x", Role: "button", Events: []v1.EventKind{v1.EventActivate}},
		{Kind: v1.KindTextInput, ID: "i", Name: "i", Role: "textbox", Events: []v1.EventKind{v1.EventChange}},
		{Kind: v1.KindList, Height: 40},
		{Kind: v1.KindDragSource, ID: "d", Name: "d", Role: "button", Events: []v1.EventKind{v1.EventPointer}},
		{Kind: v1.KindDropZone, ID: "z", Accept: []string{"zone"}, Events: []v1.EventKind{v1.EventDrop}},
	} {
		root := &v1.Node{Kind: v1.KindColumn, Children: []*v1.Node{n}}
		if _, err := Convert(root, v1.ViewTooltip); err == nil {
			t.Fatalf("tooltip accepted %s", n.Kind)
		}
	}
}

func TestConvertCopiesEverythingItReads(t *testing.T) {
	t.Parallel()

	// The wire tree came from a decoder and stays owned by the reader
	// goroutine. If conversion aliased any of it, a later message could change
	// a tree the shell had already published.
	wire := timerBar()
	got, err := Convert(wire, v1.ViewBar)
	if err != nil {
		t.Fatalf("Convert: %v", err)
	}

	wire.Children[0].Text = "mutated"
	wire.Children[1].Name = "mutated"
	wire.Children = append(wire.Children, &v1.Node{Kind: v1.KindText, Text: "extra"})
	wire.Gap = 999

	if got.Children[0].Text != "04:12" {
		t.Errorf("text followed a later mutation: %q", got.Children[0].Text)
	}
	if got.Children[1].Name != "Open the timer" {
		t.Errorf("name followed a later mutation: %q", got.Children[1].Name)
	}
	if len(got.Children) != 2 {
		t.Errorf("children followed a later append: %d", len(got.Children))
	}
	if got.Gap != 6 {
		t.Errorf("gap followed a later mutation: %d", got.Gap)
	}
}

func TestConvertRefusesToCarryFieldsTheShellCannotOwn(t *testing.T) {
	t.Parallel()

	// A converted tree must not carry anything a plugin could use to reach
	// into shell presentation: no virtual-list item function, no scroll state,
	// no focus index.
	got, err := Convert(timerBar(), v1.ViewBar)
	if err != nil {
		t.Fatalf("Convert: %v", err)
	}
	var walk func(*ui.Node)
	walk = func(n *ui.Node) {
		if n.Item != nil {
			t.Error("a converted node carries a virtual-list item function")
		}
		if n.ScrollOffset != 0 || n.ItemCount != 0 || n.ContentH != 0 {
			t.Errorf("a converted node carries scroll state: %+v", n)
		}
		if len(n.Values) != 0 {
			t.Error("a converted node carries graph samples")
		}
		for _, c := range n.Children {
			walk(c)
		}
	}
	walk(got)
}

func TestConvertRejectsATextInputInABar(t *testing.T) {
	t.Parallel()

	root := &v1.Node{Kind: v1.KindRow, Children: []*v1.Node{
		{Kind: v1.KindTextInput, ID: "i", Name: "i", Role: "textbox", Events: []v1.EventKind{v1.EventChange}},
	}}
	if _, err := Convert(root, v1.ViewBar); err == nil {
		t.Fatal("a bar accepted a keyboard field")
	}
}

func keyedClock() *v1.Node {
	return &v1.Node{Kind: v1.KindColumn, Children: []*v1.Node{
		{Kind: v1.KindText, Key: "time", Text: "12:00", Tabular: true},
		{Kind: v1.KindText, Key: "date", Text: "Mon"},
		{Kind: v1.KindButton, ID: "add", Key: "add", Text: "Add", Name: "Add a city", Role: "button",
			Events: []v1.EventKind{v1.EventActivate}},
	}}
}

func seededTree(t *testing.T) *ViewTree {
	t.Helper()
	vt := &ViewTree{View: v1.ViewPanel}
	if err := vt.ApplySnapshot(1, keyedClock()); err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	return vt
}

func TestPatchReplacesOneKeyedSubtree(t *testing.T) {
	t.Parallel()
	vt := seededTree(t)
	resync, err := vt.ApplyPatch(&v1.ViewPatch{
		ViewID: "v1", Base: 1, Revision: 2,
		Replacements: []v1.Replacement{{Key: "time", Node: &v1.Node{Kind: v1.KindText, Key: "time", Text: "09:41", Tabular: true}}},
	})
	if err != nil || resync {
		t.Fatalf("patch: resync=%v err=%v", resync, err)
	}
	if vt.Revision != 2 || vt.Root.Children[0].Text != "09:41" {
		t.Fatalf("tree = rev %d text %q", vt.Revision, vt.Root.Children[0].Text)
	}
	if vt.Root.Children[1].Text != "Mon" {
		t.Fatal("unrelated node changed")
	}
}

func TestPatchAppliesIndependentReplacements(t *testing.T) {
	t.Parallel()
	vt := seededTree(t)
	_, err := vt.ApplyPatch(&v1.ViewPatch{
		Base: 1, Revision: 2,
		Replacements: []v1.Replacement{
			{Key: "time", Node: &v1.Node{Kind: v1.KindText, Key: "time", Text: "01:00"}},
			{Key: "date", Node: &v1.Node{Kind: v1.KindText, Key: "date", Text: "Tue"}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if vt.Root.Children[0].Text != "01:00" || vt.Root.Children[1].Text != "Tue" {
		t.Fatalf("got %q %q", vt.Root.Children[0].Text, vt.Root.Children[1].Text)
	}
}

func TestPatchRejectsDuplicateTarget(t *testing.T) {
	t.Parallel()
	vt := seededTree(t)
	before := vt.Root
	resync, err := vt.ApplyPatch(&v1.ViewPatch{
		Base: 1, Revision: 2,
		Replacements: []v1.Replacement{
			{Key: "time", Node: &v1.Node{Kind: v1.KindText, Key: "time", Text: "a"}},
			{Key: "time", Node: &v1.Node{Kind: v1.KindText, Key: "time", Text: "b"}},
		},
	})
	if err == nil || !resync {
		t.Fatalf("want error and resync, got err=%v resync=%v", err, resync)
	}
	if vt.Root != before || vt.Revision != 1 {
		t.Fatal("failed patch mutated the tree")
	}
}

func TestPatchRejectsMissingTarget(t *testing.T) {
	t.Parallel()
	vt := seededTree(t)
	resync, err := vt.ApplyPatch(&v1.ViewPatch{
		Base: 1, Revision: 2,
		Replacements: []v1.Replacement{{Key: "ghost", Node: &v1.Node{Kind: v1.KindText, Key: "ghost", Text: "x"}}},
	})
	if err == nil || !resync || vt.Revision != 1 {
		t.Fatalf("err=%v resync=%v rev=%d", err, resync, vt.Revision)
	}
}

func TestPatchRejectsDuplicateKeyInReplacement(t *testing.T) {
	t.Parallel()
	vt := seededTree(t)
	resync, err := vt.ApplyPatch(&v1.ViewPatch{
		Base: 1, Revision: 2,
		Replacements: []v1.Replacement{{
			Key: "date",
			Node: &v1.Node{Kind: v1.KindColumn, Key: "date", Children: []*v1.Node{
				{Kind: v1.KindText, Key: "dup", Text: "a"},
				{Kind: v1.KindText, Key: "dup", Text: "b"},
			}},
		}},
	})
	if err == nil || !resync || vt.Revision != 1 {
		t.Fatalf("err=%v resync=%v rev=%d", err, resync, vt.Revision)
	}
}

func TestPatchRejectsWrongBaseRevision(t *testing.T) {
	t.Parallel()
	vt := seededTree(t)
	resync, err := vt.ApplyPatch(&v1.ViewPatch{
		Base: 99, Revision: 100,
		Replacements: []v1.Replacement{{Key: "time", Node: &v1.Node{Kind: v1.KindText, Key: "time", Text: "x"}}},
	})
	if err == nil || !resync || vt.Revision != 1 {
		t.Fatalf("err=%v resync=%v rev=%d", err, resync, vt.Revision)
	}
}

func TestPatchRejectsStaleRevision(t *testing.T) {
	t.Parallel()
	vt := seededTree(t)
	if _, err := vt.ApplyPatch(&v1.ViewPatch{
		Base: 1, Revision: 2,
		Replacements: []v1.Replacement{{Key: "time", Node: &v1.Node{Kind: v1.KindText, Key: "time", Text: "09:00"}}},
	}); err != nil {
		t.Fatal(err)
	}
	resync, err := vt.ApplyPatch(&v1.ViewPatch{
		Base: 1, Revision: 3,
		Replacements: []v1.Replacement{{Key: "time", Node: &v1.Node{Kind: v1.KindText, Key: "time", Text: "stale"}}},
	})
	if err == nil || !resync || vt.Root.Children[0].Text != "09:00" {
		t.Fatalf("err=%v resync=%v text=%q", err, resync, vt.Root.Children[0].Text)
	}
}

func TestResyncEmittedOnceUntilSnapshot(t *testing.T) {
	t.Parallel()
	vt := seededTree(t)
	first, err := vt.ApplyPatch(&v1.ViewPatch{Base: 0, Revision: 2, Replacements: []v1.Replacement{
		{Key: "time", Node: &v1.Node{Kind: v1.KindText, Key: "time", Text: "x"}},
	}})
	if err == nil || !first {
		t.Fatalf("first: err=%v resync=%v", err, first)
	}
	second, err := vt.ApplyPatch(&v1.ViewPatch{Base: 0, Revision: 3, Replacements: []v1.Replacement{
		{Key: "time", Node: &v1.Node{Kind: v1.KindText, Key: "time", Text: "y"}},
	}})
	if second {
		t.Fatal("second failed patch requested another resync")
	}
	if err == nil {
		t.Fatal("second failed patch returned no error")
	}
	if err := vt.ApplySnapshot(4, keyedClock()); err != nil {
		t.Fatal(err)
	}
	again, err := vt.ApplyPatch(&v1.ViewPatch{Base: 0, Revision: 5, Replacements: []v1.Replacement{
		{Key: "time", Node: &v1.Node{Kind: v1.KindText, Key: "time", Text: "z"}},
	}})
	if err == nil || !again {
		t.Fatalf("after snapshot: err=%v resync=%v", err, again)
	}
}

func TestConvertCopiesEditorWireFields(t *testing.T) {
	t.Parallel()
	root := &v1.Node{Kind: v1.KindColumn, Children: []*v1.Node{{
		Kind: v1.KindTextInput, ID: "n", Key: "note", Name: "Note", Role: "textbox",
		Text: "hi", Multiline: true, SubmitOnEnter: true, Reseed: 3,
		Events: []v1.EventKind{v1.EventChange},
	}}}
	got, err := Convert(root, v1.ViewPanel)
	if err != nil {
		t.Fatal(err)
	}
	field := got.Children[0]
	if !field.Multiline || !field.SubmitOnEnter || field.Reseed != 3 || field.Text != "hi" {
		t.Fatalf("editor fields = %+v", field)
	}
}
