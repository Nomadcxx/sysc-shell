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

func rasterizeWordmark(cells [][]wordmarkCell, cellW, cellH int, colors wordmarkColors) *ui.Image {
	if cellW <= 0 || cellH <= 0 || len(cells) == 0 {
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
	if cols > maxInt/cellW || len(cells) > maxInt/cellH {
		return nil
	}
	width, height := cols*cellW, len(cells)*cellH
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

	for rowIndex, row := range cells {
		for colIndex, cell := range row {
			x0, y0 := colIndex*cellW, rowIndex*cellH
			if cell.bg == 47 {
				fillWordmarkRect(img, x0, y0, x0+cellW, y0+cellH, wordmarkColor(47, colors), 1)
			}

			fg := wordmarkColor(cell.fg, colors)
			switch cell.glyph {
			case '█':
				fillWordmarkRect(img, x0, y0, x0+cellW, y0+cellH, fg, 1)
			case '▀':
				fillWordmarkRect(img, x0, y0, x0+cellW, y0+cellH/2, fg, 1)
			case '▄':
				fillWordmarkRect(img, x0, y0+cellH/2, x0+cellW, y0+cellH, fg, 1)
			case '▌':
				fillWordmarkRect(img, x0, y0, x0+cellW/2, y0+cellH, fg, 1)
			case '▐':
				fillWordmarkRect(img, x0+cellW/2, y0, x0+cellW, y0+cellH, fg, 1)
			case '░':
				fillWordmarkRect(img, x0, y0, x0+cellW, y0+cellH, fg, 0.25)
			case '▒':
				fillWordmarkRect(img, x0, y0, x0+cellW, y0+cellH, fg, 0.5)
			case '▓':
				fillWordmarkRect(img, x0, y0, x0+cellW, y0+cellH, fg, 0.75)
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
		return byte(math.Round(float64(source)*sourceAlpha/255 + float64(destination)*inverseAlpha))
	}
	dst[0] = blend(color.B, dst[0])
	dst[1] = blend(color.G, dst[1])
	dst[2] = blend(color.R, dst[2])
	dst[3] = byte(math.Round(sourceAlpha + float64(dst[3])*inverseAlpha))
}
