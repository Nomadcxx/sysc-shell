package settings

import "github.com/Nomadcxx/sysc-shell/internal/config"

// widgetNames gives each item id a label a person would recognise. The ids are
// configuration tokens chosen for the file, and several read poorly in an
// interface: "window-title" is not a phrase, "block" says nothing on its own,
// and "wifi" and "network" are two different widgets whose ids do not say how
// they differ.
//
// Every id in the vocabulary has an entry, asserted by a test, because a widget
// added later without a label would otherwise render in the editor as its raw
// token and nobody would notice until it shipped.
var widgetNames = map[string]string{
	"battery":       "Battery",
	"block":         "Disk activity",
	"bluetooth":     "Bluetooth",
	"clipboard":     "Clipboard",
	"clock":         "Clock",
	"cpu":           "Processor",
	"filesystem":    "Disk usage",
	"gpu":           "Graphics",
	"launcher":      "Launcher",
	"media":         "Now playing",
	"memory":        "Memory",
	"network":       "Network throughput",
	"notifications": "Notifications",
	"running-apps":  "Running apps",
	"temperature":   "Temperature",
	"volume":        "Volume",
	"wallpaper":     "Wallpaper",
	"weather":       "Weather",
	"wifi":          "Connectivity",
	"window-title":  "Window title",
	"wordmark":      "Wordmark",
	"workspace":     "Workspaces",
}

// WidgetName is the label the editor shows for one item. A group is named for
// what it is; a plugin placement is named by its entry, which is the only part
// of it that means anything to a person, and falls back to the raw id so an
// unknown token is visible rather than blank.
func WidgetName(it config.Item) string {
	switch it.ID {
	case "group":
		return "Group"
	case "plugin":
		if it.Entry != "" {
			return it.Entry
		}
		return "Plugin"
	}
	if name, ok := widgetNames[it.ID]; ok {
		return name
	}
	return it.ID
}
