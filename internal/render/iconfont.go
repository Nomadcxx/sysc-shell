package render

import (
	_ "embed"
	"sort"
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

// The battery symbols occupy the fifteen codepoints after the weather ones:
// seven discharging levels, seven charging levels, and one critical glyph.
const (
	iconBatteryLevel0 rune = iconThunderstorm + 1 + iota
	iconBatteryLevel1
	iconBatteryLevel2
	iconBatteryLevel3
	iconBatteryLevel4
	iconBatteryLevel5
	iconBatteryLevel6
	iconBatteryCharging0
	iconBatteryCharging1
	iconBatteryCharging2
	iconBatteryCharging3
	iconBatteryCharging4
	iconBatteryCharging5
	iconBatteryCharging6
	iconBatteryCritical

	batteryRuneFirst = iconBatteryLevel0
	batteryRuneLast  = iconBatteryCritical
)

// batteryLevels is how many level glyphs each state has.
const batteryLevels = 7

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

// BatteryIconRune picks the glyph for a charge and state.
//
// Critical overrides the level entirely: a battery about to die should look
// like one at every charge the caller considers critical, which is a policy
// the widget owns rather than a threshold baked in here.
func BatteryIconRune(charge float64, charging, critical bool) rune {
	if critical {
		return iconBatteryCritical
	}
	if charge < 0 {
		charge = 0
	}
	if charge > 1 {
		charge = 1
	}

	// Bands are equal width; the top band is reached only at a full charge, so
	// a battery at 99% does not render as full.
	level := int(charge * batteryLevels)
	if level >= batteryLevels {
		level = batteryLevels - 1
	}

	if charging {
		return iconBatteryCharging0 + rune(level)
	}
	return iconBatteryLevel0 + rune(level)
}

// iconNames is the catalogue a plugin addresses. A plugin names a symbol
// rather than supplying a codepoint or a file, so the set of glyphs that can
// appear in the shell stays the shell's to decide, and a name the font does
// not have is a diagnosable error instead of a missing-glyph box.
var iconNames = map[string]rune{
	"clear-day":     iconClearDay,
	"partly-cloudy": iconPartlyCloudy,
	"cloud":         iconCloud,
	"fog":           iconFog,
	"rain":          iconRain,
	"snow":          iconSnow,
	"heavy-snow":    iconHeavySnow,
	"thunderstorm":  iconThunderstorm,
}

// IconByName resolves a catalogue name to its symbol.
func IconByName(name string) (rune, bool) {
	r, ok := iconNames[name]
	return r, ok
}

// IconNames lists the catalogue in a stable order, for error messages that
// tell a plugin author what is available.
func IconNames() []string {
	out := make([]string, 0, len(iconNames))
	for name := range iconNames {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}
