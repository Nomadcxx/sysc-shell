package ui

import "unicode"

const undoLimit = 100

type editKind uint8

const (
	editNone editKind = iota
	editInsert
	editDeleteBack
	editDeleteForward
	editOther
)

type fieldSnap struct {
	text           string
	cursor, anchor int
}

type undoHistory struct {
	undo, redo []fieldSnap
	last       editKind
	lastSpace  bool // the previous coalesced insert was whitespace
}

// record snapshots the field before a mutation of the given kind, unless the
// mutation continues the current coalesced step: typed graphemes coalesce
// through a word and the whitespace after it, and a word typed after
// whitespace starts a new step; same-direction deletes coalesce; anything
// else is its own step.
func (f *Field) record(kind editKind, typed string) {
	h := &f.history
	space := typed != "" && isSpace(typed)
	coalesce := kind != editOther && kind == h.last &&
		!(kind == editInsert && !space && h.lastSpace)
	h.last, h.lastSpace = kind, space
	if kind == editOther {
		h.last = editNone
	}
	h.redo = nil
	if coalesce {
		return
	}
	h.undo = append(h.undo, fieldSnap{f.Text, f.Cursor, f.Anchor})
	if len(h.undo) > undoLimit {
		h.undo = h.undo[len(h.undo)-undoLimit:]
	}
}

func isSpace(s string) bool {
	for _, r := range s {
		if !unicode.IsSpace(r) {
			return false
		}
	}
	return true
}

// BreakUndo ends the current coalesced step without recording anything.
func (f *Field) BreakUndo() { f.history.last = editNone }

func (f *Field) Undo() bool {
	h := &f.history
	if len(h.undo) == 0 {
		return false
	}
	snap := h.undo[len(h.undo)-1]
	h.undo = h.undo[:len(h.undo)-1]
	h.redo = append(h.redo, fieldSnap{f.Text, f.Cursor, f.Anchor})
	f.restore(snap)
	return true
}

func (f *Field) Redo() bool {
	h := &f.history
	if len(h.redo) == 0 {
		return false
	}
	snap := h.redo[len(h.redo)-1]
	h.redo = h.redo[:len(h.redo)-1]
	h.undo = append(h.undo, fieldSnap{f.Text, f.Cursor, f.Anchor})
	f.restore(snap)
	return true
}

func (f *Field) restore(s fieldSnap) {
	f.Text, f.Cursor, f.Anchor, f.PreeditText, f.goalCol = s.text, s.cursor, s.anchor, "", -1
	f.history.last = editNone
}
