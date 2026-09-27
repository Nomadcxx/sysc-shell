package ui

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/rivo/uniseg"
)

// Positions are byte offsets into Text, always on a grapheme-cluster
// boundary, so a caret never lands inside an accent or a joined emoji.

func nextGrapheme(s string, i int) int {
	if i >= len(s) {
		return len(s)
	}
	cluster, _, _, _ := uniseg.FirstGraphemeClusterInString(s[i:], -1)
	return i + len(cluster)
}

func prevGrapheme(s string, i int) int {
	if i <= 0 {
		return 0
	}
	last := 0
	for pos := 0; pos < i; {
		last = pos
		pos = nextGrapheme(s, pos)
	}
	return last
}

func isWordRune(r rune) bool { return r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r) }

func runeAt(s string, i int) rune     { r, _ := utf8.DecodeRuneInString(s[i:]); return r }
func runeBefore(s string, i int) rune { r, _ := utf8.DecodeLastRuneInString(s[:i]); return r }

func nextWord(s string, i int, masked bool) int {
	if masked {
		return len(s)
	}
	for i < len(s) && !isWordRune(runeAt(s, i)) {
		i = nextGrapheme(s, i)
	}
	for i < len(s) && isWordRune(runeAt(s, i)) {
		i = nextGrapheme(s, i)
	}
	return i
}

func prevWord(s string, i int, masked bool) int {
	if masked {
		return 0
	}
	for i > 0 && !isWordRune(runeBefore(s, i)) {
		i = prevGrapheme(s, i)
	}
	for i > 0 && isWordRune(runeBefore(s, i)) {
		i = prevGrapheme(s, i)
	}
	return i
}

func lineStart(s string, i int) int { return strings.LastIndexByte(s[:i], '\n') + 1 }

func lineEnd(s string, i int) int {
	if j := strings.IndexByte(s[i:], '\n'); j >= 0 {
		return i + j
	}
	return len(s)
}

func (f *Field) Selection() (start, end int) {
	f.clamp()
	return min(f.Cursor, f.Anchor), max(f.Cursor, f.Anchor)
}

func (f *Field) HasSelection() bool { s, e := f.Selection(); return s != e }

func (f *Field) SelectedText() string { s, e := f.Selection(); return f.Text[s:e] }

func (f *Field) SelectAll() { f.Anchor, f.Cursor, f.goalCol = 0, len(f.Text), -1 }

// SetCaret moves the caret to pos (snapped back to a grapheme boundary),
// extending the selection when extend is set.
func (f *Field) SetCaret(pos int, extend bool) {
	pos = min(max(pos, 0), len(f.Text))
	if pos < len(f.Text) {
		pos = prevGrapheme(f.Text, nextGrapheme(f.Text, pos))
	}
	f.place(pos, extend)
}

func (f *Field) place(pos int, extend bool) {
	f.Cursor = pos
	if !extend {
		f.Anchor = pos
	}
	f.goalCol = -1
}

func (f *Field) SelectWordAt(pos int) {
	pos = min(max(pos, 0), len(f.Text))
	start, end := pos, pos
	if pos < len(f.Text) && isWordRune(runeAt(f.Text, pos)) || pos > 0 && isWordRune(runeBefore(f.Text, pos)) {
		start, end = prevWord(f.Text, pos, f.Masked), nextWord(f.Text, pos, f.Masked)
		if pos < len(f.Text) && !isWordRune(runeAt(f.Text, pos)) {
			end = pos
		}
	}
	f.Anchor, f.Cursor, f.goalCol = start, end, -1
}

func (f *Field) SelectLineAt(pos int) {
	pos = min(max(pos, 0), len(f.Text))
	f.Anchor, f.Cursor, f.goalCol = lineStart(f.Text, pos), lineEnd(f.Text, pos), -1
}

func (f *Field) MoveGrapheme(dir int, extend bool) {
	f.clamp()
	if !extend && f.HasSelection() {
		s, e := f.Selection()
		if dir < 0 {
			f.place(s, false)
		} else {
			f.place(e, false)
		}
		return
	}
	if dir < 0 {
		f.place(prevGrapheme(f.Text, f.Cursor), extend)
	} else {
		f.place(nextGrapheme(f.Text, f.Cursor), extend)
	}
}

func (f *Field) MoveWord(dir int, extend bool) {
	f.clamp()
	if dir < 0 {
		f.place(prevWord(f.Text, f.Cursor, f.Masked), extend)
	} else {
		f.place(nextWord(f.Text, f.Cursor, f.Masked), extend)
	}
}

func (f *Field) MoveLineEdge(end bool, extend bool) {
	f.clamp()
	if end {
		f.place(lineEnd(f.Text, f.Cursor), extend)
	} else {
		f.place(lineStart(f.Text, f.Cursor), extend)
	}
}

func (f *Field) MoveTextEdge(end bool, extend bool) {
	if end {
		f.place(len(f.Text), extend)
	} else {
		f.place(0, extend)
	}
}

// MoveVertical moves one line in a multiline field, keeping the column the
// motion started from so a short line in between does not lose it.
func (f *Field) MoveVertical(dir int, extend bool) bool {
	if !f.Multiline {
		return false
	}
	f.clamp()
	start := lineStart(f.Text, f.Cursor)
	col := f.goalCol
	if col < 0 {
		col = utf8.RuneCountInString(f.Text[start:f.Cursor])
	}
	var target int
	if dir < 0 {
		if start == 0 {
			return true
		}
		target = lineStart(f.Text, start-1)
	} else {
		end := lineEnd(f.Text, f.Cursor)
		if end == len(f.Text) {
			return true
		}
		target = end + 1
	}
	stop := lineEnd(f.Text, target)
	pos := target
	for n := 0; n < col && pos < stop; n++ {
		pos = nextGrapheme(f.Text, pos)
	}
	f.Cursor = pos
	if !extend {
		f.Anchor = pos
	}
	f.goalCol = col
	return true
}

func (f *Field) DeleteSelection() bool {
	s, e := f.Selection()
	if s == e {
		return false
	}
	f.Text = f.Text[:s] + f.Text[e:]
	f.Cursor, f.Anchor, f.goalCol = s, s, -1
	return true
}

func (f *Field) deleteRange(s, e int) bool {
	if s == e {
		return false
	}
	f.Text = f.Text[:s] + f.Text[e:]
	f.Cursor, f.Anchor, f.goalCol = s, s, -1
	return true
}

func (f *Field) DeleteGrapheme(dir int) bool {
	f.clamp()
	if f.DeleteSelection() {
		return true
	}
	if dir < 0 {
		return f.deleteRange(prevGrapheme(f.Text, f.Cursor), f.Cursor)
	}
	return f.deleteRange(f.Cursor, nextGrapheme(f.Text, f.Cursor))
}

func (f *Field) DeleteWord(dir int) bool {
	f.clamp()
	if f.DeleteSelection() {
		return true
	}
	if dir < 0 {
		return f.deleteRange(prevWord(f.Text, f.Cursor, f.Masked), f.Cursor)
	}
	return f.deleteRange(f.Cursor, nextWord(f.Text, f.Cursor, f.Masked))
}

func (f *Field) DeleteToLineEdge(end bool) bool {
	f.clamp()
	if end {
		return f.deleteRange(f.Cursor, lineEnd(f.Text, f.Cursor))
	}
	return f.deleteRange(lineStart(f.Text, f.Cursor), f.Cursor)
}
