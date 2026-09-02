package ui

import "unicode/utf8"

const KeyBackspace = 14

// Field is a single-line text value. Preedit is IME composing text and is
// not part of Text until Commit.
type Field struct {
	Text          string
	PreeditText   string
	Cursor        int
	Multiline     bool
	SubmitOnEnter bool
}

func NewField(s string) *Field {
	return &Field{Text: s, Cursor: len(s)}
}

func (f *Field) Preedit(s string) {
	if f == nil {
		return
	}
	f.PreeditText = s
}

func (f *Field) Commit(s string) {
	if f == nil {
		return
	}
	f.Insert(s)
}

// Insert writes s at the cursor. A newline is refused when the field is
// single-line or submit-on-enter, so Enter can mean submit instead of a break.
func (f *Field) Insert(s string) bool {
	if f == nil {
		return false
	}
	if s == "\n" && (!f.Multiline || f.SubmitOnEnter) {
		return false
	}
	f.PreeditText = ""
	f.clamp()
	f.Text = f.Text[:f.Cursor] + s + f.Text[f.Cursor:]
	f.Cursor += len(s)
	return true
}

func (f *Field) Move(runes int) {
	if f == nil || runes == 0 {
		return
	}
	f.clamp()
	for runes < 0 && f.Cursor > 0 {
		_, size := utf8.DecodeLastRuneInString(f.Text[:f.Cursor])
		f.Cursor -= size
		runes++
	}
	for runes > 0 && f.Cursor < len(f.Text) {
		_, size := utf8.DecodeRuneInString(f.Text[f.Cursor:])
		f.Cursor += size
		runes--
	}
}

func (f *Field) Backspace() {
	if f == nil {
		return
	}
	f.clamp()
	if f.Cursor <= 0 {
		return
	}
	_, size := utf8.DecodeLastRuneInString(f.Text[:f.Cursor])
	f.Text = f.Text[:f.Cursor-size] + f.Text[f.Cursor:]
	f.Cursor -= size
}

func (f *Field) DeleteSurrounding(before, after int) {
	if f == nil {
		return
	}
	f.clamp()
	if before < 0 {
		before = 0
	}
	if after < 0 {
		after = 0
	}
	start := f.Cursor - before
	if start < 0 {
		start = 0
	}
	end := f.Cursor + after
	if end > len(f.Text) {
		end = len(f.Text)
	}
	f.Text = f.Text[:start] + f.Text[end:]
	f.Cursor = start
}

func (f *Field) clamp() {
	if f.Cursor < 0 {
		f.Cursor = 0
	}
	if f.Cursor > len(f.Text) {
		f.Cursor = len(f.Text)
	}
}

func (f *Field) Node(name string) *Node {
	if f == nil {
		f = NewField("")
	}
	return &Node{
		Kind: KindTextField, Text: f.Text, Preedit: f.PreeditText, Cursor: f.Cursor,
		Focusable: true, Name: name, Role: "textbox", Multiline: f.Multiline,
		SubmitOnEnter: f.SubmitOnEnter,
	}
}

func (f *Field) SyncFrom(n *Node) {
	if f == nil || n == nil {
		return
	}
	f.Text, f.PreeditText, f.Cursor = n.Text, n.Preedit, n.Cursor
}

func (f *Field) SyncTo(n *Node) {
	if f == nil || n == nil {
		return
	}
	n.Text, n.Preedit, n.Cursor = f.Text, f.PreeditText, f.Cursor
}
