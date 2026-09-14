package render

import (
	_ "embed"
	"fmt"
	"sort"

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

	// The metric icons follow the battery block.
	iconCPU
	iconMemory
	iconDisk
	iconNetwork

	iconCamera
	iconCameraOff
	iconRecord
	iconStop
	iconReplay

	iconNotifications
	iconNotificationsOff
	iconClose
	iconSchedule
	iconGhost
	iconGaugeCPU
	iconGaugeMemory
	iconGaugeGPU

	batteryRuneFirst = iconBatteryLevel0
	batteryRuneLast  = iconBatteryCritical

	metricRuneFirst = iconCPU
	metricRuneLast  = iconNetwork

	recorderRuneFirst = iconCamera
	recorderRuneLast  = iconReplay

	notifyRuneFirst = iconNotifications
	notifyRuneLast  = iconGhost
	gaugeRuneFirst  = iconGaugeCPU
	gaugeRuneLast   = iconGaugeGPU
)

// The night weather glyphs extend the font after the sysmon gauges, so they
// sit outside the contiguous weather run they belong to conceptually; the
// font map routes them as their own band.
const (
	iconClearNight rune = gaugeRuneLast + 1 + iota
	iconPartlyCloudyNight
)

// batteryLevels is how many level glyphs each state has.
const batteryLevels = 7

// newIconFace returns a face for the embedded icon font.
//
// It returns a fresh face every call, and deliberately does not memoise one.
// ParseFace already caches the parsed *font.Font, which is read-only and safe
// to share; a *font.Face is not. Shaping writes to a face -- SetPpem, and the
// glyph caches behind it -- so one face handed to two surfaces means each
// corrupts the other's metrics. Each holder owns its own.
//
// A font that fails to parse yields a nil face, which falls the rune back to
// the system query and draws a notdef box: a broken icon must never fail a
// frame.
func newIconFace() *font.Face {
	face, err := ParseFace(iconTTF)
	if err != nil {
		return nil
	}
	return face
}

func (r *TextRenderer) projectFace() (*font.Face, error) {
	if r == nil {
		return nil, fmt.Errorf("render: nil renderer")
	}
	if r.project != nil {
		return r.project, nil
	}
	if r.projectErr != nil {
		return nil, r.projectErr
	}
	face, err := ParseFace(iconTTF)
	if err != nil {
		r.projectErr = fmt.Errorf("render: parse project icon face: %w", err)
		return nil, r.projectErr
	}
	r.project = face
	return face, nil
}

func (r *TextRenderer) RasterProjectIcon(name string, size int) (Mask, error) {
	glyph, ok := IconByName(name)
	if !ok {
		return Mask{}, fmt.Errorf("render: %q is not in the project icon set", name)
	}
	face, err := r.projectFace()
	if err != nil {
		return Mask{}, err
	}
	out, err := r.shapeFace(face, string(glyph), size, false)
	if err != nil {
		return Mask{}, err
	}
	return rasterRuns([]shapedFaceRun{{face: face, text: string(glyph), output: out}}, size)
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

// IconName is the catalogue name for a WMO weather code. A plugin addresses
// the symbol by this name; the host maps it to a glyph. An unrecognised code
// names the cloud, matching IconRune.
func IconName(code int) string {
	switch IconRune(code) {
	case iconClearDay:
		return "clear-day"
	case iconPartlyCloudy:
		return "partly-cloudy"
	case iconCloud:
		return "cloud"
	case iconFog:
		return "fog"
	case iconRain:
		return "rain"
	case iconSnow:
		return "snow"
	case iconHeavySnow:
		return "heavy-snow"
	case iconThunderstorm:
		return "thunderstorm"
	}
	return "cloud"
}

// WeatherIcon is the symbol for a WMO code at a time of day. Only the clear
// and partly-cloudy categories change by night, as the reference renders
// them; an absent is_day reading passes true and sees the day glyph.
func WeatherIcon(code int, isDay bool) rune {
	if !isDay {
		switch {
		case code == 0:
			return iconClearNight
		case code >= 1 && code <= 2:
			return iconPartlyCloudyNight
		}
	}
	return IconRune(code)
}

// WeatherIconName is the catalogue name for WeatherIcon.
func WeatherIconName(code int, isDay bool) string {
	switch WeatherIcon(code, isDay) {
	case iconClearDay:
		return "clear-day"
	case iconClearNight:
		return "clear-night"
	case iconPartlyCloudy:
		return "partly-cloudy"
	case iconPartlyCloudyNight:
		return "partly-cloudy-night"
	case iconCloud:
		return "cloud"
	case iconFog:
		return "fog"
	case iconRain:
		return "rain"
	case iconSnow:
		return "snow"
	case iconHeavySnow:
		return "heavy-snow"
	case iconThunderstorm:
		return "thunderstorm"
	}
	return "cloud"
}

// WeatherCondition is the one condition-word table. Every surface names a
// WMO code through this rather than keeping its own list; an unrecognised
// code reads as cloudy, matching the glyph fallback.
func WeatherCondition(code int) string {
	switch IconRune(code) {
	case iconClearDay:
		return "Clear"
	case iconPartlyCloudy:
		return "Partly cloudy"
	case iconCloud:
		return "Cloudy"
	case iconFog:
		return "Fog"
	case iconRain:
		return "Rain"
	case iconSnow:
		return "Snow"
	case iconHeavySnow:
		return "Heavy snow"
	case iconThunderstorm:
		return "Thunderstorm"
	}
	return "Cloudy"
}

// BatteryIconRune picks the glyph for a charge and state.
//
// Critical overrides the level entirely: a battery about to die should look
// like one at every charge the caller considers critical, which is a policy
// the widget owns rather than a threshold baked in here.
// MetricIconRune is the glyph naming what a metric measures. A bar shows a
// bare percentage otherwise, and a grouped one loses even the separation that
// hinted at distinct widgets. An id with no icon returns zero.
func MetricIconRune(id string) rune {
	switch id {
	case "cpu":
		return iconCPU
	case "memory":
		return iconMemory
	case "filesystem", "block":
		return iconDisk
	case "network":
		return iconNetwork
	}
	return 0
}

// GaugeIconName maps the compact system-summary gauges to their project-owned
// glyph names. These are separate from the text metric glyphs because they are
// designed for the smaller clear space inside a 22 px progress ring.
func GaugeIconName(id string) (string, bool) {
	switch id {
	case "cpu":
		return "sysmon-cpu", true
	case "memory":
		return "sysmon-memory", true
	case "gpu":
		return "sysmon-gpu", true
	}
	return "", false
}

func GaugeIconRune(id string) (rune, bool) {
	name, ok := GaugeIconName(id)
	if !ok {
		return 0, false
	}
	r, ok := IconByName(name)
	return r, ok
}

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
	"clear-day":           iconClearDay,
	"clear-night":         iconClearNight,
	"partly-cloudy":       iconPartlyCloudy,
	"partly-cloudy-night": iconPartlyCloudyNight,
	"cloud":               iconCloud,
	"fog":                 iconFog,
	"rain":                iconRain,
	"snow":                iconSnow,
	"heavy-snow":          iconHeavySnow,
	"thunderstorm":        iconThunderstorm,
	"camera":              iconCamera,
	"camera-off":          iconCameraOff,
	"record":              iconRecord,
	"stop":                iconStop,
	"replay":              iconReplay,
	"notifications":       iconNotifications,
	"notifications-off":   iconNotificationsOff,
	"close":               iconClose,
	"schedule":            iconSchedule,
	"ghost":               iconGhost,
	"sysmon-cpu":          iconGaugeCPU,
	"sysmon-memory":       iconGaugeMemory,
	"sysmon-gpu":          iconGaugeGPU,
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
