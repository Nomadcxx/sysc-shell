package shell

import (
	"testing"

	"github.com/Nomadcxx/sysc-shell/internal/render"
)

func TestParseANSIWordmark(t *testing.T) {
	rows := parseANSIWordmark(onboardingWordmarkANSI)
	if len(rows) != 8 {
		t.Fatalf("rows = %d, want 8", len(rows))
	}
	width := 0
	for _, row := range rows {
		if len(row) > width {
			width = len(row)
		}
	}
	if width != 47 {
		t.Fatalf("widest row = %d cells, want 47", width)
	}

	for _, tc := range []struct {
		name string
		row  int
		col  int
		want wordmarkCell
	}{
		{name: "dim over mid background", row: 1, col: 0, want: wordmarkCell{glyph: '▓', fg: 90, bg: 47}},
		{name: "dim shade without background", row: 0, col: 22, want: wordmarkCell{glyph: '░', fg: 90}},
		{name: "bright left", row: 7, col: 13, want: wordmarkCell{glyph: '█', fg: 97}},
		{name: "bright middle", row: 7, col: 14, want: wordmarkCell{glyph: '█', fg: 97}},
		{name: "bright right", row: 7, col: 15, want: wordmarkCell{glyph: '█', fg: 97}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := rows[tc.row][tc.col]
			if got != tc.want {
				t.Fatalf("cell (%d,%d) = %+v, want %+v", tc.row, tc.col, got, tc.want)
			}
		})
	}
}

func TestParseANSIWordmarkEmptySGRResetsColors(t *testing.T) {
	rows := parseANSIWordmark(`\033[90;47mA\033[mB`)
	want := []wordmarkCell{
		{glyph: 'A', fg: 90, bg: 47},
		{glyph: 'B', fg: 37},
	}
	if len(rows) != 1 || len(rows[0]) != len(want) {
		t.Fatalf("parsed rows = %+v, want one row of %d cells", rows, len(want))
	}
	for i := range want {
		if rows[0][i] != want[i] {
			t.Fatalf("cell %d = %+v, want %+v", i, rows[0][i], want[i])
		}
	}
}

func TestRasterizeWordmarkGeometryAndAlpha(t *testing.T) {
	colors := wordmarkColors{
		dim:    render.Color{R: 200, G: 100, B: 50, A: 255},
		mid:    render.Color{R: 100, G: 80, B: 60, A: 255},
		bright: render.Color{R: 20, G: 40, B: 60, A: 255},
	}
	cells := [][]wordmarkCell{{
		{glyph: '▀', fg: 90},
		{glyph: '▌', fg: 97},
		{glyph: '▒', fg: 90, bg: 47},
		{glyph: '░', fg: 90},
		{glyph: ' ', fg: 90},
		{glyph: '▓', fg: 90, bg: 47},
		{glyph: '▄'},
		{glyph: '▐', fg: 97},
	}}

	img := rasterizeWordmark(cells, 4, 4, colors)
	if img == nil || img.Width != 32 || img.Height != 4 || img.Stride != 128 {
		t.Fatalf("image dimensions/stride = %+v, want 32x4 stride 128", img)
	}
	pixel := func(x, y int) []byte {
		offset := y*img.Stride + x*4
		return img.Pix[offset : offset+4]
	}
	check := func(name string, x, y int, want [4]byte) {
		t.Helper()
		got := pixel(x, y)
		for i, channel := range want {
			if got[i] != channel {
				t.Fatalf("%s pixel (%d,%d) BGRA = %v, want %v", name, x, y, got, want)
			}
		}
	}

	check("top half", 0, 0, [4]byte{50, 100, 200, 255})
	check("bottom half stays transparent", 0, 3, [4]byte{})
	check("left half", 4, 1, [4]byte{60, 40, 20, 255})
	check("right half stays transparent", 7, 1, [4]byte{})
	check("50% shade over background", 8, 1, [4]byte{55, 90, 150, 255})
	check("25% shade without background", 12, 1, [4]byte{13, 25, 50, 64})
	check("space without background", 16, 1, [4]byte{})
	check("75% shade over background", 20, 1, [4]byte{53, 95, 175, 255})
	check("top half of bottom glyph stays transparent", 24, 0, [4]byte{})
	check("bottom half", 24, 3, [4]byte{60, 80, 100, 255})
	check("left half of right glyph stays transparent", 28, 1, [4]byte{})
	check("right half", 31, 1, [4]byte{60, 40, 20, 255})
}

func TestRasterizeWordmarkUsesParsedDimensions(t *testing.T) {
	rows := parseANSIWordmark(onboardingWordmarkANSI)
	img := rasterizeWordmark(rows, 5, 10, wordmarkColors{})
	if img == nil || img.Width != 47*5 || img.Height != 8*10 {
		t.Fatalf("image dimensions = %+v, want %dx%d", img, 47*5, 8*10)
	}
}
