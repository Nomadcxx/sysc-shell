package render

import (
	"strings"
	"testing"

	"github.com/go-text/typesetting/shaping"
	"golang.org/x/image/font/gofont/goregular"
	"golang.org/x/image/math/fixed"
)

func newTestRenderer(t *testing.T) *TextRenderer {
	t.Helper()
	face, err := ParseFace(goregular.TTF)
	if err != nil {
		t.Fatalf("ParseFace: %v", err)
	}
	return NewTextRenderer(face)
}

func TestClusterPrefixRejectsAPartialMultiGlyphCluster(t *testing.T) {
	t.Parallel()

	glyphs := []shaping.Glyph{
		{Advance: fixed.I(5), ClusterIndex: 0, RuneCount: 1, GlyphCount: 2},
		{Advance: fixed.I(5), ClusterIndex: 0, RuneCount: 1, GlyphCount: 2},
		{Advance: fixed.I(4), ClusterIndex: 1, RuneCount: 1, GlyphCount: 1},
	}
	keep, _ := clusterPrefix(glyphs, 7)
	if keep != 0 {
		t.Fatalf("clusterPrefix kept %d runes after only part of the first cluster fit", keep)
	}
}

func TestTruncateLeavesFittingTextAlone(t *testing.T) {
	t.Parallel()
	r := newTestRenderer(t)
	full, _, err := r.Measure("Workspace", TextSpec{Size: 16, Weight: 400}, false)
	if err != nil {
		t.Fatalf("Measure: %v", err)
	}

	got, advance, err := r.Truncate("Workspace", TextSpec{Size: 16, Weight: 400}, full+50, false)
	if err != nil {
		t.Fatalf("Truncate: %v", err)
	}
	if got != "Workspace" {
		t.Fatalf("Truncate = %q, want the input unchanged", got)
	}
	if advance > full+50 {
		t.Fatalf("advance %d exceeds the available width", advance)
	}
}

func TestTruncateAppendsAnEllipsisAndFits(t *testing.T) {
	t.Parallel()
	r := newTestRenderer(t)
	const s = "a very long workspace name that will not fit"
	full, _, err := r.Measure(s, TextSpec{Size: 16, Weight: 400}, false)
	if err != nil {
		t.Fatalf("Measure: %v", err)
	}
	avail := full / 3

	got, advance, err := r.Truncate(s, TextSpec{Size: 16, Weight: 400}, avail, false)
	if err != nil {
		t.Fatalf("Truncate: %v", err)
	}
	if got == s {
		t.Fatal("Truncate returned the input unchanged")
	}
	if !strings.HasSuffix(got, ellipsis) {
		t.Fatalf("Truncate = %q, want it to end in an ellipsis", got)
	}
	if advance > avail {
		t.Fatalf("advance %d exceeds the available width %d", advance, avail)
	}
	if !strings.HasPrefix(s, strings.TrimSuffix(got, ellipsis)) {
		t.Fatalf("Truncate = %q, want a prefix of the input plus an ellipsis", got)
	}
}

func TestTruncateRendersEmptyWhenEvenTheEllipsisDoesNotFit(t *testing.T) {
	t.Parallel()
	r := newTestRenderer(t)
	got, advance, err := r.Truncate("anything at all", TextSpec{Size: 16, Weight: 400}, 1, false)
	if err != nil {
		t.Fatalf("Truncate: %v", err)
	}
	if got != "" || advance != 0 {
		t.Fatalf("Truncate = %q/%d, want empty and zero", got, advance)
	}
}

func TestTruncateHandlesZeroAndEmptyInput(t *testing.T) {
	t.Parallel()
	r := newTestRenderer(t)
	for _, c := range []struct {
		name  string
		text  string
		avail int
	}{
		{"zero width", "text", 0},
		{"negative width", "text", -5},
		{"empty text", "", 100},
	} {
		t.Run(c.name, func(t *testing.T) {
			got, advance, err := r.Truncate(c.text, TextSpec{Size: 16, Weight: 400}, c.avail, false)
			if err != nil {
				t.Fatalf("Truncate: %v", err)
			}
			if got != "" || advance != 0 {
				t.Fatalf("Truncate = %q/%d, want empty and zero", got, advance)
			}
		})
	}
}

// Truncation is computed on shaped physical advances, so the same logical text
// survives at a fractional scale when the available width scales with it.
func TestTruncateIsStableAcrossScales(t *testing.T) {
	t.Parallel()
	r := newTestRenderer(t)
	const s = "workspace name"

	at1, _, err := r.Truncate(s, TextSpec{Size: 16, Weight: 400}, 120, false)
	if err != nil {
		t.Fatalf("Truncate at 16: %v", err)
	}
	// 1.5 scale: 16*180/120 = 24px shaped into 120*180/120 = 180px.
	at15, _, err := r.Truncate(s, TextSpec{Size: 24, Weight: 400}, 180, false)
	if err != nil {
		t.Fatalf("Truncate at 24: %v", err)
	}
	if at1 != at15 {
		t.Fatalf("truncation differs across scales: %q at 1.0 vs %q at 1.5", at1, at15)
	}
}
