package ui

import "unicode/utf8"

const KeyBackspace = 14

// Field is a single-line text value. Preedit is IME composing text and is
// not part of Text until Commit.
type Field struct {
	Text        string
	PreeditText string
	Cursor      int
	// Anchor is the other end of the selection; equal to Cursor means none.
	Anchor        int
	Multiline     bool
	SubmitOnEnter bool
	// Masked disguises the value at render time only. Text keeps the real
	// runes, so editing, cursor motion and submit are unchanged and exactly
	// one code path knows about the disguise.
	Masked bool
	// ScrollX and ScrollY are view state kept with the editor so a rebuild
	// does not jump the view: px for a single line, lines for multiline.
	ScrollX int
	ScrollY int
	// goalCol is the column vertical motion aims for, kept across short
	// lines; -1 when the next vertical move should start from the caret.
	goalCol int
	history undoHistory
}

func NewField(s string) *Field {
	return &Field{Text: s, Cursor: len(s), Anchor: len(s), goalCol: -1}
}

// MaskRune is drawn in place of each rune of a masked field.
const MaskRune = '•'

// DisplayText is what a field should draw and be measured by: the real value,
// or one bullet per rune when it is masked.
//
// Every site that derives geometry from a field's text must go through this.
// Measuring the real runes while drawing bullets, or the reverse, drifts the
// caret away from the glyphs, and a bullet's advance is not a letter's.
func DisplayText(n *Node) string {
	if n == nil {
		return ""
	}
	return maskIf(n.Text, n.Masked)
}

// DisplayPreedit is the composing text under the same disguise.
func DisplayPreedit(n *Node) string {
	if n == nil {
		return ""
	}
	return maskIf(n.Preedit, n.Masked)
}

// DisplayPrefix is the masked form of the text before the cursor, which is
// what positions the caret.
func DisplayPrefix(n *Node, cursor int) string {
	if n == nil {
		return ""
	}
	if cursor < 0 || cursor > len(n.Text) {
		cursor = len(n.Text)
	}
	return maskIf(n.Text[:cursor], n.Masked)
}

// maskIf preserves newlines so a masked multiline field still breaks where it
// should, rather than collapsing into one long row of bullets.
func maskIf(s string, masked bool) string {
	if !masked || s == "" {
		return s
	}
	out := make([]rune, 0, len(s))
	for _, r := range s {
		if r == '\n' {
			out = append(out, r)
			continue
		}
		out = append(out, MaskRune)
	}
	return string(out)
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
	f.record(editOther, "")
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
	f.DeleteSelection()
	f.Text = f.Text[:f.Cursor] + s + f.Text[f.Cursor:]
	f.Cursor += len(s)
	f.Anchor, f.goalCol = f.Cursor, -1
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
	f.Anchor = f.Cursor
}

// Backspace deletes the selection, else the grapheme before the caret.
func (f *Field) Backspace() {
	if f != nil {
		f.DeleteGrapheme(-1)
	}
}

// Clear empties the field. It is the pointer path's counterpart to holding
// Backspace: a search well with a long query has no other way back to the
// unfiltered list.
func (f *Field) Clear() {
	if f == nil {
		return
	}
	f.Text, f.PreeditText, f.Cursor, f.Anchor = "", "", 0, 0
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
	f.Anchor = f.Cursor
}

func (f *Field) clamp() {
	if f.Cursor < 0 {
		f.Cursor = 0
	}
	if f.Cursor > len(f.Text) {
		f.Cursor = len(f.Text)
	}
	f.Anchor = min(max(f.Anchor, 0), len(f.Text))
}

func (f *Field) Node(name string) *Node {
	if f == nil {
		f = NewField("")
	}
	start, end := f.Selection()
	return &Node{
		Kind: KindTextField, Text: f.Text, Preedit: f.PreeditText, Cursor: f.Cursor,
		SelStart: start, SelEnd: end,
		Focusable: true, Name: name, Role: "textbox", Multiline: f.Multiline,
		SubmitOnEnter: f.SubmitOnEnter, Masked: f.Masked,
	}
}

// SyncFrom adopts a node's value. New text is a reseed — from a plugin or a
// programmatic set — so the caret follows the node and selection, history
// and scroll reset. The same text means the node was rebuilt from this
// field, and the field's own caret, selection and history stand.
func (f *Field) SyncFrom(n *Node) {
	if f == nil || n == nil {
		return
	}
	f.PreeditText = n.Preedit
	if n.Text == f.Text {
		return
	}
	f.Text, f.Cursor = n.Text, n.Cursor
	f.clamp()
	f.Anchor, f.goalCol, f.ScrollX, f.ScrollY = f.Cursor, -1, 0, 0
	f.history = undoHistory{}
}

func (f *Field) SyncTo(n *Node) {
	if f == nil || n == nil {
		return
	}
	n.Text, n.Preedit, n.Cursor = f.Text, f.PreeditText, f.Cursor
	n.SelStart, n.SelEnd = f.Selection()
}
