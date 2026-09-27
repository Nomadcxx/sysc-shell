package ui

import "testing"

func key(sym uint32, text string, mods Mods) KeyInput {
	return KeyInput{Sym: sym, Text: text, Mods: mods}
}

func TestHandleKeyTable(t *testing.T) {
	type want struct {
		text           string
		cursor, anchor int
		res            FieldResult
	}
	cases := []struct {
		name string
		f    *Field
		k    KeyInput
		want want
	}{
		{"type", field("ac", 1, 1), key('b', "b", 0), want{"abc", 2, 2, FieldResult{Handled: true, Changed: true}}},
		{"type over selection", field("hello", 5, 0), key('x', "x", 0), want{"x", 1, 1, FieldResult{Handled: true, Changed: true}}},
		{"ctrl+letter never types", field("ab", 2, 2), key('q', "q", ModCtrl), want{"ab", 2, 2, FieldResult{}}},
		{"alt+letter never types", field("ab", 2, 2), key('q', "q", ModAlt), want{"ab", 2, 2, FieldResult{}}},
		{"left", field("abc", 2, 2), key(SymLeft, "", 0), want{"abc", 1, 1, FieldResult{Handled: true}}},
		{"shift+left extends", field("abc", 2, 2), key(SymLeft, "", ModShift), want{"abc", 1, 2, FieldResult{Handled: true}}},
		{"ctrl+left word", field("ab cd", 5, 5), key(SymLeft, "", ModCtrl), want{"ab cd", 3, 3, FieldResult{Handled: true}}},
		{"home", field("abc", 2, 2), key(SymHome, "", 0), want{"abc", 0, 0, FieldResult{Handled: true}}},
		{"end", field("abc", 0, 0), key(SymEnd, "", 0), want{"abc", 3, 3, FieldResult{Handled: true}}},
		{"ctrl+e is end", field("abc", 0, 0), key('e', "e", ModCtrl), want{"abc", 3, 3, FieldResult{Handled: true}}},
		{"backspace", field("abc", 3, 3), key(SymBackSpace, "", 0), want{"ab", 2, 2, FieldResult{Handled: true, Changed: true}}},
		{"delete", field("abc", 0, 0), key(SymDelete, "", 0), want{"bc", 0, 0, FieldResult{Handled: true, Changed: true}}},
		{"ctrl+backspace word", field("ab cd", 5, 5), key(SymBackSpace, "", ModCtrl), want{"ab ", 3, 3, FieldResult{Handled: true, Changed: true}}},
		{"ctrl+w word", field("ab cd", 5, 5), key('w', "w", ModCtrl), want{"ab ", 3, 3, FieldResult{Handled: true, Changed: true}}},
		{"ctrl+delete word", field("ab cd", 0, 0), key(SymDelete, "", ModCtrl), want{" cd", 0, 0, FieldResult{Handled: true, Changed: true}}},
		{"ctrl+u to start", field("ab cd", 3, 3), key('u', "u", ModCtrl), want{"cd", 0, 0, FieldResult{Handled: true, Changed: true}}},
		{"ctrl+k to end", field("ab cd", 2, 2), key('k', "k", ModCtrl), want{"ab", 2, 2, FieldResult{Handled: true, Changed: true}}},
		{"ctrl+a select all", field("abc", 1, 1), key('a', "a", ModCtrl), want{"abc", 3, 0, FieldResult{Handled: true}}},
		{"ctrl+c copies", field("hello", 4, 1), key('c', "c", ModCtrl), want{"hello", 4, 1, FieldResult{Handled: true, Copy: "ell"}}},
		{"ctrl+c with nothing selected", field("hello", 4, 4), key('c', "c", ModCtrl), want{"hello", 4, 4, FieldResult{Handled: true}}},
		{"ctrl+x cuts", field("hello", 4, 1), key('x', "x", ModCtrl), want{"ho", 1, 1, FieldResult{Handled: true, Changed: true, Copy: "ell"}}},
		{"ctrl+v asks for paste", field("ab", 1, 1), key('v', "v", ModCtrl), want{"ab", 1, 1, FieldResult{Handled: true, Paste: true}}},
		{"shifted ctrl+A still selects all", field("abc", 1, 1), key('A', "A", ModCtrl|ModShift), want{"abc", 3, 0, FieldResult{Handled: true}}},
		{"enter submits single line", field("ab", 2, 2), key(SymReturn, "", 0), want{"ab", 2, 2, FieldResult{Handled: true, Submit: true}}},
		{"up not handled single line", field("ab", 2, 2), key(SymUp, "", 0), want{"ab", 2, 2, FieldResult{}}},
		{"tab not handled", field("ab", 2, 2), key(SymTab, "", 0), want{"ab", 2, 2, FieldResult{}}},
		{"escape not handled", field("ab", 2, 2), key(SymEscape, "", 0), want{"ab", 2, 2, FieldResult{}}},
		{"masked refuses copy", &Field{Text: "secret", Cursor: 6, Anchor: 0, Masked: true}, key('c', "c", ModCtrl), want{"secret", 6, 0, FieldResult{Handled: true}}},
		{"masked refuses cut", &Field{Text: "secret", Cursor: 6, Anchor: 0, Masked: true}, key('x', "x", ModCtrl), want{"secret", 6, 0, FieldResult{Handled: true}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tc.f.goalCol = -1
			got := tc.f.HandleKey(tc.k)
			if got != tc.want.res || tc.f.Text != tc.want.text || tc.f.Cursor != tc.want.cursor || tc.f.Anchor != tc.want.anchor {
				t.Fatalf("got %+v text %q cursor %d anchor %d; want %+v", got, tc.f.Text, tc.f.Cursor, tc.f.Anchor, tc.want)
			}
		})
	}
}

func TestHandleKeyMultilineEnterAndVertical(t *testing.T) {
	f := &Field{Text: "ab\ncd", Cursor: 1, Anchor: 1, Multiline: true, goalCol: -1}
	if r := f.HandleKey(key(SymDown, "", 0)); !r.Handled || f.Cursor != 4 {
		t.Fatalf("down = %+v cursor %d", r, f.Cursor)
	}
	if r := f.HandleKey(key(SymReturn, "", 0)); !r.Changed || f.Text != "ab\nc\nd" {
		t.Fatalf("enter in multiline = %+v %q", r, f.Text)
	}
	s := &Field{Text: "ab", Cursor: 2, Anchor: 2, Multiline: true, SubmitOnEnter: true, goalCol: -1}
	if r := s.HandleKey(key(SymReturn, "", 0)); !r.Submit {
		t.Fatalf("submit-on-enter multiline = %+v", r)
	}
}

func TestHandleKeyUndoRedo(t *testing.T) {
	f := NewField("")
	for _, r := range "ab" {
		f.HandleKey(key(uint32(r), string(r), 0))
	}
	if r := f.HandleKey(key('z', "z", ModCtrl)); !r.Changed || f.Text != "" {
		t.Fatalf("undo = %+v %q", r, f.Text)
	}
	if r := f.HandleKey(key('Z', "Z", ModCtrl|ModShift)); !r.Changed || f.Text != "ab" {
		t.Fatalf("redo = %+v %q", r, f.Text)
	}
	f.HandleKey(key('z', "z", ModCtrl))
	if r := f.HandleKey(key('y', "y", ModCtrl)); !r.Changed || f.Text != "ab" {
		t.Fatalf("ctrl+y redo = %+v %q", r, f.Text)
	}
}
