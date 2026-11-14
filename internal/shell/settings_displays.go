package shell

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/Nomadcxx/sysc-shell/internal/config"
	"github.com/Nomadcxx/sysc-shell/internal/displays"
	"github.com/Nomadcxx/sysc-shell/internal/platform/compositor"
	"github.com/Nomadcxx/sysc-shell/internal/render"
	"github.com/Nomadcxx/sysc-shell/internal/settings"
	"github.com/Nomadcxx/sysc-shell/internal/theme"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

// displaysDiagramW and displaysDiagramH are the arrangement drawing's frame.
// It is a picture of relative placement rather than chrome, so its size is a
// fixed content box instead of a density-row measurement.
const (
	displaysDiagramW = 360
	displaysDiagramH = 100
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

	selected := displaysSelectedOutput(r, snapshot)
	lead := displaysLead(r, snapshot)
	if len(snapshot.Outputs) > 0 {
		lead = append(lead, displaysOutputSelector(h, r, snapshot, selected))
		if r.displaysDiagram != nil {
			lead = append(lead, &ui.Node{Kind: ui.KindImage, Image: r.displaysDiagram, ImageW: displaysDiagramW, ImageH: displaysDiagramH, Name: "Connected monitor arrangement", Role: "img"})
		}
		for _, out := range snapshot.Outputs {
			if out.Logical != nil {
				lead = append(lead, &ui.Node{Kind: ui.KindText, Text: fmt.Sprintf("%s: %d × %d at %d, %d", out.Name.Connector, out.Logical.Width, out.Logical.Height, out.Logical.X, out.Logical.Y), TextRole: theme.RoleCaption})
			}
		}
	}
	var entries []settings.Entry
	for _, e := range displaysEntries(r, snapshot) {
		// Display entries are page-local, so reseedSettingsFields never reaches
		// their fields: a preset, a discard or a new snapshot must still show.
		if f := h.fields[e.Path]; f != nil && e.Get != nil {
			if value := e.Get(config.Config{}); f.Text != value {
				f.SyncFrom(&ui.Node{Text: value, Cursor: len(value)})
			}
		}
		if e.Group == selected {
			entries = append(entries, e)
		}
	}
	content := settingsPageColumn(h, entries, lead...)
	content.Children = append(content.Children, displaysActions(h, r, snapshot))
	if r.displaysService == nil {
		content.Children = append([]*ui.Node{displaysAdoptCard(h, r)}, content.Children...)
	}
	return content
}

func displaysSelectedOutput(r *Registry, snapshot displays.Snapshot) string {
	for _, out := range snapshot.Outputs {
		if out.Name.Connector == r.displaysSelected {
			return r.displaysSelected
		}
	}
	if len(snapshot.Outputs) > 0 {
		r.displaysSelected = snapshot.Outputs[0].Name.Connector
	}
	return r.displaysSelected
}

func displaysOutputSelector(h *PanelHost, r *Registry, snapshot displays.Snapshot, selected string) *ui.Node {
	e := settings.Entry{Path: "displays.selected", Label: "Display", Kind: settings.KindEnum, Present: settings.PresentMenu, Get: func(config.Config) string { return selected }}
	for _, out := range snapshot.Outputs {
		e.Options = append(e.Options, out.Name.Connector)
		identity := strings.TrimSpace(out.Name.Make + " " + out.Name.Model)
		if identity != "" {
			identity += " · "
		}
		e.OptionLabels = append(e.OptionLabels, identity+out.Name.Connector)
	}
	if len(e.Options) == 1 {
		return &ui.Node{Kind: ui.KindText, Text: e.OptionLabels[0], Name: e.OptionLabels[0], Role: "heading", TextRole: theme.RoleTitle}
	}
	return settingsEntryRow(h, e, settingsBodyWidth(h))
}

// Snapshot relays build this immutable thumbnail off the Wayland owner. It
// depicts observed logical rectangles; editing uses labelled fields below.
func displaysArrangementImage(outputs []displays.Output, p Palette) *ui.Image {
	var minX, minY, maxX, maxY int64
	first := true
	for _, out := range outputs {
		if out.Logical == nil || out.Logical.Width <= 0 || out.Logical.Height <= 0 {
			continue
		}
		r := out.Logical
		x, y := int64(r.X), int64(r.Y)
		if first {
			minX, minY, maxX, maxY = x, y, x+int64(r.Width), y+int64(r.Height)
			first = false
		} else {
			minX = min(minX, x)
			minY = min(minY, y)
			maxX = max(maxX, x+int64(r.Width))
			maxY = max(maxY, y+int64(r.Height))
		}
	}
	if first {
		return nil
	}
	const w, h = displaysDiagramW, displaysDiagramH
	im := &ui.Image{Width: w, Height: h, Stride: w * 4, Pix: make([]byte, w*h*4)}
	c := render.Canvas{Width: im.Width, Height: im.Height, Stride: im.Stride, Pix: im.Pix}
	c.FillRounded(ui.Rect{W: w, H: h}, 8, p.SurfaceContainer)
	scale := math.Min(float64(w-24)/float64(maxX-minX), float64(h-24)/float64(maxY-minY))
	for _, out := range outputs {
		if out.Logical == nil || out.Logical.Width <= 0 || out.Logical.Height <= 0 {
			continue
		}
		r := out.Logical
		box := ui.Rect{X: 12 + int(float64(int64(r.X)-minX)*scale), Y: 12 + int(float64(int64(r.Y)-minY)*scale), W: max(2, int(float64(r.Width)*scale)), H: max(2, int(float64(r.Height)*scale))}
		c.FillRounded(box, 3, p.SurfaceContainerHighest)
		c.StrokeRounded(box, 3, 2, p.Outline)
	}
	return im
}

// displaysLead says where the configuration is and what is true right now. A
// failure never sits next to a claim that the settings were saved: the service's
// note is dropped the moment this command failed, so the page cannot read
// "Saved" over a refusal.
func displaysLead(r *Registry, snapshot displays.Snapshot) []*ui.Node {
	lead := []*ui.Node{{
		Kind: ui.KindText, Text: displaysRootLine(r), Name: "Compositor configuration",
		TextRole: theme.RoleBody,
	}}
	if why := displaysRootWhy(r); why != "" {
		lead = append(lead, &ui.Node{Kind: ui.KindText, Text: why, TextRole: theme.RoleCaption, Tone: ui.ToneSubtle})
	}
	// Without a service the adopt card says the page is not set up.
	if r.displaysService != nil {
		lead = append(lead, &ui.Node{
			Kind:     ui.KindText,
			Text:     "Preview applies your draft temporarily. Keep saves it to the compositor configuration; Revert or the timeout restores the previous settings.",
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
	name := "Niri"
	if compositor.Detect(os.Getenv) == compositor.SyscCompositor {
		name = "sysc-compositor"
	}
	switch {
	case r.displaysService != nil:
		return name + " configuration: " + r.displaysService.Root()
	case r.displaysRoot != "":
		return name + " configuration: " + r.displaysRoot
	case r.displaysDiscovery != nil:
		return "Proposed " + name + " configuration: " + r.displaysDiscovery.Root
	default:
		return "Looking for the " + name + " configuration…"
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
			Text:     found.Note + " Start the compositor with -c pointing at a configuration this account can write.",
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
	} else if u := snapshot.Unresolved; u != nil {
		children = append(children, displaysUnresolved(h, u, busy)...)
	} else {
		children = append(children, &ui.Node{Kind: ui.KindRow, Gap: theme.MarginS, Children: []*ui.Node{
			displaysButton(h, "Preview changes", "displays:preview", busy || r.displaysService == nil || !displaysDraftDirty(r) || displaysDraftInvalid(r)),
			displaysButton(h, "Discard changes", "displays:reset", busy || !displaysDraftDirty(r)),
			displaysButton(h, "Read again", "displays:refresh", busy),
		}})
	}
	return settingsGroupCard(h, "Changes", children)
}

// displaysUnresolved is the way past a record no command of this shell is
// driving. Preview would only be refused, so it is not offered; the one action
// either runs the recovery the timeout would have run or, for a record that
// does not load, moves it aside where it stays for inspection.
func displaysUnresolved(h *PanelHost, u *displays.Unresolved, busy bool) []*ui.Node {
	lead, label, how := "An earlier display change is not finished ("+u.State+"), so Preview is unavailable.",
		"Restore previous settings",
		"Restoring runs the same recovery the timeout would. If it cannot finish, the record is moved aside and the displays keep their current settings."
	if u.Unreadable {
		lead, label, how = "A display record cannot be read, so Preview is unavailable.",
			"Move the record aside",
			"Moving it aside keeps it for inspection and changes nothing on the displays."
	}
	nodes := []*ui.Node{
		{Kind: ui.KindText, Name: "Unresolved display change", Text: lead, TextRole: theme.RoleBody},
		{Kind: ui.KindText, Text: how, TextRole: theme.RoleCaption, Tone: ui.ToneSubtle},
	}
	if u.Note != "" {
		nodes = append(nodes, &ui.Node{Kind: ui.KindText, Text: "Last reason: " + u.Note, TextRole: theme.RoleCaption, Tone: ui.ToneSubtle})
	}
	return append(nodes, &ui.Node{Kind: ui.KindRow, Gap: theme.MarginS, Children: []*ui.Node{
		displaysButton(h, label, "displays:resolve", busy),
		displaysButton(h, "Read again", "displays:refresh", busy),
	}})
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
			displaysRefreshEntry(r, out),
			displaysScaleEntry(r, out),
			displaysCustomScaleEntry(r, out),
			displaysTransformEntry(r, out),
			displaysPositionEntry(r, out, displaysFieldPositionX, "position X", "left"),
			displaysPositionEntry(r, out, displaysFieldPositionY, "position Y", "top"),
			displaysVRREntry(r, out))
		if len(snapshot.Outputs) > 1 && out.Logical != nil {
			entries = append(entries, displaysPlacementEntry(r, out, snapshot))
		}
	}
	// The group is the connector, an identifier the card must not lowercase.
	for i := range entries {
		entries[i].GroupLiteral = true
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
		Label: connector + " resolution", Describe: "Advertised pixel dimensions for " + connector + ".",
		Section: displaysSection, Group: connector, Kind: settings.KindEnum, Present: settings.PresentMenu,
		Get: func(config.Config) string {
			size, _, _ := strings.Cut(displaysDraftValue(r, connector, displaysFieldMode), "@")
			return size
		},
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
		value := fmt.Sprintf("%dx%d", mode.Width, mode.Height)
		if slices.Contains(entry.Options, value) {
			continue
		}
		label := fmt.Sprintf("%d × %d", mode.Width, mode.Height)
		if mode.Preferred {
			label += " (preferred)"
		}
		entry.Options = append(entry.Options, value)
		entry.OptionLabels = append(entry.OptionLabels, label)
	}
	if saved := entry.Get(config.Config{}); saved != "" && !slices.Contains(entry.Options, saved) {
		entry.Options = append(entry.Options, saved)
		entry.OptionLabels = append(entry.OptionLabels, saved+" (saved; unavailable)")
		entry.Describe = "The saved resolution is not currently advertised. Select an advertised resolution to change it."
	}
	return entry
}

func displaysRefreshEntry(r *Registry, out displays.Output) settings.Entry {
	connector := out.Name.Connector
	e := settings.Entry{Path: "displays." + connector + "." + displaysFieldRefresh, Label: connector + " refresh rate", Describe: "Exact rates advertised for the selected resolution.", Section: displaysSection, Group: connector, Kind: settings.KindEnum, Present: settings.PresentMenu,
		Get: func(config.Config) string {
			_, rate, ok := strings.Cut(displaysDraftValue(r, connector, displaysFieldMode), "@")
			if !ok {
				return displaysAuto
			}
			return rate
		}, Options: []string{displaysAuto}, OptionLabels: []string{"Automatic (highest)"}}
	size, _, _ := strings.Cut(displaysDraftValue(r, connector, displaysFieldMode), "@")
	for _, mode := range out.Modes {
		if fmt.Sprintf("%dx%d", mode.Width, mode.Height) != size {
			continue
		}
		value := strconv.FormatUint(uint64(mode.RefreshMilliHz), 10)
		if slices.Contains(e.Options, value) {
			continue
		}
		e.Options = append(e.Options, value)
		e.OptionLabels = append(e.OptionLabels, displaysRefreshLabel(mode.RefreshMilliHz))
	}
	if size == displaysAuto || size == "" {
		e.OptionLabels[0] = "Automatic with resolution"
	}
	return e
}

func displaysCustomScaleEntry(r *Registry, out displays.Output) settings.Entry {
	e := displaysEntry(r, out, displaysFieldScaleCustom, out.Name.Connector+" custom scale", "Enter a scale greater than zero and below 10, or auto. Presets remain available above.")
	e.Get = func(config.Config) string { return displaysDraftValue(r, out.Name.Connector, displaysFieldScale) }
	return e
}

func displaysPlacementEntry(r *Registry, out displays.Output, snapshot displays.Snapshot) settings.Entry {
	connector := out.Name.Connector
	e := settings.Entry{Path: "displays." + connector + "." + displaysFieldPlacement, Label: connector + " arrangement", Describe: "Relative placements align top edges left/right, or left edges above/below. Numeric X/Y below support other alignments.", Section: displaysSection, Group: connector, Kind: settings.KindEnum, Present: settings.PresentMenu,
		Get: func(config.Config) string {
			if v := displaysDraftValue(r, connector, displaysFieldPlacement); v != "" {
				return v
			}
			if p, _ := displaysPositionFrom(r.displaysDraft[connector]); p != nil {
				return "numeric"
			}
			return displaysAuto
		}, Options: []string{displaysAuto, "numeric"}, OptionLabels: []string{"Automatic", "Numeric X / Y"}}
	for _, other := range snapshot.Outputs {
		if other.Name.Connector == connector || other.Logical == nil {
			continue
		}
		for _, side := range []string{"left", "right", "above", "below"} {
			e.Options = append(e.Options, side+":"+other.Name.Connector)
			e.OptionLabels = append(e.OptionLabels, side+" of "+other.Name.Connector)
		}
	}
	return e
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
	case "displays:resolve":
		ui.Activate(n)
		if svc := r.displaysService; svc != nil && !r.displaysArmed {
			r.displaysCommandLocked(h, "resolve", svc.Resolve)
		}
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
	for connector, fields := range r.displaysDraft {
		baseline := map[string]string{}
		for _, out := range r.displaysSnapshot.Outputs {
			if out.Name.Connector == connector {
				seedDisplaysDraft(baseline, out.Policy)
				break
			}
		}
		if !reflect.DeepEqual(fields, baseline) {
			return true
		}
	}
	return false
}

func displaysDraftInvalid(r *Registry) bool {
	target, err := displaysTarget(r.displaysDraft)
	if err != nil {
		return true
	}
	for _, out := range r.displaysSnapshot.Outputs {
		if p, ok := target[out.Name.Connector]; ok && p.Mode != nil {
			if err := displays.ValidateMode(out.Modes, *p.Mode); err != nil {
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
