package shell

import (
	"fmt"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/Nomadcxx/sysc-shell/internal/config"
	"github.com/Nomadcxx/sysc-shell/internal/displays"
	"github.com/Nomadcxx/sysc-shell/internal/settings"
	"github.com/Nomadcxx/sysc-shell/internal/theme"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

// displaysSettingsBody is the one scrolling column of the Display section: what
// the compositor is running, one card per output, and the actions that can
// change it. The edits in the cards are a draft; nothing reaches the
// compositor until Preview arms a transaction, and only Keep persists.
func displaysSettingsBody(r *Registry, h *PanelHost) *ui.Node {
	if r == nil {
		return settingsBody(h, theme.MarginL, settingsEmptyNote(displaysSection))
	}
	snapshot := r.displaysSnapshot
	syncDisplaysDraftLocked(r, snapshot)

	content := settingsPageColumn(h, displaysEntries(r, snapshot), displaysLead(r, snapshot)...)
	content.Children = append(content.Children, displaysActions(h, r, snapshot))
	if r.displaysService == nil {
		content.Children = append([]*ui.Node{displaysAdoptCard(h, r)}, content.Children...)
	}
	return content
}

// displaysLead says where the configuration is and what is true right now. A
// failure never sits next to a claim that the settings were saved: the service's
// note is dropped the moment this command failed, so the page cannot read
// "Saved" over a refusal.
func displaysLead(r *Registry, snapshot displays.Snapshot) []*ui.Node {
	lead := []*ui.Node{{
		Kind: ui.KindText, Text: displaysRootLine(r), Name: "Niri configuration",
		TextRole: theme.RoleBody,
	}}
	if why := displaysRootWhy(r); why != "" {
		lead = append(lead, &ui.Node{Kind: ui.KindText, Text: why, TextRole: theme.RoleCaption, Tone: ui.ToneSubtle})
	}
	if r.displaysService == nil {
		lead = append(lead, &ui.Node{
			Kind: ui.KindText, Text: displaysNotSetUpNote,
			TextRole: theme.RoleCaption, Tone: ui.ToneSubtle,
		})
	} else {
		lead = append(lead, &ui.Node{
			Kind:     ui.KindText,
			Text:     "Edits are a draft. Preview applies them live, Keep writes them to the niri configuration, and closing the page or waiting reverts them.",
			TextRole: theme.RoleCaption, Tone: ui.ToneSubtle,
		})
	}
	if r.displaysBusy {
		lead = append(lead, &ui.Node{Kind: ui.KindText, Text: "Working on the display settings…", TextRole: theme.RoleCaption})
	}
	if r.displaysErr != "" {
		lead = append(lead, &ui.Node{Kind: ui.KindText, Text: r.displaysErr, Tone: ui.ToneError, Name: "Display settings problem"})
	} else if snapshot.Note != "" {
		lead = append(lead, &ui.Node{Kind: ui.KindText, Text: snapshot.Note, TextRole: theme.RoleCaption, Tone: ui.ToneSubtle})
	}
	return lead
}

func displaysRootLine(r *Registry) string {
	switch {
	case r.displaysService != nil:
		return "Niri configuration: " + r.displaysService.Root()
	case r.displaysRoot != "":
		return "Niri configuration: " + r.displaysRoot
	case r.displaysDiscovery != nil:
		return "Proposed niri configuration: " + r.displaysDiscovery.Root
	default:
		return "Looking for the niri configuration…"
	}
}

func displaysRootWhy(r *Registry) string {
	if r.displaysService != nil {
		return "Display settings live in " + filepath.Join(filepath.Dir(r.displaysService.Root()), displays.ManagedFileName) +
			", which the file above includes. Your other output blocks stay where they are."
	}
	found := r.displaysDiscovery
	switch {
	case found == nil:
		return ""
	case found.ReadOnly:
		return found.Note
	case found.Confirm:
		return "Proposed from " + found.Source + ". Confirm it before anything is changed."
	default:
		return "Proposed from " + found.Source + "."
	}
}

// displaysAdoptCard is the only way the page edits the user's configuration,
// and it is behind a button: the root was read from the compositor's own
// process, not asked for, so the user is the one who confirms it.
func displaysAdoptCard(h *PanelHost, r *Registry) *ui.Node {
	found := r.displaysDiscovery
	children := []*ui.Node{{Kind: ui.KindText, Text: displaysNotSetUpNote, TextRole: theme.RoleCaption}}
	switch {
	case found == nil:
		children = append(children,
			displaysButton(h, "Look again", "displays:refresh", r.displaysBusy),
			&ui.Node{Kind: ui.KindText, Text: "Nothing has been changed.", TextRole: theme.RoleCaption, Tone: ui.ToneSubtle})
	case found.ReadOnly:
		children = append(children, &ui.Node{
			Kind:     ui.KindText,
			Text:     found.Note + " Start niri with -c pointing at a configuration this account can write.",
			TextRole: theme.RoleCaption, Tone: ui.ToneError,
		})
	default:
		children = append(children,
			&ui.Node{
				Kind: ui.KindText,
				Text: "Adopting adds one include line at the top of " + found.Root + " and creates " +
					filepath.Join(filepath.Dir(found.Root), displays.ManagedFileName) +
					" beside it. The new file starts as a comment, so adopting changes no output until you keep a preview.",
				TextRole: theme.RoleCaption, Tone: ui.ToneSubtle,
			},
			displaysButton(h, "Adopt this configuration", "displays:adopt", r.displaysBusy))
	}
	return settingsGroupCard(h, "Configuration", children)
}

// displaysActions is the transaction's own row. While a preview is armed it
// shows the countdown and asks: Revert first, so the keyboard starts on the
// answer that undoes the change, and Enter activates only the button the
// keyboard is on.
func displaysActions(h *PanelHost, r *Registry, snapshot displays.Snapshot) *ui.Node {
	busy := r.displaysBusy
	var children []*ui.Node
	if r.displaysArmed {
		revert := displaysButton(h, "Revert", "displays:revert", busy)
		revert.Name = "Revert preview"
		keep := displaysButton(h, "Keep", "displays:keep", busy)
		keep.Name = "Keep preview"
		children = append(children,
			&ui.Node{
				Kind: ui.KindText, Name: "Preview countdown",
				Text:     "Previewing. It reverts in " + displaysCountdown(snapshot.Remaining) + " unless you keep it.",
				TextRole: theme.RoleBody,
			},
			&ui.Node{
				Kind:     ui.KindText,
				Text:     "Escape, leaving this page or closing the panel reverts the preview.",
				TextRole: theme.RoleCaption, Tone: ui.ToneSubtle,
			},
			&ui.Node{Kind: ui.KindRow, Gap: theme.MarginS, Children: []*ui.Node{revert, keep}})
	} else {
		children = append(children, &ui.Node{Kind: ui.KindRow, Gap: theme.MarginS, Children: []*ui.Node{
			displaysButton(h, "Preview", "displays:preview", busy || r.displaysService == nil),
			displaysButton(h, "Discard changes", "displays:reset", busy || !displaysDraftDirty(r)),
			displaysButton(h, "Read again", "displays:refresh", busy),
		}})
	}
	return settingsGroupCard(h, "Changes", children)
}

func displaysCountdown(left time.Duration) string {
	if left <= 0 {
		return "a moment"
	}
	seconds := max(int(left.Round(time.Second)/time.Second), 1)
	if seconds == 1 {
		return "1 second"
	}
	return strconv.Itoa(seconds) + " seconds"
}

// displaysEntries is one card per output, in the order the service reported
// them. Every field is offered in explicit saved and automatic states, so an
// output that is not mapped, a mode the hardware no longer advertises and a
// scale outside the presets are all visible rather than guessed.
func displaysEntries(r *Registry, snapshot displays.Snapshot) []settings.Entry {
	entries := make([]settings.Entry, 0, len(snapshot.Outputs)*6)
	for _, out := range snapshot.Outputs {
		entries = append(entries,
			displaysModeEntry(r, out),
			displaysScaleEntry(r, out),
			displaysTransformEntry(r, out),
			displaysPositionEntry(r, out, displaysFieldPositionX, "position X", "left"),
			displaysPositionEntry(r, out, displaysFieldPositionY, "position Y", "top"),
			displaysVRREntry(r, out))
	}
	return entries
}

// displaysDraftValue reads one field of one connector's draft.
func displaysDraftValue(r *Registry, connector, field string) string {
	return r.displaysDraft[connector][field]
}

func displaysEntry(r *Registry, out displays.Output, field, label, describe string) settings.Entry {
	connector := out.Name.Connector
	return settings.Entry{
		Path:     "displays." + connector + "." + field,
		Label:    label,
		Describe: describe,
		Section:  displaysSection,
		Group:    connector,
		Kind:     settings.KindString,
		Get:      func(config.Config) string { return displaysDraftValue(r, connector, field) },
	}
}

func displaysModeEntry(r *Registry, out displays.Output) settings.Entry {
	connector := out.Name.Connector
	entry := settings.Entry{
		Path:  "displays." + connector + "." + displaysFieldMode,
		Label: connector + " resolution", Describe: "Resolution and refresh rate for " + connector + ".",
		Section: displaysSection, Group: connector, Kind: settings.KindEnum, Present: settings.PresentMenu,
		Get:          func(config.Config) string { return displaysDraftValue(r, connector, displaysFieldMode) },
		Options:      []string{displaysAuto},
		OptionLabels: []string{"Automatic"},
	}
	if out.Logical == nil {
		// An output that is not mapped reports no modes, so there is nothing
		// to choose from; a single option renders as a stated fact.
		entry.Options = []string{displaysAuto}
		entry.OptionLabels = []string{"Not available while " + connector + " is not mapped"}
		entry.Describe = "Connect and enable " + connector + " to choose a resolution."
		return entry
	}
	for _, mode := range out.Modes {
		value := displaysModeValue(&displays.ModePolicy{
			Width: mode.Width, Height: mode.Height, RefreshMilliHz: mode.RefreshMilliHz,
		})
		label := displaysModeLabel(mode)
		if mode.Preferred {
			label += " (preferred)"
		}
		entry.Options = append(entry.Options, value)
		entry.OptionLabels = append(entry.OptionLabels, label)
	}
	if saved := displaysDraftValue(r, connector, displaysFieldMode); !slices.Contains(entry.Options, saved) {
		// A saved value the hardware no longer advertises is never snapped to
		// another mode: it stays editable so the user can see it and fix it.
		entry.Kind = settings.KindString
		entry.Describe = fmt.Sprintf("Saved value %q is not one of the modes %s advertises. Enter WIDTHxHEIGHT@REFRESH, with the refresh rate in millihertz, or Automatic.", saved, connector)
	}
	return entry
}

func displaysScaleEntry(r *Registry, out displays.Output) settings.Entry {
	connector := out.Name.Connector
	entry := settings.Entry{
		Path:  "displays." + connector + "." + displaysFieldScale,
		Label: connector + " scale", Describe: "Fractional scale for " + connector + ".",
		Section: displaysSection, Group: connector, Kind: settings.KindEnum, Present: settings.PresentMenu,
		Get:          func(config.Config) string { return displaysDraftValue(r, connector, displaysFieldScale) },
		Options:      []string{displaysAuto},
		OptionLabels: []string{"Automatic"},
	}
	for _, preset := range displays.ScalePresets {
		value := displaysScaleValue(&preset)
		entry.Options = append(entry.Options, value)
		entry.OptionLabels = append(entry.OptionLabels, value+"×")
	}
	if saved := displaysDraftValue(r, connector, displaysFieldScale); saved != displaysAuto && !slices.Contains(entry.Options, saved) {
		// The presets are a convenience, not a cage: a saved 1.3 stays 1.3.
		entry.Kind = settings.KindString
		entry.Describe = "Saved scale " + saved + " is not a preset. Enter a number, or Automatic."
	}
	return entry
}

func displaysTransformEntry(r *Registry, out displays.Output) settings.Entry {
	connector := out.Name.Connector
	entry := settings.Entry{
		Path:  "displays." + connector + "." + displaysFieldTransform,
		Label: connector + " rotation", Describe: "How the picture is turned for " + connector + ".",
		Section: displaysSection, Group: connector, Kind: settings.KindEnum, Present: settings.PresentMenu,
		Get:          func(config.Config) string { return displaysDraftValue(r, connector, displaysFieldTransform) },
		Options:      []string{displaysAuto},
		OptionLabels: []string{"Automatic"},
	}
	for _, name := range displays.Transforms {
		entry.Options = append(entry.Options, name)
		entry.OptionLabels = append(entry.OptionLabels, displaysTransformLabel(name))
	}
	if saved := displaysDraftValue(r, connector, displaysFieldTransform); saved != displaysAuto && !slices.Contains(entry.Options, saved) {
		entry.Kind = settings.KindString
		entry.Describe = "Saved rotation " + saved + " is not one niri can apply. Enter one of: " + strings.Join(displays.Transforms, ", ") + "."
	}
	return entry
}

func displaysPositionEntry(r *Registry, out displays.Output, field, which, edge string) settings.Entry {
	connector := out.Name.Connector
	return displaysEntry(r, out, field,
		connector+" "+which,
		"Logical pixels from the "+edge+" of the arrangement. Both coordinates, or Automatic for both.")
}

func displaysVRREntry(r *Registry, out displays.Output) settings.Entry {
	connector := out.Name.Connector
	entry := settings.Entry{
		Path:  "displays." + connector + "." + displaysFieldVRR,
		Label: connector + " variable refresh", Describe: "Let " + connector + " vary its refresh rate.",
		Section: displaysSection, Group: connector, Kind: settings.KindEnum, Present: settings.PresentMenu,
		Get:          func(config.Config) string { return displaysDraftValue(r, connector, displaysFieldVRR) },
		Options:      []string{"false", "true", displaysVRROnDemand},
		OptionLabels: []string{"Off", "On", "On demand"},
	}
	if !out.VRRSupported {
		// One option renders as a stated fact rather than a control that
		// cannot be used, and the description says why.
		entry.Options = []string{"false"}
		entry.OptionLabels = []string{"Not available on " + connector}
		entry.Describe = "The hardware on " + connector + " cannot vary its refresh rate."
	}
	return entry
}

// displaysTransformLabel is how a rotation reads on screen. niri spells the
// eight transforms "Normal", "90", "Flipped" and "Flipped270"; a person wants
// degrees.
func displaysTransformLabel(name string) string {
	if name == "Normal" {
		return "Normal"
	}
	flipped := strings.HasPrefix(name, "Flipped")
	angle := strings.TrimPrefix(name, "Flipped")
	if angle == "" {
		return "Flipped"
	}
	if flipped {
		return "Flipped " + angle + "°"
	}
	return angle + "°"
}

// activateDisplaysAction runs the page's own commands. Each one either starts
// a command off the Wayland owner or refuses in one line; none of them touches
// the compositor from here.
func activateDisplaysAction(r *Registry, h *PanelHost, n *ui.Node) bool {
	if r == nil || h == nil || n == nil {
		return false
	}
	switch n.Action {
	case "displays:preview":
		ui.Activate(n)
		r.displaysPreviewLocked(h)
		return true
	case "displays:keep":
		ui.Activate(n)
		r.displaysKeepLocked(h)
		return true
	case "displays:revert":
		ui.Activate(n)
		r.displaysRevertLocked(h)
		return true
	case "displays:reset":
		ui.Activate(n)
		r.displaysDraft = nil
		syncDisplaysDraftLocked(r, r.displaysSnapshot)
		r.displaysErr = ""
		r.rebuildPanel(h)
		return true
	case "displays:refresh":
		ui.Activate(n)
		r.displaysRefreshLocked(h)
		return true
	case "displays:adopt":
		ui.Activate(n)
		r.displaysAdoptLocked(h)
		return true
	default:
		return false
	}
}

// displaysSearchEntries adds the live output rows to a settings search. They are
// not config entries, so a search for an output name or "VRR" would otherwise
// find nothing and look like the page did not exist.
func displaysSearchEntries(r *Registry, query string) []settings.Entry {
	if r == nil || r.displaysService == nil {
		return nil
	}
	needle := strings.ToLower(strings.TrimSpace(query))
	if needle == "" {
		return nil
	}
	entries := displaysEntries(r, r.displaysSnapshot)
	var hits []settings.Entry
	for _, e := range entries {
		// The path carries the field name, so a search for "vrr" or
		// "position.y" reaches the row as well as a search for its label.
		hay := strings.ToLower(e.Path + " " + e.Label + " " + e.Describe)
		if strings.Contains(hay, needle) {
			hits = append(hits, e)
		}
	}
	return hits
}

func displaysDraftDirty(r *Registry) bool {
	for _, fields := range r.displaysDraft {
		for _, value := range fields {
			if value != "" {
				return true
			}
		}
	}
	return false
}

// displaysButton is the page's action button, disabled with the shell's own
// disabled semantics when the command cannot run.
func displaysButton(h *PanelHost, text, action string, disabled bool) *ui.Node {
	n := settingsButton(h, text, action)
	if disabled {
		n.State |= ui.StateDisabled
		n.AriaDisabled = true
	}
	return n
}
