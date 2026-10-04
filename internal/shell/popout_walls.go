package shell

import (
	"fmt"
	"slices"
	"strings"

	"github.com/Nomadcxx/sysc-shell/internal/config"
	"github.com/Nomadcxx/sysc-shell/internal/settings"
	"github.com/Nomadcxx/sysc-shell/internal/theme"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
	"github.com/Nomadcxx/sysc-shell/internal/walls"
)

var screensaverEffects = []string{
	"matrix", "matrix-art", "fire", "fire-text", "fireworks", "rain", "rain-art",
	"beams", "beam-text", "decrypt", "pour", "aquarium", "print", "blackhole", "ring-text",
}

var screensaverThemes = []string{
	"dracula", "gruvbox", "nord", "tokyo-night", "catppuccin", "material", "solarized",
	"monochrome", "trainsishardjob", "rama", "eldritch", "dark",
}

var screensaverPositions = []string{"top", "center", "bottom"}

func screensaverSettingsBody(r *Registry, h *PanelHost) *ui.Node {
	snapshot := walls.Snapshot{}
	locked := false
	caffeine := false
	if r != nil {
		snapshot = r.wallsSnapshot
		locked = r.lockerAcquired
		caffeine = r.inhibitWanted
	}
	ensureWallsDraft(h, snapshot)
	syncWallsDraft(h, snapshot)

	lead := []*ui.Node{
		{Kind: ui.KindText, Text: wallsServiceStatus(snapshot), Name: "Screensaver service status", TextRole: theme.RoleBody},
		{Kind: ui.KindText, Text: wallsConfigStatus(snapshot), TextRole: theme.RoleCaption, Tone: ui.ToneSubtle},
		{Kind: ui.KindText, Text: "The screensaver runs on its own idle timeout. Shell blank and suspend timers still apply.", TextRole: theme.RoleCaption, Tone: ui.ToneSubtle},
		{Kind: ui.KindText, Text: "Display coverage is unknown until sysc-walls reports it.", TextRole: theme.RoleCaption, Tone: ui.ToneSubtle},
	}
	if locked {
		lead = append(lead, &ui.Node{Kind: ui.KindText, Text: "Settings and Preview are unavailable while locked.", Tone: ui.ToneError})
	}
	if caffeine && snapshot.Running() {
		lead = append(lead, &ui.Node{Kind: ui.KindText, Text: "Caffeine does not pause the screensaver.", TextRole: theme.RoleCaption, Tone: ui.ToneSubtle})
	}
	if snapshot.Previewing && wallsDraftIsDirty(h) {
		lead = append(lead, &ui.Node{Kind: ui.KindText, Text: "Stop Preview before applying settings.", TextRole: theme.RoleCaption, Tone: ui.ToneSubtle})
	}
	if snapshot.ActionPending {
		message := "Updating screensaver service…"
		if snapshot.PreviewStopping {
			message = "Stopping Preview session…"
		} else if snapshot.Previewing && !snapshot.PreviewReady {
			message = "Starting Preview session…"
		}
		lead = append(lead, &ui.Node{Kind: ui.KindText, Text: message, TextRole: theme.RoleCaption})
	} else if snapshot.ActionMessage != "" {
		lead = append(lead, &ui.Node{Kind: ui.KindText, Text: snapshot.ActionMessage, TextRole: theme.RoleCaption})
	}
	for _, problem := range []string{snapshot.ActionError, snapshot.PreviewError} {
		if problem != "" {
			lead = append(lead, &ui.Node{Kind: ui.KindText, Text: problem, Tone: ui.ToneError})
		}
	}
	if !snapshot.CanApply {
		lead = append(lead, &ui.Node{Kind: ui.KindText, Text: "Apply unavailable: " + wallsCapabilityReason(snapshot, "apply"), TextRole: theme.RoleCaption, Tone: ui.ToneSubtle})
	}
	if !snapshot.CanPreview && !snapshot.Previewing {
		lead = append(lead, &ui.Node{Kind: ui.KindText, Text: "Preview unavailable: " + wallsCapabilityReason(snapshot, "preview"), TextRole: theme.RoleCaption, Tone: ui.ToneSubtle})
	}
	if snapshot.Previewing && snapshot.PreviewReady {
		lead = append(lead, &ui.Node{Kind: ui.KindText, Text: "Preview process is active; display coverage is unknown.", TextRole: theme.RoleCaption})
	}

	entries := screensaverEntries(h)
	content := settingsPageColumn(h, entries, lead...)
	serviceCard := screensaverServiceCard(h, snapshot, locked)
	content.Children = append([]*ui.Node{serviceCard}, content.Children...)
	actions := screensaverActions(h, snapshot, locked)
	content.Children = append(content.Children, actions)
	if locked {
		setScreensaverDraftControlsDisabled(content)
	}
	return content
}

func setScreensaverDraftControlsDisabled(root *ui.Node) {
	var walk func(*ui.Node)
	walk = func(n *ui.Node) {
		if n == nil {
			return
		}
		if strings.HasPrefix(n.Action, "set:walls.") || strings.HasPrefix(n.Action, "pick:walls.") || strings.HasPrefix(n.Action, "reset:walls.") {
			n.State |= ui.StateDisabled
			n.AriaDisabled = true
		}
		for _, child := range n.Children {
			walk(child)
		}
	}
	walk(root)
}

func screensaverEntries(h *PanelHost) []settings.Entry {
	get := func(key string) settings.Getter {
		return func(config.Config) string { return h.wallsDraft[key] }
	}
	return []settings.Entry{
		screensaverChoiceEntry(h, get, "effect", "Effect", "Choose the animation used by sysc-walls.", "Appearance", screensaverEffects),
		screensaverChoiceEntry(h, get, "theme", "Theme", "Choose the animation palette.", "Appearance", screensaverThemes),
		{Path: "walls.file", Label: "Artwork", Describe: "Enter a readable image path under ~/.local/share, ~/.config, /usr/share, or /usr/local/share. Leave empty to use built-in artwork.", Section: "Screensaver", Group: "Appearance", Kind: settings.KindString, Get: get("file")},
		screensaverBooleanEntry(h, get),
		screensaverChoiceEntry(h, get, "datetime-position", "Date and time position", "Choose where date and time appears.", "Appearance", screensaverPositions),
		{Path: "walls.timeout", Label: "Idle timeout", Describe: "Whole seconds, minutes, or hours; maximum 24 hours (for example, 5m).", Section: "Screensaver", Group: "Timing", Kind: settings.KindString, Get: get("timeout")},
	}
}

func screensaverChoiceEntry(h *PanelHost, get func(string) settings.Getter, key, label, description, group string, options []string) settings.Entry {
	kind := settings.KindEnum
	if value := h.wallsDraft[key]; !slices.Contains(options, value) {
		kind = settings.KindString
		description = fmt.Sprintf("Saved value %q is unsupported. Enter one of: %s.", value, strings.Join(options, ", "))
	}
	return settings.Entry{
		Path: "walls." + key, Label: label, Describe: description, Section: "Screensaver",
		Group: group, Kind: kind, Options: options, Get: get(key),
	}
}

func screensaverBooleanEntry(h *PanelHost, get func(string) settings.Getter) settings.Entry {
	value := h.wallsDraft["datetime"]
	entry := settings.Entry{
		Path: "walls.datetime", Label: "Show date and time", Section: "Screensaver",
		Group: "Appearance", Kind: settings.KindBool, Get: get("datetime"),
	}
	if value != "true" && value != "false" {
		entry.Kind = settings.KindString
		entry.Describe = fmt.Sprintf("Saved value %q is invalid. Enter true or false to correct it.", value)
	}
	return entry
}

func screensaverServiceCard(h *PanelHost, snapshot walls.Snapshot, locked bool) *ui.Node {
	enabled := snapshot.EnabledAtLogin()
	allowed := snapshot.ServiceAvailable && snapshot.UnitKnown && !snapshot.UnitStale && !snapshot.Previewing &&
		(snapshot.UnitFileState == "enabled" || snapshot.UnitFileState == "disabled") &&
		!snapshot.ActionPending && !locked
	toggle := &ui.Node{
		Kind: ui.KindToggle, Value: wallsBoolValue(enabled), Action: "walls:enable",
		Focusable: true, Name: "Enable screensaver", Role: "switch",
	}
	if !allowed {
		toggle.State |= ui.StateDisabled
		toggle.AriaDisabled = true
	}
	row := &ui.Node{Kind: ui.KindRow, PinEnd: true, Children: []*ui.Node{
		{Kind: ui.KindColumn, Width: max(settingsBodyWidth(h)-h.metrics().BaseWidget-theme.MarginL, 0), Children: []*ui.Node{
			{Kind: ui.KindText, Text: "Enable screensaver", Name: "Enable screensaver"},
			{Kind: ui.KindText, Text: wallsUnitEnabledState(snapshot) + " · " + wallsUnitRunningState(snapshot), TextRole: theme.RoleCaption, Tone: ui.ToneSubtle},
		}},
		toggle,
	}}
	children := []*ui.Node{row}
	if snapshot.Previewing {
		children = append(children, &ui.Node{Kind: ui.KindText, Text: "Stop Preview before changing service enablement.", TextRole: theme.RoleCaption, Tone: ui.ToneSubtle})
	}
	return settingsGroupCard(h, "Service", children)
}

func screensaverActions(h *PanelHost, snapshot walls.Snapshot, locked bool) *ui.Node {
	dirty := wallsDraftIsDirty(h)
	applyDisabled := locked || !snapshot.CanApply || snapshot.ActionPending || snapshot.Previewing || !dirty
	apply := screensaverButton(h, "Apply", "walls:apply", applyDisabled)
	reset := screensaverButton(h, "Reset", "walls:reset", locked || snapshot.ActionPending || !dirty)
	previewLabel, previewAction, previewName := "Preview", "walls:preview", "Preview"
	previewDisabled := locked || snapshot.ActionPending || !snapshot.CanPreview
	if dirty {
		previewLabel, previewName = "Preview saved settings", "Preview saved settings"
	}
	if snapshot.Previewing {
		previewLabel, previewName = "Stop preview", "Stop preview"
		previewAction = "walls:stop"
		previewDisabled = locked || snapshot.PreviewStopping
	}
	preview := screensaverButton(h, previewLabel, previewAction, previewDisabled)
	preview.Name = previewName
	return settingsGroupCard(h, "Actions", []*ui.Node{{
		Kind: ui.KindRow, Gap: theme.MarginS, Children: []*ui.Node{preview, reset, apply},
	}})
}

func screensaverButton(h *PanelHost, text, action string, disabled bool) *ui.Node {
	n := &ui.Node{
		Kind: ui.KindButton, Text: text, Action: action, Name: text, Role: "button",
		Focusable: true, Height: h.metrics().StandardControl, Fill: ui.FillOutline, Shape: ui.ShapeMedium,
	}
	if disabled {
		n.State |= ui.StateDisabled
		n.AriaDisabled = true
	}
	return n
}

func wallsBoolValue(value bool) float64 {
	if value {
		return 1
	}
	return 0
}
