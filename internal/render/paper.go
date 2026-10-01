package render

import "github.com/Nomadcxx/sysc-shell/internal/ui"

// Sticky-note paper is fixed, not derived from the theme: a note must read as
// yellow or mint in every palette, and a dark ink on a light paper clears the
// contrast floor without draining the colour, which the old 25% wash did.
var notePapers = map[ui.Fill][2]Color{
	ui.FillNoteSun:   {{R: 0xF7, G: 0xE8, B: 0xA4, A: 0xFF}, {R: 0x3A, G: 0x33, B: 0x20, A: 0xFF}},
	ui.FillNoteMint:  {{R: 0xCF, G: 0xEB, B: 0xD5, A: 0xFF}, {R: 0x1F, G: 0x3A, B: 0x2A, A: 0xFF}},
	ui.FillNoteSky:   {{R: 0xD2, G: 0xE4, B: 0xF7, A: 0xFF}, {R: 0x1C, G: 0x2F, B: 0x45, A: 0xFF}},
	ui.FillNoteRose:  {{R: 0xF7, G: 0xD5, B: 0xDA, A: 0xFF}, {R: 0x45, G: 0x22, B: 0x2A, A: 0xFF}},
	ui.FillNoteLilac: {{R: 0xE3, G: 0xD9, B: 0xF4, A: 0xFF}, {R: 0x2F, G: 0x26, B: 0x45, A: 0xFF}},
}

// paperErrorInk is error text on any paper: a dark red that clears the text
// contrast floor on all five.
var paperErrorInk = Color{R: 0x8C, G: 0x1D, B: 0x18, A: 0xFF}

// PaperPair is the paper and ink a sticky-note fill paints.
func PaperPair(f ui.Fill) (paper, ink Color, ok bool) {
	p, ok := notePapers[f]
	return p[0], p[1], ok
}

// WithPaper re-grounds a surface on sticky-note paper: the root paints the
// paper opaquely, text uses the ink, and the levels controls rest on are the
// paper darkened, so fields and buttons stay legible without theme colours.
// Any other fill leaves the style as it was.
func (s Style) WithPaper(f ui.Fill) Style {
	paper, ink, ok := PaperPair(f)
	if !ok {
		return s
	}
	shade := func(pct uint32) Color {
		m := func(v uint8) uint8 { return uint8(uint32(v) * (100 - pct) / 100) }
		return Color{R: m(paper.R), G: m(paper.G), B: m(paper.B), A: 0xFF}
	}
	inkAt := func(a uint8) Color { c := ink; c.A = a; return c }
	// Secondary text is the ink blended into the paper, opaque, so it stays
	// above the text contrast floor; the theme's own subtle grey is a
	// dark-surface colour and vanishes on paper. Errors use a fixed dark red
	// for the same reason.
	blend := func(over Color, a uint32) Color {
		m := func(o, u uint8) uint8 { return uint8((uint32(o)*a + uint32(u)*(255-a)) / 255) }
		return Color{R: m(over.R, paper.R), G: m(over.G, paper.G), B: m(over.B, paper.B), A: 0xFF}
	}
	s.Background, s.SurfaceOpacity, s.NoGround = paper, 0xFF, false
	s.Foreground, s.OnContainer = ink, ink
	s.Capsule, s.Container, s.Track = shade(6), shade(6), shade(12)
	s.ContainerHighest = shade(10)
	s.Outline, s.OutlineVariant = inkAt(0x59), inkAt(0x2E)
	s.Subtle = blend(ink, 0xB8)
	s.Error = paperErrorInk
	return s
}
