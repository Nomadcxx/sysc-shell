package ui

// KeepCaretVisible returns the horizontal scroll, in logical px, that keeps a
// caret at caretX inside a view viewW wide, margin clear of either edge. It
// never scrolls past the end of the text, so no dead space follows the last
// glyph, and never below zero. Pure: the host measures, this decides.
func KeepCaretVisible(scroll, caretX, textW, viewW, margin int) int {
	if viewW <= 0 || textW <= viewW {
		return 0
	}
	margin = min(max(margin, 0), viewW/2)
	if caretX-scroll > viewW-margin {
		scroll = caretX - (viewW - margin)
	}
	if caretX-scroll < margin {
		scroll = caretX - margin
	}
	return min(max(scroll, 0), textW-viewW)
}
