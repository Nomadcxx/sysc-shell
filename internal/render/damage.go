package render

import "github.com/Nomadcxx/sysc-shell/internal/ui"

// DamageSet accumulates the regions one frame changed.
//
// The zero value is "nothing tracked", which reports Full: a surface that does
// not know what changed must damage everything. Damage is an optimisation and
// never a correctness boundary, so every uncertain path ends in Full rather
// than in a smaller rectangle.
type DamageSet struct {
	rects []ui.Rect
	full  bool
}

// Add records one changed region. Degenerate rectangles are dropped rather
// than submitted: a zero-area damage is a protocol call that repairs nothing.
func (d *DamageSet) Add(r ui.Rect) {
	if d.full || r.W <= 0 || r.H <= 0 {
		return
	}
	d.rects = append(d.rects, r)
}

// Moved records a node that changed position or size. Both bounds are damaged,
// because the pixels the node vacated need repainting as much as the ones it
// now covers.
func (d *DamageSet) Moved(old, new ui.Rect) {
	d.Add(old)
	d.Add(new)
}

// MarkFull abandons rectangle tracking for this frame.
func (d *DamageSet) MarkFull() {
	d.full = true
	d.rects = d.rects[:0]
}

// Full reports whether the caller should damage the whole buffer. A set with
// no rectangles is Full, so "tracked nothing" and "changed nothing" both take
// the safe path.
func (d *DamageSet) Full() bool { return d.full || len(d.rects) == 0 }

// Rects returns the accumulated regions, empty when Full.
func (d *DamageSet) Rects() []ui.Rect {
	if d.full {
		return nil
	}
	return d.rects
}

// Reset clears the set for the next frame.
func (d *DamageSet) Reset() {
	d.rects = d.rects[:0]
	d.full = false
}
