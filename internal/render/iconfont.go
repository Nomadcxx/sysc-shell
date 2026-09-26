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

	// The weather-detail run and the ai-usage glyph that continues it are
	// one contiguous band; the face router needs it as a whole.
	detailRuneFirst = iconThermometer
	detailRuneLast  = iconAIUsage
)

// The night weather glyphs extend the font after the sysmon gauges, so they
// sit outside the contiguous weather run they belong to conceptually; the
// font map routes them as their own band.
const (
	iconClearNight rune = gaugeRuneLast + 1 + iota
	iconPartlyCloudyNight
)

// The weather detail glyphs extend the font after the night pair. They label
// the panel's detail rows: feels-like, wind, humidity, sun times, elevation.
const (
	iconThermometer rune = iconPartlyCloudyNight + 1 + iota
	iconWind
	iconHumidity
	iconSunrise
	iconSunset
	iconElevation
)

// The ai-usage glyph closes the font after the weather-detail run: the
// catalogue's assistant mark for the AI Usage plugin.
const (
	iconAIUsage rune = iconElevation + 1
)

// The device and communication glyphs extend the font after the ai-usage
// glyph, for the KDE Connect phone plugin: the device types, the
// ring/browse/clipboard/share/SMS actions, the daemon refresh control, and
// the offline state.
const (
	iconSmartphone rune = iconAIUsage + 1 + iota
	iconPhonelinkOff
	iconTablet
	iconLaptop
	iconDesktopWindows
	iconTV
	iconDevices
	iconPhoneInTalk
	iconFolderOpen
	iconContentPaste
	iconShare
	iconSMS
	iconNotificationsActive
	iconRefresh
)

// The cellular network glyphs extend the font after the device set, for the
// phone plugin's per-strength and per-type connectivity readouts.
const (
	icon5G rune = iconRefresh + 1 + iota
	icon4GMobiledata
	icon3GMobiledata
	iconGMobiledata
	iconSignalCellularNull
	iconSignalCellular1Bar
	iconSignalCellular2Bar
	iconSignalCellular3Bar
	iconSignalCellular4Bar
)

const iconGPU rune = iconSignalCellular4Bar + 1

// The cross follows the GPU metric glyph: the Faith plugin's bar glyph, a
// Latin cross with its crossbar in the upper third.
const iconCross rune = iconGPU + 1

// The cat glyphs close the font after the cellular set, for the Cat plugin.
// Each act is a run of distinct poses and the acts follow one another in
// catActs order. A plugin strings an act's poses into a sprite cycle,
// repeating a name where the motion returns through a pose.
// build.py's CAT_ACTS carries the same table.
var catActs = []struct {
	name  string
	poses int
}{
	{"sleep", 4}, {"sit", 4}, {"groom", 5}, {"scratch", 3},
	{"stretch", 4}, {"walk", 8}, {"run", 12},
}

const (
	catRuneFirst = iconCross + 1
	catRuneLast  = catRuneFirst + 40 - 1
)

// The Docker whale closes the font after the cat band, for the mini-docker
// plugin's bar pill.
const iconDocker rune = catRuneLast + 1

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

// RasterProjectIconIn rasterises a project glyph so its whole design box
// fits a square of box pixels. The font's box is 1.2 em -- the ascent and
// descent build.py sets -- so a glyph shaped at box pixels paints a fifth
// larger than the square it was measured into and spills over its
// neighbours. The icon painter uses this form; text runs keep the em sizing
// that sits a glyph beside body text.
func (r *TextRenderer) RasterProjectIconIn(name string, box int) (Mask, error) {
	return r.RasterProjectIcon(name, max(box*5/6, 1))
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
// MetricIconRune returns the project glyph naming a metric. An id with no
// metric icon returns zero.
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
	case "gpu":
		return iconGPU
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
	"clear-day":             iconClearDay,
	"clear-night":           iconClearNight,
	"partly-cloudy":         iconPartlyCloudy,
	"partly-cloudy-night":   iconPartlyCloudyNight,
	"cloud":                 iconCloud,
	"fog":                   iconFog,
	"rain":                  iconRain,
	"snow":                  iconSnow,
	"heavy-snow":            iconHeavySnow,
	"thunderstorm":          iconThunderstorm,
	"thermometer":           iconThermometer,
	"wind":                  iconWind,
	"humidity":              iconHumidity,
	"sunrise":               iconSunrise,
	"sunset":                iconSunset,
	"elevation":             iconElevation,
	"camera":                iconCamera,
	"camera-off":            iconCameraOff,
	"record":                iconRecord,
	"stop":                  iconStop,
	"replay":                iconReplay,
	"notifications":         iconNotifications,
	"notifications-off":     iconNotificationsOff,
	"close":                 iconClose,
	"schedule":              iconSchedule,
	"ghost":                 iconGhost,
	"sysmon-cpu":            iconGaugeCPU,
	"sysmon-memory":         iconGaugeMemory,
	"sysmon-gpu":            iconGaugeGPU,
	"ai-usage":              iconAIUsage,
	"battery-0":             iconBatteryLevel0,
	"battery-1":             iconBatteryLevel1,
	"battery-2":             iconBatteryLevel2,
	"battery-3":             iconBatteryLevel3,
	"battery-4":             iconBatteryLevel4,
	"battery-5":             iconBatteryLevel5,
	"battery-6":             iconBatteryLevel6,
	"battery-charging-0":    iconBatteryCharging0,
	"battery-charging-1":    iconBatteryCharging1,
	"battery-charging-2":    iconBatteryCharging2,
	"battery-charging-3":    iconBatteryCharging3,
	"battery-charging-4":    iconBatteryCharging4,
	"battery-charging-5":    iconBatteryCharging5,
	"battery-charging-6":    iconBatteryCharging6,
	"battery-critical":      iconBatteryCritical,
	"network":               iconNetwork,
	"smartphone":            iconSmartphone,
	"phonelink-off":         iconPhonelinkOff,
	"tablet":                iconTablet,
	"laptop":                iconLaptop,
	"desktop-windows":       iconDesktopWindows,
	"tv":                    iconTV,
	"devices":               iconDevices,
	"phone-in-talk":         iconPhoneInTalk,
	"folder-open":           iconFolderOpen,
	"content-paste":         iconContentPaste,
	"share":                 iconShare,
	"sms":                   iconSMS,
	"notifications-active":  iconNotificationsActive,
	"refresh":               iconRefresh,
	"5g":                    icon5G,
	"4g-mobiledata":         icon4GMobiledata,
	"3g-mobiledata":         icon3GMobiledata,
	"g-mobiledata":          iconGMobiledata,
	"signal-cellular-null":  iconSignalCellularNull,
	"signal-cellular-1-bar": iconSignalCellular1Bar,
	"signal-cellular-2-bar": iconSignalCellular2Bar,
	"signal-cellular-3-bar": iconSignalCellular3Bar,
	"signal-cellular-4-bar": iconSignalCellular4Bar,
	"cross":                 iconCross,
	"docker":                iconDocker,
}

func init() {
	r := catRuneFirst
	for _, act := range catActs {
		for i := 0; i < act.poses; i++ {
			iconNames[fmt.Sprintf("cat-%s-%d", act.name, i)] = r
			r++
		}
	}
	if r-1 != catRuneLast {
		panic(fmt.Sprintf("render: cat acts end at %U, the band at %U", r-1, catRuneLast))
	}
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
