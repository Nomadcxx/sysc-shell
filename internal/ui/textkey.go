package ui

// FieldResult reports what one key did to a field. Copy and Paste are
// requests for the host to carry to the system clipboard.
type FieldResult struct {
	Handled, Changed bool
	Copy             string
	Paste            bool
	Submit           bool
}

// lowerSym folds an ASCII capital keysym so Ctrl+Shift+z matches 'z'.
func lowerSym(sym uint32) uint32 {
	if sym >= 'A' && sym <= 'Z' {
		return sym + ('a' - 'A')
	}
	return sym
}

// HandleKey applies one resolved key: GUI-first bindings plus the readline
// keys that do not conflict with them. Keys it does not handle (Tab, Esc,
// Up and Down in a single-line field, unbound shortcuts) are left to the
// host's navigation. Pure: no clocks, no I/O.
func (f *Field) HandleKey(k KeyInput) FieldResult {
	f.clamp()
	shift, ctrl := k.Mods.Has(ModShift), k.Mods.Has(ModCtrl)
	moved := func() FieldResult { f.BreakUndo(); return FieldResult{Handled: true} }
	edit := func(kind editKind, typed string, apply func() bool) FieldResult {
		before := fieldSnap{f.Text, f.Cursor, f.Anchor}
		f.record(kind, typed)
		if !apply() {
			// Nothing changed: drop the snapshot record just pushed, if any.
			if n := len(f.history.undo); n > 0 && f.history.undo[n-1] == before {
				f.history.undo = f.history.undo[:n-1]
			}
			return FieldResult{Handled: true}
		}
		return FieldResult{Handled: true, Changed: true}
	}

	if ctrl && !k.Mods.Has(ModAlt) {
		switch lowerSym(k.Sym) {
		case 'a':
			f.SelectAll()
			return moved()
		case 'e':
			f.MoveLineEdge(true, shift)
			return moved()
		case 'c':
			if f.Masked {
				return FieldResult{Handled: true}
			}
			return FieldResult{Handled: true, Copy: f.SelectedText()}
		case 'x':
			if f.Masked || !f.HasSelection() {
				return FieldResult{Handled: true}
			}
			cut := f.SelectedText()
			r := edit(editOther, "", f.DeleteSelection)
			r.Copy = cut
			return r
		case 'v':
			return FieldResult{Handled: true, Paste: true}
		case 'z':
			if shift {
				return FieldResult{Handled: true, Changed: f.Redo()}
			}
			return FieldResult{Handled: true, Changed: f.Undo()}
		case 'y':
			return FieldResult{Handled: true, Changed: f.Redo()}
		case 'w':
			return edit(editOther, "", func() bool { return f.DeleteWord(-1) })
		case 'u':
			return edit(editOther, "", func() bool { return f.DeleteToLineEdge(false) })
		case 'k':
			return edit(editOther, "", func() bool { return f.DeleteToLineEdge(true) })
		}
	}

	switch k.Sym {
	case SymLeft, SymRight:
		dir := 1
		if k.Sym == SymLeft {
			dir = -1
		}
		if ctrl {
			f.MoveWord(dir, shift)
		} else {
			f.MoveGrapheme(dir, shift)
		}
		return moved()
	case SymHome, SymEnd:
		if ctrl {
			f.MoveTextEdge(k.Sym == SymEnd, shift)
		} else {
			f.MoveLineEdge(k.Sym == SymEnd, shift)
		}
		return moved()
	case SymUp, SymDown:
		dir := 1
		if k.Sym == SymUp {
			dir = -1
		}
		if !f.MoveVertical(dir, shift) {
			return FieldResult{}
		}
		return moved()
	case SymBackSpace:
		if ctrl {
			return edit(editOther, "", func() bool { return f.DeleteWord(-1) })
		}
		return edit(editDeleteBack, "", func() bool { return f.DeleteGrapheme(-1) })
	case SymDelete:
		if ctrl {
			return edit(editOther, "", func() bool { return f.DeleteWord(+1) })
		}
		return edit(editDeleteForward, "", func() bool { return f.DeleteGrapheme(+1) })
	case SymReturn, SymKPEnter:
		if f.Multiline && !f.SubmitOnEnter {
			return edit(editOther, "", func() bool { return f.Insert("\n") })
		}
		return FieldResult{Handled: true, Submit: true}
	}

	if k.Text != "" && !ctrl && !k.Mods.Has(ModAlt) && !k.Mods.Has(ModSuper) {
		return edit(editInsert, k.Text, func() bool { return f.Insert(k.Text) })
	}
	return FieldResult{}
}
