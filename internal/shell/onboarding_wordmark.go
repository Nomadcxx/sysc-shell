package shell

import (
	_ "embed"
	"math"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/Nomadcxx/sysc-shell/internal/render"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

//go:embed onboarding_wordmark.ans
var onboardingWordmarkANSI string

var onboardingWordmarkCells = parseANSIWordmark(onboardingWordmarkANSI)

// The wordmark reserves 5×10 logical px per cell: 47×8 cells.
const (
	onbWordmarkW = 235
	onbWordmarkH = 80
)

// onbWordmarkKey is what a cached wordmark raster was painted for.
type onbWordmarkKey struct {
	w, h   int
	colors wordmarkColors
}

// onboardingWordmark is the rail's SYSC art as a w×h logical image, painted
// at the panel's scale in the host palette.
func onboardingWordmark(h *PanelHost, w, height int) *ui.Node {
	scale := ui.Scale120(h.scale120)
	if !scale.Valid() {
		scale = 120
	}
	key := onbWordmarkKey{
		w: scale.Physical(w), h: scale.Physical(height),
		colors: wordmarkColors{
			dim:    h.theme.Palette.Outline,
			mid:    h.theme.Palette.OnSurfaceVariant,
			bright: h.theme.Palette.OnSurface,
		},
	}
	if h.onbWordmark == nil || h.onbWordmarkFor != key {
		h.onbWordmark = rasterizeWordmark(onboardingWordmarkCells, key.w, key.h, key.colors)
		h.onbWordmarkFor = key
	}
	return &ui.Node{
		Kind: ui.KindImage, Key: "onb-wordmark", Name: "SYSC", Role: "img",
		ImageW: w, ImageH: height, Image: h.onbWordmark,
	}
}

type wordmarkCell struct {
	glyph rune
	fg    int
	bg    int
}

type wordmarkColors struct {
	dim    render.Color
	mid    render.Color
	bright render.Color
}

func parseANSIWordmark(text string) [][]wordmarkCell {
	text = strings.ReplaceAll(text, `\033`, "\x1b")
	text = strings.TrimRight(text, "\r\n")
	if text == "" {
		return nil
	}

	lines := strings.Split(text, "\n")
	rows := make([][]wordmarkCell, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSuffix(line, "\r")
		fg, bg := 37, 0
		row := make([]wordmarkCell, 0, utf8.RuneCountInString(line))
		for len(line) > 0 {
			if strings.HasPrefix(line, "\x1b[") {
				if end := strings.IndexByte(line, 'm'); end >= 2 {
					applyWordmarkSGR(line[2:end], &fg, &bg)
					line = line[end+1:]
					continue
				}
			}
			glyph, size := utf8.DecodeRuneInString(line)
			row = append(row, wordmarkCell{glyph: glyph, fg: fg, bg: bg})
			line = line[size:]
		}
		rows = append(rows, row)
	}
	return rows
}

func applyWordmarkSGR(params string, fg, bg *int) {
	for _, param := range strings.Split(params, ";") {
		code := 0
		if param != "" {
			var err error
			code, err = strconv.Atoi(param)
			if err != nil {
				continue
			}
		}
		switch code {
		case 0:
			*fg, *bg = 37, 0
		case 37, 90, 97:
			*fg = code
		case 39:
			*fg = 37
		case 47:
			*bg = code
		case 49:
			*bg = 0
		}
	}
}

// rasterizeWordmark paints the cells into a width×height raster. Cell edges
// are spread proportionally, so a fractional scale tiles the box with cells
// one pixel apart in size rather than leaving a gap at the edge.
func rasterizeWordmark(cells [][]wordmarkCell, width, height int, colors wordmarkColors) *ui.Image {
	if width <= 0 || height <= 0 || len(cells) == 0 {
		return nil
	}
	cols := 0
	for _, row := range cells {
		if len(row) > cols {
			cols = len(row)
		}
	}
	if cols == 0 {
		return nil
	}

	maxInt := int(^uint(0) >> 1)
	if width > maxInt/4 {
		return nil
	}
	stride := width * 4
	if height > maxInt/stride {
		return nil
	}
	img := &ui.Image{
		Width: width, Height: height, Stride: stride,
		Pix: make([]byte, stride*height),
	}

	rows := len(cells)
	for rowIndex, row := range cells {
		y0, y1 := rowIndex*height/rows, (rowIndex+1)*height/rows
		midY := y0 + (y1-y0)/2
		for colIndex, cell := range row {
			x0, x1 := colIndex*width/cols, (colIndex+1)*width/cols
			midX := x0 + (x1-x0)/2
			if cell.bg == 47 {
				fillWordmarkRect(img, x0, y0, x1, y1, wordmarkColor(47, colors), 1)
			}

			fg := wordmarkColor(cell.fg, colors)
			switch cell.glyph {
			case '█':
				fillWordmarkRect(img, x0, y0, x1, y1, fg, 1)
			case '▀':
				fillWordmarkRect(img, x0, y0, x1, midY, fg, 1)
			case '▄':
				fillWordmarkRect(img, x0, midY, x1, y1, fg, 1)
			case '▌':
				fillWordmarkRect(img, x0, y0, midX, y1, fg, 1)
			case '▐':
				fillWordmarkRect(img, midX, y0, x1, y1, fg, 1)
			case '░':
				fillWordmarkRect(img, x0, y0, x1, y1, fg, 0.25)
			case '▒':
				fillWordmarkRect(img, x0, y0, x1, y1, fg, 0.5)
			case '▓':
				fillWordmarkRect(img, x0, y0, x1, y1, fg, 0.75)
			}
		}
	}
	return img
}

func wordmarkColor(code int, colors wordmarkColors) render.Color {
	switch code {
	case 90:
		return colors.dim
	case 97:
		return colors.bright
	default:
		return colors.mid
	}
}

func fillWordmarkRect(img *ui.Image, x0, y0, x1, y1 int, color render.Color, coverage float64) {
	for y := y0; y < y1; y++ {
		for x := x0; x < x1; x++ {
			offset := y*img.Stride + x*4
			overWordmarkColor(img.Pix[offset:offset+4], color, coverage)
		}
	}
}

func overWordmarkColor(dst []byte, color render.Color, coverage float64) {
	sourceAlpha := float64(color.A) * coverage
	inverseAlpha := 1 - sourceAlpha/255
	blend := func(source, destination uint8) byte {
		return byte(math.RoundToEven(float64(source)*sourceAlpha/255 + float64(destination)*inverseAlpha))
	}
	dst[0] = blend(color.B, dst[0])
	dst[1] = blend(color.G, dst[1])
	dst[2] = blend(color.R, dst[2])
	dst[3] = byte(math.RoundToEven(sourceAlpha + float64(dst[3])*inverseAlpha))
}
