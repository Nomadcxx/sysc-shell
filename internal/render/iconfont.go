package render

import (
	_ "embed"
	"sync"

	"github.com/go-text/typesetting/font"
)

// iconTTF is the project's own symbol font. It is committed rather than
// generated at build time: the charter forbids an external conversion process,
// and a font is deterministic once authored.
//
//go:embed icons/sysc-icons.ttf
var iconTTF []byte

// The eight symbols occupy consecutive private-use codepoints. A private-use
// range is chosen so an icon rune can never collide with real text.
const (
	iconClearDay rune = 0xE000 + iota
	iconPartlyCloudy
	iconCloud
	iconFog
	iconRain
	iconSnow
	iconHeavySnow
	iconThunderstorm

	iconRuneFirst = iconClearDay
	iconRuneLast  = iconThunderstorm
)

var (
	iconOnce sync.Once
	iconFace *font.Face
)

// loadIconFace parses the embedded font once. A font that fails to parse
// leaves the face nil, which falls the rune back to the system query and draws
// a notdef box: a broken icon must never fail a frame.
func loadIconFace() *font.Face {
	iconOnce.Do(func() {
		face, err := ParseFace(iconTTF)
		if err != nil {
			return
		}
		iconFace = face
	})
	return iconFace
}

// IconRune maps a WMO weather code to its symbol.
//
// The whole code set reduces to eight symbols, which is what both reference
// shells do. An unrecognised code renders the cloud rather than nothing, so a
// code the API adds later degrades instead of leaving a gap.
func IconRune(code int) rune {
	switch {
	case code == 0:
		return iconClearDay
	case code >= 1 && code <= 2:
		return iconPartlyCloudy
	case code == 3:
		return iconCloud
	case code >= 45 && code <= 48:
		return iconFog
	case code >= 51 && code <= 67, code >= 80 && code <= 82:
		return iconRain
	case code >= 71 && code <= 73, code == 85:
		return iconSnow
	case code == 75, code == 77, code == 86:
		return iconHeavySnow
	case code >= 95:
		return iconThunderstorm
	}
	return iconCloud
}
